package model

import "time"

// PushSubscription 对应 migrations 中的 push_subscriptions 表（Task17 Web Push）。
// P256dh/Auth 为浏览器 PushManager.subscribe() 返回的订阅密钥材料（base64url 编码）。
type PushSubscription struct {
	ID        string
	UserID    string
	Endpoint  string
	P256dh    string
	Auth      string
	CreatedAt time.Time
}
