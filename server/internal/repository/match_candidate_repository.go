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

// MatchCandidateRepository 是 service.MatchCandidateStore 的真实 PostgreSQL 实现（Task20）。
type MatchCandidateRepository struct {
	db *pgxpool.Pool
}

func NewMatchCandidateRepository(db *pgxpool.Pool) *MatchCandidateRepository {
	return &MatchCandidateRepository{db: db}
}

// Create 插入一条候选记录；命中表上 `UNIQUE (user_a_id, user_b_id, room_id)` 约束时
// 静默跳过（同一对用户在同一房间维度已生成过候选，不重复插入/不报错，保持生成动作
// 可安全重复触发这一特性）。
func (r *MatchCandidateRepository) Create(ctx context.Context, m *model.MatchCandidate) (inserted bool, err error) {
	var roomID interface{}
	if m.RoomID != "" {
		roomID = m.RoomID
	}
	tag, err := r.db.Exec(ctx,
		`INSERT INTO match_candidates (id, user_a_id, user_b_id, shared_topic, room_id, match_reason, match_score, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (user_a_id, user_b_id, room_id) DO NOTHING`,
		m.ID, m.UserAID, m.UserBID, m.SharedTopic, roomID, m.MatchReason, m.MatchScore, m.Status,
	)
	if err != nil {
		return false, fmt.Errorf("insert match candidate: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListPendingByUser 返回该用户名下全部待确认（pending_review）候选记录（Task20 推荐列表）。
func (r *MatchCandidateRepository) ListPendingByUser(ctx context.Context, userID string) ([]model.MatchCandidate, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, user_a_id, user_b_id, COALESCE(shared_topic, ''), COALESCE(room_id::text, ''),
		        COALESCE(match_reason, ''), COALESCE(match_score, 0), status, created_at
		 FROM match_candidates
		 WHERE (user_a_id = $1 OR user_b_id = $1) AND status = 'pending_review'
		 ORDER BY match_score DESC, created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query match candidates: %w", err)
	}
	defer rows.Close()

	var result []model.MatchCandidate
	for rows.Next() {
		var m model.MatchCandidate
		if err := rows.Scan(&m.ID, &m.UserAID, &m.UserBID, &m.SharedTopic, &m.RoomID, &m.MatchReason, &m.MatchScore, &m.Status, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan match candidate: %w", err)
		}
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate match candidates: %w", err)
	}
	return result, nil
}

// FindByID 按候选 ID 查找（用于 PUT /api/recommendations/{id} 校验操作人是否为候选双方之一）。
func (r *MatchCandidateRepository) FindByID(ctx context.Context, id string) (*model.MatchCandidate, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, user_a_id, user_b_id, COALESCE(shared_topic, ''), COALESCE(room_id::text, ''),
		        COALESCE(match_reason, ''), COALESCE(match_score, 0), status, created_at
		 FROM match_candidates WHERE id = $1`,
		id,
	)
	var m model.MatchCandidate
	if err := row.Scan(&m.ID, &m.UserAID, &m.UserBID, &m.SharedTopic, &m.RoomID, &m.MatchReason, &m.MatchScore, &m.Status, &m.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.ErrRepositoryCandidateNotFound
		}
		return nil, fmt.Errorf("scan match candidate: %w", err)
	}
	return &m, nil
}

// UpdateStatus 更新候选记录状态（confirm/dismiss），仅当当前仍是 pending_review 才更新成功。
func (r *MatchCandidateRepository) UpdateStatus(ctx context.Context, id, newStatus string) (updated bool, err error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE match_candidates SET status = $1 WHERE id = $2 AND status = 'pending_review'`,
		newStatus, id,
	)
	if err != nil {
		return false, fmt.Errorf("update match candidate status: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
