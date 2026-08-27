package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
)

// MessageRepository 是 service.MessageStore（读路径）+ ws.MessageWriter（写路径）
// 的真实 PostgreSQL 实现。写路径在 Task4 接入 WS send_message 时补充（见
// docs/04-task4-work-log.md 的范围调整说明：Task4 吸收了消息落库+幂等去重，
// Task3 已完成的读路径保持不变）。
type MessageRepository struct {
	db *pgxpool.Pool
}

func NewMessageRepository(db *pgxpool.Pool) *MessageRepository {
	return &MessageRepository{db: db}
}

// ListByRoom 按创建时间倒序分页查询（对应 T21：按时间倒序，has_more 标识是否还有更早消息）。
func (r *MessageRepository) ListByRoom(ctx context.Context, roomID string, page, size int) ([]model.Message, bool, error) {
	offset := (page - 1) * size
	// 多查一条用于判断 has_more，避免额外一次 COUNT(*) 查询。
	rows, err := r.db.Query(ctx,
		`SELECT id, msg_id, room_id, sender_id, sender_type, content, content_type, is_blocked, created_at
		 FROM messages WHERE room_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		roomID, size+1, offset,
	)
	if err != nil {
		return nil, false, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var messages []model.Message
	for rows.Next() {
		var m model.Message
		if err := rows.Scan(&m.ID, &m.MsgID, &m.RoomID, &m.SenderID, &m.SenderType, &m.Content, &m.ContentType, &m.IsBlocked, &m.CreatedAt); err != nil {
			return nil, false, fmt.Errorf("scan message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate messages: %w", err)
	}

	hasMore := len(messages) > size
	if hasMore {
		messages = messages[:size]
	}
	return messages, hasMore, nil
}

// Create 落库一条群聊消息；依赖 `messages` 表上的 `UNIQUE (room_id, msg_id)` 约束
// 做幂等去重（对应 T34：相同 msg_id 重复发送时不重复落库）。
// 返回 inserted=false 表示本次是重复消息（唯一约束冲突），调用方（Hub）据此跳过广播。
func (r *MessageRepository) Create(ctx context.Context, m *model.Message) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`INSERT INTO messages (id, msg_id, room_id, sender_id, sender_type, content, content_type)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (room_id, msg_id) DO NOTHING`,
		m.ID, m.MsgID, m.RoomID, m.SenderID, m.SenderType, m.Content, m.ContentType,
	)
	if err != nil {
		return false, fmt.Errorf("insert message: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Exists 实现 service.MessageExistenceChecker（Task18/T80：举报目标存在性校验）。
func (r *MessageRepository) Exists(ctx context.Context, id string) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM messages WHERE id = $1)`, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("check message exists: %w", err)
	}
	return exists, nil
}
