package model

import (
	"encoding/json"
	"time"
)

// InteractionEvent 对应 migrations/0001_init_schema.up.sql 中的 interaction_events
// 表（Task19，P0 字段），Plan 原始设计意图是"训练信号的核心来源，历史数据无法
// 回溯补采，必须从上线第一天开始记录"。能力补齐项（2026-08-19）首次真正让这张
// 表承载数据：此前从建表起到现在一行都没写过，任何"未来投喂给模型训练用户替身"
// 的设想都缺少最基础的行为原始数据支撑。
//
// Payload 用 JSONB 存储，字段结构随 EventType 变化（不同事件类型关心的细节不同，
// 比如 send_message 关心 content_type/msg_id，join_room 基本不需要额外字段）；
// 故意不在 Payload 里重复存储消息正文——原始文本已经完整落在 messages/
// direct_messages 表，行为事件表通过 msg_id 关联即可，避免同一份内容维护两份
// 拷贝（数据一致性风险）。
type InteractionEvent struct {
	ID string
	// UserID 是产生这个行为的用户（一定非空）。
	UserID string
	// RoomID/TargetUserID 均可为空（对应表结构里的可空外键），语义随 EventType
	// 变化：join_room/send_message 关心 RoomID；add_friend_request 关心
	// TargetUserID；两者互斥填充，不强制同时非空。
	RoomID       *string
	TargetUserID *string
	// EventType 取值见 EventType* 常量：join_room/send_message/
	// send_direct_message/add_friend_request 等，与 Plan 文档里列出的事件
	// 类型命名保持一致（"join_room/send_message/view_profile/long_dwell/
	// add_friend 等"）。
	EventType string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// 与 Plan Part3「AI-native 二期扩展设计」列出的事件类型命名保持一致，本轮
// 首批接入的最小子集（覆盖 WS 实时行为 + REST 触发行为两类来源，证明这套
// 机制不局限于某一条协议路径）。未来可按需扩展更多事件类型，不需要改表结构
// （JSONB Payload 天然支持异构字段）。
const (
	EventTypeJoinRoom         = "join_room"
	EventTypeSendMessage      = "send_message"
	EventTypeAddFriendRequest = "add_friend_request"
)
