// Package main 实现"LLM 驱动机器人"的最小验证工具（能力补齐项）。
//
// 用途：给此前只有 schema 预留（users.is_bot/bot_action_log 等，见
// migrations/0001_init_schema.up.sql 与 docs/00-brainstorm-and-plan.md
// 「AI-native 二期扩展设计」）、从未真正跑通过的"训练替身机器人代替用户进行
// 前期社交"设计，补一个最小的、真实可运行的验证闭环——不做自动化决策/多轮
// 长期社交，只验证最基础的一条完整链路：
//
//  1. 机器人账号（is_bot=true）登录并加入一个真实房间；
//  2. 调用通义千问（Qwen/DashScope）LLM API 生成一条消息内容（这是"用 LLM
//     驱动机器人行为"的真实落地点，不是硬编码模板）；
//  3. 通过真实 WS 协议把这条 LLM 生成的内容发到房间——sender_type 由服务端
//     根据账号的 is_bot 权威判定（见 internal/ws 改动），前端据此展示
//     "（机器人）"标识，对应 ★13 强制披露的透明度要求；
//  4. 落一条 bot_action_log 决策记录（此前这张表从建表起从未被写入过）；
//  5. 向若干个真实存在的用户名发起好友请求（复用现有 REST 接口，机器人账号
//     在这一点上与普通用户完全一样，不做任何特殊处理）。
//
// 用法：
//
//	export LLM_API_KEY=sk-xxx        # 或 export DASHSCOPE_API_KEY=sk-xxx
//	export BOT_TARGET_USERNAMES=moonteng,lili   # 可省略，默认就是这两个
//	go run ./cmd/bot
//
// 依赖当前已通过 `docker compose -f deploy/docker-compose.yml up -d` 跑起来
// 的 postgres/redis/server（默认假设它们在 localhost:5432/6379/8080，与
// docker-compose.yml 的端口映射一致；也可通过 POSTGRES_DSN/BOT_SERVER_BASE_URL
// 环境变量覆盖，指向别的部署环境）。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"xingqudazi-im/server/internal/config"
	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/repository"
	"xingqudazi-im/server/internal/ws"
	"xingqudazi-im/server/pkg/llm"
)

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if cfg.LLMAPIKey == "" {
		log.Fatalf("未配置 LLM_API_KEY / DASHSCOPE_API_KEY，机器人行为无法被 LLM 驱动，退出")
	}

	serverBaseURL := strings.TrimSuffix(envOr("BOT_SERVER_BASE_URL", "http://localhost:8080"), "/")
	botUsername := envOr("BOT_USERNAME", "ai_zhidai_bot")
	botPassword := envOr("BOT_PASSWORD", "BotPass1234")
	roomNameHint := envOr("BOT_ROOM_NAME", "")
	targetUsernames := splitAndTrim(envOr("BOT_TARGET_USERNAMES", "moonteng,lili"))

	log.Printf("目标服务: %s | LLM: provider=%s model=%s | 目标好友: %v",
		serverBaseURL, cfg.LLMProvider, cfg.LLMModel, targetUsernames)

	dbPool, err := repository.NewPostgresPool(ctx, cfg.PostgresDSN, cfg.PostgresMaxConns, cfg.PostgresMinConns)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer dbPool.Close()

	userRepo := repository.NewUserRepository(dbPool)
	botLogRepo := repository.NewBotActionLogRepository(dbPool)

	httpClient := &http.Client{Timeout: 15 * time.Second}

	// ---------- 1. 机器人账号：登录优先，不存在则注册（幂等，方便重复运行本工具） ----------
	token, botUserID, err := ensureBotAccount(ctx, httpClient, serverBaseURL, botUsername, botPassword)
	if err != nil {
		log.Fatalf("准备机器人账号失败: %v", err)
	}
	log.Printf("机器人账号已就绪：username=%s user_id=%s", botUsername, botUserID)

	// ---------- 2. 标记机器人身份（直连数据库，故意不通过任何公开 HTTP 接口，
	// 见 UserRepository.SetIsBot 注释） ----------
	if err := userRepo.SetIsBot(ctx, botUserID, true); err != nil {
		log.Fatalf("标记 is_bot=true 失败: %v", err)
	}
	log.Printf("已将账号 %s 标记为机器人身份（is_bot=true）", botUserID)

	// ---------- 3. 选定房间 ----------
	room, err := pickRoom(httpClient, serverBaseURL, roomNameHint)
	if err != nil {
		log.Fatalf("选择房间失败: %v", err)
	}
	log.Printf("已选定房间：%s（room_id=%s，话题：%s）", room.Name, room.ID, room.Topic)

	// ---------- 4. 调用 LLM 生成消息内容（真实驱动机器人行为的核心步骤） ----------
	llmClient := llm.NewQwenClient(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel)
	systemPrompt := "你是兴趣社交App「兴趣搭子」里的一个AI机器人角色，正在以机器人身份" +
		"（前端会显式标注“（机器人）”，符合透明度披露要求）在兴趣主题群聊房间里发言。" +
		"请生成一条简短自然的中文开场白/破冰消息，控制在40字以内，语气友好、切合房间主题，" +
		"不要使用引号包裹，不要输出除消息正文外的任何解释性文字。"
	userPrompt := fmt.Sprintf("房间名称：%s；房间话题：%s。请生成一条适合在这个房间发送的开场白。", room.Name, room.Topic)

	content, usageSummary, err := llmClient.GenerateBotMessage(ctx, systemPrompt, userPrompt)
	if err != nil {
		log.Fatalf("调用 LLM 生成消息失败: %v", err)
	}
	content = strings.TrimSpace(content)
	if content == "" {
		log.Fatalf("LLM 返回了空内容，中止（不发送空消息）")
	}
	log.Printf("LLM(%s) 生成内容: %q", cfg.LLMModel, content)
	log.Printf("LLM 调用统计: %s", usageSummary)

	// ---------- 5. 通过真实 WS 协议加入房间并发送该消息 ----------
	msgID, err := sendRoomMessageViaWS(serverBaseURL, token, room.ID, content)
	if err != nil {
		log.Fatalf("通过 WS 发送消息失败: %v", err)
	}
	log.Printf("消息已发送并收到服务端广播确认，msg_id=%s", msgID)

	// ---------- 6. 落一条 bot_action_log 决策记录（此前从未被写入过的表） ----------
	roomIDCopy := room.ID
	if err := botLogRepo.Create(ctx, &model.BotActionLog{
		ID:             uuid.NewString(),
		BotUserID:      botUserID,
		RoomID:         &roomIDCopy,
		DecisionReason: fmt.Sprintf("LLM(%s)生成开场白并发送：%s | %s", cfg.LLMModel, content, usageSummary),
	}); err != nil {
		log.Printf("[WARN] 写入 bot_action_log 失败（不影响机器人行为本身，仅决策留痕缺失）: %v", err)
	} else {
		log.Printf("已写入 bot_action_log 决策记录")
	}

	// ---------- 7. 向目标用户发起好友请求 ----------
	for _, uname := range targetUsernames {
		if err := attemptSendFriendRequest(httpClient, serverBaseURL, token, uname); err != nil {
			log.Printf("[WARN] 向 %s 发起好友请求未成功: %v", uname, err)
		}
	}

	log.Println("机器人最小验证流程执行完毕。")
}

// envOr 是本命令自用的最小环境变量读取辅助函数（不复用 internal/config 的
// getEnv，因为那是包内私有函数；cmd/bot 只需要几个额外的、与核心 server
// 配置无关的自定义变量，没必要为此改动 config 包的导出面）。
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func splitAndTrim(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// ---------- HTTP：账号准备 ----------

type authResponse struct {
	Token  string `json:"token"`
	UserID string `json:"user_id"`
}

// ensureBotAccount 登录优先，登录失败（多半是账号尚不存在）则尝试注册后
// 再登录一次；这样重复运行本工具会复用同一个机器人账号，而不是每次都
// 创建新账号（幂等，与 scripts/demo_seed.py 的设计原则一致）。
func ensureBotAccount(ctx context.Context, client *http.Client, baseURL, username, password string) (token, userID string, err error) {
	token, userID, loginErr := loginBot(ctx, client, baseURL, username, password)
	if loginErr == nil {
		return token, userID, nil
	}
	if regErr := registerBot(ctx, client, baseURL, username, password); regErr != nil {
		return "", "", fmt.Errorf("login failed (%v) and register also failed: %w", loginErr, regErr)
	}
	return loginBot(ctx, client, baseURL, username, password)
}

func loginBot(ctx context.Context, client *http.Client, baseURL, username, password string) (string, string, error) {
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/auth/login", bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("login returned %d: %s", resp.StatusCode, string(respBody))
	}

	var out authResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", "", fmt.Errorf("unmarshal login response: %w", err)
	}
	return out.Token, out.UserID, nil
}

func registerBot(ctx context.Context, client *http.Client, baseURL, username, password string) error {
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/auth/register", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusCreated {
		return nil
	}
	// username_taken 视为"账号已存在"，不算失败（多次运行本工具应能复用同一账号）。
	if resp.StatusCode == http.StatusBadRequest && strings.Contains(string(respBody), "username_taken") {
		return nil
	}
	return fmt.Errorf("register returned %d: %s", resp.StatusCode, string(respBody))
}

// ---------- HTTP：房间/用户查找/好友请求 ----------

type roomInfo struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Topic string `json:"topic"`
}

// pickRoom 拉取真实房间列表并选定一个：nameHint 非空时精确匹配房间名，
// 否则取列表第一个（当前 4 个预置房间任选其一均可用于本次最小验证）。
func pickRoom(client *http.Client, baseURL, nameHint string) (roomInfo, error) {
	resp, err := client.Get(baseURL + "/api/rooms")
	if err != nil {
		return roomInfo{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return roomInfo{}, fmt.Errorf("list rooms returned %d: %s", resp.StatusCode, string(body))
	}

	var rooms []roomInfo
	if err := json.Unmarshal(body, &rooms); err != nil {
		return roomInfo{}, fmt.Errorf("unmarshal rooms: %w", err)
	}
	if len(rooms) == 0 {
		return roomInfo{}, fmt.Errorf("no rooms available")
	}
	if nameHint == "" {
		return rooms[0], nil
	}
	for _, r := range rooms {
		if r.Name == nameHint {
			return r, nil
		}
	}
	return roomInfo{}, fmt.Errorf("room named %q not found", nameHint)
}

func lookupUserID(client *http.Client, baseURL, token, username string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/users/lookup?username="+url.QueryEscape(username), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("用户 %q 不存在", username)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("lookup %q returned %d: %s", username, resp.StatusCode, string(body))
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("unmarshal lookup response: %w", err)
	}
	return out.ID, nil
}

// attemptSendFriendRequest 先按用户名查到目标 user_id，再发起好友请求；
// 409（已是好友/请求已存在）视为"目标状态已达成"，不算失败——多次运行本
// 工具是预期用法，不应该每次都因为"已经加过好友"而报错退出。
func attemptSendFriendRequest(client *http.Client, baseURL, token, targetUsername string) error {
	targetID, err := lookupUserID(client, baseURL, token, targetUsername)
	if err != nil {
		return err
	}

	body, err := json.Marshal(map[string]string{"target_user_id": targetID})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/friends/requests", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusCreated:
		log.Printf("已向 %s（user_id=%s）发起好友请求", targetUsername, targetID)
		return nil
	case http.StatusConflict:
		log.Printf("向 %s 发起好友请求：%s（视为已达成目标状态，不算失败）", targetUsername, string(respBody))
		return nil
	default:
		return fmt.Errorf("发起好友请求返回 %d: %s", resp.StatusCode, string(respBody))
	}
}

// ---------- WS：加入房间 + 发送消息 ----------

// sendRoomMessageViaWS 建立真实 WS 连接、加入房间、发送消息，并等待服务端的
// message_received 广播回执（自己也是房间成员，会收到自己发的消息）确认
// 消息确实被服务端处理，而不是"发出去就假设成功"。
func sendRoomMessageViaWS(baseURL, token, roomID, content string) (string, error) {
	wsURL := strings.Replace(strings.Replace(baseURL, "https://", "wss://", 1), "http://", "ws://", 1) +
		"/ws?token=" + url.QueryEscape(token)

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return "", fmt.Errorf("dial ws failed: %w", err)
	}
	defer conn.Close()

	connected, err := waitForEvent(conn, ws.EventConnected, 5*time.Second)
	if err != nil {
		return "", fmt.Errorf("wait for connected event failed: %w", err)
	}
	if connected.InstanceID != "" {
		log.Printf("WS 已连接，落在实例：%s", connected.InstanceID)
	}

	if err := conn.WriteJSON(ws.ClientMessage{Type: ws.EventJoinRoom, RoomID: roomID}); err != nil {
		return "", fmt.Errorf("send join_room failed: %w", err)
	}
	if _, err := waitForEvent(conn, ws.EventJoined, 5*time.Second); err != nil {
		return "", fmt.Errorf("wait for joined event failed: %w", err)
	}

	msgID := uuid.NewString()
	if err := conn.WriteJSON(ws.ClientMessage{Type: ws.EventSendMessage, RoomID: roomID, MsgID: msgID, Content: content}); err != nil {
		return "", fmt.Errorf("send send_message failed: %w", err)
	}

	echo, err := waitForEvent(conn, ws.EventMessageReceived, 8*time.Second)
	if err != nil {
		return "", fmt.Errorf("wait for message_received echo failed: %w", err)
	}
	if echo.MsgID != msgID {
		return "", fmt.Errorf("echo msg_id mismatch: expected %s got %s", msgID, echo.MsgID)
	}
	if echo.SenderType != "bot" {
		// 不视为致命错误（消息已经真实发出并落库），但这是一个值得大声提醒的
		// 异常：说明服务端没有正确识别这个连接对应的账号是机器人身份。
		log.Printf("[WARN] 期望广播 sender_type=bot，实际为 %q，请检查 is_bot 是否已正确落库", echo.SenderType)
	}
	return msgID, nil
}

// waitForEvent 按类型过滤读取 WS 消息，跳过中间可能出现的噪音事件（如
// room_user_count_update），遇到 error 事件直接返回错误，超时则报错——
// 与 internal/ws 测试文件里 drainUntilType 的设计思路一致。
func waitForEvent(conn *websocket.Conn, eventType string, timeout time.Duration) (ws.ServerMessage, error) {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return ws.ServerMessage{}, fmt.Errorf("timed out waiting for event %q", eventType)
		}
		_ = conn.SetReadDeadline(deadline)
		var msg ws.ServerMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return ws.ServerMessage{}, err
		}
		if msg.Type == eventType {
			return msg, nil
		}
		if msg.Type == ws.EventError {
			return ws.ServerMessage{}, fmt.Errorf("server returned error event: code=%s", msg.Code)
		}
	}
}
