package service

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/pkg/webpush"
)

// fakeSubscriberKeys 生成一组真实合法的浏览器订阅密钥材料（ECDH P-256 公钥 + 16字节
// auth secret），供测试构造 model.PushSubscription 时使用，确保 webpush.Send() 内部
// 的真实加密流程不会因为无效密钥格式而在到达 HTTP 层之前就失败——测试的关注点是
// PushService 的业务逻辑（在线态判断/失效订阅清理），不是重复验证 webpush 包本身的
// 加密正确性（已在 pkg/webpush/webpush_test.go 独立覆盖）。
func fakeSubscriberKeys(t *testing.T) (p256dh, auth string) {
	t.Helper()
	priv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate subscriber key: %v", err)
	}
	authBytes := make([]byte, 16)
	if _, err := rand.Read(authBytes); err != nil {
		t.Fatalf("generate auth secret: %v", err)
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(priv.PublicKey().Bytes()), enc.EncodeToString(authBytes)
}

// fakePushSubscriptionStore 是 PushSubscriptionStore 的内存假实现。
type fakePushSubscriptionStore struct {
	mu   sync.Mutex
	subs map[string][]model.PushSubscription // userID -> subscriptions
}

func newFakePushSubscriptionStore() *fakePushSubscriptionStore {
	return &fakePushSubscriptionStore{subs: make(map[string][]model.PushSubscription)}
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
			break
		}
	}
	return nil
}

func (f *fakePushSubscriptionStore) ListByUser(_ context.Context, userID string) ([]model.PushSubscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]model.PushSubscription{}, f.subs[userID]...), nil
}

func (f *fakePushSubscriptionStore) DeleteByID(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for userID, list := range f.subs {
		for i, s := range list {
			if s.ID == id {
				f.subs[userID] = append(list[:i], list[i+1:]...)
				return nil
			}
		}
	}
	return nil
}

func TestPushService_Subscribe_And_Unsubscribe(t *testing.T) {
	store := newFakePushSubscriptionStore()
	presence := &fakePresenceChecker{online: map[string]bool{}}
	vapid, _ := webpush.GenerateVAPIDKeys()
	svc := NewPushService(store, presence, vapid.PublicKey, vapid.PrivateKey, "mailto:test@example.com")

	if err := svc.Subscribe(context.Background(), "alice", "https://push.example.com/ep1", "p256dh-value", "auth-value"); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	subs, _ := store.ListByUser(context.Background(), "alice")
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription after Subscribe, got %d", len(subs))
	}

	if err := svc.Unsubscribe(context.Background(), "alice", "https://push.example.com/ep1"); err != nil {
		t.Fatalf("Unsubscribe failed: %v", err)
	}
	subs, _ = store.ListByUser(context.Background(), "alice")
	if len(subs) != 0 {
		t.Fatalf("expected 0 subscriptions after Unsubscribe, got %d", len(subs))
	}
}

func TestPushService_Subscribe_InvalidInput(t *testing.T) {
	store := newFakePushSubscriptionStore()
	presence := &fakePresenceChecker{online: map[string]bool{}}
	vapid, _ := webpush.GenerateVAPIDKeys()
	svc := NewPushService(store, presence, vapid.PublicKey, vapid.PrivateKey, "mailto:test@example.com")

	if err := svc.Subscribe(context.Background(), "alice", "", "p256dh", "auth"); err != ErrInvalidPushSubscription {
		t.Fatalf("expected ErrInvalidPushSubscription for empty endpoint, got %v", err)
	}
}

func TestPushService_NotifyOfflineUser_SkipsWhenOnline(t *testing.T) {
	store := newFakePushSubscriptionStore()
	presence := &fakePresenceChecker{online: map[string]bool{"alice": true}}
	vapid, _ := webpush.GenerateVAPIDKeys()
	svc := NewPushService(store, presence, vapid.PublicKey, vapid.PrivateKey, "mailto:test@example.com")

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	if err := svc.Subscribe(context.Background(), "alice", server.URL+"/ep1", "p256dh-value", "auth-value"); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if err := svc.NotifyOfflineUser(context.Background(), "alice", "title", "body"); err != nil {
		t.Fatalf("NotifyOfflineUser failed: %v", err)
	}
	if requestCount != 0 {
		t.Fatalf("expected 0 push requests sent for an online user, got %d", requestCount)
	}
}

func TestPushService_NotifyOfflineUser_SendsWhenOffline(t *testing.T) {
	store := newFakePushSubscriptionStore()
	presence := &fakePresenceChecker{online: map[string]bool{}}
	vapid, _ := webpush.GenerateVAPIDKeys()
	svc := NewPushService(store, presence, vapid.PublicKey, vapid.PrivateKey, "mailto:test@example.com")

	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Header.Get("Content-Encoding") != "aes128gcm" {
			t.Errorf("expected Content-Encoding=aes128gcm, got %q", r.Header.Get("Content-Encoding"))
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	p256dh, auth := fakeSubscriberKeys(t)
	if err := svc.Subscribe(context.Background(), "bob", server.URL+"/ep1", p256dh, auth); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if err := svc.NotifyOfflineUser(context.Background(), "bob", "新的好友请求", "alice 想加你为好友"); err != nil {
		t.Fatalf("NotifyOfflineUser failed: %v", err)
	}
	if requestCount != 1 {
		t.Fatalf("expected exactly 1 push request sent for an offline user with 1 subscription, got %d", requestCount)
	}
}

func TestPushService_NotifyOfflineUser_CleansUpExpiredSubscription(t *testing.T) {
	store := newFakePushSubscriptionStore()
	presence := &fakePresenceChecker{online: map[string]bool{}}
	vapid, _ := webpush.GenerateVAPIDKeys()
	svc := NewPushService(store, presence, vapid.PublicKey, vapid.PrivateKey, "mailto:test@example.com")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusGone) // 模拟推送服务返回 410，代表该订阅已失效
	}))
	defer server.Close()

	p256dh, auth := fakeSubscriberKeys(t)
	if err := svc.Subscribe(context.Background(), "carol", server.URL+"/ep1", p256dh, auth); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if err := svc.NotifyOfflineUser(context.Background(), "carol", "title", "body"); err != nil {
		t.Fatalf("NotifyOfflineUser failed: %v", err)
	}

	subs, _ := store.ListByUser(context.Background(), "carol")
	if len(subs) != 0 {
		t.Fatalf("expected expired subscription to be cleaned up after 410 response, got %d remaining", len(subs))
	}
}

// TestPushService_NotifyOfflineUser_RetriesOnTransient5xxThenSucceeds
// 对应能力补齐项：发送失败重试。模拟推送服务前两次返回 503（瞬时故障），
// 第三次才成功——验证 sendWithRetry 确实会重试，而不是第一次失败就放弃。
func TestPushService_NotifyOfflineUser_RetriesOnTransient5xxThenSucceeds(t *testing.T) {
	store := newFakePushSubscriptionStore()
	presence := &fakePresenceChecker{online: map[string]bool{}}
	vapid, _ := webpush.GenerateVAPIDKeys()
	svc := NewPushService(store, presence, vapid.PublicKey, vapid.PrivateKey, "mailto:test@example.com")

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requestCount, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	p256dh, auth := fakeSubscriberKeys(t)
	if err := svc.Subscribe(context.Background(), "dave", server.URL+"/ep1", p256dh, auth); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if err := svc.NotifyOfflineUser(context.Background(), "dave", "title", "body"); err != nil {
		t.Fatalf("expected eventual success after retries, got error: %v", err)
	}
	if got := atomic.LoadInt32(&requestCount); got != 3 {
		t.Fatalf("expected exactly 3 attempts (2 failures + 1 success), got %d", got)
	}
}

// TestPushService_NotifyOfflineUser_GivesUpAfterMaxAttempts 验证持续 5xx 时
// 最终会放弃（不会无限重试），且尝试次数恰好等于 pushSendMaxAttempts。
func TestPushService_NotifyOfflineUser_GivesUpAfterMaxAttempts(t *testing.T) {
	store := newFakePushSubscriptionStore()
	presence := &fakePresenceChecker{online: map[string]bool{}}
	vapid, _ := webpush.GenerateVAPIDKeys()
	svc := NewPushService(store, presence, vapid.PublicKey, vapid.PrivateKey, "mailto:test@example.com")

	var requestCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	p256dh, auth := fakeSubscriberKeys(t)
	if err := svc.Subscribe(context.Background(), "erin", server.URL+"/ep1", p256dh, auth); err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	if err := svc.NotifyOfflineUser(context.Background(), "erin", "title", "body"); err == nil {
		t.Fatal("expected error after exhausting all retry attempts, got nil")
	}
	if got := atomic.LoadInt32(&requestCount); got != pushSendMaxAttempts {
		t.Fatalf("expected exactly %d attempts, got %d", pushSendMaxAttempts, got)
	}
}
