package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

type conversationTestDeps struct {
	router  *gin.Engine
	convs   *fakeConversationStore
	dms     *fakeDirectMessageStore
	friends *fakeFriendChecker
}

func newConversationTestRouter(userID string) *conversationTestDeps {
	gin.SetMode(gin.TestMode)
	convs := newFakeConversationStore()
	dms := newFakeDirectMessageStore()
	friends := newFakeFriendChecker()
	svc := service.NewConversationService(convs, dms, friends)
	h := &ConversationHandler{ConversationService: svc}

	r := gin.New()
	r.Use(fakeAuthMiddleware(userID))
	r.GET("/api/conversations", h.ListConversations)
	r.GET("/api/conversations/:id/messages", h.ListMessages)
	return &conversationTestDeps{router: r, convs: convs, dms: dms, friends: friends}
}

// TestListConversations_XSSEscaped 覆盖 T50：会话列表的最新消息预览必须做 HTML 转义。
func TestListConversations_XSSEscaped(t *testing.T) {
	deps := newConversationTestRouter("alice")
	deps.convs.summaries["alice"] = []model.ConversationSummary{
		{ConversationID: "conv1", PeerID: "bob", LastMessage: "<img src=x onerror=alert(1)>", LastMessageAt: time.Now()},
	}

	req := httptest.NewRequest("GET", "/api/conversations", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(resp))
	}
	if got := resp[0]["last_message"]; got != "&lt;img src=x onerror=alert(1)&gt;" {
		t.Fatalf("expected HTML-escaped last_message, got %q", got)
	}
}

// TestListConversationMessages_InvalidConversationIDFormat 覆盖非 UUID 格式的
// 会话 ID 应在到达 service 层前被 400 拦截。
func TestListConversationMessages_InvalidConversationIDFormat(t *testing.T) {
	deps := newConversationTestRouter("alice")

	req := httptest.NewRequest("GET", "/api/conversations/not-a-uuid/messages", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"invalid_conversation_id"`) {
		t.Fatalf("expected invalid_conversation_id, got %s", w.Body.String())
	}
}

// TestListConversationMessages_NotFound 覆盖合法 UUID 但会话不存在时的 404。
func TestListConversationMessages_NotFound(t *testing.T) {
	deps := newConversationTestRouter("alice")

	req := httptest.NewRequest("GET", "/api/conversations/"+uuid.NewString()+"/messages", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"conversation_not_found"`) {
		t.Fatalf("expected conversation_not_found, got %s", w.Body.String())
	}
}

// TestListConversationMessages_ForbiddenForNonParticipant 覆盖越权校验：
// 非会话参与者查看历史消息应返回 403（T72 私聊隐私边界）。
func TestListConversationMessages_ForbiddenForNonParticipant(t *testing.T) {
	deps := newConversationTestRouter("mallory") // 既不是 alice 也不是 bob
	convID := uuid.NewString()
	deps.convs.put(&model.Conversation{ID: convID, UserAID: "alice", UserBID: "bob"})

	req := httptest.NewRequest("GET", "/api/conversations/"+convID+"/messages", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"error":"forbidden"`) {
		t.Fatalf("expected forbidden, got %s", w.Body.String())
	}
}

// TestListConversationMessages_ParticipantCanView 覆盖会话参与者可正常查看历史消息。
func TestListConversationMessages_ParticipantCanView(t *testing.T) {
	deps := newConversationTestRouter("alice")
	convID := uuid.NewString()
	deps.convs.put(&model.Conversation{ID: convID, UserAID: "alice", UserBID: "bob"})
	deps.dms.messages[convID] = []model.DirectMessage{
		{ID: "m1", MsgID: "msg1", ConversationID: convID, SenderID: "bob", SenderType: "human", Content: "hi", ContentType: "text", CreatedAt: time.Now()},
	}

	req := httptest.NewRequest("GET", "/api/conversations/"+convID+"/messages", nil)
	w := httptest.NewRecorder()
	deps.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"content":"hi"`) {
		t.Fatalf("expected message content in response, got %s", w.Body.String())
	}
}
