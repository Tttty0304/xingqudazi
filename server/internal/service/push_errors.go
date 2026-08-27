package service

import "errors"

// Web Push（Task17）相关的语义化错误。
var (
	ErrInvalidPushSubscription = errors.New("invalid_push_subscription")
)
