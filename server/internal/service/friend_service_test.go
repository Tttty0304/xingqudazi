package service

import (
	"context"
	"sync"
	"testing"

	"xingqudazi-im/server/internal/model"
)

// fakeFriendshipStore 是 FriendshipStore 的内存假实现，仅用于单测。
type fakeFriendshipStore struct {
	mu   sync.Mutex
	byID map[string]*model.Friendship
}

func newFakeFriendshipStore() *fakeFriendshipStore {
	return &fakeFriendshipStore{byID: make(map[string]*model.Friendship)}
}

func (f *fakeFriendshipStore) Create(_ context.Context, fr *model.Friendship) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.byID {
		if existing.RequesterID == fr.RequesterID && existing.TargetID == fr.TargetID {
			return false, nil // 唯一约束命中
		}
	}
	copyFr := *fr
	f.byID[fr.ID] = &copyFr
	return true, nil
}

func (f *fakeFriendshipStore) FindByID(_ context.Context, id string) (*model.Friendship, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fr, ok := f.byID[id]
	if !ok {
		return nil, ErrRepositoryFriendshipNotFound
	}
	copyFr := *fr
	return &copyFr, nil
}

func (f *fakeFriendshipStore) FindBetween(_ context.Context, userA, userB string) (*model.Friendship, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fr := range f.byID {
		if (fr.RequesterID == userA && fr.TargetID == userB) || (fr.RequesterID == userB && fr.TargetID == userA) {
			copyFr := *fr
			return &copyFr, nil
		}
	}
	return nil, ErrRepositoryFriendshipNotFound
}

func (f *fakeFriendshipStore) UpdateStatus(_ context.Context, id, newStatus string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fr, ok := f.byID[id]
	if !ok || fr.Status != "pending" {
		return false, nil
	}
	fr.Status = newStatus
	return true, nil
}

func (f *fakeFriendshipStore) ListAcceptedByUser(_ context.Context, userID string) ([]model.Friendship, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []model.Friendship
	for _, fr := range f.byID {
		if fr.Status == "accepted" && (fr.RequesterID == userID || fr.TargetID == userID) {
			result = append(result, *fr)
		}
	}
	return result, nil
}

func (f *fakeFriendshipStore) ListPendingByUser(_ context.Context, userID string) ([]model.Friendship, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []model.Friendship
	for _, fr := range f.byID {
		if fr.Status == "pending" && (fr.RequesterID == userID || fr.TargetID == userID) {
			result = append(result, *fr)
		}
	}
	return result, nil
}

func (f *fakeFriendshipStore) Delete(_ context.Context, operatorID, peerID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, fr := range f.byID {
		if fr.Status == "accepted" && ((fr.RequesterID == operatorID && fr.TargetID == peerID) || (fr.RequesterID == peerID && fr.TargetID == operatorID)) {
			delete(f.byID, id)
			return true, nil
		}
	}
	return false, nil
}

// fakePresenceChecker 是 PresenceChecker 的内存假实现。
type fakePresenceChecker struct {
	online map[string]bool
}

func (f *fakePresenceChecker) IsOnline(_ context.Context, userID string) (bool, error) {
	return f.online[userID], nil
}

// fakeFriendNotifier 记录被通知过的目标用户，供断言使用。
type fakeFriendNotifier struct {
	notified []string
	failWith error
}

func (f *fakeFriendNotifier) NotifyFriendRequestReceived(_ context.Context, targetUserID, _, _ string) error {
	f.notified = append(f.notified, targetUserID)
	return f.failWith
}

func newTestFriendService(users *fakeUserStore, presence *fakePresenceChecker, notifier *fakeFriendNotifier) (*FriendService, *fakeFriendshipStore) {
	store := newFakeFriendshipStore()
	return NewFriendService(store, users, presence, notifier), store
}

// TestFriendService_SendRequest_T60 对应 T60：发起好友请求，对方收到 WS 通知。
func TestFriendService_SendRequest_T60(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	notifier := &fakeFriendNotifier{}
	svc, _ := newTestFriendService(users, &fakePresenceChecker{online: map[string]bool{}}, notifier)

	fr, err := svc.SendRequest(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}
	if fr.Status != "pending" {
		t.Errorf("expected status=pending, got %s", fr.Status)
	}
	if len(notifier.notified) != 1 || notifier.notified[0] != "bob" {
		t.Errorf("expected bob to be notified, got %v", notifier.notified)
	}
}

// TestFriendService_SendRequest_TargetNotFound 对应 Part2 矩阵：对方不存在 404。
func TestFriendService_SendRequest_TargetNotFound(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	svc, _ := newTestFriendService(users, &fakePresenceChecker{}, &fakeFriendNotifier{})

	_, err := svc.SendRequest(context.Background(), "alice", "no-such-user")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// TestFriendService_SendRequest_Self 校验不能对自己发起好友请求。
func TestFriendService_SendRequest_Self(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	svc, _ := newTestFriendService(users, &fakePresenceChecker{}, &fakeFriendNotifier{})

	_, err := svc.SendRequest(context.Background(), "alice", "alice")
	if err != ErrCannotFriendSelf {
		t.Fatalf("expected ErrCannotFriendSelf, got %v", err)
	}
}

// TestFriendService_SendRequest_DuplicatePending 对应 T60 边界：重复发起同一好友请求幂等/拒绝。
func TestFriendService_SendRequest_DuplicatePending(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	svc, _ := newTestFriendService(users, &fakePresenceChecker{}, &fakeFriendNotifier{})

	if _, err := svc.SendRequest(context.Background(), "alice", "bob"); err != nil {
		t.Fatalf("first SendRequest failed: %v", err)
	}
	_, err := svc.SendRequest(context.Background(), "alice", "bob")
	if err != ErrFriendRequestExists {
		t.Fatalf("expected ErrFriendRequestExists, got %v", err)
	}
}

// fakeEventRecorder 是 EventRecorder 的内存假实现（能力补齐项：给"未来投喂给
// 模型训练用户替身"补最基础的行为原始数据），供测试断言 SendRequest 在正确
// 时机记录了行为事件。
type fakeEventRecorder struct {
	mu      sync.Mutex
	created []model.InteractionEvent
}

func newFakeEventRecorder() *fakeEventRecorder {
	return &fakeEventRecorder{}
}

func (f *fakeEventRecorder) Create(_ context.Context, e *model.InteractionEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, *e)
	return nil
}

// TestFriendService_SendRequest_RecordsInteractionEvent 覆盖能力补齐项：发起
// 好友请求成功后应记一条 add_friend_request 事件，UserID/TargetUserID 均正确。
func TestFriendService_SendRequest_RecordsInteractionEvent(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	svc, _ := newTestFriendService(users, &fakePresenceChecker{}, &fakeFriendNotifier{})
	recorder := newFakeEventRecorder()
	svc.SetEventRecorder(recorder)

	if _, err := svc.SendRequest(context.Background(), "alice", "bob"); err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.created) != 1 {
		t.Fatalf("expected exactly 1 event, got %d: %+v", len(recorder.created), recorder.created)
	}
	got := recorder.created[0]
	if got.EventType != model.EventTypeAddFriendRequest || got.UserID != "alice" {
		t.Fatalf("unexpected event: %+v", got)
	}
	if got.TargetUserID == nil || *got.TargetUserID != "bob" {
		t.Fatalf("expected TargetUserID=bob, got %v", got.TargetUserID)
	}
}

// TestFriendService_SendRequest_DuplicatePending_DoesNotRecordEvent 覆盖边界：
// 重复发起（未真正创建成功）不应产生第二条事件，避免训练数据混入噪音。
func TestFriendService_SendRequest_DuplicatePending_DoesNotRecordEvent(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	svc, _ := newTestFriendService(users, &fakePresenceChecker{}, &fakeFriendNotifier{})
	recorder := newFakeEventRecorder()
	svc.SetEventRecorder(recorder)

	if _, err := svc.SendRequest(context.Background(), "alice", "bob"); err != nil {
		t.Fatalf("first SendRequest failed: %v", err)
	}
	if _, err := svc.SendRequest(context.Background(), "alice", "bob"); err != ErrFriendRequestExists {
		t.Fatalf("expected ErrFriendRequestExists, got %v", err)
	}

	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if len(recorder.created) != 1 {
		t.Fatalf("expected exactly 1 event (dedup on duplicate request), got %d: %+v", len(recorder.created), recorder.created)
	}
}

// TestFriendService_RespondRequest_Accept_T61 对应 T61：接受好友请求。
func TestFriendService_RespondRequest_Accept_T61(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	svc, _ := newTestFriendService(users, &fakePresenceChecker{}, &fakeFriendNotifier{})

	fr, err := svc.SendRequest(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	resolved, err := svc.RespondRequest(context.Background(), fr.ID, "bob", "accept")
	if err != nil {
		t.Fatalf("RespondRequest failed: %v", err)
	}
	if resolved.Status != "accepted" {
		t.Errorf("expected status=accepted, got %s", resolved.Status)
	}
}

// TestFriendService_RespondRequest_Forbidden 对应 T51 同款越权校验：非接收方不能操作。
func TestFriendService_RespondRequest_Forbidden(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	svc, _ := newTestFriendService(users, &fakePresenceChecker{}, &fakeFriendNotifier{})

	fr, _ := svc.SendRequest(context.Background(), "alice", "bob")

	_, err := svc.RespondRequest(context.Background(), fr.ID, "eve", "accept")
	if err != ErrForbiddenFriendResponse {
		t.Fatalf("expected ErrForbiddenFriendResponse, got %v", err)
	}
}

// TestFriendService_RespondRequest_AlreadyResolved_T62 对应 T62：重复操作已处理过的请求。
func TestFriendService_RespondRequest_AlreadyResolved_T62(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	svc, _ := newTestFriendService(users, &fakePresenceChecker{}, &fakeFriendNotifier{})

	fr, _ := svc.SendRequest(context.Background(), "alice", "bob")
	if _, err := svc.RespondRequest(context.Background(), fr.ID, "bob", "accept"); err != nil {
		t.Fatalf("first respond failed: %v", err)
	}

	_, err := svc.RespondRequest(context.Background(), fr.ID, "bob", "accept")
	if err != ErrFriendRequestResolved {
		t.Fatalf("expected ErrFriendRequestResolved, got %v", err)
	}
}

// TestFriendService_ListFriends_T63 对应 T63：好友列表 + 实时在线态。
func TestFriendService_ListFriends_T63(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	presence := &fakePresenceChecker{online: map[string]bool{"bob": true}}
	svc, _ := newTestFriendService(users, presence, &fakeFriendNotifier{})

	fr, _ := svc.SendRequest(context.Background(), "alice", "bob")
	if _, err := svc.RespondRequest(context.Background(), fr.ID, "bob", "accept"); err != nil {
		t.Fatalf("respond failed: %v", err)
	}

	friends, err := svc.ListFriends(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ListFriends failed: %v", err)
	}
	if len(friends) != 1 {
		t.Fatalf("expected 1 friend, got %d", len(friends))
	}
	if friends[0].UserID != "bob" || !friends[0].Online {
		t.Errorf("expected bob online=true, got %+v", friends[0])
	}

	// 双向可见：bob 视角也应看到 alice。
	friendsOfBob, err := svc.ListFriends(context.Background(), "bob")
	if err != nil {
		t.Fatalf("ListFriends(bob) failed: %v", err)
	}
	if len(friendsOfBob) != 1 || friendsOfBob[0].UserID != "alice" {
		t.Errorf("expected bob to see alice as friend, got %+v", friendsOfBob)
	}
}

// TestFriendService_ListPendingRequests_T120 对应 T120（本轮新增）：好友请求可事后
// 查看（不再依赖必须在线才能收到 WS 通知），且能区分收到/发出的方向。
func TestFriendService_ListPendingRequests_T120(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob", Username: "bob"})
	svc, _ := newTestFriendService(users, &fakePresenceChecker{}, &fakeFriendNotifier{})

	if _, err := svc.SendRequest(context.Background(), "alice", "bob"); err != nil {
		t.Fatalf("SendRequest failed: %v", err)
	}

	// bob 视角：一条 incoming 请求，来自 alice。
	pendingForBob, err := svc.ListPendingRequests(context.Background(), "bob")
	if err != nil {
		t.Fatalf("ListPendingRequests(bob) failed: %v", err)
	}
	if len(pendingForBob) != 1 || pendingForBob[0].Direction != "incoming" || pendingForBob[0].PeerUsername != "alice" {
		t.Fatalf("expected 1 incoming request from alice, got %+v", pendingForBob)
	}

	// alice 视角：一条 outgoing 请求，目标是 bob。
	pendingForAlice, err := svc.ListPendingRequests(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ListPendingRequests(alice) failed: %v", err)
	}
	if len(pendingForAlice) != 1 || pendingForAlice[0].Direction != "outgoing" || pendingForAlice[0].PeerUsername != "bob" {
		t.Fatalf("expected 1 outgoing request to bob, got %+v", pendingForAlice)
	}

	// 请求被接受后不再出现在待处理列表中。
	if _, err := svc.RespondRequest(context.Background(), pendingForBob[0].RequestID, "bob", "accept"); err != nil {
		t.Fatalf("RespondRequest failed: %v", err)
	}
	pendingForBobAfter, err := svc.ListPendingRequests(context.Background(), "bob")
	if err != nil {
		t.Fatalf("ListPendingRequests(bob) after accept failed: %v", err)
	}
	if len(pendingForBobAfter) != 0 {
		t.Fatalf("expected 0 pending requests after accept, got %+v", pendingForBobAfter)
	}
}
