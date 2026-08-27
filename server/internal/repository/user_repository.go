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

// UserRepository 是 service.UserStore 的真实 PostgreSQL 实现。
type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, u *model.User) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO users (id, username, password_hash, is_guest) VALUES ($1, $2, $3, $4)`,
		u.ID, u.Username, nullableString(u.PasswordHash), u.IsGuest,
	)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, username, COALESCE(password_hash, ''), is_guest, is_bot, COALESCE(avatar_url, ''), bio, created_at FROM users WHERE username = $1`,
		username,
	)
	return scanUser(row)
}

func (r *UserRepository) FindByID(ctx context.Context, id string) (*model.User, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, username, COALESCE(password_hash, ''), is_guest, is_bot, COALESCE(avatar_url, ''), bio, created_at FROM users WHERE id = $1`,
		id,
	)
	return scanUser(row)
}

// FindByIDs 一次查询批量返回用户基础信息，供前端"批量展示用户名"场景使用
// （`GET /api/users?ids=`），SQL 用 `= ANY($1)` 避免逐条查询的 N+1 开销。
// `id` 列是 UUID 类型，非法格式的 ID（如前端传入的脏数据/已注销用户的陈旧引用）
// 会导致 Postgres 在做隐式类型转换时报错而非"查不到"，因此这里预先过滤掉不是
// 合法 UUID 格式的 ID（与"找不到的 ID 静默忽略"语义保持一致，而不是让整个批量
// 请求因为一个非法 ID 而 500）。
func (r *UserRepository) FindByIDs(ctx context.Context, ids []string) ([]model.User, error) {
	validIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, err := uuid.Parse(id); err == nil {
			validIDs = append(validIDs, id)
		}
	}
	if len(validIDs) == 0 {
		return nil, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT id, username, COALESCE(password_hash, ''), is_guest, is_bot, COALESCE(avatar_url, ''), bio, created_at FROM users WHERE id = ANY($1)`,
		validIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("query users by ids: %w", err)
	}
	defer rows.Close()

	var result []model.User
	for rows.Next() {
		var u model.User
		if err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsGuest, &u.IsBot, &u.AvatarURL, &u.Bio, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		result = append(result, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return result, nil
}

// SetIsBot 显式标记/取消标记某个账号为机器人身份（能力补齐项：LLM 驱动机器人
// 最小验证，见 server/cmd/bot）。故意不通过任何公开 HTTP 接口暴露——机器人
// 身份是一个需要被信任的标签（WS 广播据此决定 sender_type，前端据此展示
// "（机器人）"标识，属于 ★13 强制披露机制的一部分），若任意已登录用户能通过
// 接口自我标记为机器人，等同于给了每个用户一个绕过透明度要求的开关，因此
// 只保留为内部工具（cmd/bot 启动时直连数据库调用）可用的仓储方法。
func (r *UserRepository) SetIsBot(ctx context.Context, userID string, isBot bool) error {
	tag, err := r.db.Exec(ctx, `UPDATE users SET is_bot = $1, updated_at = now() WHERE id = $2`, isBot, userID)
	if err != nil {
		return fmt.Errorf("update is_bot: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return service.ErrRepositoryUserNotFound
	}
	return nil
}

// IsBot 实现 ws.BotChecker（WS 握手鉴权时查询"这个连接对应的账号是否为机器人"，
// 用于服务端权威判定 sender_type，而不是信任客户端自称）。找不到用户时返回
// false（不视为错误——一个已通过 JWT 鉴权、但账号已被删除的边界场景，不应该
// 阻塞 WS 升级，只是必然不是机器人身份）。
func (r *UserRepository) IsBot(ctx context.Context, userID string) (bool, error) {
	var isBot bool
	err := r.db.QueryRow(ctx, `SELECT is_bot FROM users WHERE id = $1`, userID).Scan(&isBot)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query is_bot: %w", err)
	}
	return isBot, nil
}

func (r *UserRepository) UpdateProfile(ctx context.Context, userID, avatarURL, bio string) (*model.User, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE users SET avatar_url = $1, bio = $2, updated_at = now() WHERE id = $3
		 RETURNING id, username, COALESCE(password_hash, ''), is_guest, is_bot, COALESCE(avatar_url, ''), bio, created_at`,
		nullableString(avatarURL), bio, userID,
	)
	return scanUser(row)
}

func scanUser(row pgx.Row) (*model.User, error) {
	var u model.User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsGuest, &u.IsBot, &u.AvatarURL, &u.Bio, &u.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.ErrRepositoryUserNotFound
		}
		return nil, fmt.Errorf("scan user: %w", err)
	}
	return &u, nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
