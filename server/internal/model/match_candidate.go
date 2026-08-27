package model

import "time"

// MatchCandidate 对应 migrations 中的 match_candidates 表（Task20：AI 推荐规则化
// 匹配演示的候选记录）。UserAID/UserBID 按字符串字典序规范化存储（UserAID < UserBID），
// 避免同一对用户出现两条方向不同的重复候选。
type MatchCandidate struct {
	ID          string
	UserAID     string
	UserBID     string
	SharedTopic string
	RoomID      string // 可空
	MatchReason string
	MatchScore  float64
	Status      string // pending_review | confirmed | dismissed
	CreatedAt   time.Time
}
