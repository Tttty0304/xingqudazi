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

// RoomRepository 是 service.RoomStore 的真实 PostgreSQL 实现。
type RoomRepository struct {
	db *pgxpool.Pool
}

func NewRoomRepository(db *pgxpool.Pool) *RoomRepository {
	return &RoomRepository{db: db}
}

func (r *RoomRepository) List(ctx context.Context) ([]model.Room, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, name, COALESCE(topic, ''), is_preset, COALESCE(creator_id::text, ''), created_at FROM rooms ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query rooms: %w", err)
	}
	defer rows.Close()

	var rooms []model.Room
	for rows.Next() {
		var room model.Room
		if err := rows.Scan(&room.ID, &room.Name, &room.Topic, &room.IsPreset, &room.CreatorID, &room.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rooms: %w", err)
	}
	return rooms, nil
}

func (r *RoomRepository) FindByID(ctx context.Context, id string) (*model.Room, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, name, COALESCE(topic, ''), is_preset, COALESCE(creator_id::text, ''), created_at FROM rooms WHERE id = $1`,
		id,
	)
	var room model.Room
	if err := row.Scan(&room.ID, &room.Name, &room.Topic, &room.IsPreset, &room.CreatorID, &room.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.ErrRepositoryRoomNotFound
		}
		return nil, fmt.Errorf("scan room: %w", err)
	}
	return &room, nil
}

func (r *RoomRepository) Create(ctx context.Context, room *model.Room) error {
	if _, err := r.db.Exec(ctx,
		`INSERT INTO rooms (id, name, topic, is_preset, creator_id) VALUES ($1, $2, $3, false, $4)`,
		room.ID, room.Name, room.Topic, room.CreatorID,
	); err != nil {
		return fmt.Errorf("insert room: %w", err)
	}
	return nil
}
