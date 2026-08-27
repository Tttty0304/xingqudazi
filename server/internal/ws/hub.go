package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

// broadcastChannelPrefix / broadcastChannelSuffix 构成 Redis Pub/Sub 频道命名规则：
// room:{roomID}:broadcast —— 与 Plan Part3「接口/数据/事件映射」中定义的键结构保持一致。
const (
	broadcastChannelPattern = "room:*:broadcast"
	// userNotifyChannelPattern 是点对点用户通知频道的订阅模式（Task14 起新增，
	// 如好友请求推送），与房间广播共享同一条 ★3 强制的多实例扇出设计。
	userNotifyChannelPattern = "user:*:notify"
)

func broadcastChannel(roomID string) string {
	return fmt.Sprintf("room:%s:broadcast", roomID)
}

func onlineUsersKey(roomID string) string {
	return fmt.Sprintf("room:%s:online_users", roomID)
}

func userNotifyChannel(userID string) string {
	return fmt.Sprintf("user:%s:notify", userID)
}

// MessageWriter 是 Hub 持久化群聊消息所需的最小接口，真实实现见
// repository.MessageRepository.Create（新增方法）。之所以在 Task4 就接入写路径，
// 是因为 T33/T34（消息已落库、msgId 幂等去重）必须有真实持久化才能验证，
// Task3 已完成的是读路径，Task4 在此基础上补齐写路径，Task5 不再重复这部分工作
// （详见 docs/04-task4-work-log.md 的范围调整说明）。
type MessageWriter interface {
	Create(ctx context.Context, msg *model.Message) (inserted bool, err error)
}

// PresenceTracker 由 Hub 在连接建立/断开时调用，维护全局用户在线态
// （Task14 起新增，供好友在线状态查询 T63 使用）。真实实现见
// repository.RedisUserPresence。
type PresenceTracker interface {
	MarkOnline(ctx context.Context, userID string) error
	MarkOffline(ctx context.Context, userID string) error
}

// DirectMessageSender 是 Hub 处理私聊消息（Task15/T70）所需的最小接口，真实实现见
// service.ConversationService.SendDirectMessage。友情校验（T72）/会话创建/落库+
// 幂等去重均封装在其内部，Hub 只负责 WS 层的协议解析与广播分发，业务逻辑不下沉到 ws 包
// （与好友请求推送 Task14 的分层原则一致：ws 只做"连接与分发"）。
type DirectMessageSender interface {
	SendDirectMessage(ctx context.Context, senderID, targetID, msgID, content, contentType string) (conversationID string, inserted bool, err error)
}

// PushNotifier 用于在私聊消息目标用户离线时触发 Web Push（Task17/T103），真实实现见
// service.PushService.NotifyOfflineUser；与 FriendService 的同名接口结构一致，
// 各自在所属包内独立定义（依赖倒置：谁使用就由谁定义接口）。
type PushNotifier interface {
	NotifyOfflineUser(ctx context.Context, userID, title, body string) error
}

// EventRecorder 供 Hub 在发生关键用户行为时记录一条 interaction_events 行为
// 事件（能力补齐项：给"未来投喂给模型训练用户替身"这个设想补最基础的行为
// 原始数据，见 model.InteractionEvent 注释），真实实现见
// repository.InteractionEventRepository。可为 nil：未注入时静默跳过，不影响
// 任何现有功能——记录行为事件是纯旁路能力，不应该因为这条能力故障/未启用就
// 影响用户发消息/加好友的核心体验，与 PushNotifier 的可选注入原则一致。
type EventRecorder interface {
	Create(ctx context.Context, e *model.InteractionEvent) error
}

// Hub 是单个服务实例内的 WebSocket 连接管理器。★3 已确认：多实例扇出为强制
// 设计，因此 Hub 广播消息**统一**先发布到 Redis 频道，再由（本实例或其他实例）
// 的订阅协程转发给本地连接——不存在"进程内直接查找本地连接广播"的捷径路径，
// 单实例部署时效果等价于自己发布自己订阅，但代码路径与多实例完全一致。
type Hub struct {
	redis         *redis.Client
	msgStore      MessageWriter
	presence      PresenceTracker
	dmSender      DirectMessageSender
	pushNotifier  PushNotifier  // 可为 nil：未配置 Web Push 时静默跳过，不影响私聊消息主流程
	eventRecorder EventRecorder // 可为 nil：未注入时静默跳过，不影响任何现有功能
	log           *slog.Logger

	maxMessageLength int
	// rateLimitPerMinute 对应 Task6/T40：单连接每分钟允许发言（群聊+私聊合计）的上限，
	// <=0 表示不限流。真实值来自 cfg.RateLimitPerMinute。
	rateLimitPerMinute int
	// sensitiveWords 对应 Task18/T81：命中即拦截，不落库不广播（仅对 content_type=="text"
	// 生效，图片消息的 Content 是媒体 URL，不做敏感词匹配）。
	sensitiveWords []string
	// instanceID 标识本进程实例（能力补齐项，见 ServerMessage.InstanceID 注释），
	// 随 `connected` 事件下发给客户端，用于验证多实例部署下连接真实落在哪个
	// 物理进程上。可为空字符串（未设置时不影响任何现有功能，仅这一项可观测性
	// 能力不可用）。
	instanceID string

	mu    sync.RWMutex
	rooms map[string]map[*Client]bool // roomID -> 本地连接集合
	users map[string]map[*Client]bool // userID -> 本地连接集合（Task14 起新增，点对点推送用）
}

func NewHub(redisClient *redis.Client, msgStore MessageWriter, maxMessageLength int, presence PresenceTracker, dmSender DirectMessageSender, rateLimitPerMinute int, sensitiveWords []string, instanceID string) *Hub {
	h := &Hub{
		redis:              redisClient,
		msgStore:           msgStore,
		presence:           presence,
		dmSender:           dmSender,
		log:                slog.Default(),
		maxMessageLength:   maxMessageLength,
		rateLimitPerMinute: rateLimitPerMinute,
		sensitiveWords:     sensitiveWords,
		instanceID:         instanceID,
		rooms:              make(map[string]map[*Client]bool),
		users:              make(map[string]map[*Client]bool),
	}
	go h.subscribeLoop()
	return h
}

// SetPushNotifier 注入 Task17 Web Push 能力（可选，向后兼容，不改变 NewHub 签名，
// 避免影响既有调用点/测试）。
func (h *Hub) SetPushNotifier(p PushNotifier) {
	h.pushNotifier = p
}

// SetEventRecorder 注入行为事件记录能力（能力补齐项，可选，同款不改变 NewHub
// 签名的注入方式）。
func (h *Hub) SetEventRecorder(r EventRecorder) {
	h.eventRecorder = r
}

// recordInteractionEvent 是 EventRecorder.Create 的统一调用入口：自动生成 ID，
// 失败只记录日志不影响调用方主流程（与本文件其它旁路能力的失败处理原则
// 一致）。e.ID 由本方法负责填充，调用方不需要关心。
func (h *Hub) recordInteractionEvent(ctx context.Context, e *model.InteractionEvent) {
	if h.eventRecorder == nil {
		return
	}
	e.ID = uuid.NewString()
	if err := h.eventRecorder.Create(ctx, e); err != nil {
		h.log.Error("ws_record_interaction_event_failed", "user_id", e.UserID, "event_type", e.EventType, "error", err)
	}
}

// Shutdown 对应 Task6/T41：在收到 SIGTERM/SIGINT 后，向所有本地存活连接主动发送 WS
// Close 帧，使客户端能感知到"服务端正常关闭"而不是被强制断连（网络层面区分不出二者，
// 但协议层面 Close 帧是明确的关闭通知）。gorilla/websocket 文档明确 WriteControl 可以
// 与其他读写方法并发调用，因此无需与 writePump 做额外同步。
func (h *Hub) Shutdown() {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.users))
	for _, set := range h.users {
		for c := range set {
			clients = append(clients, c)
		}
	}
	h.mu.RUnlock()

	closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "server_shutting_down")
	for _, c := range clients {
		if err := c.conn.WriteControl(websocket.CloseMessage, closeMsg, time.Now().Add(writeWait)); err != nil {
			h.log.Info("ws_shutdown_close_write_failed", "user_id", c.userID, "error", err)
		}
	}
	h.log.Info("ws_shutdown_close_frames_sent", "client_count", len(clients))
}

// subscribeLoop 订阅全部房间广播频道 + 全部用户通知频道（模式订阅，避免每次
// join_room/连接建立都动态订阅/取消订阅带来的竞态复杂度；demo/中等规模下模式订阅
// 的开销可接受，性能优化点记录在 docs 的遗留项中）。收到消息后按频道前缀分发。
func (h *Hub) subscribeLoop() {
	ctx := context.Background()
	sub := h.redis.PSubscribe(ctx, broadcastChannelPattern, userNotifyChannelPattern)
	defer sub.Close()

	ch := sub.Channel()
	h.log.Info("ws_subscribe_loop_started", "patterns", []string{broadcastChannelPattern, userNotifyChannelPattern})

	for msg := range ch {
		switch {
		case strings.HasPrefix(msg.Channel, "room:"):
			var envelope broadcastEnvelope
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				h.log.Error("ws_subscribe_decode_failed", "error", err, "channel", msg.Channel)
				continue
			}
			h.dispatchToLocalClients(envelope.RoomID, envelope.Payload)
		case strings.HasPrefix(msg.Channel, "user:"):
			var envelope userNotifyEnvelope
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				h.log.Error("ws_subscribe_decode_failed", "error", err, "channel", msg.Channel)
				continue
			}
			h.dispatchToLocalUser(envelope.UserID, envelope.Payload)
		default:
			h.log.Error("ws_subscribe_unknown_channel", "channel", msg.Channel)
		}
	}
}

func (h *Hub) dispatchToLocalClients(roomID string, payload ServerMessage) {
	h.mu.RLock()
	clients := h.rooms[roomID]
	// 复制一份待发送列表，避免长时间持锁阻塞其他 goroutine 的 join/leave 操作。
	targets := make([]*Client, 0, len(clients))
	for c := range clients {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	data, err := json.Marshal(payload)
	if err != nil {
		h.log.Error("ws_marshal_payload_failed", "error", err)
		return
	}

	for _, c := range targets {
		select {
		case c.send <- data:
		default:
			// send channel 已满，说明该客户端消费跟不上（慢客户端），丢弃这条广播而不阻塞整个
			// Hub；这是可靠性权衡：优先保证 Hub 不被单个慢客户端拖死，代价是慢客户端可能丢部分消息。
			h.log.Info("ws_client_send_buffer_full_dropped", "user_id", c.userID, "room_id", roomID)
		}
	}
}

// dispatchToLocalUser 把点对点通知投递给本地持有该 userID 连接的全部客户端
// （Task14 起新增；一个用户理论上可能有多端同时在线，均会收到）。
func (h *Hub) dispatchToLocalUser(userID string, payload ServerMessage) {
	h.mu.RLock()
	clients := h.users[userID]
	targets := make([]*Client, 0, len(clients))
	for c := range clients {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	data, err := json.Marshal(payload)
	if err != nil {
		h.log.Error("ws_marshal_payload_failed", "error", err)
		return
	}

	for _, c := range targets {
		select {
		case c.send <- data:
		default:
			h.log.Info("ws_client_send_buffer_full_dropped", "user_id", c.userID)
		}
	}
}

// publish 把消息发布到 Redis 房间频道；所有实例（含本实例）通过 subscribeLoop 统一接收。
func (h *Hub) publish(ctx context.Context, roomID string, payload ServerMessage) error {
	envelope := broadcastEnvelope{RoomID: roomID, Payload: payload}
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal broadcast envelope: %w", err)
	}
	if err := h.redis.Publish(ctx, broadcastChannel(roomID), data).Err(); err != nil {
		return fmt.Errorf("publish to redis: %w", err)
	}
	return nil
}

// publishToUser 把点对点通知发布到 Redis 用户频道，跨实例统一走 subscribeLoop 接收，
// 与 publish（房间广播）遵循同一设计原则（★3）。
func (h *Hub) publishToUser(ctx context.Context, userID string, payload ServerMessage) error {
	envelope := userNotifyEnvelope{UserID: userID, Payload: payload}
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal user notify envelope: %w", err)
	}
	if err := h.redis.Publish(ctx, userNotifyChannel(userID), data).Err(); err != nil {
		return fmt.Errorf("publish to redis: %w", err)
	}
	return nil
}

// NotifyFriendRequestReceived 实现 service.FriendNotifier（结构化匹配，无需相互
// import），供 FriendService 在创建好友请求后推送给目标用户（T60）。
func (h *Hub) NotifyFriendRequestReceived(ctx context.Context, targetUserID, requestID, fromUserID string) error {
	return h.publishToUser(ctx, targetUserID, ServerMessage{
		Type:       EventFriendRequestReceived,
		RequestID:  requestID,
		FromUserID: fromUserID,
	})
}

// register 在连接建立并鉴权通过后调用，登记本地用户连接、标记全局在线态，
// 并发送 connected 事件（T30）。
func (h *Hub) register(c *Client) {
	h.mu.Lock()
	if h.users[c.userID] == nil {
		h.users[c.userID] = make(map[*Client]bool)
	}
	h.users[c.userID][c] = true
	h.mu.Unlock()

	h.log.Info("ws_client_connected", "user_id", c.userID, "instance_id", h.instanceID)
	c.send <- mustMarshal(ServerMessage{Type: EventConnected, UserID: c.userID, InstanceID: h.instanceID})

	if h.presence != nil {
		if err := h.presence.MarkOnline(context.Background(), c.userID); err != nil {
			h.log.Error("ws_presence_mark_online_failed", "user_id", c.userID, "error", err)
		}
	}
}

// unregister 在连接断开时调用：清理该连接加入过的全部房间（本地 map + Redis online set）、
// 清理用户连接登记、更新全局在线态，并广播人数变化（对应"断线自动离开房间"的可靠性要求）。
func (h *Hub) unregister(c *Client) {
	h.mu.Lock()
	roomsToLeave := make([]string, 0, len(c.rooms))
	for roomID := range c.rooms {
		roomsToLeave = append(roomsToLeave, roomID)
		if set, ok := h.rooms[roomID]; ok {
			delete(set, c)
			if len(set) == 0 {
				delete(h.rooms, roomID)
			}
		}
	}
	if set, ok := h.users[c.userID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.users, c.userID)
		}
	}
	h.mu.Unlock()

	close(c.send)
	h.log.Info("ws_client_disconnected", "user_id", c.userID, "rooms_left", roomsToLeave)

	ctx := context.Background()
	for _, roomID := range roomsToLeave {
		h.leaveRoomOnlineSet(ctx, roomID, c.userID)
		h.broadcastRoomUserCount(ctx, roomID)
	}

	if h.presence != nil {
		if err := h.presence.MarkOffline(ctx, c.userID); err != nil {
			h.log.Error("ws_presence_mark_offline_failed", "user_id", c.userID, "error", err)
		}
	}
}

// handleClientMessage 解析并分发客户端消息（T32-T35 的入口）。
func (h *Hub) handleClientMessage(c *Client, raw []byte) {
	msg, err := decodeClientMessage(raw)
	if err != nil {
		h.log.Info("ws_message_decode_failed", "user_id", c.userID, "error", err)
		c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "invalid_message_format"})
		return
	}

	ctx := context.Background()
	switch msg.Type {
	case EventJoinRoom:
		h.handleJoinRoom(ctx, c, msg.RoomID)
	case EventLeaveRoom:
		h.handleLeaveRoom(ctx, c, msg.RoomID)
	case EventSendMessage:
		h.handleSendMessage(ctx, c, msg)
	case EventSendDirectMessage:
		h.handleSendDirectMessage(ctx, c, msg)
	default:
		c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "unknown_event_type"})
	}
}

// decodeClientMessage 对 WS JSON 帧实施与 HTTP 相同的严格格式契约：拒绝未知字段、
// 多个 JSON 值和不属于当前事件的非法 UUID。错误帧只影响当前命令，不会关闭连接，
// 因此后续合法命令仍可继续处理。
func decodeClientMessage(raw []byte) (ClientMessage, error) {
	var msg ClientMessage
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&msg); err != nil {
		return ClientMessage{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return ClientMessage{}, errors.New("multiple json values")
		}
		return ClientMessage{}, err
	}
	switch msg.Type {
	case EventJoinRoom, EventLeaveRoom, EventSendMessage:
		if _, err := uuid.Parse(msg.RoomID); err != nil {
			return ClientMessage{}, errors.New("invalid room_id")
		}
	case EventSendDirectMessage:
		if _, err := uuid.Parse(msg.TargetUserID); err != nil {
			return ClientMessage{}, errors.New("invalid target_user_id")
		}
	}
	return msg, nil
}

// handleJoinRoom 对应 T32：加入房间，本地 map 登记 + Redis online set 新增 + 广播人数更新。
func (h *Hub) handleJoinRoom(ctx context.Context, c *Client, roomID string) {
	if roomID == "" {
		c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "missing_room_id"})
		return
	}

	h.mu.Lock()
	if h.rooms[roomID] == nil {
		h.rooms[roomID] = make(map[*Client]bool)
	}
	// 幂等：重复 join 同一房间不重复计数（T32 边界要求）。
	alreadyJoined := h.rooms[roomID][c]
	h.rooms[roomID][c] = true
	c.rooms[roomID] = true
	h.mu.Unlock()

	if err := h.redis.SAdd(ctx, onlineUsersKey(roomID), c.userID).Err(); err != nil {
		h.log.Error("ws_join_room_redis_sadd_failed", "user_id", c.userID, "room_id", roomID, "error", err)
	}

	c.send <- mustMarshal(ServerMessage{Type: EventJoined, RoomID: roomID})

	if !alreadyJoined {
		h.broadcastRoomUserCount(ctx, roomID)
		// 能力补齐项：只在"首次加入"记一条行为事件，重复 join（前端重连/切
		// 页面等场景）不产生噪音信号，与上面 T32 幂等口径保持一致。
		h.recordInteractionEvent(ctx, &model.InteractionEvent{
			UserID:    c.userID,
			RoomID:    &roomID,
			EventType: model.EventTypeJoinRoom,
		})
	}
}

// handleLeaveRoom 对应 T37：离开房间。
func (h *Hub) handleLeaveRoom(ctx context.Context, c *Client, roomID string) {
	if _, err := uuid.Parse(roomID); err != nil {
		c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "invalid_room_id"})
		return
	}
	h.mu.Lock()
	if set, ok := h.rooms[roomID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.rooms, roomID)
		}
	}
	delete(c.rooms, roomID)
	h.mu.Unlock()

	h.leaveRoomOnlineSet(ctx, roomID, c.userID)
	c.send <- mustMarshal(ServerMessage{Type: EventLeft, RoomID: roomID})
	h.broadcastRoomUserCount(ctx, roomID)
}

func (h *Hub) leaveRoomOnlineSet(ctx context.Context, roomID, userID string) {
	if err := h.redis.SRem(ctx, onlineUsersKey(roomID), userID).Err(); err != nil {
		h.log.Error("ws_leave_room_redis_srem_failed", "user_id", userID, "room_id", roomID, "error", err)
	}
}

func (h *Hub) broadcastRoomUserCount(ctx context.Context, roomID string) {
	count, err := h.redis.SCard(ctx, onlineUsersKey(roomID)).Result()
	if err != nil {
		h.log.Error("ws_scard_failed", "room_id", roomID, "error", err)
		return
	}
	if err := h.publish(ctx, roomID, ServerMessage{
		Type:        EventRoomUserCountUpdate,
		RoomID:      roomID,
		OnlineCount: count,
	}); err != nil {
		h.log.Error("ws_broadcast_room_user_count_failed", "room_id", roomID, "error", err)
	}
}

// validSendContentTypes 是 Task16/P0 阶段允许的消息内容类型：text（默认）+ image；
// voice/file 为 P1，本次不接受通过 WS 发送（即使数据库 CHECK 约束允许，应用层先拦截）。
var validSendContentTypes = map[string]bool{
	"":      true, // 未传时按 text 处理
	"text":  true,
	"image": true,
}

// normalizeContentType 把空字符串规范化为 "text"，避免下游各处重复判断空值。
func normalizeContentType(contentType string) string {
	if contentType == "" {
		return "text"
	}
	return contentType
}

// containsSensitiveWord 对应 Task18/T81：大小写不敏感的子串匹配，命中词库中任一词即拦截。
// demo/评估场景用固定小词表 + 简单子串匹配，足以演示"内容安全兜底"这一工程能力项，
// 生产环境应替换为更完善的分词/词库管理方案（已在文档中如实标注为简化点）。
func containsSensitiveWord(content string, sensitiveWords []string) (hit string, blocked bool) {
	lower := strings.ToLower(content)
	for _, w := range sensitiveWords {
		if w == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(w)) {
			return w, true
		}
	}
	return "", false
}

// validateSendMessage 是 handleSendMessage 的纯校验逻辑（对应 T33-T35，Task16 起
// 扩展 content_type 校验），抽成独立函数便于无需真实 WebSocket 连接即可单测。
func validateSendMessage(msg ClientMessage, maxMessageLength int) (errCode string, ok bool) {
	if msg.RoomID == "" || msg.Content == "" {
		return "invalid_message", false
	}
	if !validSendContentTypes[msg.ContentType] {
		return "unsupported_content_type", false
	}
	if len(msg.Content) > maxMessageLength {
		return "content_too_long", false
	}
	return "", true
}

// handleSendMessage 对应 T33-T35：内容校验 -> 限流(T40) -> 敏感词过滤(T81，仅text)
// -> 落库（含 msgId 幂等去重）-> 发布广播（text 内容转义 T50，image 为 URL 不转义）。
func (h *Hub) handleSendMessage(ctx context.Context, c *Client, msg ClientMessage) {
	if code, ok := validateSendMessage(msg, h.maxMessageLength); !ok {
		c.send <- mustMarshal(ServerMessage{Type: EventError, Code: code})
		return
	}
	if !c.allowMessage(h.rateLimitPerMinute) {
		// T40：超过限流阈值，拒绝且不落库不广播；窗口过期后自动恢复放行。
		c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "rate_limited"})
		return
	}

	contentType := normalizeContentType(msg.ContentType)
	if contentType == "text" {
		if _, blocked := containsSensitiveWord(msg.Content, h.sensitiveWords); blocked {
			// T81：命中敏感词，不落库不广播。
			c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "content_blocked"})
			return
		}
	}

	msgID := msg.MsgID
	if msgID == "" {
		msgID = uuid.NewString()
	}

	// senderType 由服务端根据握手时权威判定的账号身份决定（能力补齐项：
	// LLM 驱动机器人最小验证），不信任/不存在客户端在消息体里声明身份的路径，
	// 对应 ★13 强制披露的透明度要求——机器人消息必须能被前端正确识别展示。
	senderType := "human"
	if c.isBot {
		senderType = "bot"
	}

	// 落库保留原始内容（未转义），转义只发生在对外输出边界（本函数下方广播 / HTTP 历史
	// 消息接口），避免"存储即转义"污染原始数据、且方便未来更换转义/审核策略而不用回填数据。
	inserted, err := h.msgStore.Create(ctx, &model.Message{
		ID:          uuid.NewString(),
		MsgID:       msgID,
		RoomID:      msg.RoomID,
		SenderID:    c.userID,
		SenderType:  senderType,
		Content:     msg.Content,
		ContentType: contentType,
	})
	if err != nil {
		h.log.Error("ws_persist_message_failed", "user_id", c.userID, "room_id", msg.RoomID, "error", err)
		c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "internal_error"})
		return
	}
	if !inserted {
		// T34：msgId 已存在（重复发送），识别为幂等重复，不重复广播、不重复落库。
		h.log.Info("ws_duplicate_message_skipped", "user_id", c.userID, "msg_id", msgID)
		return
	}

	outContent := msg.Content
	if contentType == "text" {
		outContent = html.EscapeString(outContent) // T50：XSS 转义（仅文本，图片 URL 不转义）
	}
	if err := h.publish(ctx, msg.RoomID, ServerMessage{
		Type:        EventMessageReceived,
		RoomID:      msg.RoomID,
		MsgID:       msgID,
		Content:     outContent,
		ContentType: contentType,
		SenderID:    c.userID,
		SenderType:  senderType,
	}); err != nil {
		h.log.Error("ws_broadcast_message_failed", "room_id", msg.RoomID, "error", err)
	}

	// 能力补齐项：记录行为事件，只在真正落库+广播成功（走到这里）之后才记，
	// 不对内容校验失败/限流/敏感词拦截/重复消息这些"没有产生真实行为"的
	// 分支记事件——避免训练数据里混入噪音。故意不在 Payload 里重复存储消息
	// 正文，原始内容已经完整落在 messages 表，这里通过 msg_id 关联即可
	// （见 model.InteractionEvent 注释）。
	payload, err := json.Marshal(map[string]string{"msg_id": msgID, "content_type": contentType})
	if err != nil {
		h.log.Error("ws_marshal_interaction_event_payload_failed", "error", err)
	} else {
		h.recordInteractionEvent(ctx, &model.InteractionEvent{
			UserID:    c.userID,
			RoomID:    &msg.RoomID,
			EventType: model.EventTypeSendMessage,
			Payload:   payload,
		})
	}
}

// validateSendDirectMessage 是 handleSendDirectMessage 的纯校验逻辑（Task15/T70，
// Task16 起扩展 content_type 校验），与 validateSendMessage（群聊）同款设计。
func validateSendDirectMessage(msg ClientMessage, maxMessageLength int) (errCode string, ok bool) {
	if msg.TargetUserID == "" || msg.Content == "" {
		return "invalid_message", false
	}
	if !validSendContentTypes[msg.ContentType] {
		return "unsupported_content_type", false
	}
	if len(msg.Content) > maxMessageLength {
		return "content_too_long", false
	}
	return "", true
}

// handleSendDirectMessage 对应 T70（私聊发送）+ T72（已确认口径：仅好友可私聊）+
// T40（限流，与群聊共用同一连接级计数器）+ T50（文本内容广播转义）+ T81（敏感词过滤）。
// 好友校验/会话惰性创建/落库+幂等去重全部封装在 h.dmSender（service.ConversationService）内，
// Hub 只负责协议解析与跨实例广播分发。
func (h *Hub) handleSendDirectMessage(ctx context.Context, c *Client, msg ClientMessage) {
	if code, ok := validateSendDirectMessage(msg, h.maxMessageLength); !ok {
		c.send <- mustMarshal(ServerMessage{Type: EventError, Code: code})
		return
	}
	if !c.allowMessage(h.rateLimitPerMinute) {
		c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "rate_limited"})
		return
	}

	contentType := normalizeContentType(msg.ContentType)
	if contentType == "text" {
		if _, blocked := containsSensitiveWord(msg.Content, h.sensitiveWords); blocked {
			c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "content_blocked"})
			return
		}
	}

	msgID := msg.MsgID
	if msgID == "" {
		msgID = uuid.NewString()
	}

	conversationID, inserted, err := h.dmSender.SendDirectMessage(ctx, c.userID, msg.TargetUserID, msgID, msg.Content, contentType)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrFriendRequiredForDirectMessage):
			// T72：非好友发起私聊被拒绝。
			c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "friend_required"})
		case errors.Is(err, service.ErrCannotMessageSelf):
			c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "cannot_message_self"})
		default:
			h.log.Error("ws_persist_direct_message_failed", "user_id", c.userID, "target_user_id", msg.TargetUserID, "error", err)
			c.send <- mustMarshal(ServerMessage{Type: EventError, Code: "internal_error"})
		}
		return
	}
	if !inserted {
		// 与群聊 T34 同款：msgId 已存在（重复发送），幂等跳过，不重复推送。
		h.log.Info("ws_duplicate_direct_message_skipped", "user_id", c.userID, "msg_id", msgID)
		return
	}

	outContent := msg.Content
	if contentType == "text" {
		outContent = html.EscapeString(outContent) // T50：XSS 转义（仅文本，图片 URL 不转义）
	}
	payload := ServerMessage{
		Type:           EventDirectMessageReceived,
		ConversationID: conversationID,
		MsgID:          msgID,
		Content:        outContent,
		ContentType:    contentType,
		SenderID:       c.userID,
		SenderType:     "human",
		TargetUserID:   msg.TargetUserID,
	}
	// 双方均通过用户级 Redis 频道推送（与 T60 好友请求推送同一套跨实例扇出设计），
	// 发送者也走同一条路径收到自己发的消息（作为"已送达"确认），不使用进程内直接回写的捷径。
	if err := h.publishToUser(ctx, c.userID, payload); err != nil {
		h.log.Error("ws_publish_direct_message_to_sender_failed", "user_id", c.userID, "error", err)
	}
	if err := h.publishToUser(ctx, msg.TargetUserID, payload); err != nil {
		h.log.Error("ws_publish_direct_message_to_target_failed", "target_user_id", msg.TargetUserID, "error", err)
	}

	// Task17/T103：对方离线时额外发一条 Web Push（在线用户已经通过上面的 WS 通知实时
	// 收到，NotifyOfflineUser 内部会自行判断在线态，在线时静默跳过）。通知文案不含发送者
	// 用户名——Hub 只持有 userID，查用户名需要额外依赖 UserStore，为保持 ws 包依赖最小化，
	// 通知正文用"你有一条新的私聊消息"这类不含身份的通用文案（图片消息则提示"发来一张图片"）。
	if h.pushNotifier != nil {
		body := "你有一条新的私聊消息"
		if contentType == "image" {
			body = "对方给你发来一张图片"
		}
		if err := h.pushNotifier.NotifyOfflineUser(ctx, msg.TargetUserID, "新的私聊消息", body); err != nil {
			h.log.Error("ws_push_notify_offline_user_failed", "target_user_id", msg.TargetUserID, "error", err)
		}
	}
}

func mustMarshal(v ServerMessage) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		// 内部结构体序列化失败属于编程错误，不应该发生；记录后返回空 JSON 对象兜底，
		// 避免 panic 影响整个连接。
		slog.Default().Error("ws_marshal_server_message_failed", "error", err)
		return []byte(`{}`)
	}
	return data
}
