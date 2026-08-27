package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
)

// TestBotActionLogRepository_CreateAndListByBotUser 覆盖能力补齐项（LLM 驱动
// 机器人最小验证）：这张表此前从 0001 建表起就存在，但从未被任何代码真正
// 写入过（Plan 原始设计"本次不产生机器人消息，故暂不写入"）。本测试验证
// Create/ListByBotUser 对真实 Postgres 的往返正确性，是这张表第一次有单测
// 覆盖真实写入路径。
func TestBotActionLogRepository_CreateAndListByBotUser(t *testing.T) {
	db := testDB(t)
	repo := NewBotActionLogRepository(db)
	ctx := context.Background()

	botUserID := createTestUser(t, db)

	logID := uuid.NewString()
	if err := repo.Create(ctx, &model.BotActionLog{
		ID:             logID,
		BotUserID:      botUserID,
		RoomID:         nil,
		DecisionReason: "LLM(qwen-plus)生成开场白并发送：大家好，我是机器人",
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list, err := repo.ListByBotUser(ctx, botUserID)
	if err != nil {
		t.Fatalf("ListByBotUser failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly 1 log entry, got %d", len(list))
	}
	if list[0].ID != logID || list[0].BotUserID != botUserID {
		t.Fatalf("unexpected log entry: %+v", list[0])
	}
	if list[0].RoomID != nil {
		t.Fatalf("expected nil RoomID (not passed), got %v", *list[0].RoomID)
	}
}

// TestBotActionLogRepository_Create_WithRoomID 覆盖 RoomID 非空场景（对应
// 真实 cmd/bot 用法：每次在具体房间发消息后都会带上 room_id）。
func TestBotActionLogRepository_Create_WithRoomID(t *testing.T) {
	db := testDB(t)
	repo := NewBotActionLogRepository(db)
	ctx := context.Background()

	botUserID := createTestUser(t, db)
	roomID := createTestRoom(t, db)

	if err := repo.Create(ctx, &model.BotActionLog{
		ID:             uuid.NewString(),
		BotUserID:      botUserID,
		RoomID:         &roomID,
		DecisionReason: "测试用途",
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list, err := repo.ListByBotUser(ctx, botUserID)
	if err != nil {
		t.Fatalf("ListByBotUser failed: %v", err)
	}
	if len(list) != 1 || list[0].RoomID == nil || *list[0].RoomID != roomID {
		t.Fatalf("expected RoomID=%s, got %+v", roomID, list)
	}
}
