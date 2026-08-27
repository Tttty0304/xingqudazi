package service

import "errors"

// 私聊相关的语义化错误（Task15，对应 Testcase T70-T72）。
var (
	ErrRepositoryConversationNotFound = errors.New("repository_conversation_not_found")

	ErrConversationNotFound        = errors.New("conversation_not_found")
	ErrForbiddenConversationAccess = errors.New("forbidden") // 非会话参与者查询历史消息
	// ErrFriendRequiredForDirectMessage 对应 T72（已确认口径）：仅好友可私聊，
	// 非好友发起私聊被拒绝。
	ErrFriendRequiredForDirectMessage = errors.New("friend_required")
	ErrCannotMessageSelf              = errors.New("cannot_message_self")
)
