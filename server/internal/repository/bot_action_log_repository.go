package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
)

// BotActionLogRepository 是 bot_action_log 表（Task19 预留，能力补齐项首次真正
// 写入）的真实 PostgreSQL 实现，供 cmd/bot 在机器人每次由 LLM 驱动发出一条
// 消息后调用一次 Create，留下可追溯的决策记录。
type BotActionLogRepository struct {
	db *pgxpool.Pool
}

func NewBotActionLogRepository(db *pgxpool.Pool) *BotActionLogRepository {
	return &BotActionLogRepository{db: db}
}

// Create 落库一条机器人决策记录。TriggerWatchTopicID/RoomID 均可为 nil
// （对应表结构里的可空外键，`ON DELETE SET NULL`）。
func (r *BotActionLogRepository) Create(ctx context.Context, log *model.BotActionLog) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO bot_action_log (id, bot_user_id, trigger_watch_topic_id, room_id, decision_reason)
		 VALUES ($1, $2, $3, $4, $5)`,
		log.ID, log.BotUserID, log.TriggerWatchTopicID, log.RoomID, log.DecisionReason,
	)
	if err != nil {
		return fmt.Errorf("insert bot_action_log: %w", err)
	}
	return nil
}

// ListByBotUser 按创建时间倒序查询某个机器人账号的全部决策记录，供
// cmd/bot 验证运行结果、以及未来运营侧查看"机器人做过什么"使用。
func (r *BotActionLogRepository) ListByBotUser(ctx context.Context, botUserID string) ([]model.BotActionLog, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, bot_user_id, trigger_watch_topic_id, room_id, decision_reason, created_at
		 FROM bot_action_log WHERE bot_user_id = $1 ORDER BY created_at DESC`,
		botUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("query bot_action_log: %w", err)
	}
	defer rows.Close()

	var result []model.BotActionLog
	for rows.Next() {
		var l model.BotActionLog
		if err := rows.Scan(&l.ID, &l.BotUserID, &l.TriggerWatchTopicID, &l.RoomID, &l.DecisionReason, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan bot_action_log: %w", err)
		}
		result = append(result, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bot_action_log: %w", err)
	}
	return result, nil
}
