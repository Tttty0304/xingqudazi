package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/service"
)

func newWatchTopicTestRouter(userID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	store := newFakeWatchTopicStore()
	svc := service.NewWatchTopicService(store)
	h := &WatchTopicHandler{WatchTopicService: svc}

	r := gin.New()
	r.Use(fakeAuthMiddleware(userID))
	r.POST("/api/watch-topics", h.Create)
	r.GET("/api/watch-topics", h.List)
	r.DELETE("/api/watch-topics/:id", h.Delete)
	return r
}

// TestCreateWatchTopic_Success 覆盖 T94：创建关注事项成功。
func TestCreateWatchTopic_Success(t *testing.T) {
	router := newWatchTopicTestRouter("alice")
	w := doJSONRequest(router, "POST", "/api/watch-topics", `{"keywords":"摄影,徒步"}`, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"topic_id":"`) {
		t.Fatalf("expected topic_id in response, got %s", w.Body.String())
	}
}

// TestCreateWatchTopic_EmptyKeywords 覆盖关键词为空时应被拒绝（binding required
// 会先拦截空字符串? 实际上 Go 的 binding:"required" 对空字符串也判定为缺失）。
func TestCreateWatchTopic_EmptyKeywords(t *testing.T) {
	router := newWatchTopicTestRouter("alice")
	w := doJSONRequest(router, "POST", "/api/watch-topics", `{"keywords":""}`, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d, body=%s", w.Code, w.Body.String())
	}
}

// TestCreateWatchTopic_InvalidExpiresAt 覆盖 expires_at 非 RFC3339 格式时的 400。
func TestCreateWatchTopic_InvalidExpiresAt(t *testing.T) {
	router := newWatchTopicTestRouter("alice")
	w := doJSONRequest(router, "POST", "/api/watch-topics", `{"keywords":"摄影","expires_at":"not-a-date"}`, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"invalid_expires_at"`) {
		t.Fatalf("expected invalid_expires_at, got %s", w.Body.String())
	}
}

// TestListWatchTopics_OnlyOwnTopics 覆盖列表只返回当前用户自己的关注事项，
// 不会泄露其他用户的数据。
func TestListWatchTopics_OnlyOwnTopics(t *testing.T) {
	routerAlice := newWatchTopicTestRouterSharedStore()
	deps := routerAlice

	doJSONRequest(deps.asAlice, "POST", "/api/watch-topics", `{"keywords":"摄影"}`, nil)
	doJSONRequest(deps.asBob, "POST", "/api/watch-topics", `{"keywords":"徒步"}`, nil)

	req := httptest.NewRequest("GET", "/api/watch-topics", nil)
	w := httptest.NewRecorder()
	deps.asAlice.ServeHTTP(w, req)

	if !contains(w.Body.String(), "摄影") {
		t.Fatalf("expected alice's own topic to be listed, got %s", w.Body.String())
	}
	if contains(w.Body.String(), "徒步") {
		t.Fatalf("expected bob's topic to NOT leak into alice's list, got %s", w.Body.String())
	}
}

type watchTopicSharedRouters struct {
	asAlice *gin.Engine
	asBob   *gin.Engine
}

// newWatchTopicTestRouterSharedStore 让 alice/bob 两个视角的 router 共用同一个
// handler 实例（同一个底层 fakeWatchTopicStore），才能真实验证"越权访问他人数据"
// 这类跨用户场景（各自独立 store 的话，bob 永远看不到 alice 的数据，测试没有意义）。
func newWatchTopicTestRouterSharedStore() watchTopicSharedRouters {
	gin.SetMode(gin.TestMode)
	store := newFakeWatchTopicStore()
	svc := service.NewWatchTopicService(store)
	h := &WatchTopicHandler{WatchTopicService: svc}

	asAlice := gin.New()
	asAlice.Use(fakeAuthMiddleware("alice"))
	asAlice.POST("/api/watch-topics", h.Create)
	asAlice.GET("/api/watch-topics", h.List)
	asAlice.DELETE("/api/watch-topics/:id", h.Delete)

	asBob := gin.New()
	asBob.Use(fakeAuthMiddleware("bob"))
	asBob.POST("/api/watch-topics", h.Create)
	asBob.GET("/api/watch-topics", h.List)
	asBob.DELETE("/api/watch-topics/:id", h.Delete)

	return watchTopicSharedRouters{asAlice: asAlice, asBob: asBob}
}

// TestDeleteWatchTopic_NotOwner 覆盖 T123：非本人无法删除他人的关注事项（返回 404，
// 与"不存在"同码，不额外暴露"这个 ID 其实存在但不是你的"这一细节）。
func TestDeleteWatchTopic_NotOwner(t *testing.T) {
	deps := newWatchTopicTestRouterSharedStore()
	createResp := doJSONRequest(deps.asAlice, "POST", "/api/watch-topics", `{"keywords":"摄影"}`, nil)
	topicID := extractJSONStringField(t, createResp.Body.Bytes(), "topic_id")

	req := httptest.NewRequest("DELETE", "/api/watch-topics/"+topicID, nil)
	w := httptest.NewRecorder()
	deps.asBob.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-owner delete, got %d, body=%s", w.Code, w.Body.String())
	}

	// alice 自己应仍能看到该记录未被删除（验证 bob 的删除请求真的没有生效，
	// 而不只是"返回了404但实际已经删掉了"这种更隐蔽的bug）。
	listReq := httptest.NewRequest("GET", "/api/watch-topics", nil)
	listW := httptest.NewRecorder()
	deps.asAlice.ServeHTTP(listW, listReq)
	if !contains(listW.Body.String(), "摄影") {
		t.Fatalf("expected alice's topic to remain after bob's failed delete attempt, got %s", listW.Body.String())
	}
}

// TestDeleteWatchTopic_Success 覆盖本人删除自己的关注事项成功，返回 204。
func TestDeleteWatchTopic_Success(t *testing.T) {
	router := newWatchTopicTestRouter("alice")
	createResp := doJSONRequest(router, "POST", "/api/watch-topics", `{"keywords":"摄影"}`, nil)
	topicID := extractJSONStringField(t, createResp.Body.Bytes(), "topic_id")

	req := httptest.NewRequest("DELETE", "/api/watch-topics/"+topicID, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d, body=%s", w.Code, w.Body.String())
	}
}

// TestDeleteWatchTopic_NotFound 覆盖删除不存在的 ID 时返回 404。
func TestDeleteWatchTopic_NotFound(t *testing.T) {
	router := newWatchTopicTestRouter("alice")

	req := httptest.NewRequest("DELETE", "/api/watch-topics/no-such-id", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"watch_topic_not_found"`) {
		t.Fatalf("expected watch_topic_not_found, got %s", w.Body.String())
	}
}
