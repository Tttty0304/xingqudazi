package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
)

// InteractionEventRepository 是 interaction_events 表（能力补齐项：给"未来
// 投喂给模型训练用户替身"这个设想补最基础的行为原始数据）的真实 PostgreSQL
// 实现，供 ws.Hub / service.FriendService 等业务代码在发生关键用户行为时
// 各记一条事件。
type InteractionEventRepository struct {
	db *pgxpool.Pool
}

func NewInteractionEventRepository(db *pgxpool.Pool) *InteractionEventRepository {
	return &InteractionEventRepository{db: db}
}

// Create 落库一条行为事件。RoomID/TargetUserID 均可为 nil；Payload 为 nil 时
// 落库为 SQL NULL（而非 JSON 的 "null" 字符串，与 JSONB 列语义保持一致）。
// 失败时返回 error，但调用方（ws.Hub/service.FriendService）应当只记录日志
// 而不阻塞主流程——记录一条训练用的行为事件不应该因为偶发的数据库抖动就影响
// 用户发消息/加好友这些核心功能的可用性，与本项目其它"旁路能力"（如
// Web Push 通知）的失败处理原则一致。
func (r *InteractionEventRepository) Create(ctx context.Context, e *model.InteractionEvent) error {
	var payload interface{}
	if len(e.Payload) > 0 {
		payload = e.Payload
	}
	_, err := r.db.Exec(ctx,
		`INSERT INTO interaction_events (id, user_id, room_id, target_user_id, event_type, payload)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		e.ID, e.UserID, e.RoomID, e.TargetUserID, e.EventType, payload,
	)
	if err != nil {
		return fmt.Errorf("insert interaction_event: %w", err)
	}
	return nil
}

// ListByUser 按创建时间倒序查询某个用户的全部行为事件，供
// cmd/export_training_data 等导出工具使用，验证"这份数据格式上确实可以被
// 整理成结构化训练语料"这条链路。
func (r *InteractionEventRepository) ListByUser(ctx context.Context, userID string) ([]model.InteractionEvent, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, room_id, target_user_id, event_type, payload, created_at
		 FROM interaction_events WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query interaction_events: %w", err)
	}
	defer rows.Close()

	var result []model.InteractionEvent
	for rows.Next() {
		var e model.InteractionEvent
		var payload []byte
		if err := rows.Scan(&e.ID, &e.UserID, &e.RoomID, &e.TargetUserID, &e.EventType, &payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan interaction_event: %w", err)
		}
		if len(payload) > 0 {
			e.Payload = json.RawMessage(payload)
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate interaction_events: %w", err)
	}
	return result, nil
}
