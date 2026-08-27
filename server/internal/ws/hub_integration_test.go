package ws

// 本文件覆盖 Hub 的核心业务逻辑（join/leave/发消息/私聊/好友通知/在线态维护），
// 此前 internal/ws 覆盖率仅 13.6%——除 client_test.go/hub_test.go 里的纯函数
// （allowMessage/validateSendMessage/containsSensitiveWord 等）外，Hub 上真正
// 有状态的方法（register/handleJoinRoom/handleSendMessage/...）完全没有测试
// 直接覆盖，只能靠人工/playwright-cli 真机验证间接兜底。
//
// 关键设计决策：不需要真实 WebSocket 连接。Hub 的这些方法只操作内存 map 和
// Client.send channel，不直接触碰 Client.conn（那是 readPump/writePump 才用到
// 的），因此可以构造一个 conn=nil 的裸 *Client，直接调用 Hub 方法、从
// c.send channel 里读断言，比起真开 WebSocket 连接快得多也简单得多。
// 真实 Redis Pub/Sub 走完整路径（含跨"实例"广播），因为 Hub 的设计就是
// "自己发布也自己订阅"，用真实 Redis 才能验证这条完整链路是否真的通。

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

// ---------- 测试用真实 Redis + fake 依赖 ----------

func testWSRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("skip ws integration test: cannot connect to test redis (%v)", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type fakeMessageWriter struct {
	created []model.Message
	seen    map[string]bool // msgID -> 是否已存在，用于模拟幂等去重
}

func newFakeMessageWriter() *fakeMessageWriter {
	return &fakeMessageWriter{seen: map[string]bool{}}
}

func (f *fakeMessageWriter) Create(_ context.Context, msg *model.Message) (bool, error) {
	if f.seen[msg.MsgID] {
		return false, nil
	}
	f.seen[msg.MsgID] = true
	f.created = append(f.created, *msg)
	return true, nil
}

type fakePresenceTracker struct {
	online map[string]bool
}

func newFakePresenceTracker() *fakePresenceTracker {
	return &fakePresenceTracker{online: map[string]bool{}}
}

func (f *fakePresenceTracker) MarkOnline(_ context.Context, userID string) error {
	f.online[userID] = true
	return nil
}

func (f *fakePresenceTracker) MarkOffline(_ context.Context, userID string) error {
	delete(f.online, userID)
	return nil
}

type fakeDMSender struct {
	err            error
	insertedResult bool
	conversationID string
	sentMessages   []struct{ sender, target, content string }
}

func newFakeDMSender() *fakeDMSender {
	return &fakeDMSender{insertedResult: true, conversationID: "conv-1"}
}

func (f *fakeDMSender) SendDirectMessage(_ context.Context, senderID, targetID, _, content, _ string) (string, bool, error) {
	if f.err != nil {
		return "", false, f.err
	}
	f.sentMessages = append(f.sentMessages, struct{ sender, target, content string }{senderID, targetID, content})
	return f.conversationID, f.insertedResult, nil
}

// newTestHub 构造一个连真实 Redis 的 Hub，注入 fake 业务依赖。返回值同时暴露
// fake 依赖指针，方便测试用例断言"是否真的落库/是否真的标记在线"等副作用。
func newTestHub(t *testing.T, rateLimitPerMinute int, sensitiveWords []string) (*Hub, *fakeMessageWriter, *fakePresenceTracker, *fakeDMSender) {
	t.Helper()
	redisClient := testWSRedis(t)
	msgWriter := newFakeMessageWriter()
	presence := newFakePresenceTracker()
	dmSender := newFakeDMSender()
	hub := NewHub(redisClient, msgWriter, 1000, presence, dmSender, rateLimitPerMinute, sensitiveWords, "test-instance")
	// subscribeLoop 是异步启动的 goroutine，给它一点时间完成 Redis PSUBSCRIBE，
	// 避免测试里"发布在订阅生效之前"导致的偶发丢消息（真实生产环境里 Hub 生命周期
	// 远早于第一条消息，这个等待纯粹是测试环境下的时序保护）。
	time.Sleep(150 * time.Millisecond)
	return hub, msgWriter, presence, dmSender
}

// newTestClient 构造一个不带真实 WebSocket 连接的裸 Client，仅用于测试 Hub
// 方法（这些方法只读写 c.send channel 和 c.rooms map，不触碰 c.conn）。
func newTestClient(userID string) *Client {
	return &Client{
		userID: userID,
		send:   make(chan []byte, 32),
		rooms:  make(map[string]bool),
	}
}

// newTestBotClient 与 newTestClient 同构，但 isBot=true（能力补齐项：LLM 驱动
// 机器人最小验证），用于验证 handleSendMessage 按服务端权威判定的账号身份
// （而非客户端自称）决定 sender_type。
func newTestBotClient(userID string) *Client {
	c := newTestClient(userID)
	c.isBot = true
	return c
}

// recvServerMessage 从 client 的 send channel 里取一条消息并解析，超时视为失败
// （避免测试因为消息未按预期到达而永久阻塞）。
func recvServerMessage(t *testing.T, c *Client) ServerMessage {
	t.Helper()
	select {
	case data := <-c.send:
		var msg ServerMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			t.Fatalf("failed to unmarshal server message: %v, raw=%s", err, data)
		}
		return msg
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for server message")
		return ServerMessage{}
	}
}

// drainUntilType 从 channel 里连续读取，直到读到期望类型的消息（跳过中间可能
// 出现的其它事件，如 room_user_count_update 广播），或超时失败。
//
// 之所以需要这个"跳过噪音事件"的辅助函数：handleJoinRoom/handleLeaveRoom 除了
// 直接同步写入 c.send 的 joined/left 确认外，还会额外触发一次经由真实 Redis
// Pub/Sub 异步往返的 room_user_count_update 广播——两条消息谁先到达 c.send
// 完全取决于 Redis 网络往返的真实时序（异步、非确定性），直接假设"下一条消息
// 就是我要的那条"在真实 Redis 环境下是不可靠的，必须按类型过滤。
func drainUntilType(t *testing.T, c *Client, eventType string) ServerMessage {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case data := <-c.send:
			var msg ServerMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			if msg.Type == eventType {
				return msg
			}
		case <-deadline:
			t.Fatalf("timed out waiting for event type %q", eventType)
			return ServerMessage{}
		}
	}
}

// drainUntilLatestOfType 与 drainUntilType 的区别：当同类型的消息在真实 Redis
// 异步往返下被广播了多次（例如两次 join 各触发一次 room_user_count_update，
// 分别是 count=1、count=2），channel 里可能同时积压着"陈旧"和"最新"两条同类型
// 消息——drainUntilType 只会返回排在前面的那条（可能是陈旧值），而这里要的是
// "等一段时间让所有异步广播落定后，取最后收到的那条"。做法：先等一小段时间让
// 未到达的广播有机会落地，再耗尽 channel 中所有该类型的消息，返回最后一条。
func drainUntilLatestOfType(t *testing.T, c *Client, eventType string) ServerMessage {
	t.Helper()
	time.Sleep(300 * time.Millisecond) // 给 Redis Pub/Sub 往返留出落定时间

	var latest ServerMessage
	found := false
	deadline := time.After(3 * time.Second)
	for {
		select {
		case data := <-c.send:
			var msg ServerMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			if msg.Type == eventType {
				latest = msg
				found = true
			}
		default:
			if found {
				return latest
			}
			select {
			case <-deadline:
				t.Fatalf("timed out waiting for event type %q", eventType)
				return ServerMessage{}
			case <-time.After(50 * time.Millisecond):
				// 短暂等待，给可能仍在路上的异步消息一点机会，避免"channel 恰好
				// 瞬间为空"就误判为已经收完。
			}
		}
	}
}

// drainUntilErrorCode 与 drainUntilType 同款设计，但用于精确匹配 error 事件的
// Code 字段（error 事件的 Type 统一是 EventError，必须靠 Code 区分具体错误）。
func drainUntilErrorCode(t *testing.T, c *Client, code string) ServerMessage {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case data := <-c.send:
			var msg ServerMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				t.Fatalf("failed to unmarshal: %v", err)
			}
			if msg.Type == EventError && msg.Code == code {
				return msg
			}
		case <-deadline:
			t.Fatalf("timed out waiting for error code %q", code)
			return ServerMessage{}
		}
	}
}

// ---------- register / unregister ----------

// TestHub_Register_SendsConnectedEventAndMarksOnline 覆盖 T30：连接建立后应
// 收到 connected 事件，且全局在线态被标记。
func TestHub_Register_SendsConnectedEventAndMarksOnline(t *testing.T) {
	hub, _, presence, _ := newTestHub(t, 0, nil)
	c := newTestClient("alice")

	hub.register(c)

	msg := recvServerMessage(t, c)
	if msg.Type != EventConnected || msg.UserID != "alice" {
		t.Fatalf("expected connected event for alice, got %+v", msg)
	}
	// 能力补齐项：connected 事件应携带 instance_id，供客户端/运维观测"这条
	// 连接实际落在哪个物理后端进程上"（多实例部署下验证负载均衡/跨实例广播
	// 是否真的生效的关键信息）。
	if msg.InstanceID != "test-instance" {
		t.Fatalf("expected connected event to carry instance_id=test-instance, got %q", msg.InstanceID)
	}
	if !presence.online["alice"] {
		t.Fatal("expected alice to be marked online after register")
	}
}

// TestHub_Unregister_MarksOfflineAndLeavesRooms 覆盖断线自动离开房间的可靠性要求：
// 断开连接后应清理其加入过的全部房间，并标记离线。
func TestHub_Unregister_MarksOfflineAndLeavesRooms(t *testing.T) {
	hub, _, presence, _ := newTestHub(t, 0, nil)
	c := newTestClient("alice")
	hub.register(c)
	recvServerMessage(t, c) // connected

	hub.handleJoinRoom(context.Background(), c, "room-1")
	drainUntilType(t, c, EventJoined)

	hub.unregister(c)

	if presence.online["alice"] {
		t.Fatal("expected alice to be marked offline after unregister")
	}
	hub.mu.RLock()
	_, stillInRoom := hub.rooms["room-1"][c]
	hub.mu.RUnlock()
	if stillInRoom {
		t.Fatal("expected client to be removed from room-1's local client set after unregister")
	}
}

// ---------- join / leave room ----------

// TestHub_HandleJoinRoom_Idempotent 覆盖 T32 边界：重复 join 同一房间不重复计数。
func TestHub_HandleJoinRoom_Idempotent(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 0, nil)
	c := newTestClient("alice")
	ctx := context.Background()

	hub.handleJoinRoom(ctx, c, "room-1")
	drainUntilType(t, c, EventJoined)
	hub.handleJoinRoom(ctx, c, "room-1")
	drainUntilType(t, c, EventJoined)

	hub.mu.RLock()
	count := len(hub.rooms["room-1"])
	hub.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected exactly 1 client registered in room-1 after duplicate joins, got %d", count)
	}
}

// TestHub_HandleJoinRoom_MissingRoomID 覆盖缺少 room_id 时返回 missing_room_id 错误。
func TestHub_HandleJoinRoom_MissingRoomID(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 0, nil)
	c := newTestClient("alice")

	hub.handleJoinRoom(context.Background(), c, "")

	msg := recvServerMessage(t, c)
	if msg.Type != EventError || msg.Code != "missing_room_id" {
		t.Fatalf("expected missing_room_id error, got %+v", msg)
	}
}

// TestHub_HandleLeaveRoom_BroadcastsUpdatedCount 覆盖离开房间后人数广播能通过
// 真实 Redis Pub/Sub 回传给本地客户端（验证★3 多实例扇出设计的完整闭环，而非
// 仅验证"发布调用没有返回 error"）。
//
// 用 uuid 生成唯一房间 ID（而不是固定的 "room-1"）：本测试断言的是"在线人数
// 精确等于 1"这类具体数值，而 online_users 在线人数集合存在于真实共享 Redis
// 里，若和其它测试用例复用同一个固定房间 ID，会因为 Redis Set 里残留其它
// 测试用例写入过的 userID 而导致人数断言不稳定（真实踩坑：最初用固定
// "room-1" 时，因为其它用例也用同一个房间名，人数断言时而为2时而为1）。
func TestHub_HandleLeaveRoom_BroadcastsUpdatedCount(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 0, nil)
	ctx := context.Background()
	roomID := uuid.NewString()
	alice := newTestClient("alice-" + uuid.NewString())
	bob := newTestClient("bob-" + uuid.NewString())
	t.Cleanup(func() { _ = hub.redis.Del(context.Background(), onlineUsersKey(roomID)).Err() })

	hub.handleJoinRoom(ctx, alice, roomID)
	drainUntilType(t, alice, EventJoined)
	hub.handleJoinRoom(ctx, bob, roomID)
	drainUntilType(t, bob, EventJoined)
	// alice/bob 各自的 join 都会触发一次 room_user_count_update 广播（分别是
	// count=1、count=2），两条消息可能都还积压在 channel 里，用
	// drainUntilLatestOfType 拿到"落定后的最新一条"而不是排在前面的陈旧值，
	// 借此把两个客户端的接收状态都清空到一个干净的基线，避免干扰后续断言。
	drainUntilLatestOfType(t, alice, EventRoomUserCountUpdate)
	drainUntilLatestOfType(t, bob, EventRoomUserCountUpdate)

	hub.handleLeaveRoom(ctx, alice, roomID)
	leftMsg := drainUntilType(t, alice, EventLeft)
	if leftMsg.RoomID != roomID {
		t.Fatalf("expected left event for %s, got %+v", roomID, leftMsg)
	}

	// bob 应通过 Redis Pub/Sub 收到最新在线人数（alice 离开后应为 1）。
	countMsg := drainUntilLatestOfType(t, bob, EventRoomUserCountUpdate)
	if countMsg.OnlineCount != 1 {
		t.Fatalf("expected online_count=1 after alice left, got %d", countMsg.OnlineCount)
	}
}

// ---------- handleSendMessage（群聊） ----------

// TestHub_HandleSendMessage_BroadcastsToRoomMembers 覆盖 T33：发送消息后房间内
// 其它成员应通过真实 Redis Pub/Sub 收到广播（含发送者自己）。
func TestHub_HandleSendMessage_BroadcastsToRoomMembers(t *testing.T) {
	hub, msgWriter, _, _ := newTestHub(t, 0, nil)
	ctx := context.Background()
	alice := newTestClient("alice")
	bob := newTestClient("bob")

	hub.handleJoinRoom(ctx, alice, "room-1")
	drainUntilType(t, alice, EventJoined)
	hub.handleJoinRoom(ctx, bob, "room-1")
	drainUntilType(t, bob, EventJoined)
	drainUntilType(t, alice, EventRoomUserCountUpdate)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "msg-1", Content: "hello room"})

	aliceMsg := drainUntilType(t, alice, EventMessageReceived)
	bobMsg := drainUntilType(t, bob, EventMessageReceived)
	if aliceMsg.Content != "hello room" || bobMsg.Content != "hello room" {
		t.Fatalf("expected both members to receive the message, got alice=%+v bob=%+v", aliceMsg, bobMsg)
	}
	if len(msgWriter.created) != 1 {
		t.Fatalf("expected message to be persisted exactly once, got %d", len(msgWriter.created))
	}
}

// TestHub_HandleSendMessage_XSSEscaped 覆盖 T50：广播出去的文本内容必须 HTML 转义。
func TestHub_HandleSendMessage_XSSEscaped(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 0, nil)
	ctx := context.Background()
	alice := newTestClient("alice")
	hub.handleJoinRoom(ctx, alice, "room-1")
	drainUntilType(t, alice, EventJoined)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "msg-xss", Content: "<script>alert(1)</script>"})

	msg := drainUntilType(t, alice, EventMessageReceived)
	if msg.Content != "&lt;script&gt;alert(1)&lt;/script&gt;" {
		t.Fatalf("expected HTML-escaped content, got %q", msg.Content)
	}
}

// TestHub_HandleSendMessage_DuplicateMsgIDSkipped 覆盖 T34：相同 msg_id 重复发送
// 应被幂等去重，不重复广播、不重复落库。
func TestHub_HandleSendMessage_DuplicateMsgIDSkipped(t *testing.T) {
	hub, msgWriter, _, _ := newTestHub(t, 0, nil)
	ctx := context.Background()
	alice := newTestClient("alice")
	hub.handleJoinRoom(ctx, alice, "room-1")
	drainUntilType(t, alice, EventJoined)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "dup-1", Content: "first"})
	drainUntilType(t, alice, EventMessageReceived)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "dup-1", Content: "duplicate resend"})

	select {
	case data := <-alice.send:
		t.Fatalf("expected no second broadcast for duplicate msg_id, but got: %s", data)
	case <-time.After(500 * time.Millisecond):
		// 符合预期：没有第二次广播。
	}
	if len(msgWriter.created) != 1 {
		t.Fatalf("expected exactly 1 persisted message despite duplicate send, got %d", len(msgWriter.created))
	}
}

// TestHub_HandleSendMessage_SensitiveWordBlocked 覆盖 T81：命中敏感词应被拦截，
// 不落库不广播。
func TestHub_HandleSendMessage_SensitiveWordBlocked(t *testing.T) {
	hub, msgWriter, _, _ := newTestHub(t, 0, []string{"违禁词"})
	ctx := context.Background()
	alice := newTestClient("alice")
	hub.handleJoinRoom(ctx, alice, "room-1")
	drainUntilType(t, alice, EventJoined)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "bad-1", Content: "这是违禁词内容"})

	// 用 drainUntilErrorCode 而非直接取下一条消息：join 触发的 room_user_count_update
	// 广播经由真实 Redis 异步往返，可能与本次同步写入的 content_blocked 错误交错到达，
	// 必须按 Code 精确匹配，不能假设"下一条就是我要的那条"。
	drainUntilErrorCode(t, alice, "content_blocked")
	if len(msgWriter.created) != 0 {
		t.Fatalf("expected no message to be persisted when blocked, got %d", len(msgWriter.created))
	}
}

// TestHub_HandleSendMessage_RateLimited 覆盖 T40：超过限流阈值应被拒绝。
func TestHub_HandleSendMessage_RateLimited(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 1, nil) // 每分钟仅允许1条
	ctx := context.Background()
	alice := newTestClient("alice")
	hub.handleJoinRoom(ctx, alice, "room-1")
	drainUntilType(t, alice, EventJoined)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "m1", Content: "first"})
	drainUntilType(t, alice, EventMessageReceived)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "m2", Content: "second"})
	drainUntilErrorCode(t, alice, "rate_limited")
}

// TestHub_HandleSendMessage_InvalidContentRejected 覆盖内容校验失败（空内容）
// 时直接返回错误，不进入限流/敏感词/落库流程。
func TestHub_HandleSendMessage_InvalidContentRejected(t *testing.T) {
	hub, msgWriter, _, _ := newTestHub(t, 0, nil)
	alice := newTestClient("alice")

	hub.handleSendMessage(context.Background(), alice, ClientMessage{RoomID: "room-1", MsgID: "m1", Content: ""})

	msg := recvServerMessage(t, alice)
	if msg.Type != EventError || msg.Code != "invalid_message" {
		t.Fatalf("expected invalid_message error, got %+v", msg)
	}
	if len(msgWriter.created) != 0 {
		t.Fatal("expected no persistence for invalid message")
	}
}

// ---------- handleSendDirectMessage（私聊） ----------

// TestHub_HandleSendDirectMessage_BothSidesReceive 覆盖 T70：发送私聊消息后，
// 发送者与目标用户均应通过 Redis 用户频道收到（发送者收到的是"已送达"确认）。
func TestHub_HandleSendDirectMessage_BothSidesReceive(t *testing.T) {
	hub, _, _, dmSender := newTestHub(t, 0, nil)
	ctx := context.Background()
	alice := newTestClient("alice")
	bob := newTestClient("bob")
	hub.register(alice)
	recvServerMessage(t, alice)
	hub.register(bob)
	recvServerMessage(t, bob)

	hub.handleSendDirectMessage(ctx, alice, ClientMessage{TargetUserID: "bob", MsgID: "dm-1", Content: "hi bob"})

	aliceMsg := drainUntilType(t, alice, EventDirectMessageReceived)
	bobMsg := drainUntilType(t, bob, EventDirectMessageReceived)
	if aliceMsg.Content != "hi bob" || bobMsg.Content != "hi bob" {
		t.Fatalf("expected both sender and target to receive the DM, got alice=%+v bob=%+v", aliceMsg, bobMsg)
	}
	if len(dmSender.sentMessages) != 1 || dmSender.sentMessages[0].target != "bob" {
		t.Fatalf("expected SendDirectMessage to be called with target=bob, got %+v", dmSender.sentMessages)
	}
}

// TestHub_HandleSendDirectMessage_FriendRequired 覆盖 T72：非好友发起私聊应被拒绝。
func TestHub_HandleSendDirectMessage_FriendRequired(t *testing.T) {
	hub, _, _, dmSender := newTestHub(t, 0, nil)
	dmSender.err = service.ErrFriendRequiredForDirectMessage
	alice := newTestClient("alice")

	hub.handleSendDirectMessage(context.Background(), alice, ClientMessage{TargetUserID: "stranger", MsgID: "dm-1", Content: "hi"})

	msg := recvServerMessage(t, alice)
	if msg.Type != EventError || msg.Code != "friend_required" {
		t.Fatalf("expected friend_required error, got %+v", msg)
	}
}

// TestHub_HandleSendDirectMessage_CannotMessageSelf 覆盖不能给自己发私聊。
func TestHub_HandleSendDirectMessage_CannotMessageSelf(t *testing.T) {
	hub, _, _, dmSender := newTestHub(t, 0, nil)
	dmSender.err = service.ErrCannotMessageSelf
	alice := newTestClient("alice")

	hub.handleSendDirectMessage(context.Background(), alice, ClientMessage{TargetUserID: "alice", MsgID: "dm-1", Content: "hi"})

	msg := recvServerMessage(t, alice)
	if msg.Type != EventError || msg.Code != "cannot_message_self" {
		t.Fatalf("expected cannot_message_self error, got %+v", msg)
	}
}

// TestHub_HandleSendDirectMessage_DuplicateSkipped 覆盖私聊消息的幂等去重
// （与群聊 T34 同款设计）：重复 msg_id 不重复推送。
func TestHub_HandleSendDirectMessage_DuplicateSkipped(t *testing.T) {
	hub, _, _, dmSender := newTestHub(t, 0, nil)
	dmSender.insertedResult = false // 模拟"该 msgID 已存在"
	alice := newTestClient("alice")

	hub.handleSendDirectMessage(context.Background(), alice, ClientMessage{TargetUserID: "bob", MsgID: "dup-dm", Content: "hi"})

	select {
	case data := <-alice.send:
		t.Fatalf("expected no broadcast for duplicate direct message, got: %s", data)
	case <-time.After(500 * time.Millisecond):
		// 符合预期。
	}
}

// TestHub_HandleSendMessage_BotSenderTypeBroadcast 覆盖能力补齐项（LLM 驱动
// 机器人最小验证）：当发消息的 Client.isBot 为 true 时，落库与广播的
// sender_type 均应为 "bot"，且这个判定完全来自服务端持有的 Client 状态，
// 不依赖/不信任 ClientMessage 里的任何字段（协议里也确实没有让客户端声明
// 身份的字段），对应 ★13 强制披露要求的服务端实现基础。
func TestHub_HandleSendMessage_BotSenderTypeBroadcast(t *testing.T) {
	hub, msgWriter, _, _ := newTestHub(t, 0, nil)
	ctx := context.Background()
	bot := newTestBotClient("bot-" + uuid.NewString())

	hub.handleJoinRoom(ctx, bot, "room-1")
	drainUntilType(t, bot, EventJoined)

	hub.handleSendMessage(ctx, bot, ClientMessage{RoomID: "room-1", MsgID: "bot-msg-1", Content: "大家好，我是机器人"})

	msg := drainUntilType(t, bot, EventMessageReceived)
	if msg.SenderType != "bot" {
		t.Fatalf("expected broadcast sender_type=bot, got %q", msg.SenderType)
	}
	if len(msgWriter.created) != 1 || msgWriter.created[0].SenderType != "bot" {
		t.Fatalf("expected persisted message sender_type=bot, got %+v", msgWriter.created)
	}
}

// TestHub_HandleSendMessage_HumanSenderTypeUnaffected 是上一测试的对照组：
// 非机器人账号发送的消息 sender_type 仍应为 "human"，确认新增分支没有
// 意外影响既有的人类发言路径。
func TestHub_HandleSendMessage_HumanSenderTypeUnaffected(t *testing.T) {
	hub, msgWriter, _, _ := newTestHub(t, 0, nil)
	ctx := context.Background()
	alice := newTestClient("alice-" + uuid.NewString())

	hub.handleJoinRoom(ctx, alice, "room-1")
	drainUntilType(t, alice, EventJoined)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "human-msg-1", Content: "hello"})

	msg := drainUntilType(t, alice, EventMessageReceived)
	if msg.SenderType != "human" {
		t.Fatalf("expected broadcast sender_type=human, got %q", msg.SenderType)
	}
	if len(msgWriter.created) != 1 || msgWriter.created[0].SenderType != "human" {
		t.Fatalf("expected persisted message sender_type=human, got %+v", msgWriter.created)
	}
}

// fakeEventRecorder 是 EventRecorder 的内存假实现（能力补齐项：给"未来投喂给
// 模型训练用户替身"补最基础的行为原始数据），供测试断言 Hub 在正确的时机、
// 携带正确的字段调用了 Create，而不依赖真实 Postgres（真实落库正确性已在
// repository 层的 TestInteractionEventRepository_* 单测覆盖）。
type fakeEventRecorder struct {
	mu      sync.Mutex
	created []model.InteractionEvent
}

func newFakeEventRecorder() *fakeEventRecorder {
	return &fakeEventRecorder{}
}

func (f *fakeEventRecorder) Create(_ context.Context, e *model.InteractionEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, *e)
	return nil
}

// TestHub_HandleJoinRoom_RecordsInteractionEvent 覆盖能力补齐项：首次加入房间
// 应记一条 join_room 行为事件；重复 join（幂等场景）不应重复记录，避免训练
// 数据里混入噪音信号。
func TestHub_HandleJoinRoom_RecordsInteractionEvent(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 0, nil)
	recorder := newFakeEventRecorder()
	hub.SetEventRecorder(recorder)
	ctx := context.Background()
	roomID := uuid.NewString()
	alice := newTestClient("alice-" + uuid.NewString())
	t.Cleanup(func() { _ = hub.redis.Del(context.Background(), onlineUsersKey(roomID)).Err() })

	hub.handleJoinRoom(ctx, alice, roomID)
	drainUntilType(t, alice, EventJoined)
	// 重复 join：不应产生第二条事件（幂等口径与 T32 一致）。
	hub.handleJoinRoom(ctx, alice, roomID)
	drainUntilType(t, alice, EventJoined)

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.created) != 1 {
		t.Fatalf("expected exactly 1 join_room event (dedup on repeat join), got %d: %+v", len(recorder.created), recorder.created)
	}
	got := recorder.created[0]
	if got.EventType != model.EventTypeJoinRoom || got.UserID != alice.userID {
		t.Fatalf("unexpected event: %+v", got)
	}
	if got.RoomID == nil || *got.RoomID != roomID {
		t.Fatalf("expected RoomID=%s, got %v", roomID, got.RoomID)
	}
}

// TestHub_HandleSendMessage_RecordsInteractionEvent 覆盖能力补齐项：成功发送
// 的消息应记一条 send_message 事件，Payload 携带 msg_id/content_type（不重复
// 存储消息正文，正文已完整落在 messages 表）。
func TestHub_HandleSendMessage_RecordsInteractionEvent(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 0, nil)
	recorder := newFakeEventRecorder()
	hub.SetEventRecorder(recorder)
	ctx := context.Background()
	alice := newTestClient("alice-" + uuid.NewString())

	hub.handleJoinRoom(ctx, alice, "room-1")
	drainUntilType(t, alice, EventJoined)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "evt-msg-1", Content: "hello"})
	drainUntilType(t, alice, EventMessageReceived)

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	var sendEvents []model.InteractionEvent
	for _, e := range recorder.created {
		if e.EventType == model.EventTypeSendMessage {
			sendEvents = append(sendEvents, e)
		}
	}
	if len(sendEvents) != 1 {
		t.Fatalf("expected exactly 1 send_message event, got %d: %+v", len(sendEvents), sendEvents)
	}
	got := sendEvents[0]
	if got.RoomID == nil || *got.RoomID != "room-1" {
		t.Fatalf("expected RoomID=room-1, got %v", got.RoomID)
	}
	var payload map[string]string
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if payload["msg_id"] != "evt-msg-1" || payload["content_type"] != "text" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

// TestHub_HandleSendMessage_BlockedContent_DoesNotRecordEvent 覆盖边界：命中
// 敏感词被拦截的消息不应产生行为事件（没有发生真实的"发消息"行为）。
func TestHub_HandleSendMessage_BlockedContent_DoesNotRecordEvent(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 0, []string{"违禁词"})
	recorder := newFakeEventRecorder()
	hub.SetEventRecorder(recorder)
	ctx := context.Background()
	alice := newTestClient("alice-" + uuid.NewString())

	hub.handleJoinRoom(ctx, alice, "room-1")
	drainUntilType(t, alice, EventJoined)

	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "blocked-1", Content: "这是违禁词内容"})
	drainUntilErrorCode(t, alice, "content_blocked")

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	for _, e := range recorder.created {
		if e.EventType == model.EventTypeSendMessage {
			t.Fatalf("expected no send_message event for blocked content, got %+v", e)
		}
	}
}

// TestHub_EventRecorder_Nil_DoesNotPanic 覆盖边界：未注入 EventRecorder（nil，
// 默认状态）时，join_room/send_message 均应正常工作，不受影响——与
// PushNotifier 的可选注入原则一致。
func TestHub_EventRecorder_Nil_DoesNotPanic(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 0, nil)
	ctx := context.Background()
	alice := newTestClient("alice-" + uuid.NewString())

	hub.handleJoinRoom(ctx, alice, "room-1")
	drainUntilType(t, alice, EventJoined)
	hub.handleSendMessage(ctx, alice, ClientMessage{RoomID: "room-1", MsgID: "nil-recorder-1", Content: "hello"})
	drainUntilType(t, alice, EventMessageReceived)
}

// ---------- NotifyFriendRequestReceived ----------

// TestHub_NotifyFriendRequestReceived_DeliversToTargetUser 覆盖 T60：好友请求
// 应通过用户级 Redis 频道推送给目标用户（跨实例扇出设计）。
func TestHub_NotifyFriendRequestReceived_DeliversToTargetUser(t *testing.T) {
	hub, _, _, _ := newTestHub(t, 0, nil)
	bob := newTestClient("bob")
	hub.register(bob)
	recvServerMessage(t, bob) // connected

	if err := hub.NotifyFriendRequestReceived(context.Background(), "bob", "req-1", "alice"); err != nil {
		t.Fatalf("NotifyFriendRequestReceived failed: %v", err)
	}

	msg := drainUntilType(t, bob, EventFriendRequestReceived)
	if msg.RequestID != "req-1" || msg.FromUserID != "alice" {
		t.Fatalf("expected request_id=req-1 from_user_id=alice, got %+v", msg)
	}
}
