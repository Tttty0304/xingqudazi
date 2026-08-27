package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
)

// ReportRepository 是 service.ReportStore 的真实 PostgreSQL 实现（Task18/T80）。
type ReportRepository struct {
	db *pgxpool.Pool
}

func NewReportRepository(db *pgxpool.Pool) *ReportRepository {
	return &ReportRepository{db: db}
}

func (r *ReportRepository) Create(ctx context.Context, rep *model.Report) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO reports (id, reporter_id, target_type, target_id, reason, status)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		rep.ID, rep.ReporterID, rep.TargetType, rep.TargetID, rep.Reason, rep.Status,
	)
	if err != nil {
		return fmt.Errorf("insert report: %w", err)
	}
	return nil
}

// FindExisting 查找同一举报人对同一目标是否已举报过（T80 幂等边界：重复举报
// 不重复计数，返回已有记录供 service 层直接复用）。找不到时返回 (nil, nil)，
// 与其它 repository 用 sentinel error 表示"未找到"的风格不同——这里因为
// "未找到"是正常预期路径（首次举报），不是异常，用 nil 更直接。
func (r *ReportRepository) FindExisting(ctx context.Context, reporterID, targetType, targetID string) (*model.Report, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, reporter_id, target_type, target_id, reason, status, created_at
		 FROM reports WHERE reporter_id = $1 AND target_type = $2 AND target_id = $3
		 ORDER BY created_at DESC LIMIT 1`,
		reporterID, targetType, targetID,
	)
	var rep model.Report
	if err := row.Scan(&rep.ID, &rep.ReporterID, &rep.TargetType, &rep.TargetID, &rep.Reason, &rep.Status, &rep.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan report: %w", err)
	}
	return &rep, nil
}
