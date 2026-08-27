package ws

// 客户端 -> 服务端 事件类型（对应 Plan Part3 WS 事件映射 + Testcase T30-T38）。
const (
	EventJoinRoom          = "join_room"
	EventLeaveRoom         = "leave_room"
	EventSendMessage       = "send_message"
	EventSendDirectMessage = "send_direct_message" // Task15/T70：发起私聊消息
)

// 服务端 -> 客户端 事件类型。
const (
	EventConnected             = "connected"
	EventJoined                = "joined"
	EventLeft                  = "left"
	EventRoomUserCountUpdate   = "room_user_count_update"
	EventMessageReceived       = "message_received"
	EventError                 = "error"
	EventFriendRequestReceived = "friend_request_received" // Task14/T60：对方收到好友请求
	EventDirectMessageReceived = "direct_message_received" // Task15/T70：双方收到私聊消息
)

// ClientMessage 是客户端发来的通用消息外层结构；具体字段按 Type 解析到对应子结构。
type ClientMessage struct {
	Type         string `json:"type"`
	RoomID       string `json:"room_id"`
	MsgID        string `json:"msg_id"`
	Content      string `json:"content"`
	TargetUserID string `json:"target_user_id"` // Task15：send_direct_message 的对方用户 ID
	// ContentType 供 Task16 图片消息使用："text"（默认，未传时按此处理）| "image"。
	// image 时 Content 承载的是 Task16 上传接口返回的媒体 URL，不是用户输入的自由文本，
	// 因此不受 T50 XSS 转义处理（转义面向"用户输入的可展示文本"，URL 转义反而会破坏引用）。
	ContentType string `json:"content_type"`
}

// ServerMessage 是服务端推给客户端的通用消息外层结构。
type ServerMessage struct {
	Type string `json:"type"`
	// InstanceID 仅在 `connected` 事件中携带（能力补齐项：多实例部署下的
	// 可观测性/验证手段，见 Hub.instanceID 注释），标识本次 WS 连接实际
	// 落在哪一个物理后端进程上——这是验证"负载均衡是否真的把连接分流到了
	// 多个独立实例"、"跨实例广播是否真的跨越了两个进程"的关键信息，此前
	// 完全没有办法从外部观测到这一点。
	InstanceID  string `json:"instance_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	RoomID      string `json:"room_id,omitempty"`
	MsgID       string `json:"msg_id,omitempty"`
	Content     string `json:"content,omitempty"`
	ContentType string `json:"content_type,omitempty"` // Task16：text | image
	SenderID    string `json:"sender_id,omitempty"`
	SenderType  string `json:"sender_type,omitempty"`
	OnlineCount int64  `json:"online_count,omitempty"`
	Code        string `json:"code,omitempty"`
	// RequestID/FromUserID 供 EventFriendRequestReceived 使用（Task14/T60）。
	RequestID  string `json:"request_id,omitempty"`
	FromUserID string `json:"from_user_id,omitempty"`
	// ConversationID/TargetUserID 供 EventDirectMessageReceived 使用（Task15/T70），
	// 双方收到同一份 payload，客户端据 SenderID/TargetUserID 判断对方是谁。
	ConversationID string `json:"conversation_id,omitempty"`
	TargetUserID   string `json:"target_user_id,omitempty"`
}

// broadcastEnvelope 是通过 Redis Pub/Sub 在多实例间传递的**房间**广播消息包裹结构。
// 覆盖 room_user_count_update 与 message_received 两类广播；本进程发布后，
// 自身也通过订阅收到并转发给本地连接（不区分"本机广播"和"跨机广播"，
// 统一走同一条路径，这正是 ★3 强制要求的多实例扇出设计——不依赖进程内
// 内存直接查找连接的捷径实现）。
type broadcastEnvelope struct {
	RoomID  string        `json:"room_id"`
	Payload ServerMessage `json:"payload"`
}

// userNotifyEnvelope 与 broadcastEnvelope 同构，但用于**点对点**推送给指定用户
// （Task14 起新增，如 friend_request_received），同样统一走 Redis Pub/Sub，
// 不依赖进程内直接查找本地连接的捷径实现，跨实例推送与房间广播遵循同一设计原则。
type userNotifyEnvelope struct {
	UserID  string        `json:"user_id"`
	Payload ServerMessage `json:"payload"`
}
