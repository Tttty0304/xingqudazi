package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

// ConversationRepository 是 service.ConversationStore 的真实 PostgreSQL 实现。
type ConversationRepository struct {
	db *pgxpool.Pool
}

func NewConversationRepository(db *pgxpool.Pool) *ConversationRepository {
	return &ConversationRepository{db: db}
}

// canonicalPair 把两个用户 ID 按字典序规范化为 (较小, 较大)，保证同一对用户
// 无论谁先发起私聊，都落在同一行，避免 `UNIQUE (user_a_id, user_b_id)` 因方向不同
// 产生两条重复会话记录（该约束只按精确字段匹配，不理解"方向无关"）。
func canonicalPair(userA, userB string) (string, string) {
	if userA <= userB {
		return userA, userB
	}
	return userB, userA
}

// GetOrCreate 获取或创建两用户之间的私聊会话（T70 首次发消息时惰性创建）。
func (r *ConversationRepository) GetOrCreate(ctx context.Context, userA, userB string) (*model.Conversation, error) {
	a, b := canonicalPair(userA, userB)

	var id string
	err := r.db.QueryRow(ctx,
		`INSERT INTO conversations (id, user_a_id, user_b_id) VALUES ($1, $2, $3)
		 ON CONFLICT (user_a_id, user_b_id) DO NOTHING RETURNING id`,
		uuid.NewString(), a, b,
	).Scan(&id)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("insert conversation: %w", err)
		}
		// 唯一约束冲突（ON CONFLICT DO NOTHING 不返回行）：会话已存在，查询现有记录。
		row := r.db.QueryRow(ctx, `SELECT id FROM conversations WHERE user_a_id = $1 AND user_b_id = $2`, a, b)
		if err := row.Scan(&id); err != nil {
			return nil, fmt.Errorf("find existing conversation: %w", err)
		}
	}
	return &model.Conversation{ID: id, UserAID: a, UserBID: b}, nil
}

// FindByID 按会话 ID 查找（用于 T70 历史消息查询前的参与者校验）。
func (r *ConversationRepository) FindByID(ctx context.Context, id string) (*model.Conversation, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, user_a_id, user_b_id, created_at FROM conversations WHERE id = $1`,
		id,
	)
	var c model.Conversation
	if err := row.Scan(&c.ID, &c.UserAID, &c.UserBID, &c.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.ErrRepositoryConversationNotFound
		}
		return nil, fmt.Errorf("scan conversation: %w", err)
	}
	return &c, nil
}

// ListByUser 返回该用户参与的全部会话 + 最近一条消息摘要（T71），按最近消息时间倒序。
// 没有任何消息的会话（理论上不应出现，因为会话是随首条消息惰性创建的）用会话创建时间兜底排序。
func (r *ConversationRepository) ListByUser(ctx context.Context, userID string) ([]model.ConversationSummary, error) {
	rows, err := r.db.Query(ctx,
		`SELECT c.id,
		        CASE WHEN c.user_a_id = $1 THEN c.user_b_id ELSE c.user_a_id END AS peer_id,
		        COALESCE(dm.content, '') AS last_message,
		        COALESCE(dm.created_at, c.created_at) AS last_message_at,
		        COALESCE(unread.count, 0) AS unread_count
		 FROM conversations c
		 LEFT JOIN conversation_read_cursors cursor
		   ON cursor.conversation_id = c.id AND cursor.user_id = $1
		 LEFT JOIN LATERAL (
		     SELECT content, created_at FROM direct_messages
		     WHERE conversation_id = c.id ORDER BY created_at DESC LIMIT 1
		 ) dm ON true
		 LEFT JOIN LATERAL (
		     SELECT COUNT(*) AS count FROM direct_messages unread_message
		     WHERE unread_message.conversation_id = c.id
		       AND unread_message.sender_id <> $1
		       AND unread_message.created_at > COALESCE(cursor.read_at, to_timestamp(0))
		 ) unread ON true
		 WHERE c.user_a_id = $1 OR c.user_b_id = $1
		 ORDER BY last_message_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query conversations: %w", err)
	}
	defer rows.Close()

	var result []model.ConversationSummary
	for rows.Next() {
		var s model.ConversationSummary
		if err := rows.Scan(&s.ConversationID, &s.PeerID, &s.LastMessage, &s.LastMessageAt, &s.UnreadCount); err != nil {
			return nil, fmt.Errorf("scan conversation summary: %w", err)
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate conversations: %w", err)
	}
	return result, nil
}

// MarkRead 用服务端时间写入用户在私聊会话内的已读游标。UPSERT 使重复进入会话、
// 网络重试都幂等；只向前推进 read_at，不会被并发的旧请求回退。
func (r *ConversationRepository) MarkRead(ctx context.Context, userID, conversationID string) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO conversation_read_cursors (user_id, conversation_id, read_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (user_id, conversation_id)
		 DO UPDATE SET read_at = GREATEST(conversation_read_cursors.read_at, EXCLUDED.read_at)`,
		userID, conversationID,
	)
	if err != nil {
		return fmt.Errorf("mark conversation read: %w", err)
	}
	return nil
}
