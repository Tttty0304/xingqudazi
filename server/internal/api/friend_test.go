package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

func extractJSONStringField(t *testing.T, body []byte, field string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("failed to parse JSON: %v, body=%s", err, body)
	}
	v, ok := m[field].(string)
	if !ok {
		t.Fatalf("field %q not found or not a string in %s", field, body)
	}
	return v
}

func newTestTokenServiceForFriendTest() *service.TokenService {
	return service.NewTokenService("test-secret-for-friend-test", time.Hour)
}

// fakeAuthMiddleware 模拟 RequireAuth 已经通过校验并把 user_id 写入 context 的效果，
// 让 handler 层测试不需要真实构造 JWT——这一层要测的是 handler 本身的逻辑，
// 鉴权本身已由 middleware 层单测（auth_test.go）独立覆盖。
func fakeAuthMiddleware(userID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("auth_user_id", userID)
		c.Next()
	}
}

type friendTestDeps struct {
	users   *fakeUserStore
	friends *fakeFriendshipStore
}

func newFriendTestRouter(currentUserID string) (*gin.Engine, *friendTestDeps) {
	gin.SetMode(gin.TestMode)
	users := newFakeUserStore()
	friends := newFakeFriendshipStore()
	svc := service.NewFriendService(friends, users, &fakePresenceChecker{online: map[string]bool{}}, &fakeFriendNotifier{})
	h := &FriendHandler{FriendService: svc}

	r := gin.New()
	r.Use(fakeAuthMiddleware(currentUserID))
	r.POST("/api/friends/requests", h.SendRequest)
	r.PUT("/api/friends/requests/:id", h.RespondRequest)
	r.GET("/api/friends", h.ListFriends)
	r.GET("/api/friends/requests", h.ListPendingRequests)
	r.DELETE("/api/friends/:id", h.DeleteFriend)
	return r, &friendTestDeps{users: users, friends: friends}
}

// TestSendFriendRequest_Success 覆盖 T60：向存在的用户发起好友请求成功，返回 201。
func TestSendFriendRequest_Success(t *testing.T) {
	router, deps := newFriendTestRouter("alice")
	deps.users.put(&model.User{ID: "bob", Username: "bob"})

	w := doJSONRequest(router, "POST", "/api/friends/requests", `{"target_user_id":"bob"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"status":"pending"`) {
		t.Fatalf("expected status=pending, got %s", w.Body.String())
	}
}

// TestSendFriendRequest_CannotFriendSelf 覆盖不能加自己为好友的边界校验。
func TestSendFriendRequest_CannotFriendSelf(t *testing.T) {
	router, deps := newFriendTestRouter("alice")
	deps.users.put(&model.User{ID: "alice", Username: "alice"})

	w := doJSONRequest(router, "POST", "/api/friends/requests", `{"target_user_id":"alice"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"cannot_friend_self"`) {
		t.Fatalf("expected cannot_friend_self, got %s", w.Body.String())
	}
}

// TestSendFriendRequest_TargetUserNotFound 覆盖目标用户不存在时的 404。
func TestSendFriendRequest_TargetUserNotFound(t *testing.T) {
	router, _ := newFriendTestRouter("alice")

	w := doJSONRequest(router, "POST", "/api/friends/requests", `{"target_user_id":"no-such-user"}`, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"user_not_found"`) {
		t.Fatalf("expected user_not_found, got %s", w.Body.String())
	}
}

// TestSendFriendRequest_DuplicateRequest 覆盖重复发起同一好友请求时的 409。
func TestSendFriendRequest_DuplicateRequest(t *testing.T) {
	router, deps := newFriendTestRouter("alice")
	deps.users.put(&model.User{ID: "bob", Username: "bob"})

	first := doJSONRequest(router, "POST", "/api/friends/requests", `{"target_user_id":"bob"}`, nil)
	if first.Code != http.StatusCreated {
		t.Fatalf("first request should succeed, got %d", first.Code)
	}

	second := doJSONRequest(router, "POST", "/api/friends/requests", `{"target_user_id":"bob"}`, nil)
	if second.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", second.Code)
	}
	if !contains(second.Body.String(), `"error":"friend_request_already_exists"`) {
		t.Fatalf("expected friend_request_already_exists, got %s", second.Body.String())
	}
}

// TestRespondFriendRequest_AcceptSuccess 覆盖 T61：接收方接受请求成功。
func TestRespondFriendRequest_AcceptSuccess(t *testing.T) {
	routerAsBob, deps := newFriendTestRouter("bob")
	deps.users.put(&model.User{ID: "alice", Username: "alice"})
	deps.users.put(&model.User{ID: "bob", Username: "bob"})

	// alice 发起请求需要用 alice 视角的 router，但共享同一份 friends store。
	routerAsAlice := gin.New()
	svc := service.NewFriendService(deps.friends, deps.users, &fakePresenceChecker{online: map[string]bool{}}, &fakeFriendNotifier{})
	aliceHandler := &FriendHandler{FriendService: svc}
	routerAsAlice.Use(fakeAuthMiddleware("alice"))
	routerAsAlice.POST("/api/friends/requests", aliceHandler.SendRequest)

	sendResp := doJSONRequest(routerAsAlice, "POST", "/api/friends/requests", `{"target_user_id":"bob"}`, nil)
	if sendResp.Code != http.StatusCreated {
		t.Fatalf("send request failed: %d", sendResp.Code)
	}
	requestID := extractJSONStringField(t, sendResp.Body.Bytes(), "request_id")

	acceptResp := doJSONRequest(routerAsBob, "PUT", "/api/friends/requests/"+requestID, `{"action":"accept"}`, nil)
	if acceptResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", acceptResp.Code, acceptResp.Body.String())
	}
	if !contains(acceptResp.Body.String(), `"status":"accepted"`) {
		t.Fatalf("expected status=accepted, got %s", acceptResp.Body.String())
	}
}

// TestRespondFriendRequest_Forbidden 覆盖越权校验：只有请求的接收方（target）
// 才能接受/拒绝，第三方（甚至发起方本人）操作应返回 403。
func TestRespondFriendRequest_Forbidden(t *testing.T) {
	deps := &friendTestDeps{users: newFakeUserStore(), friends: newFakeFriendshipStore()}
	deps.users.put(&model.User{ID: "alice", Username: "alice"})
	deps.users.put(&model.User{ID: "bob", Username: "bob"})
	svc := service.NewFriendService(deps.friends, deps.users, &fakePresenceChecker{online: map[string]bool{}}, &fakeFriendNotifier{})

	routerAsAlice := gin.New()
	routerAsAlice.Use(fakeAuthMiddleware("alice"))
	aliceHandler := &FriendHandler{FriendService: svc}
	routerAsAlice.POST("/api/friends/requests", aliceHandler.SendRequest)
	sendResp := doJSONRequest(routerAsAlice, "POST", "/api/friends/requests", `{"target_user_id":"bob"}`, nil)
	requestID := extractJSONStringField(t, sendResp.Body.Bytes(), "request_id")

	// 发起方（alice 自己）尝试"接受"自己发出的请求，应被拒绝。
	routerAsAlice.PUT("/api/friends/requests/:id", aliceHandler.RespondRequest)
	forbiddenResp := doJSONRequest(routerAsAlice, "PUT", "/api/friends/requests/"+requestID, `{"action":"accept"}`, nil)
	if forbiddenResp.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d, body=%s", forbiddenResp.Code, forbiddenResp.Body.String())
	}
	if !contains(forbiddenResp.Body.String(), `"error":"forbidden"`) {
		t.Fatalf("expected forbidden, got %s", forbiddenResp.Body.String())
	}
}

// TestRespondFriendRequest_InvalidAction 覆盖 action 字段非 accept/reject 时的 400。
func TestRespondFriendRequest_InvalidAction(t *testing.T) {
	router, _ := newFriendTestRouter("bob")
	w := doJSONRequest(router, "PUT", "/api/friends/requests/some-id", `{"action":"maybe"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"invalid_action"`) {
		t.Fatalf("expected invalid_action, got %s", w.Body.String())
	}
}

// TestRespondFriendRequest_NotFound 覆盖操作不存在的请求 ID 时的 404。
func TestRespondFriendRequest_NotFound(t *testing.T) {
	router, _ := newFriendTestRouter("bob")
	w := doJSONRequest(router, "PUT", "/api/friends/requests/no-such-request-id", `{"action":"accept"}`, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"friend_request_not_found"`) {
		t.Fatalf("expected friend_request_not_found, got %s", w.Body.String())
	}
}

// TestListFriends_ReturnsOnlineStatus 覆盖 T63：好友列表携带实时在线态。
func TestListFriends_ReturnsOnlineStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := newFakeUserStore()
	users.put(&model.User{ID: "alice", Username: "alice"})
	users.put(&model.User{ID: "bob", Username: "bob"})
	friends := newFakeFriendshipStore()
	friends.byID["f1"] = &model.Friendship{ID: "f1", RequesterID: "alice", TargetID: "bob", Status: "accepted"}
	presence := &fakePresenceChecker{online: map[string]bool{"bob": true}}
	svc := service.NewFriendService(friends, users, presence, &fakeFriendNotifier{})
	h := &FriendHandler{FriendService: svc}

	router := gin.New()
	router.Use(fakeAuthMiddleware("alice"))
	router.GET("/api/friends", h.ListFriends)

	req := httptest.NewRequest("GET", "/api/friends", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"online":true`) {
		t.Fatalf("expected bob to show online:true, got %s", w.Body.String())
	}
}

// TestListPendingRequests_DirectionLabeling 覆盖 T120：待处理请求应正确标注方向
// （incoming：别人发给我；outgoing：我发出的）。
func TestListPendingRequests_DirectionLabeling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := newFakeUserStore()
	users.put(&model.User{ID: "alice", Username: "alice"})
	users.put(&model.User{ID: "bob", Username: "bob"})
	users.put(&model.User{ID: "carol", Username: "carol"})
	friends := newFakeFriendshipStore()
	friends.byID["f1"] = &model.Friendship{ID: "f1", RequesterID: "bob", TargetID: "alice", Status: "pending"}
	friends.byID["f2"] = &model.Friendship{ID: "f2", RequesterID: "alice", TargetID: "carol", Status: "pending"}
	svc := service.NewFriendService(friends, users, &fakePresenceChecker{online: map[string]bool{}}, &fakeFriendNotifier{})
	h := &FriendHandler{FriendService: svc}

	router := gin.New()
	router.Use(fakeAuthMiddleware("alice"))
	router.GET("/api/friends/requests", h.ListPendingRequests)

	req := httptest.NewRequest("GET", "/api/friends/requests", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"peer_username":"bob","direction":"incoming"`) {
		t.Fatalf("expected bob's request to be labeled incoming, got %s", w.Body.String())
	}
	if !contains(w.Body.String(), `"peer_username":"carol","direction":"outgoing"`) {
		t.Fatalf("expected carol's request to be labeled outgoing, got %s", w.Body.String())
	}
}

// TestDeleteFriend_NotFriendsIsIdempotent 覆盖重复删除时目标状态已达成，仍返回 204。
func TestDeleteFriend_NotFriendsIsIdempotent(t *testing.T) {
	router, _ := newFriendTestRouter("alice")

	req := httptest.NewRequest("DELETE", "/api/friends/bob", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

// TestDeleteFriend_Success 覆盖删除已存在的好友关系成功，返回 204。
func TestDeleteFriend_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := newFakeUserStore()
	friends := newFakeFriendshipStore()
	friends.byID["f1"] = &model.Friendship{ID: "f1", RequesterID: "alice", TargetID: "bob", Status: "accepted"}
	svc := service.NewFriendService(friends, users, &fakePresenceChecker{online: map[string]bool{}}, &fakeFriendNotifier{})
	h := &FriendHandler{FriendService: svc}

	router := gin.New()
	router.Use(fakeAuthMiddleware("alice"))
	router.DELETE("/api/friends/:id", h.DeleteFriend)

	req := httptest.NewRequest("DELETE", "/api/friends/bob", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d, body=%s", w.Code, w.Body.String())
	}
}

// TestMiddlewareIntegration_RequireAuth_ProtectsRealRoute 用真实的
// middleware.RequireAuth（而非 fakeAuthMiddleware）串联一次，确认 handler 与
// 真实鉴权中间件组合工作正常（前面的 fakeAuthMiddleware 都是为了聚焦测试
// handler 自身逻辑，这里补一个"整条链路都是真实组件"的烟雾测试）。
func TestMiddlewareIntegration_RequireAuth_ProtectsRealRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	users := newFakeUserStore()
	friends := newFakeFriendshipStore()
	svc := service.NewFriendService(friends, users, &fakePresenceChecker{online: map[string]bool{}}, &fakeFriendNotifier{})
	h := &FriendHandler{FriendService: svc}
	tokenSvc := newTestTokenServiceForFriendTest()

	router := gin.New()
	router.GET("/api/friends", middleware.RequireAuth(tokenSvc, nil), h.ListFriends)

	unauthed := httptest.NewRequest("GET", "/api/friends", nil)
	wUnauthed := httptest.NewRecorder()
	router.ServeHTTP(wUnauthed, unauthed)
	if wUnauthed.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", wUnauthed.Code)
	}

	token, _ := tokenSvc.GenerateToken("alice", false)
	authed := httptest.NewRequest("GET", "/api/friends", nil)
	authed.Header.Set("Authorization", "Bearer "+token)
	wAuthed := httptest.NewRecorder()
	router.ServeHTTP(wAuthed, authed)
	if wAuthed.Code != http.StatusOK {
		t.Fatalf("expected 200 with valid token, got %d", wAuthed.Code)
	}
}
