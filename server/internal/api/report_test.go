package api

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

func newReportTestRouter(userID string, existingMessageIDs, existingDMIDs []string, users *fakeUserStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	store := newFakeReportStore()
	msgChecker := newFakeExistenceChecker(existingMessageIDs...)
	dmChecker := newFakeExistenceChecker(existingDMIDs...)
	svc := service.NewReportService(store, msgChecker, dmChecker, users)
	h := &ReportHandler{ReportService: svc}

	r := gin.New()
	r.Use(fakeAuthMiddleware(userID))
	r.POST("/api/reports", h.CreateReport)
	return r
}

// TestCreateReport_Message_Success 覆盖 T80：举报存在的消息成功。
func TestCreateReport_Message_Success(t *testing.T) {
	router := newReportTestRouter("alice", []string{"msg-1"}, nil, newFakeUserStore())

	w := doJSONRequest(router, "POST", "/api/reports", `{"target_type":"message","target_id":"msg-1","reason":"垂钓广告"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"report_id":"`) {
		t.Fatalf("expected report_id in response, got %s", w.Body.String())
	}
}

// TestCreateReport_InvalidTargetType 覆盖非法举报目标类型时的 400。
func TestCreateReport_InvalidTargetType(t *testing.T) {
	router := newReportTestRouter("alice", nil, nil, newFakeUserStore())

	w := doJSONRequest(router, "POST", "/api/reports", `{"target_type":"room","target_id":"r1","reason":"x"}`, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"invalid_target_type"`) {
		t.Fatalf("expected invalid_target_type, got %s", w.Body.String())
	}
}

// TestCreateReport_TargetNotFound 覆盖举报目标（消息）不存在时的 404。
func TestCreateReport_TargetNotFound(t *testing.T) {
	router := newReportTestRouter("alice", nil, nil, newFakeUserStore())

	w := doJSONRequest(router, "POST", "/api/reports", `{"target_type":"message","target_id":"no-such-msg","reason":"x"}`, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"report_target_not_found"`) {
		t.Fatalf("expected report_target_not_found, got %s", w.Body.String())
	}
}

// TestCreateReport_UserTarget_Success 覆盖举报用户类型目标（通过 UserStore 校验存在性）。
func TestCreateReport_UserTarget_Success(t *testing.T) {
	users := newFakeUserStore()
	users.put(&model.User{ID: "bob", Username: "bob"})
	router := newReportTestRouter("alice", nil, nil, users)

	w := doJSONRequest(router, "POST", "/api/reports", `{"target_type":"user","target_id":"bob","reason":"骚扰"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", w.Code, w.Body.String())
	}
}

// TestCreateReport_DuplicateIsIdempotent 覆盖 T80 边界：同一举报人对同一目标重复
// 举报应幂等返回已有记录，而不是报错或产生第二条重复记录。
func TestCreateReport_DuplicateIsIdempotent(t *testing.T) {
	router := newReportTestRouter("alice", []string{"msg-1"}, nil, newFakeUserStore())

	first := doJSONRequest(router, "POST", "/api/reports", `{"target_type":"message","target_id":"msg-1","reason":"first reason"}`, nil)
	second := doJSONRequest(router, "POST", "/api/reports", `{"target_type":"message","target_id":"msg-1","reason":"second reason"}`, nil)

	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("expected both to succeed (idempotent), got first=%d second=%d", first.Code, second.Code)
	}
	firstID := extractJSONStringField(t, first.Body.Bytes(), "report_id")
	secondID := extractJSONStringField(t, second.Body.Bytes(), "report_id")
	if firstID != secondID {
		t.Fatalf("expected same report_id on duplicate report (idempotent), got %q vs %q", firstID, secondID)
	}
}

// TestCreateReport_DirectMessageTarget_Success 覆盖举报私聊消息类型目标。
func TestCreateReport_DirectMessageTarget_Success(t *testing.T) {
	router := newReportTestRouter("alice", nil, []string{"dm-1"}, newFakeUserStore())

	w := doJSONRequest(router, "POST", "/api/reports", `{"target_type":"direct_message","target_id":"dm-1","reason":"x"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", w.Code, w.Body.String())
	}
}
