package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

type recommendationTestDeps struct {
	router     *gin.Engine
	candidates *fakeMatchCandidateStore
	users      *fakeUserStore
	friends    *fakeFriendChecker
	topics     *fakeWatchTopicLister
}

func newRecommendationTestRouter(userID string) *recommendationTestDeps {
	gin.SetMode(gin.TestMode)
	candidates := newFakeMatchCandidateStore()
	users := newFakeUserStore()
	friends := newFakeFriendChecker()
	topics := &fakeWatchTopicLister{}
	svc := service.NewRecommendationService(candidates, topics, friends, users)
	h := &RecommendationHandler{RecommendationService: svc}

	r := gin.New()
	r.Use(fakeAuthMiddleware(userID))
	r.POST("/api/recommendations/generate", h.Generate)
	r.GET("/api/recommendations", h.List)
	r.PUT("/api/recommendations/:id", h.Respond)
	return &recommendationTestDeps{router: r, candidates: candidates, users: users, friends: friends, topics: topics}
}

// TestGenerateRecommendations_CreatesMatchingCandidates 覆盖 T110：两个有共同关键词
// 的用户应生成候选，已是好友的用户对不生成候选。
func TestGenerateRecommendations_CreatesMatchingCandidates(t *testing.T) {
	deps := newRecommendationTestRouter("alice")
	deps.users.put(&model.User{ID: "alice", Username: "alice"})
	deps.users.put(&model.User{ID: "bob", Username: "bob"})
	deps.users.put(&model.User{ID: "carol", Username: "carol"})
	deps.topics.topics = []model.WatchTopic{
		{UserID: "alice", Keywords: "摄影,徒步"},
		{UserID: "bob", Keywords: "摄影"},   // 与 alice 有共同关键词，应生成候选
		{UserID: "carol", Keywords: "美食"}, // 与 alice 无交集，不应生成候选
	}
	deps.friends.setFriends("alice", "carol") // 即便有交集也不会（这里没交集，仅验证不影响其它对）

	req := httptest.NewRequest("POST", "/api/recommendations/generate", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"created":1`) {
		t.Fatalf("expected exactly 1 candidate created (alice-bob), got %s", w.Body.String())
	}
}

// TestGenerateRecommendations_ExcludesExistingFriends 覆盖已是好友的用户对
// 即使关键词重合也不生成候选（推荐目的是认识新朋友）。
func TestGenerateRecommendations_ExcludesExistingFriends(t *testing.T) {
	deps := newRecommendationTestRouter("alice")
	deps.users.put(&model.User{ID: "alice", Username: "alice"})
	deps.users.put(&model.User{ID: "bob", Username: "bob"})
	deps.topics.topics = []model.WatchTopic{
		{UserID: "alice", Keywords: "摄影"},
		{UserID: "bob", Keywords: "摄影"},
	}
	deps.friends.setFriends("alice", "bob")

	req := httptest.NewRequest("POST", "/api/recommendations/generate", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)

	if !contains(w.Body.String(), `"created":0`) {
		t.Fatalf("expected 0 candidates created for existing friends, got %s", w.Body.String())
	}
}

// TestListRecommendations_ResolvesPeerUsername 覆盖 T111：候选列表返回对方真实
// 用户名而非 ID。
func TestListRecommendations_ResolvesPeerUsername(t *testing.T) {
	deps := newRecommendationTestRouter("alice")
	deps.users.put(&model.User{ID: "alice", Username: "alice"})
	deps.users.put(&model.User{ID: "bob", Username: "bob"})
	deps.candidates.put(&model.MatchCandidate{
		ID: "c1", UserAID: "alice", UserBID: "bob",
		MatchReason: "你们都关注：摄影", MatchScore: 2, Status: "pending_review",
	})

	req := httptest.NewRequest("GET", "/api/recommendations", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"peer_username":"bob"`) {
		t.Fatalf("expected peer_username=bob, got %s", w.Body.String())
	}
}

// TestRespondRecommendation_ConfirmSuccess 覆盖 T112：确认推荐候选成功。
func TestRespondRecommendation_ConfirmSuccess(t *testing.T) {
	deps := newRecommendationTestRouter("alice")
	deps.candidates.put(&model.MatchCandidate{ID: "c1", UserAID: "alice", UserBID: "bob", Status: "pending_review"})

	w := doJSONRequest(deps.router, "PUT", "/api/recommendations/c1", `{"action":"confirm"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
}

// TestRespondRecommendation_Forbidden 覆盖越权：非候选双方之一的用户不能操作。
func TestRespondRecommendation_Forbidden(t *testing.T) {
	deps := newRecommendationTestRouter("mallory")
	deps.candidates.put(&model.MatchCandidate{ID: "c1", UserAID: "alice", UserBID: "bob", Status: "pending_review"})

	w := doJSONRequest(deps.router, "PUT", "/api/recommendations/c1", `{"action":"confirm"}`, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d, body=%s", w.Code, w.Body.String())
	}
}

// TestRespondRecommendation_AlreadyResolved 覆盖对已处理过的候选再次操作返回 409。
func TestRespondRecommendation_AlreadyResolved(t *testing.T) {
	deps := newRecommendationTestRouter("alice")
	deps.candidates.put(&model.MatchCandidate{ID: "c1", UserAID: "alice", UserBID: "bob", Status: "confirmed"})

	w := doJSONRequest(deps.router, "PUT", "/api/recommendations/c1", `{"action":"dismiss"}`, nil)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"error":"already_resolved"`) {
		t.Fatalf("expected already_resolved, got %s", w.Body.String())
	}
}

// TestRespondRecommendation_NotFound 覆盖候选 ID 不存在返回 404。
func TestRespondRecommendation_NotFound(t *testing.T) {
	deps := newRecommendationTestRouter("alice")
	w := doJSONRequest(deps.router, "PUT", "/api/recommendations/no-such-id", `{"action":"confirm"}`, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"candidate_not_found"`) {
		t.Fatalf("expected candidate_not_found, got %s", w.Body.String())
	}
}
