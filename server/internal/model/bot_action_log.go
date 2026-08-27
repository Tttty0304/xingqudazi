package model

import "time"

// BotActionLog 对应 migrations/0001_init_schema.up.sql 中的 bot_action_log 表
// （Task19，P0 字段/此前 P2 排除真实写入逻辑："本次不产生机器人消息，故暂不
// 写入，仅预留表结构"）。能力补齐项（cmd/bot 最小验证）首次真正写入这张表：
// 机器人每次由 LLM 驱动发出一条消息后，落一条决策留痕，兼顾未来训练数据/
// 可解释性需求（Plan Part3 原始设计意图）。
type BotActionLog struct {
	ID                  string
	BotUserID           string
	TriggerWatchTopicID *string // 可空：本轮验证不依赖关注事项触发，恒为 nil
	RoomID              *string
	DecisionReason      string
	CreatedAt           time.Time
}
