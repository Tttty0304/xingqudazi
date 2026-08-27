package model

import "time"

// Friendship 对应 migrations 中的 friendships 表（好友关系链，Task14）。
// requester_id 为发起方，target_id 为接收方；status: pending/accepted/rejected。
type Friendship struct {
	ID          string
	RequesterID string
	TargetID    string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Friend 是 GET /api/friends 的响应形态：好友的用户基础信息 + 实时在线状态（T63）。
// Online 来自 Redis 实时在线态查询（见 repository.RedisUserPresence），不是缓存的静态字段。
type Friend struct {
	UserID   string
	Username string
	Online   bool
}

// PendingFriendRequest 是 GET /api/friends/requests 的响应形态（T120，本轮新增）：
// 展示当前用户视角下全部待处理好友请求（收到的 + 自己发出的），Direction 标明方向，
// 前端据此区分"可操作（接受/拒绝）"与"仅可查看等待中"两种交互。
type PendingFriendRequest struct {
	RequestID    string
	PeerID       string
	PeerUsername string
	Direction    string // incoming | outgoing
	CreatedAt    time.Time
}
