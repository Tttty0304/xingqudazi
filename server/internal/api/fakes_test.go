package api

// 本文件集中定义 api 层 handler 测试所需的内存假实现（fake stores），
// 供各 handler 测试用真实 service 实例 + fake repository 依赖组合出完整的
// HTTP 请求/响应链路测试（能力补齐项：此前 internal/api 覆盖率仅 0.3%，
// 几乎完全依赖黑盒集成测试脚本兜底，HTTP 层的路由参数解析/错误码映射/
// 状态码选择等逻辑本身没有任何单测直接覆盖）。
//
// 与 repository 层测试的策略不同：这里选择 fake 而非连真实数据库，因为
// 这一层要测试的是"handler 如何把 service 返回的各种结果/错误正确翻译成
// HTTP 状态码和 JSON 结构"，跟 SQL 是否正确无关，fake 完全够用且更快、
// 更容易构造边界场景（如"数据库返回一个非预期的错误"）。

import (
	"context"
	"sync"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

// ---------- UserStore ----------

type fakeUserStore struct {
	mu       sync.Mutex
	byID     map[string]*model.User
	byName   map[string]*model.User
	createFn func(u *model.User) error
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{byID: map[string]*model.User{}, byName: map[string]*model.User{}}
}

func (f *fakeUserStore) Create(_ context.Context, u *model.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createFn != nil {
		if err := f.createFn(u); err != nil {
			return err
		}
	}
	if _, exists := f.byName[u.Username]; exists {
		return service.ErrUsernameTaken
	}
	copy := *u
	f.byID[u.ID] = &copy
	f.byName[u.Username] = &copy
	return nil
}

func (f *fakeUserStore) FindByUsername(_ context.Context, username string) (*model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byName[username]
	if !ok {
		return nil, service.ErrRepositoryUserNotFound
	}
	copy := *u
	return &copy, nil
}

func (f *fakeUserStore) FindByID(_ context.Context, id string) (*model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return nil, service.ErrRepositoryUserNotFound
	}
	copy := *u
	return &copy, nil
}

func (f *fakeUserStore) FindByIDs(_ context.Context, ids []string) ([]model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]model.User, 0, len(ids))
	for _, id := range ids {
		if u, ok := f.byID[id]; ok {
			result = append(result, *u)
		}
	}
	return result, nil
}

func (f *fakeUserStore) UpdateProfile(_ context.Context, userID, avatarURL, bio string) (*model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	user, ok := f.byID[userID]
	if !ok {
		return nil, service.ErrRepositoryUserNotFound
	}
	user.AvatarURL = avatarURL
	user.Bio = bio
	copy := *user
	return &copy, nil
}

func (f *fakeUserStore) put(u *model.User) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := *u
	f.byID[u.ID] = &copy
	f.byName[u.Username] = &copy
}

// ---------- FriendshipStore ----------

type fakeFriendshipStore struct {
	mu   sync.Mutex
	byID map[string]*model.Friendship
}

func newFakeFriendshipStore() *fakeFriendshipStore {
	return &fakeFriendshipStore{byID: map[string]*model.Friendship{}}
}

func (f *fakeFriendshipStore) Create(_ context.Context, fr *model.Friendship) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byID[fr.ID]; exists {
		return false, nil
	}
	copy := *fr
	f.byID[fr.ID] = &copy
	return true, nil
}

func (f *fakeFriendshipStore) FindByID(_ context.Context, id string) (*model.Friendship, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fr, ok := f.byID[id]
	if !ok {
		return nil, service.ErrRepositoryFriendshipNotFound
	}
	copy := *fr
	return &copy, nil
}

func (f *fakeFriendshipStore) FindBetween(_ context.Context, a, b string) (*model.Friendship, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fr := range f.byID {
		if (fr.RequesterID == a && fr.TargetID == b) || (fr.RequesterID == b && fr.TargetID == a) {
			copy := *fr
			return &copy, nil
		}
	}
	return nil, service.ErrRepositoryFriendshipNotFound
}

func (f *fakeFriendshipStore) UpdateStatus(_ context.Context, id, newStatus string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fr, ok := f.byID[id]
	if !ok {
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

func (f *fakeFriendshipStore) Delete(_ context.Context, operatorID, peerID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, fr := range f.byID {
		if fr.Status == "accepted" &&
			((fr.RequesterID == operatorID && fr.TargetID == peerID) || (fr.RequesterID == peerID && fr.TargetID == operatorID)) {
			delete(f.byID, id)
			return true, nil
		}
	}
	return false, nil
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

// ---------- PresenceChecker ----------

type fakePresenceChecker struct {
	online map[string]bool
	err    error
}

func (f *fakePresenceChecker) IsOnline(_ context.Context, userID string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.online[userID], nil
}

// ---------- FriendNotifier ----------

type fakeFriendNotifier struct {
	err error
}

func (f *fakeFriendNotifier) NotifyFriendRequestReceived(_ context.Context, _, _, _ string) error {
	return f.err
}

// ---------- RoomStore ----------

type fakeRoomStore struct {
	rooms map[string]*model.Room
}

func newFakeRoomStore(rooms ...model.Room) *fakeRoomStore {
	m := map[string]*model.Room{}
	for i := range rooms {
		r := rooms[i]
		m[r.ID] = &r
	}
	return &fakeRoomStore{rooms: m}
}

func (f *fakeRoomStore) List(_ context.Context) ([]model.Room, error) {
	result := make([]model.Room, 0, len(f.rooms))
	for _, r := range f.rooms {
		result = append(result, *r)
	}
	return result, nil
}

func (f *fakeRoomStore) FindByID(_ context.Context, id string) (*model.Room, error) {
	r, ok := f.rooms[id]
	if !ok {
		return nil, service.ErrRepositoryRoomNotFound
	}
	copy := *r
	return &copy, nil
}

func (f *fakeRoomStore) Create(_ context.Context, room *model.Room) error {
	copy := *room
	f.rooms[room.ID] = &copy
	return nil
}

// ---------- OnlineCounter ----------

type fakeOnlineCounter struct {
	counts map[string]int64
	err    error
}

func (f *fakeOnlineCounter) Count(_ context.Context, roomID string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.counts[roomID], nil
}

// ---------- MessageStore (群聊) ----------

type fakeMessageStore struct {
	messages []model.Message
	hasMore  bool
	err      error
}

func (f *fakeMessageStore) ListByRoom(_ context.Context, _ string, _, _ int) ([]model.Message, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	return f.messages, f.hasMore, nil
}

// ---------- WatchTopicStore ----------

type fakeWatchTopicStore struct {
	mu   sync.Mutex
	byID map[string]*model.WatchTopic
}

func newFakeWatchTopicStore() *fakeWatchTopicStore {
	return &fakeWatchTopicStore{byID: map[string]*model.WatchTopic{}}
}

func (f *fakeWatchTopicStore) Create(_ context.Context, t *model.WatchTopic) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := *t
	f.byID[t.ID] = &copy
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

// ---------- MatchCandidateStore ----------

type fakeMatchCandidateStore struct {
	mu   sync.Mutex
	byID map[string]*model.MatchCandidate
}

func newFakeMatchCandidateStore() *fakeMatchCandidateStore {
	return &fakeMatchCandidateStore{byID: map[string]*model.MatchCandidate{}}
}

func (f *fakeMatchCandidateStore) Create(_ context.Context, m *model.MatchCandidate) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.byID[m.ID]; exists {
		return false, nil
	}
	copy := *m
	f.byID[m.ID] = &copy
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
		return nil, service.ErrRepositoryCandidateNotFound
	}
	copy := *m
	return &copy, nil
}

func (f *fakeMatchCandidateStore) UpdateStatus(_ context.Context, id, newStatus string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.byID[id]
	if !ok {
		return false, nil
	}
	m.Status = newStatus
	return true, nil
}

func (f *fakeMatchCandidateStore) put(m *model.MatchCandidate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := *m
	f.byID[m.ID] = &copy
}

// ---------- WatchTopicLister ----------

type fakeWatchTopicLister struct {
	topics []model.WatchTopic
	err    error
}

func (f *fakeWatchTopicLister) ListAll(_ context.Context) ([]model.WatchTopic, error) {
	return f.topics, f.err
}

// ---------- FriendChecker ----------

type fakeFriendChecker struct {
	friends map[string]bool // key: "userA|userB" 已规范化排序
	err     error
}

func newFakeFriendChecker() *fakeFriendChecker {
	return &fakeFriendChecker{friends: map[string]bool{}}
}

func friendPairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}

func (f *fakeFriendChecker) IsFriend(_ context.Context, a, b string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.friends[friendPairKey(a, b)], nil
}

func (f *fakeFriendChecker) setFriends(a, b string) {
	f.friends[friendPairKey(a, b)] = true
}

// ---------- ConversationStore ----------

type fakeConversationStore struct {
	mu        sync.Mutex
	byID      map[string]*model.Conversation
	summaries map[string][]model.ConversationSummary
	err       error
}

func newFakeConversationStore() *fakeConversationStore {
	return &fakeConversationStore{byID: map[string]*model.Conversation{}, summaries: map[string][]model.ConversationSummary{}}
}

func (f *fakeConversationStore) GetOrCreate(_ context.Context, a, b string) (*model.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.byID {
		if (c.UserAID == a && c.UserBID == b) || (c.UserAID == b && c.UserBID == a) {
			copy := *c
			return &copy, nil
		}
	}
	c := &model.Conversation{ID: "conv-" + a + "-" + b, UserAID: a, UserBID: b}
	f.byID[c.ID] = c
	return c, nil
}

func (f *fakeConversationStore) FindByID(_ context.Context, id string) (*model.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok {
		return nil, service.ErrRepositoryConversationNotFound
	}
	copy := *c
	return &copy, nil
}

func (f *fakeConversationStore) ListByUser(_ context.Context, userID string) ([]model.ConversationSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.summaries[userID], nil
}

func (f *fakeConversationStore) put(c *model.Conversation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	copy := *c
	f.byID[c.ID] = &copy
}

// ---------- DirectMessageStore ----------

type fakeDirectMessageStore struct {
	messages map[string][]model.DirectMessage // key: conversationID
	hasMore  bool
	err      error
}

func newFakeDirectMessageStore() *fakeDirectMessageStore {
	return &fakeDirectMessageStore{messages: map[string][]model.DirectMessage{}}
}

func (f *fakeDirectMessageStore) Create(_ context.Context, msg *model.DirectMessage) (bool, error) {
	f.messages[msg.ConversationID] = append(f.messages[msg.ConversationID], *msg)
	return true, nil
}

func (f *fakeDirectMessageStore) ListByConversation(_ context.Context, conversationID string, _, _ int) ([]model.DirectMessage, bool, error) {
	if f.err != nil {
		return nil, false, f.err
	}
	return f.messages[conversationID], f.hasMore, nil
}

// ---------- PushSubscriptionStore ----------

type fakePushSubscriptionStore struct {
	mu   sync.Mutex
	subs map[string][]model.PushSubscription // key: userID
}

func newFakePushSubscriptionStore() *fakePushSubscriptionStore {
	return &fakePushSubscriptionStore{subs: map[string][]model.PushSubscription{}}
}

func (f *fakePushSubscriptionStore) Create(_ context.Context, s *model.PushSubscription) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.subs[s.UserID] = append(f.subs[s.UserID], *s)
	return nil
}

func (f *fakePushSubscriptionStore) DeleteByEndpoint(_ context.Context, userID, endpoint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.subs[userID]
	for i, s := range list {
		if s.Endpoint == endpoint {
			f.subs[userID] = append(list[:i], list[i+1:]...)
			return nil
		}
	}
	return nil
}

func (f *fakePushSubscriptionStore) ListByUser(_ context.Context, userID string) ([]model.PushSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subs[userID], nil
}

func (f *fakePushSubscriptionStore) DeleteByID(_ context.Context, id string) error {
	return nil
}

// ---------- ReportStore ----------

type fakeReportStore struct {
	mu       sync.Mutex
	byKey    map[string]*model.Report // key: reporterID+targetType+targetID
	createFn func(r *model.Report) error
}

func newFakeReportStore() *fakeReportStore {
	return &fakeReportStore{byKey: map[string]*model.Report{}}
}

func reportKey(reporterID, targetType, targetID string) string {
	return reporterID + "|" + targetType + "|" + targetID
}

func (f *fakeReportStore) Create(_ context.Context, r *model.Report) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createFn != nil {
		if err := f.createFn(r); err != nil {
			return err
		}
	}
	copy := *r
	f.byKey[reportKey(r.ReporterID, r.TargetType, r.TargetID)] = &copy
	return nil
}

func (f *fakeReportStore) FindExisting(_ context.Context, reporterID, targetType, targetID string) (*model.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byKey[reportKey(reporterID, targetType, targetID)]
	if !ok {
		return nil, nil
	}
	copy := *r
	return &copy, nil
}

// ---------- MessageExistenceChecker ----------

type fakeExistenceChecker struct {
	existing map[string]bool
}

func newFakeExistenceChecker(ids ...string) *fakeExistenceChecker {
	m := map[string]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return &fakeExistenceChecker{existing: m}
}

func (f *fakeExistenceChecker) Exists(_ context.Context, id string) (bool, error) {
	return f.existing[id], nil
}

// ---------- MediaStore ----------

type fakeMediaStore struct {
	mu     sync.Mutex
	assets []model.MediaAsset
}

func newFakeMediaStore() *fakeMediaStore {
	return &fakeMediaStore{}
}

func (f *fakeMediaStore) Create(_ context.Context, m *model.MediaAsset) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assets = append(f.assets, *m)
	return nil
}
