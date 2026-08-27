package service

import (
	"context"
	"sync"
	"testing"

	"xingqudazi-im/server/internal/model"
)

// fakeConversationStore 是 ConversationStore 的内存假实现，仅用于单测。
type fakeConversationStore struct {
	mu    sync.Mutex
	byID  map[string]*model.Conversation
	seqID int
}

func newFakeConversationStore() *fakeConversationStore {
	return &fakeConversationStore{byID: make(map[string]*model.Conversation)}
}

func (f *fakeConversationStore) GetOrCreate(_ context.Context, userA, userB string) (*model.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.byID {
		if (c.UserAID == userA && c.UserBID == userB) || (c.UserAID == userB && c.UserBID == userA) {
			copyC := *c
			return &copyC, nil
		}
	}
	f.seqID++
	c := &model.Conversation{ID: "conv-" + itoa(f.seqID), UserAID: userA, UserBID: userB}
	f.byID[c.ID] = c
	copyC := *c
	return &copyC, nil
}

func (f *fakeConversationStore) FindByID(_ context.Context, id string) (*model.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byID[id]
	if !ok {
		return nil, ErrRepositoryConversationNotFound
	}
	copyC := *c
	return &copyC, nil
}

func (f *fakeConversationStore) ListByUser(_ context.Context, userID string) ([]model.ConversationSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []model.ConversationSummary
	for _, c := range f.byID {
		if c.UserAID == userID || c.UserBID == userID {
			peer := c.UserBID
			if c.UserAID != userID {
				peer = c.UserAID
			}
			result = append(result, model.ConversationSummary{ConversationID: c.ID, PeerID: peer})
		}
	}
	return result, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// fakeDirectMessageStore 是 DirectMessageStore 的内存假实现。
type fakeDirectMessageStore struct {
	mu       sync.Mutex
	messages []model.DirectMessage
	msgIDSet map[string]bool // conversationID+msgID -> exists，用于幂等判定
}

func newFakeDirectMessageStore() *fakeDirectMessageStore {
	return &fakeDirectMessageStore{msgIDSet: make(map[string]bool)}
}

func (f *fakeDirectMessageStore) Create(_ context.Context, msg *model.DirectMessage) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := msg.ConversationID + ":" + msg.MsgID
	if f.msgIDSet[key] {
		return false, nil
	}
	f.msgIDSet[key] = true
	f.messages = append(f.messages, *msg)
	return true, nil
}

func (f *fakeDirectMessageStore) ListByConversation(_ context.Context, conversationID string, _, _ int) ([]model.DirectMessage, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result []model.DirectMessage
	for _, m := range f.messages {
		if m.ConversationID == conversationID {
			result = append(result, m)
		}
	}
	return result, false, nil
}

// fakeFriendChecker 是 FriendChecker 的内存假实现。
type fakeFriendChecker struct {
	friends map[string]bool // "userA:userB"（已规范化排序）-> true
}

func newFakeFriendChecker() *fakeFriendChecker {
	return &fakeFriendChecker{friends: make(map[string]bool)}
}

func (f *fakeFriendChecker) setFriend(a, b string) {
	f.friends[friendKey(a, b)] = true
}

func friendKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + ":" + b
}

func (f *fakeFriendChecker) IsFriend(_ context.Context, userA, userB string) (bool, error) {
	return f.friends[friendKey(userA, userB)], nil
}

func newTestConversationService() (*ConversationService, *fakeConversationStore, *fakeDirectMessageStore, *fakeFriendChecker) {
	convStore := newFakeConversationStore()
	dmStore := newFakeDirectMessageStore()
	friends := newFakeFriendChecker()
	return NewConversationService(convStore, dmStore, friends), convStore, dmStore, friends
}

// TestConversationService_SendDirectMessage_T70 对应 T70：好友之间私聊，惰性创建会话并落库。
func TestConversationService_SendDirectMessage_T70(t *testing.T) {
	svc, _, dmStore, friends := newTestConversationService()
	friends.setFriend("alice", "bob")

	convID, inserted, err := svc.SendDirectMessage(context.Background(), "alice", "bob", "msg-1", "hello bob", "text")
	if err != nil {
		t.Fatalf("SendDirectMessage failed: %v", err)
	}
	if !inserted {
		t.Fatal("expected inserted=true for first message")
	}
	if convID == "" {
		t.Fatal("expected non-empty conversation id")
	}
	if len(dmStore.messages) != 1 || dmStore.messages[0].Content != "hello bob" {
		t.Errorf("expected message persisted, got %+v", dmStore.messages)
	}
}

// TestConversationService_SendDirectMessage_T72_FriendRequired 对应 T72（已确认口径：
// 仅好友可私聊），非好友发起私聊被拒绝。
func TestConversationService_SendDirectMessage_T72_FriendRequired(t *testing.T) {
	svc, _, _, _ := newTestConversationService()

	_, _, err := svc.SendDirectMessage(context.Background(), "alice", "stranger", "msg-1", "hi", "text")
	if err != ErrFriendRequiredForDirectMessage {
		t.Fatalf("expected ErrFriendRequiredForDirectMessage, got %v", err)
	}
}

// TestConversationService_SendDirectMessage_Self 校验不能给自己发私聊。
func TestConversationService_SendDirectMessage_Self(t *testing.T) {
	svc, _, _, friends := newTestConversationService()
	friends.setFriend("alice", "alice")

	_, _, err := svc.SendDirectMessage(context.Background(), "alice", "alice", "msg-1", "hi", "text")
	if err != ErrCannotMessageSelf {
		t.Fatalf("expected ErrCannotMessageSelf, got %v", err)
	}
}

// TestConversationService_SendDirectMessage_DuplicateMsgID 对应群聊 T34 同款幂等去重设计。
func TestConversationService_SendDirectMessage_DuplicateMsgID(t *testing.T) {
	svc, _, dmStore, friends := newTestConversationService()
	friends.setFriend("alice", "bob")

	if _, inserted, err := svc.SendDirectMessage(context.Background(), "alice", "bob", "dup-1", "first", "text"); err != nil || !inserted {
		t.Fatalf("first send failed: inserted=%v err=%v", inserted, err)
	}
	_, inserted, err := svc.SendDirectMessage(context.Background(), "alice", "bob", "dup-1", "first", "text")
	if err != nil {
		t.Fatalf("second send failed: %v", err)
	}
	if inserted {
		t.Fatal("expected inserted=false for duplicate msg_id")
	}
	if len(dmStore.messages) != 1 {
		t.Errorf("expected only 1 persisted message, got %d", len(dmStore.messages))
	}
}

// TestConversationService_ListMessages_Forbidden 对应"仅会话参与者可查看历史消息"越权校验。
func TestConversationService_ListMessages_Forbidden(t *testing.T) {
	svc, _, _, friends := newTestConversationService()
	friends.setFriend("alice", "bob")

	convID, _, err := svc.SendDirectMessage(context.Background(), "alice", "bob", "msg-1", "hi", "text")
	if err != nil {
		t.Fatalf("SendDirectMessage failed: %v", err)
	}

	_, _, err = svc.ListMessages(context.Background(), convID, "eve", 1, 20)
	if err != ErrForbiddenConversationAccess {
		t.Fatalf("expected ErrForbiddenConversationAccess, got %v", err)
	}
}

// TestConversationService_ListMessages_NotFound 对应会话不存在。
func TestConversationService_ListMessages_NotFound(t *testing.T) {
	svc, _, _, _ := newTestConversationService()

	_, _, err := svc.ListMessages(context.Background(), "no-such-conversation", "alice", 1, 20)
	if err != ErrConversationNotFound {
		t.Fatalf("expected ErrConversationNotFound, got %v", err)
	}
}

// TestConversationService_ListConversations_T71 对应 T71：会话列表。
func TestConversationService_ListConversations_T71(t *testing.T) {
	svc, _, _, friends := newTestConversationService()
	friends.setFriend("alice", "bob")

	if _, _, err := svc.SendDirectMessage(context.Background(), "alice", "bob", "msg-1", "hi", "text"); err != nil {
		t.Fatalf("SendDirectMessage failed: %v", err)
	}

	summaries, err := svc.ListConversations(context.Background(), "alice")
	if err != nil {
		t.Fatalf("ListConversations failed: %v", err)
	}
	if len(summaries) != 1 || summaries[0].PeerID != "bob" {
		t.Errorf("expected alice to see bob as conversation peer, got %+v", summaries)
	}
}
