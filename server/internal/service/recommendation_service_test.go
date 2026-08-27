package service

import (
	"context"
	"sync"
	"testing"

	"xingqudazi-im/server/internal/model"
)

// fakeMatchCandidateStore 是 MatchCandidateStore 的内存假实现，仅用于单测。
type fakeMatchCandidateStore struct {
	mu   sync.Mutex
	byID map[string]*model.MatchCandidate
	keys map[string]bool // "userA|userB|roomID" 唯一性模拟，与真实表的 UNIQUE 约束对应
}

func newFakeMatchCandidateStore() *fakeMatchCandidateStore {
	return &fakeMatchCandidateStore{byID: make(map[string]*model.MatchCandidate), keys: make(map[string]bool)}
}

func (f *fakeMatchCandidateStore) Create(_ context.Context, m *model.MatchCandidate) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := m.UserAID + "|" + m.UserBID + "|" + m.RoomID
	if f.keys[key] {
		return false, nil
	}
	f.keys[key] = true
	copyM := *m
	f.byID[m.ID] = &copyM
	return true, nil
}

func (f *fakeMatchCandidateStore) ListPendingByUser(_ context.Context, userID string) ([]model.MatchCandidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []model.MatchCandidate
	for _, m := range f.byID {
		if m.Status == "pending_review" && (m.UserAID == userID || m.UserBID == userID) {
			result = append(result, *m)
		}
	}
	return result, nil
}

func (f *fakeMatchCandidateStore) FindByID(_ context.Context, id string) (*model.MatchCandidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byID[id]
	if !ok {
		return nil, ErrRepositoryCandidateNotFound
	}
	copyM := *m
	return &copyM, nil
}

func (f *fakeMatchCandidateStore) UpdateStatus(_ context.Context, id, newStatus string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byID[id]
	if !ok || m.Status != "pending_review" {
		return false, nil
	}
	m.Status = newStatus
	return true, nil
}

// fakeWatchTopicLister 是 WatchTopicLister 的内存假实现。
type fakeWatchTopicLister struct {
	topics []model.WatchTopic
}

func (f *fakeWatchTopicLister) ListAll(_ context.Context) ([]model.WatchTopic, error) {
	return f.topics, nil
}

// fakeFriendChecker（复用 conversation_service_test.go 中已定义的同名类型/构造函数
// newFakeFriendChecker()/setFriend()，二者结构完全一致，Task20 与 Task15 共用同一份
// 测试假实现，不重复声明）。

func newTestRecommendationService(users *fakeUserStore, topics *fakeWatchTopicLister, friends *fakeFriendChecker) (*RecommendationService, *fakeMatchCandidateStore) {
	store := newFakeMatchCandidateStore()
	return NewRecommendationService(store, topics, friends, users), store
}

// TestMatchTopics 覆盖 Task20 规则化匹配的纯逻辑：关键词重合 + 共同房间信号。
func TestMatchTopics(t *testing.T) {
	t.Run("no_overlap", func(t *testing.T) {
		a := []model.WatchTopic{{Keywords: "数码,摄影"}}
		b := []model.WatchTopic{{Keywords: "美食,旅行"}}
		shared, room := matchTopics(a, b)
		if len(shared) != 0 || room != "" {
			t.Errorf("expected no overlap, got shared=%v room=%q", shared, room)
		}
	})

	t.Run("keyword_overlap_case_insensitive", func(t *testing.T) {
		a := []model.WatchTopic{{Keywords: "数码,Gaming"}}
		b := []model.WatchTopic{{Keywords: "摄影,gaming"}}
		shared, _ := matchTopics(a, b)
		if len(shared) != 1 || shared[0] != "gaming" {
			t.Errorf("expected shared=[gaming], got %v", shared)
		}
	})

	t.Run("shared_room_signal", func(t *testing.T) {
		a := []model.WatchTopic{{Keywords: "数码", RoomID: "room-1"}}
		b := []model.WatchTopic{{Keywords: "美食", RoomID: "room-1"}}
		shared, room := matchTopics(a, b)
		if len(shared) != 0 {
			t.Errorf("expected no keyword overlap, got %v", shared)
		}
		if room != "room-1" {
			t.Errorf("expected shared room-1, got %q", room)
		}
	})
}

// TestRecommendationService_GenerateCandidates 对应 T110：跨用户扫描关注事项生成候选。
func TestRecommendationService_GenerateCandidates(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	users.Create(context.Background(), &model.User{ID: "carol", Username: "carol"})

	topics := &fakeWatchTopicLister{topics: []model.WatchTopic{
		{UserID: "alice", Keywords: "数码,摄影"},
		{UserID: "bob", Keywords: "摄影,旅行"},
		{UserID: "carol", Keywords: "美食"}, // 与 alice/bob 均无重合
	}}
	friends := newFakeFriendChecker()
	svc, store := newTestRecommendationService(users, topics, friends)

	created, err := svc.GenerateCandidates(context.Background())
	if err != nil {
		t.Fatalf("GenerateCandidates failed: %v", err)
	}
	if created != 1 {
		t.Fatalf("expected 1 candidate created (alice/bob share 摄影), got %d", created)
	}
	if len(store.byID) != 1 {
		t.Fatalf("expected 1 candidate stored, got %d", len(store.byID))
	}

	// 重复调用应保持幂等：不产生新的重复候选（表 UNIQUE 约束等价语义）。
	createdAgain, err := svc.GenerateCandidates(context.Background())
	if err != nil {
		t.Fatalf("second GenerateCandidates failed: %v", err)
	}
	if createdAgain != 0 {
		t.Errorf("expected second run to create 0 new candidates (idempotent), got %d", createdAgain)
	}
}

// TestRecommendationService_GenerateCandidates_ExcludesFriends 覆盖"已是好友不生成候选"规则。
func TestRecommendationService_GenerateCandidates_ExcludesFriends(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})

	topics := &fakeWatchTopicLister{topics: []model.WatchTopic{
		{UserID: "alice", Keywords: "数码"},
		{UserID: "bob", Keywords: "数码"},
	}}
	friends := newFakeFriendChecker()
	friends.setFriend("alice", "bob")
	svc, store := newTestRecommendationService(users, topics, friends)

	created, err := svc.GenerateCandidates(context.Background())
	if err != nil {
		t.Fatalf("GenerateCandidates failed: %v", err)
	}
	if created != 0 || len(store.byID) != 0 {
		t.Fatalf("expected already-friends pair to be excluded, got created=%d stored=%d", created, len(store.byID))
	}
}

// TestRecommendationService_ListAndRespond 对应 T111/T112：查询候选列表 + 确认/忽略。
func TestRecommendationService_ListAndRespond(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})

	topics := &fakeWatchTopicLister{topics: []model.WatchTopic{
		{UserID: "alice", Keywords: "数码"},
		{UserID: "bob", Keywords: "数码"},
	}}
	svc, _ := newTestRecommendationService(users, topics, newFakeFriendChecker())

	if _, err := svc.GenerateCandidates(context.Background()); err != nil {
		t.Fatalf("GenerateCandidates failed: %v", err)
	}

	candidates, err := svc.ListCandidates(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ListCandidates failed: %v", err)
	}
	if len(candidates) != 1 || candidates[0].PeerUsername != "bob" {
		t.Fatalf("expected alice to see bob as candidate, got %+v", candidates)
	}
	candidateID := candidates[0].CandidateID

	// 非候选双方之一操作应被拒绝。
	if err := svc.RespondCandidate(context.Background(), candidateID, "eve", "confirm"); err != ErrForbiddenCandidateRespond {
		t.Fatalf("expected ErrForbiddenCandidateRespond, got %v", err)
	}

	if err := svc.RespondCandidate(context.Background(), candidateID, "alice", "confirm"); err != nil {
		t.Fatalf("RespondCandidate(confirm) failed: %v", err)
	}

	// 已确认后不应再出现在待确认列表中。
	remaining, err := svc.ListCandidates(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ListCandidates after confirm failed: %v", err)
	}
	if len(remaining) != 0 {
		t.Errorf("expected 0 pending candidates after confirm, got %d", len(remaining))
	}

	// 重复响应已处理过的候选应报错。
	if err := svc.RespondCandidate(context.Background(), candidateID, "bob", "dismiss"); err != ErrCandidateAlreadyResolved {
		t.Fatalf("expected ErrCandidateAlreadyResolved, got %v", err)
	}
}

// TestRecommendationService_RespondCandidate_NotFound 覆盖候选不存在的场景。
func TestRecommendationService_RespondCandidate_NotFound(t *testing.T) {
	users := newFakeUserStore()
	svc, _ := newTestRecommendationService(users, &fakeWatchTopicLister{}, newFakeFriendChecker())

	if err := svc.RespondCandidate(context.Background(), "no-such-id", "alice", "confirm"); err != ErrCandidateNotFound {
		t.Fatalf("expected ErrCandidateNotFound, got %v", err)
	}
}

// TestRecommendationService_RespondCandidate_InvalidAction 覆盖非法 action 参数。
func TestRecommendationService_RespondCandidate_InvalidAction(t *testing.T) {
	users := newFakeUserStore()
	svc, _ := newTestRecommendationService(users, &fakeWatchTopicLister{}, newFakeFriendChecker())

	if err := svc.RespondCandidate(context.Background(), "any-id", "alice", "maybe"); err != ErrInvalidCandidateAction {
		t.Fatalf("expected ErrInvalidCandidateAction, got %v", err)
	}
}
