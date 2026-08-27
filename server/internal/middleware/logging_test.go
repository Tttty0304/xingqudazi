package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	logger "xingqudazi-im/server/pkg/log"
)

func newLoggingRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogging())
	r.GET("/ping", func(c *gin.Context) {
		// 断言 handler 内部能通过 context 拿到 trace_id（供业务日志关联同一次请求）。
		traceID := logger.TraceIDFromContext(c.Request.Context())
		c.JSON(http.StatusOK, gin.H{"trace_id": traceID})
	})
	return r
}

// TestRequestLogging_GeneratesTraceIDWhenMissing 覆盖客户端未传 X-Trace-Id 时，
// 中间件应自动生成一个非空的 trace_id，且响应头回传。
func TestRequestLogging_GeneratesTraceIDWhenMissing(t *testing.T) {
	router := newLoggingRouter()
	req := httptest.NewRequest("GET", "/ping", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	respTraceID := w.Header().Get("X-Trace-Id")
	if respTraceID == "" {
		t.Fatal("expected non-empty X-Trace-Id response header")
	}
	if !jsonContains(w.Body.String(), respTraceID) {
		t.Fatalf("expected handler to see the same trace_id via context, response=%s traceID=%s", w.Body.String(), respTraceID)
	}
}

// TestRequestLogging_PropagatesClientTraceID 覆盖客户端已传 X-Trace-Id 时，
// 中间件应透传（而不是覆盖生成新的），便于跨服务/客户端-服务端链路追踪拼接。
func TestRequestLogging_PropagatesClientTraceID(t *testing.T) {
	router := newLoggingRouter()
	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("X-Trace-Id", "client-supplied-trace-id-123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got := w.Header().Get("X-Trace-Id"); got != "client-supplied-trace-id-123" {
		t.Fatalf("expected trace_id to be propagated unchanged, got %q", got)
	}
	if !jsonContains(w.Body.String(), "client-supplied-trace-id-123") {
		t.Fatalf("expected handler context to see the propagated trace_id, got %s", w.Body.String())
	}
}

// TestRequestLogging_DoesNotBlockRequest 覆盖中间件不影响正常请求的状态码传递
// （防止日志记录逻辑意外吞掉/篡改后续 handler 的响应）。
func TestRequestLogging_DoesNotBlockRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RequestLogging())
	r.GET("/not-found-case", func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not_found"})
	})

	req := httptest.NewRequest("GET", "/not-found-case", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected handler's own status code 404 to pass through, got %d", w.Code)
	}
}
