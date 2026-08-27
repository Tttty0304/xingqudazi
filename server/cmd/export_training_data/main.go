// Package main 实现"数据存储格式能否直接支撑未来投喂给模型训练用户替身"
// 这一问题的最小验证工具（能力补齐项，2026-08-19）。
//
// 背景：`interaction_events` 表此前从建表起从未被写入过（见
// docs/24-training-data-pipeline-work-log.md）；本轮已让 ws.Hub/
// service.FriendService 在关键行为发生时真实写入这张表（join_room/
// send_message/add_friend_request）。但"数据库里有行为事件"和"这份数据的
// 格式真的可以被整理成训练语料"是两件事——本工具验证后者：把某个用户的
// 基础画像（账号信息 + 关注事项）与行为事件历史，组装成一份结构化 JSON，
// 证明这条"从原始行为表到可投喂格式"的链路确实是通的。
//
// 用法：
//
//	export EXPORT_USERNAME=alice_xxx
//	go run ./cmd/export_training_data > export.json
//
// 依赖已通过 `docker compose -f deploy/docker-compose.yml up -d` 起的
// postgres（默认假设 localhost:5432，可通过 POSTGRES_DSN 环境变量覆盖）。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"xingqudazi-im/server/internal/config"
	"xingqudazi-im/server/internal/repository"
	"xingqudazi-im/server/internal/service"
)

// exportedUser / exportedWatchTopic / exportedInteractionEvent 是本工具定义的
// 导出 JSON 结构（与 model 层的内部字段命名解耦，专门为"训练数据消费者"设计
// 的对外格式：snake_case 字段名、扁平化的可空字段处理），这正是"格式层面"
// 这个问题真正要回答的部分——数据库表结构本身不等于训练数据格式，中间需要
// 这样一层显式的转换。
type exportedUser struct {
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	IsGuest   bool      `json:"is_guest"`
	IsBot     bool      `json:"is_bot"`
	CreatedAt time.Time `json:"created_at"`
}

type exportedWatchTopic struct {
	Keywords  string `json:"keywords"`
	RoomID    string `json:"room_id,omitempty"`
	Priority  int    `json:"priority"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type exportedInteractionEvent struct {
	EventType    string          `json:"event_type"`
	RoomID       *string         `json:"room_id,omitempty"`
	TargetUserID *string         `json:"target_user_id,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

// trainingDataExport 是本工具产出的完整导出结构：一个用户的画像 + 行为事件
// 时间线，`FormatVersion` 显式标注格式版本号——训练数据管道的格式一旦被下游
// 消费，后续变更需要走版本兼容策略，从第一版就带上版本号是基本的工程习惯。
type trainingDataExport struct {
	FormatVersion     string                     `json:"format_version"`
	GeneratedAt       time.Time                  `json:"generated_at"`
	User              exportedUser               `json:"user"`
	WatchTopics       []exportedWatchTopic       `json:"watch_topics"`
	InteractionEvents []exportedInteractionEvent `json:"interaction_events"`
}

func main() {
	ctx := context.Background()

	username := os.Getenv("EXPORT_USERNAME")
	if username == "" {
		log.Fatalf("请设置 EXPORT_USERNAME 环境变量（要导出哪个用户的数据）")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	dbPool, err := repository.NewPostgresPool(ctx, cfg.PostgresDSN, cfg.PostgresMaxConns, cfg.PostgresMinConns)
	if err != nil {
		log.Fatalf("连接数据库失败: %v", err)
	}
	defer dbPool.Close()

	userRepo := repository.NewUserRepository(dbPool)
	watchTopicRepo := repository.NewWatchTopicRepository(dbPool)
	eventRepo := repository.NewInteractionEventRepository(dbPool)

	user, err := userRepo.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, service.ErrRepositoryUserNotFound) {
			log.Fatalf("用户 %q 不存在", username)
		}
		log.Fatalf("查询用户失败: %v", err)
	}

	watchTopics, err := watchTopicRepo.ListByUser(ctx, user.ID)
	if err != nil {
		log.Fatalf("查询关注事项失败: %v", err)
	}

	events, err := eventRepo.ListByUser(ctx, user.ID)
	if err != nil {
		log.Fatalf("查询行为事件失败: %v", err)
	}

	export := trainingDataExport{
		FormatVersion: "v1-minimal",
		GeneratedAt:   time.Now().UTC(),
		User: exportedUser{
			UserID:    user.ID,
			Username:  user.Username,
			IsGuest:   user.IsGuest,
			IsBot:     user.IsBot,
			CreatedAt: user.CreatedAt,
		},
		WatchTopics:       make([]exportedWatchTopic, 0, len(watchTopics)),
		InteractionEvents: make([]exportedInteractionEvent, 0, len(events)),
	}

	for _, wt := range watchTopics {
		item := exportedWatchTopic{Keywords: wt.Keywords, RoomID: wt.RoomID, Priority: wt.Priority}
		if wt.ExpiresAt != nil {
			item.ExpiresAt = wt.ExpiresAt.Format(time.RFC3339)
		}
		export.WatchTopics = append(export.WatchTopics, item)
	}

	for _, e := range events {
		export.InteractionEvents = append(export.InteractionEvents, exportedInteractionEvent{
			EventType:    e.EventType,
			RoomID:       e.RoomID,
			TargetUserID: e.TargetUserID,
			Payload:      e.Payload,
			CreatedAt:    e.CreatedAt,
		})
	}

	out, err := json.MarshalIndent(export, "", "  ")
	if err != nil {
		log.Fatalf("序列化导出结果失败: %v", err)
	}
	fmt.Println(string(out))

	log.Printf("[stderr] 导出完成：用户=%s，关注事项=%d 条，行为事件=%d 条", username, len(export.WatchTopics), len(export.InteractionEvents))
}
