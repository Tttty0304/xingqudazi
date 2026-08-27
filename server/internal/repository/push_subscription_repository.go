package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
)

// PushSubscriptionRepository 是 service.PushSubscriptionStore 的真实 PostgreSQL 实现
// （Task17 Web Push）。
type PushSubscriptionRepository struct {
	db *pgxpool.Pool
}

func NewPushSubscriptionRepository(db *pgxpool.Pool) *PushSubscriptionRepository {
	return &PushSubscriptionRepository{db: db}
}

// Create 插入或更新（同一用户+同一 endpoint 重复订阅，如浏览器刷新页面重新调用
// subscribe，视为幂等覆盖而不是报错）一条订阅记录。
func (r *PushSubscriptionRepository) Create(ctx context.Context, s *model.PushSubscription) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id, endpoint) DO UPDATE SET p256dh = EXCLUDED.p256dh, auth = EXCLUDED.auth`,
		s.ID, s.UserID, s.Endpoint, s.P256dh, s.Auth,
	)
	if err != nil {
		return fmt.Errorf("upsert push subscription: %w", err)
	}
	return nil
}

// DeleteByEndpoint 对应浏览器取消订阅（PushManager.unsubscribe()）后通知后端清理。
func (r *PushSubscriptionRepository) DeleteByEndpoint(ctx context.Context, userID, endpoint string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2`, userID, endpoint)
	if err != nil {
		return fmt.Errorf("delete push subscription: %w", err)
	}
	return nil
}

// ListByUser 返回该用户名下全部有效订阅（一个用户可能有多端/多浏览器同时订阅）。
func (r *PushSubscriptionRepository) ListByUser(ctx context.Context, userID string) ([]model.PushSubscription, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_id, endpoint, p256dh, auth, created_at FROM push_subscriptions WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query push subscriptions: %w", err)
	}
	defer rows.Close()

	var result []model.PushSubscription
	for rows.Next() {
		var s model.PushSubscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan push subscription: %w", err)
		}
		result = append(result, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate push subscriptions: %w", err)
	}
	return result, nil
}

// DeleteByID 供推送失败（订阅已失效，如浏览器返回 404/410）时清理僵尸订阅使用。
func (r *PushSubscriptionRepository) DeleteByID(ctx context.Context, id string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete push subscription by id: %w", err)
	}
	return nil
}
