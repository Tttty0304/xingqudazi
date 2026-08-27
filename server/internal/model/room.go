package model

import "time"

// Room 对应 migrations 中的 rooms 表。
type Room struct {
	ID        string
	Name      string
	Topic     string
	IsPreset  bool
	CreatorID string
	CreatedAt time.Time
}

// RoomWithOnlineCount 是 GET /api/rooms 的响应形态：房间基础信息 + 实时在线人数
// （在线人数来自 Redis，见 repository.RoomOnlineCounter，Task4 起真实维护该计数，
// 当前 Task3 阶段先返回 0，占位但不伪造非零假数据）。
type RoomWithOnlineCount struct {
	Room
	OnlineCount int64
}

// Message 对应 migrations 中的 messages 表（群聊消息）。
type Message struct {
	ID          string
	MsgID       string
	RoomID      string
	SenderID    string
	SenderType  string // human | bot
	Content     string
	ContentType string // text | image | voice | file
	IsBlocked   bool
	CreatedAt   time.Time
}
