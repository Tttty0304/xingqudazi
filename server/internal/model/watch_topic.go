package model

import "time"

// WatchTopic 对应 migrations 中的 user_watch_topics 表（Task19/P1：关注事项，
// 是 Task20 AI 推荐规则化匹配演示的直接输入源）。RoomID 为空表示全局关注，
// ExpiresAt 为空表示不过期。
type WatchTopic struct {
	ID        string
	UserID    string
	RoomID    string // 可空（全局关注）
	Keywords  string
	Priority  int
	ExpiresAt *time.Time
	CreatedAt time.Time
}
