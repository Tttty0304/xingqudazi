package model

import "time"

// User 对应 migrations/0001_init_schema.up.sql 中的 users 表核心字段。
//
// IsBot 是 AI-native 参与者模型预留字段（Plan Part3「AI-native 二期扩展设计」，
// Task19 P0 schema）：此前该字段在 DB 里一直存在，但业务代码从未读写过（见
// docs/00-brainstorm-and-plan.md「机器人训练/推理管线明确排除本次范围」）。
// 能力补齐项（cmd/bot 最小验证）首次真正让这个字段参与业务逻辑：由
// UserRepository.SetIsBot 显式设置（仅供内部工具调用，不通过任何公开 HTTP
// 接口暴露——避免"任意用户给自己打机器人标签"这类滥用），WS 握手时读取用于
// 决定消息的 sender_type，实现"消息来源可分辨"的透明度要求（★13）。
//
// ProxyForUserID（机器人代理哪个真人用户）当前仍未接入任何业务逻辑，保持
// 预留状态：本轮验证的机器人是独立身份，不代理任何具体用户，避免超出
// "最小验证"范围。
type User struct {
	ID           string
	Username     string
	PasswordHash string // 访客用户为空字符串
	IsGuest      bool
	IsBot        bool
	AvatarURL    string
	Bio          string
	CreatedAt    time.Time
}
