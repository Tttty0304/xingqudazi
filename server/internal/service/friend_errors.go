package service

import "errors"

// 好友关系链相关的语义化错误（Task14，对应 Testcase T60-T63）。
// api 层据此翻译成对应 HTTP 状态码；命名与 room_errors.go/errors.go 的既有风格保持一致。
var (
	ErrRepositoryFriendshipNotFound = errors.New("repository_friendship_not_found")

	ErrCannotFriendSelf        = errors.New("cannot_friend_self")
	ErrFriendRequestNotFound   = errors.New("friend_request_not_found")
	ErrFriendRequestExists     = errors.New("friend_request_already_exists")
	ErrAlreadyFriends          = errors.New("already_friends")
	ErrFriendRequestResolved   = errors.New("friend_request_already_resolved") // T62：重复操作已处理过的请求
	ErrForbiddenFriendResponse = errors.New("forbidden")                       // T51 同款：非请求接收方操作
	ErrInvalidFriendAction     = errors.New("invalid_action")
	ErrNotFriends              = errors.New("not_friends")
)
