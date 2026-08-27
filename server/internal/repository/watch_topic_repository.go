package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
)

// WatchTopicRepository 是 service.WatchTopicStore 的真实 PostgreSQL 实现（Task19/P1）。
type WatchTopicRepository struct {
	db *pgxpool.Pool
}

func NewWatchTopicRepository(db *pgxpool.Pool) *WatchTopicRepository {
	return &WatchTopicRepository{db: db}
}

func (r *WatchTopicRepository) Create(ctx context.Context, t *model.WatchTopic) error {
	var roomID interface{}
	if t.RoomID != "" {
		roomID = t.RoomID
	}
	var expiresAt interface{}
	if t.ExpiresAt != nil {
		expiresAt = *t.ExpiresAt
	}
	_, err := r.db.Exec(ctx,
		`INSERT INTO user_watch_topics (id, user_id, room_id, keywords, priority, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		t.ID, t.UserID, roomID, t.Keywords, t.Priority, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert watch topic: %w", err)
	}
	return nil
}

func (r *WatchTopicRepository) ListByUser(ctx context.Context, userID string) ([]model.WatchTopic, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, COALESCE(room_id::text, ''), keywords, priority, expires_at, created_at
		 FROM user_watch_topics WHERE user_id = $1 ORDER BY priority DESC, created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query watch topics: %w", err)
	}
	defer rows.Close()

	var result []model.WatchTopic
	for rows.Next() {
		var t model.WatchTopic
		if err := rows.Scan(&t.ID, &t.UserID, &t.RoomID, &t.Keywords, &t.Priority, &t.ExpiresAt, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan watch topic: %w", err)
		}
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate watch topics: %w", err)
	}
	return result, nil
}

// Delete 对应 T123（本轮新增）：删除仅限本人所有的关注事项。
func (r *WatchTopicRepository) Delete(ctx context.Context, id, userID string) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM user_watch_topics WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return false, fmt.Errorf("delete watch topic: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListAll 返回全部未过期的关注事项（Task20：RecommendationService 生成候选时的
// 输入源，需要跨用户全量扫描做两两匹配，量级足够小时无需分页）。
func (r *WatchTopicRepository) ListAll(ctx context.Context) ([]model.WatchTopic, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, COALESCE(room_id::text, ''), keywords, priority, expires_at, created_at
		 FROM user_watch_topics
		 WHERE expires_at IS NULL OR expires_at > now()`,
	)
	if err != nil {
		return nil, fmt.Errorf("query all watch topics: %w", err)
	}
	defer rows.Close()

	var result []model.WatchTopic
	for rows.Next() {
		var t model.WatchTopic
		if err := rows.Scan(&t.ID, &t.UserID, &t.RoomID, &t.Keywords, &t.Priority, &t.ExpiresAt, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan watch topic: %w", err)
		}
		result = append(result, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate all watch topics: %w", err)
	}
	return result, nil
}
