package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newCORSRouter(allowedOrigins []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORS(allowedOrigins))
	r.GET("/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return r
}

// TestCORS_DefaultAllowAll 覆盖开发默认行为：为使 HttpOnly Cookie 能随跨端口
// 请求发送，必须反射 Origin 并允许凭据，不能和通配符 `*` 同时使用。
func TestCORS_DefaultAllowAll(t *testing.T) {
	router := newCORSRouter(nil)
	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "https://random-untrusted-site.example")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://random-untrusted-site.example" {
		t.Fatalf("expected reflected origin, got %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("expected credentials support, got %q", got)
	}
}

// TestCORS_ExplicitWildcard 覆盖显式传入 ["*"] 时同样完全放开。
func TestCORS_ExplicitWildcard(t *testing.T) {
	router := newCORSRouter([]string{"*"})
	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "https://anything.example")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.example" {
		t.Fatalf("expected reflected origin, got %q", got)
	}
}

// TestCORS_WhitelistedOrigin_Reflected 覆盖白名单命中场景：应反射该 Origin 并带 Vary 头。
func TestCORS_WhitelistedOrigin_Reflected(t *testing.T) {
	router := newCORSRouter([]string{"https://chat.example.com"})
	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "https://chat.example.com")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://chat.example.com" {
		t.Fatalf("expected reflected origin, got %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("expected Vary: Origin header, got %q", got)
	}
}

// TestCORS_NonWhitelistedOrigin_NotReflected 覆盖白名单未命中场景（能力补齐项的核心安全行为）：
// 不在白名单内的 Origin 不应该被反射到响应头，浏览器会因此拒绝读取响应。
func TestCORS_NonWhitelistedOrigin_NotReflected(t *testing.T) {
	router := newCORSRouter([]string{"https://chat.example.com"})
	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no Access-Control-Allow-Origin header for non-whitelisted origin, got %q", got)
	}
}

// TestCORS_OPTIONS_Preflight 覆盖预检请求被直接短路返回 204，不进入后续 handler。
func TestCORS_OPTIONS_Preflight(t *testing.T) {
	router := newCORSRouter([]string{"*"})
	req := httptest.NewRequest("OPTIONS", "/ping", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS preflight, got %d", w.Code)
	}
}

// TestCORS_MethodsAndHeaders_AlwaysSet 覆盖 Allow-Methods/Allow-Headers 始终被设置，
// 不受白名单命中与否影响。
func TestCORS_MethodsAndHeaders_AlwaysSet(t *testing.T) {
	router := newCORSRouter([]string{"https://chat.example.com"})
	req := httptest.NewRequest("GET", "/ping", nil)
	req.Header.Set("Origin", "https://not-whitelisted.example")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected Access-Control-Allow-Methods to always be set")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("expected Access-Control-Allow-Headers to always be set")
	}
	if got := w.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "Idempotency-Key") {
		t.Fatalf("expected Idempotency-Key to be allowed for safe command replay, got %q", got)
	}
}

// TestCORS_NoOriginHeader 覆盖同源请求（未带 Origin 头，如服务端到服务端调用/curl）的场景，
// 不应因为没有 Origin 头而 panic 或产生异常行为。
func TestCORS_NoOriginHeader(t *testing.T) {
	router := newCORSRouter([]string{"https://chat.example.com"})
	req := httptest.NewRequest("GET", "/ping", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
