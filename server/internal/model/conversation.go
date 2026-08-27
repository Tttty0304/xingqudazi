package model

import "time"

// Conversation 对应 migrations 中的 conversations 表（Task15 私聊）。
// UserAID/UserBID 按字典序规范化存储（UserAID <= UserBID），避免同一对用户
// 因发起方向不同而产生两条重复会话记录（表上 `UNIQUE (user_a_id, user_b_id)` 只按
// 精确字段匹配，不理解"方向无关"，规范化职责落在 repository 层）。
type Conversation struct {
	ID        string
	UserAID   string
	UserBID   string
	CreatedAt time.Time
}

// DirectMessage 对应 migrations 中的 direct_messages 表（私聊消息）。
type DirectMessage struct {
	ID             string
	MsgID          string
	ConversationID string
	SenderID       string
	SenderType     string // human | bot
	Content        string
	ContentType    string // text | image | voice | file
	IsBlocked      bool
	CreatedAt      time.Time
}

// ConversationSummary 是 GET /api/conversations 的响应形态（T71）：
// 会话 + 对方用户 ID + 最近一条消息摘要。
type ConversationSummary struct {
	ConversationID string
	PeerID         string
	LastMessage    string
	LastMessageAt  time.Time
	UnreadCount    int64
}
