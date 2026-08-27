package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
)

// MediaAssetRepository 是 service.MediaStore 的真实 PostgreSQL 实现（Task16）。
type MediaAssetRepository struct {
	db *pgxpool.Pool
}

func NewMediaAssetRepository(db *pgxpool.Pool) *MediaAssetRepository {
	return &MediaAssetRepository{db: db}
}

func (r *MediaAssetRepository) Create(ctx context.Context, m *model.MediaAsset) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO media_assets (id, owner_id, media_type, url, mime_type, size_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		m.ID, m.OwnerID, m.MediaType, m.URL, m.MimeType, m.SizeBytes,
	)
	if err != nil {
		return fmt.Errorf("insert media asset: %w", err)
	}
	return nil
}
