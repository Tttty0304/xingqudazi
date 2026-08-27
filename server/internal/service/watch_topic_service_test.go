package service

import (
	"context"
	"sync"
	"testing"

	"xingqudazi-im/server/internal/model"
)

// fakeWatchTopicStore 是 WatchTopicStore 的内存假实现，仅用于单测。
type fakeWatchTopicStore struct {
	mu   sync.Mutex
	byID map[string]*model.WatchTopic
}

func newFakeWatchTopicStore() *fakeWatchTopicStore {
	return &fakeWatchTopicStore{byID: make(map[string]*model.WatchTopic)}
}

func (f *fakeWatchTopicStore) Create(_ context.Context, t *model.WatchTopic) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copyT := *t
	f.byID[t.ID] = &copyT
	return nil
}

func (f *fakeWatchTopicStore) ListByUser(_ context.Context, userID string) ([]model.WatchTopic, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []model.WatchTopic
	for _, t := range f.byID {
		if t.UserID == userID {
			result = append(result, *t)
		}
	}
	return result, nil
}

func (f *fakeWatchTopicStore) Delete(_ context.Context, id, userID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.byID[id]
	if !ok || t.UserID != userID {
		return false, nil
	}
	delete(f.byID, id)
	return true, nil
}

// TestWatchTopicService_CreateAndList_T94_T95 对应 T94/T95：创建关注事项 + 列出当前用户全部关注事项。
func TestWatchTopicService_CreateAndList_T94_T95(t *testing.T) {
	store := newFakeWatchTopicStore()
	svc := NewWatchTopicService(store)

	topic, err := svc.CreateWatchTopic(context.Background(), "alice", "", "数码,摄影", 5, nil)
	if err != nil {
		t.Fatalf("CreateWatchTopic failed: %v", err)
	}
	if topic.ID == "" {
		t.Fatal("expected non-empty topic id")
	}

	topics, err := svc.ListWatchTopics(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ListWatchTopics failed: %v", err)
	}
	if len(topics) != 1 || topics[0].Keywords != "数码,摄影" {
		t.Fatalf("expected 1 topic with keywords, got %+v", topics)
	}
}

// TestWatchTopicService_CreateEmptyKeywords_Invalid 校验空关键词被拒绝。
func TestWatchTopicService_CreateEmptyKeywords_Invalid(t *testing.T) {
	store := newFakeWatchTopicStore()
	svc := NewWatchTopicService(store)

	_, err := svc.CreateWatchTopic(context.Background(), "alice", "", "", 0, nil)
	if err != ErrInvalidWatchTopic {
		t.Fatalf("expected ErrInvalidWatchTopic, got %v", err)
	}
}

// TestWatchTopicService_DeleteWatchTopic_T123 对应 T123（本轮新增）：删除关注事项，
// 补齐此前"只能创建不能删除"的半成品问题；非本人所有时返回 not found。
func TestWatchTopicService_DeleteWatchTopic_T123(t *testing.T) {
	store := newFakeWatchTopicStore()
	svc := NewWatchTopicService(store)

	topic, _ := svc.CreateWatchTopic(context.Background(), "alice", "", "数码", 1, nil)

	// 非本人删除：视为不存在。
	if err := svc.DeleteWatchTopic(context.Background(), topic.ID, "bob"); err != ErrWatchTopicNotFound {
		t.Fatalf("expected ErrWatchTopicNotFound for non-owner, got %v", err)
	}

	// 本人删除：成功。
	if err := svc.DeleteWatchTopic(context.Background(), topic.ID, "alice"); err != nil {
		t.Fatalf("DeleteWatchTopic failed: %v", err)
	}

	topics, _ := svc.ListWatchTopics(context.Background(), "alice")
	if len(topics) != 0 {
		t.Fatalf("expected 0 topics after delete, got %+v", topics)
	}

	// 重复删除：not found。
	if err := svc.DeleteWatchTopic(context.Background(), topic.ID, "alice"); err != ErrWatchTopicNotFound {
		t.Fatalf("expected ErrWatchTopicNotFound on repeat delete, got %v", err)
	}
}
