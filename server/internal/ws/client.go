package ws

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10 // 必须小于 pongWait，否则会在收到 pong 前先超时
	maxMessageSize = 8192                // 单帧最大字节数，防止恶意超大帧攻击（系统安全性）

	// burstWindow/burstLimit 实现 T40 要求的"短时突发"防护：仅按分钟计数无法拦住
	// "1秒内连发20条"这类瞬时刷屏（分钟级配额在窗口起始瞬间本就允许被瞬间打满），
	// 因此叠加一个更短窗口的硬性突发上限，与按分钟的长期频率上限（rateLimitPerMinute，
	// 来自配置）互相独立、任一超限即拒绝。
	burstWindow = 2 * time.Second
	burstLimit  = 10
)

// Client 代表一条已建立的 WebSocket 连接（对应一个已鉴权用户在本实例上的会话）。
type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	userID string
	// isBot 标识该连接对应的账号是否为机器人身份（能力补齐项：LLM 驱动机器人
	// 最小验证）。在握手阶段（ws/handler.go ServeWS）由服务端查库权威判定后
	// 写入，握手成功后不再变化，读取无需加锁；不存在"客户端在消息里声称自己是
	// 机器人"这条路径——sender_type 完全由服务端根据这个字段决定，避免任意
	// 用户伪造机器人身份（或反过来，绕过机器人应有的透明度标识）。
	isBot bool

	send chan []byte // 待发往客户端的原始 JSON 字节，写协程独占消费，避免并发写同一个 conn

	// rooms 记录该连接当前加入了哪些房间，断线时用于清理（离开全部房间）。
	rooms map[string]bool

	// rateMu 保护下方两组独立计数器，实现 Task6/T40 的发言限流：群聊 send_message
	// 与私聊 send_direct_message 共用同一套计数器（限制的是"该连接的整体发言频率"，
	// 而非按事件类型分别计数）。per-connection 粒度已足够覆盖 demo 场景的刷屏防护，
	// 不引入 Redis 跨实例计数的额外复杂度。
	rateMu sync.Mutex
	// rateWindowStart/rateCount：按分钟的长期频率上限（阈值来自配置 RateLimitPerMinute）。
	rateWindowStart time.Time
	rateCount       int
	// burstWindowStart/burstCount：短时突发上限（固定常量 burstWindow/burstLimit），
	// 专门拦截"1秒内连发N条"这类分钟级配额无法及时反应的瞬时刷屏。
	burstWindowStart time.Time
	burstCount       int
}

func newClient(hub *Hub, conn *websocket.Conn, userID string, isBot bool) *Client {
	return &Client{
		hub:    hub,
		conn:   conn,
		userID: userID,
		isBot:  isBot,
		send:   make(chan []byte, 32),
		rooms:  make(map[string]bool),
	}
}

// allowMessage 对应 T40：limit<=0 表示不限流（本地/测试场景可通过配置关闭）；
// 否则同时施加两层限制，任一超限即拒绝：
//  1. 短时突发上限（burstWindow/burstLimit，固定常量）：拦截"1秒内连发N条"级别的瞬时刷屏；
//  2. 按分钟的长期频率上限（limit，来自配置 RateLimitPerMinute）：拦截持续高频发言。
//
// 两个窗口各自独立滚动重置，过期后自动恢复放行。
func (c *Client) allowMessage(limit int) bool {
	if limit <= 0 {
		return true
	}
	c.rateMu.Lock()
	defer c.rateMu.Unlock()

	now := time.Now()

	if now.Sub(c.burstWindowStart) > burstWindow {
		c.burstWindowStart = now
		c.burstCount = 0
	}
	c.burstCount++
	if c.burstCount > burstLimit {
		return false
	}

	if now.Sub(c.rateWindowStart) > time.Minute {
		c.rateWindowStart = now
		c.rateCount = 0
	}
	c.rateCount++
	return c.rateCount <= limit
}

// readPump 持续读取客户端发来的消息，解析后交给 Hub 处理；
// 连接关闭或读错误时退出循环并触发清理。这是"入口"，必须有日志（改动前后 Task 通用要求）。
func (c *Client) readPump() {
	defer c.hub.unregister(c)
	defer c.conn.Close()

	c.conn.SetReadLimit(maxMessageSize)
	// 心跳超时期限设置是"最佳努力"操作：仅在底层连接已损坏时才会失败，失败后续的
	// ReadMessage 也会立即报错并触发上面 defer 的清理逻辑，这里无需重复处理，
	// 显式丢弃返回值（Task11 lint 收尾时 errcheck 命中，此前一直是隐式忽略）。
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			// 正常断开（客户端主动关闭/网络中断）不算错误级别的异常，DEBUG 即可；
			// 这里统一用 Info 级别，避免正常断线被误判为服务异常。
			c.hub.log.Info("ws_read_pump_exit", "user_id", c.userID, "reason", err.Error())
			return
		}
		c.hub.handleClientMessage(c, data)
	}
}

// writePump 独占消费 send channel 写入 conn，并周期性发送 ping 帧维持心跳（T30 心跳保活）。
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	defer c.conn.Close()

	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				// 连接即将关闭：最佳努力发送 Close 帧，失败也无需处理（函数即将 return，
				// 显式丢弃返回值，Task11 lint 收尾时 errcheck 命中）。
				_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
