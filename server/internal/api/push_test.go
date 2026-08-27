package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/service"
	"xingqudazi-im/server/pkg/webpush"
)

func newPushTestRouter(userID string) (*gin.Engine, *fakePushSubscriptionStore) {
	gin.SetMode(gin.TestMode)
	store := newFakePushSubscriptionStore()
	vapid, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		panic(err)
	}
	presence := &fakePresenceChecker{online: map[string]bool{}}
	svc := service.NewPushService(store, presence, vapid.PublicKey, vapid.PrivateKey, "mailto:test@example.com")
	h := &PushHandler{PushService: svc}

	r := gin.New()
	r.GET("/api/push/vapid-public-key", h.VAPIDPublicKey)
	authed := r.Group("/")
	authed.Use(fakeAuthMiddleware(userID))
	authed.POST("/api/push/subscriptions", h.Subscribe)
	authed.DELETE("/api/push/subscriptions", h.Unsubscribe)
	return r, store
}

// TestPushVAPIDPublicKey_NoAuthRequired 覆盖公钥接口无需鉴权即可访问。
func TestPushVAPIDPublicKey_NoAuthRequired(t *testing.T) {
	router, _ := newPushTestRouter("")

	req := httptest.NewRequest("GET", "/api/push/vapid-public-key", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"public_key":"`) {
		t.Fatalf("expected public_key in response, got %s", w.Body.String())
	}
}

// TestPushSubscribe_Success 覆盖 T100：合法订阅信息应成功创建。
func TestPushSubscribe_Success(t *testing.T) {
	router, store := newPushTestRouter("alice")
	body := `{"endpoint":"https://push.example.com/ep1","keys":{"p256dh":"abc","auth":"def"}}`

	w := doJSONRequest(router, "POST", "/api/push/subscriptions", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", w.Code, w.Body.String())
	}
	subs, _ := store.ListByUser(context.Background(), "alice")
	if len(subs) != 1 || subs[0].Endpoint != "https://push.example.com/ep1" {
		t.Fatalf("expected subscription to be persisted, got %+v", subs)
	}
}

// TestPushSubscribe_MissingFields 覆盖缺少必填字段（endpoint/p256dh/auth）时的 400。
func TestPushSubscribe_MissingFields(t *testing.T) {
	router, _ := newPushTestRouter("alice")

	w := doJSONRequest(router, "POST", "/api/push/subscriptions", `{"endpoint":"https://x.example.com"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
}

// TestPushUnsubscribe_Success 覆盖 T101：取消订阅成功返回 204。
func TestPushUnsubscribe_Success(t *testing.T) {
	router, store := newPushTestRouter("alice")
	doJSONRequest(router, "POST", "/api/push/subscriptions", `{"endpoint":"https://push.example.com/ep1","keys":{"p256dh":"abc","auth":"def"}}`, nil)

	w := doJSONRequest(router, "DELETE", "/api/push/subscriptions", `{"endpoint":"https://push.example.com/ep1"}`, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d, body=%s", w.Code, w.Body.String())
	}
	subs, _ := store.ListByUser(context.Background(), "alice")
	if len(subs) != 0 {
		t.Fatalf("expected subscription to be removed, got %+v", subs)
	}
}

// TestPushUnsubscribe_MissingEndpoint 覆盖缺少 endpoint 字段时的 400。
func TestPushUnsubscribe_MissingEndpoint(t *testing.T) {
	router, _ := newPushTestRouter("alice")

	w := doJSONRequest(router, "DELETE", "/api/push/subscriptions", `{}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
