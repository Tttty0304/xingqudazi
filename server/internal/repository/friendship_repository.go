package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

// FriendshipRepository 是 service.FriendshipStore 的真实 PostgreSQL 实现。
type FriendshipRepository struct {
	db *pgxpool.Pool
}

func NewFriendshipRepository(db *pgxpool.Pool) *FriendshipRepository {
	return &FriendshipRepository{db: db}
}

// Create 插入一条待处理好友请求（requester -> target）。表上的
// `UNIQUE (requester_id, target_id)` 约束命中时返回 inserted=false，
// 由 service 层结合 FindBetween 的结果翻译为语义化错误（T60 边界：重复发起幂等/拒绝）。
func (r *FriendshipRepository) Create(ctx context.Context, f *model.Friendship) (inserted bool, err error) {
	tag, err := r.db.Exec(ctx,
		`INSERT INTO friendships (id, requester_id, target_id, status) VALUES ($1, $2, $3, 'pending')
		 ON CONFLICT (requester_id, target_id) DO NOTHING`,
		f.ID, f.RequesterID, f.TargetID,
	)
	if err != nil {
		return false, fmt.Errorf("insert friendship: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// FindByID 按请求 ID 查找（用于 T61/T62 接受或拒绝好友请求）。
func (r *FriendshipRepository) FindByID(ctx context.Context, id string) (*model.Friendship, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, requester_id, target_id, status, created_at, updated_at FROM friendships WHERE id = $1`,
		id,
	)
	return scanFriendship(row)
}

// FindBetween 查找两用户之间任意方向已存在的关系（用于 T60 判断是否已是好友/已有请求）。
// 找不到时返回 service.ErrRepositoryFriendshipNotFound，与其它 repository 的"未找到"约定一致。
func (r *FriendshipRepository) FindBetween(ctx context.Context, userA, userB string) (*model.Friendship, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, requester_id, target_id, status, created_at, updated_at FROM friendships
		 WHERE (requester_id = $1 AND target_id = $2) OR (requester_id = $2 AND target_id = $1)
		 ORDER BY created_at DESC LIMIT 1`,
		userA, userB,
	)
	return scanFriendship(row)
}

// UpdateStatus 更新好友请求状态，仅当当前状态仍为 pending 才更新成功（T62：
// 已处理过的请求重复操作时 RowsAffected=0，service 层据此判定 409）。
func (r *FriendshipRepository) UpdateStatus(ctx context.Context, id, newStatus string) (updated bool, err error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE friendships SET status = $1, updated_at = now() WHERE id = $2 AND status = 'pending'`,
		newStatus, id,
	)
	if err != nil {
		return false, fmt.Errorf("update friendship status: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListAcceptedByUser 返回该用户全部已接受好友关系的原始记录（T63），
// service 层据此计算出"对方用户 ID"并查询用户名/在线态。
func (r *FriendshipRepository) ListAcceptedByUser(ctx context.Context, userID string) ([]model.Friendship, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, requester_id, target_id, status, created_at, updated_at FROM friendships
		 WHERE (requester_id = $1 OR target_id = $1) AND status = 'accepted'`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query friendships: %w", err)
	}
	defer rows.Close()

	var result []model.Friendship
	for rows.Next() {
		var f model.Friendship
		if err := rows.Scan(&f.ID, &f.RequesterID, &f.TargetID, &f.Status, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan friendship: %w", err)
		}
		result = append(result, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate friendships: %w", err)
	}
	return result, nil
}

// ListPendingByUser 返回该用户涉及的全部待处理（pending）好友请求原始记录
// （T120，本轮新增：此前只能通过 WS `friend_request_received` 实时收到请求通知，
// 若接收方当时离线，之后没有任何接口能补看"我有哪些待处理的好友请求"，是一个
// 真实的功能缺口，本次补齐）。service 层据此计算方向（incoming/outgoing）与对方用户名。
func (r *FriendshipRepository) ListPendingByUser(ctx context.Context, userID string) ([]model.Friendship, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, requester_id, target_id, status, created_at, updated_at FROM friendships
		 WHERE (requester_id = $1 OR target_id = $1) AND status = 'pending'
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query pending friendships: %w", err)
	}
	defer rows.Close()

	var result []model.Friendship
	for rows.Next() {
		var f model.Friendship
		if err := rows.Scan(&f.ID, &f.RequesterID, &f.TargetID, &f.Status, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan pending friendship: %w", err)
		}
		result = append(result, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending friendships: %w", err)
	}
	return result, nil
}

// Delete 删除一条已存在的好友关系（对应 Plan Part3 `DELETE /api/friends/{id}`）。
// id 为对方 user_id，operatorID 为当前登录用户，仅删除双方之间的关系，不校验方向。
func (r *FriendshipRepository) Delete(ctx context.Context, operatorID, peerID string) (deleted bool, err error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM friendships
		 WHERE ((requester_id = $1 AND target_id = $2) OR (requester_id = $2 AND target_id = $1))
		 AND status = 'accepted'`,
		operatorID, peerID,
	)
	if err != nil {
		return false, fmt.Errorf("delete friendship: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// IsFriend 实现 service.FriendChecker（Task15/T72：仅好友可私聊）。方向无关。
func (r *FriendshipRepository) IsFriend(ctx context.Context, userA, userB string) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS (
			SELECT 1 FROM friendships
			WHERE status = 'accepted'
			AND ((requester_id = $1 AND target_id = $2) OR (requester_id = $2 AND target_id = $1))
		 )`,
		userA, userB,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check is friend: %w", err)
	}
	return exists, nil
}

func scanFriendship(row pgx.Row) (*model.Friendship, error) {
	var f model.Friendship
	if err := row.Scan(&f.ID, &f.RequesterID, &f.TargetID, &f.Status, &f.CreatedAt, &f.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.ErrRepositoryFriendshipNotFound
		}
		return nil, fmt.Errorf("scan friendship: %w", err)
	}
	return &f, nil
}
