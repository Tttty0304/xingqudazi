package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
)

// DirectMessageRepository 是 service.DirectMessageStore（读路径）+
// ws.DirectMessageSender 依赖的写路径 的真实 PostgreSQL 实现。
// 与 MessageRepository（群聊）的落库+幂等去重模式完全一致，只是表名和唯一约束
// 换成 `direct_messages` 上的 `UNIQUE (conversation_id, msg_id)`。
type DirectMessageRepository struct {
	db *pgxpool.Pool
}

func NewDirectMessageRepository(db *pgxpool.Pool) *DirectMessageRepository {
	return &DirectMessageRepository{db: db}
}

// Create 落库一条私聊消息，依赖 `UNIQUE (conversation_id, msg_id)` 做幂等去重
// （与群聊 T34 同款设计）。返回 inserted=false 表示重复消息，调用方应跳过广播。
func (r *DirectMessageRepository) Create(ctx context.Context, m *model.DirectMessage) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`INSERT INTO direct_messages (id, msg_id, conversation_id, sender_id, sender_type, content, content_type)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (conversation_id, msg_id) DO NOTHING`,
		m.ID, m.MsgID, m.ConversationID, m.SenderID, m.SenderType, m.Content, m.ContentType,
	)
	if err != nil {
		return false, fmt.Errorf("insert direct message: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListByConversation 按创建时间倒序分页查询私聊历史（与 MessageRepository.ListByRoom 同款设计）。
func (r *DirectMessageRepository) ListByConversation(ctx context.Context, conversationID string, page, size int) ([]model.DirectMessage, bool, error) {
	offset := (page - 1) * size
	rows, err := r.db.Query(ctx,
		`SELECT id, msg_id, conversation_id, sender_id, sender_type, content, content_type, is_blocked, created_at
		 FROM direct_messages WHERE conversation_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		conversationID, size+1, offset,
	)
	if err != nil {
		return nil, false, fmt.Errorf("query direct messages: %w", err)
	}
	defer rows.Close()

	var messages []model.DirectMessage
	for rows.Next() {
		var m model.DirectMessage
		if err := rows.Scan(&m.ID, &m.MsgID, &m.ConversationID, &m.SenderID, &m.SenderType, &m.Content, &m.ContentType, &m.IsBlocked, &m.CreatedAt); err != nil {
			return nil, false, fmt.Errorf("scan direct message: %w", err)
		}
		messages = append(messages, m)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("iterate direct messages: %w", err)
	}

	hasMore := len(messages) > size
	if hasMore {
		messages = messages[:size]
	}
	return messages, hasMore, nil
}

// Exists 实现 service.MessageExistenceChecker（Task18/T80：举报目标存在性校验）。
func (r *DirectMessageRepository) Exists(ctx context.Context, id string) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM direct_messages WHERE id = $1)`, id).Scan(&exists); err != nil {
		return false, fmt.Errorf("check direct message exists: %w", err)
	}
	return exists, nil
}
