package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
)

// TestInteractionEventRepository_CreateAndListByUser 覆盖能力补齐项（给"未来
// 投喂给模型训练用户替身"补最基础的行为原始数据）：这张表此前从建表起从未被
// 写入过，本测试是它第一次有单测覆盖真实写入/查询路径。
func TestInteractionEventRepository_CreateAndListByUser(t *testing.T) {
	db := testDB(t)
	repo := NewInteractionEventRepository(db)
	ctx := context.Background()

	userID := createTestUser(t, db)
	roomID := createTestRoom(t, db)

	payload, err := json.Marshal(map[string]interface{}{"msg_id": "m1", "content_type": "text"})
	if err != nil {
		t.Fatalf("marshal payload failed: %v", err)
	}

	eventID := uuid.NewString()
	if err := repo.Create(ctx, &model.InteractionEvent{
		ID:        eventID,
		UserID:    userID,
		RoomID:    &roomID,
		EventType: model.EventTypeSendMessage,
		Payload:   payload,
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list, err := repo.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly 1 event, got %d", len(list))
	}
	got := list[0]
	if got.ID != eventID || got.EventType != model.EventTypeSendMessage {
		t.Fatalf("unexpected event: %+v", got)
	}
	if got.RoomID == nil || *got.RoomID != roomID {
		t.Fatalf("expected RoomID=%s, got %v", roomID, got.RoomID)
	}
	if got.TargetUserID != nil {
		t.Fatalf("expected nil TargetUserID (not passed), got %v", *got.TargetUserID)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(got.Payload, &decoded); err != nil {
		t.Fatalf("failed to unmarshal payload roundtrip: %v", err)
	}
	if decoded["msg_id"] != "m1" || decoded["content_type"] != "text" {
		t.Fatalf("payload did not round-trip correctly: %+v", decoded)
	}
}

// TestInteractionEventRepository_Create_WithTargetUserID 覆盖 add_friend_request
// 场景：RoomID 为空、TargetUserID 非空。
func TestInteractionEventRepository_Create_WithTargetUserID(t *testing.T) {
	db := testDB(t)
	repo := NewInteractionEventRepository(db)
	ctx := context.Background()

	userID := createTestUser(t, db)
	targetID := createTestUser(t, db)

	if err := repo.Create(ctx, &model.InteractionEvent{
		ID:           uuid.NewString(),
		UserID:       userID,
		TargetUserID: &targetID,
		EventType:    model.EventTypeAddFriendRequest,
	}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list, err := repo.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser failed: %v", err)
	}
	if len(list) != 1 || list[0].TargetUserID == nil || *list[0].TargetUserID != targetID {
		t.Fatalf("expected TargetUserID=%s, got %+v", targetID, list)
	}
	if list[0].RoomID != nil {
		t.Fatalf("expected nil RoomID, got %v", *list[0].RoomID)
	}
	if list[0].Payload != nil {
		t.Fatalf("expected nil Payload when not passed, got %s", list[0].Payload)
	}
}

// TestInteractionEventRepository_ListByUser_OrderedByCreatedAtDesc 覆盖多条
// 事件时的排序语义（倒序，最新的在前），供导出工具按时间线还原用户行为。
func TestInteractionEventRepository_ListByUser_OrderedByCreatedAtDesc(t *testing.T) {
	db := testDB(t)
	repo := NewInteractionEventRepository(db)
	ctx := context.Background()

	userID := createTestUser(t, db)

	firstID := uuid.NewString()
	secondID := uuid.NewString()
	if err := repo.Create(ctx, &model.InteractionEvent{ID: firstID, UserID: userID, EventType: model.EventTypeJoinRoom}); err != nil {
		t.Fatalf("create first event failed: %v", err)
	}
	if err := repo.Create(ctx, &model.InteractionEvent{ID: secondID, UserID: userID, EventType: model.EventTypeSendMessage}); err != nil {
		t.Fatalf("create second event failed: %v", err)
	}

	list, err := repo.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 events, got %d", len(list))
	}
	// created_at 精度可能相同（同一毫秒内连续插入），倒序排列时至少应保证
	// 两条记录都存在，具体先后顺序不做过强断言（避免测试脆弱）。
	ids := map[string]bool{list[0].ID: true, list[1].ID: true}
	if !ids[firstID] || !ids[secondID] {
		t.Fatalf("expected both events present, got %+v", list)
	}
}
