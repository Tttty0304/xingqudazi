package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
)

func TestWatchTopicRepository_CreateListDelete(t *testing.T) {
	db := testDB(t)
	repo := NewWatchTopicRepository(db)
	ctx := context.Background()

	userID := createTestUser(t, db)

	topic := &model.WatchTopic{
		ID:       uuid.NewString(),
		UserID:   userID,
		Keywords: "摄影,徒步",
		Priority: 1,
	}
	if err := repo.Create(ctx, topic); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	list, err := repo.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser failed: %v", err)
	}
	if len(list) != 1 || list[0].Keywords != "摄影,徒步" {
		t.Fatalf("expected exactly 1 watch topic with matching keywords, got %+v", list)
	}

	// Delete 只允许本人删除自己的记录（T123），先用另一个合法格式但不是所有者
	// 的 UUID 验证不生效（user_id 列是 UUID 类型，传非 UUID 格式字符串会导致
	// Postgres 类型转换报错而非"找不到"，因此这里必须用合法格式的 UUID）。
	deleted, err := repo.Delete(ctx, topic.ID, uuid.NewString())
	if err != nil {
		t.Fatalf("Delete (wrong owner) failed: %v", err)
	}
	if deleted {
		t.Fatal("expected Delete to report false when userID does not match owner")
	}

	deleted, err = repo.Delete(ctx, topic.ID, userID)
	if err != nil {
		t.Fatalf("Delete (correct owner) failed: %v", err)
	}
	if !deleted {
		t.Fatal("expected Delete to report true for the actual owner")
	}

	list, err = repo.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListByUser after delete failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 watch topics after delete, got %d", len(list))
	}
}

// TestWatchTopicRepository_ListAll_ExcludesExpired 对应 Task20 AI 推荐候选生成
// 依赖的输入源过滤逻辑：已过期的关注事项不应出现在 ListAll 结果里。
func TestWatchTopicRepository_ListAll_ExcludesExpired(t *testing.T) {
	db := testDB(t)
	repo := NewWatchTopicRepository(db)
	ctx := context.Background()

	userID := createTestUser(t, db)

	activeID := uuid.NewString()
	if err := repo.Create(ctx, &model.WatchTopic{ID: activeID, UserID: userID, Keywords: "摄影"}); err != nil {
		t.Fatalf("Create active topic failed: %v", err)
	}

	expiredAt := time.Now().Add(-time.Hour)
	expiredID := uuid.NewString()
	if err := repo.Create(ctx, &model.WatchTopic{
		ID: expiredID, UserID: userID, Keywords: "已过期关键词", ExpiresAt: &expiredAt,
	}); err != nil {
		t.Fatalf("Create expired topic failed: %v", err)
	}

	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll failed: %v", err)
	}
	for _, topic := range all {
		if topic.ID == expiredID {
			t.Fatalf("expired watch topic %s should be excluded from ListAll, but was present", expiredID)
		}
	}
	found := false
	for _, topic := range all {
		if topic.ID == activeID {
			found = true
		}
	}
	if !found {
		t.Fatal("expected the non-expired watch topic to be present in ListAll")
	}
}
