package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/service"
)

func newTestTokenService() *service.TokenService {
	return service.NewTokenService("test-secret-for-middleware", time.Hour)
}

var errRedisDown = errors.New("redis down")

func newRouterWithAuth(tokenSvc *service.TokenService, blacklist TokenBlacklist) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/protected", RequireAuth(tokenSvc, blacklist), func(c *gin.Context) {
		userID, ok := UserIDFromContext(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "no_user_id_in_context"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"user_id": userID})
	})
	return r
}

// TestRequireAuth_MissingToken 覆盖没有 Authorization 头的场景。
func TestRequireAuth_MissingToken(t *testing.T) {
	router := newRouterWithAuth(newTestTokenService(), nil)
	req := httptest.NewRequest("GET", "/protected", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if !jsonContains(w.Body.String(), `"error":"missing_token"`) {
		t.Fatalf("expected missing_token error, got %s", w.Body.String())
	}
}

// TestRequireAuth_MalformedHeader 覆盖 Authorization 头存在但不是 "Bearer " 前缀的场景。
func TestRequireAuth_MalformedHeader(t *testing.T) {
	router := newRouterWithAuth(newTestTokenService(), nil)
	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Basic abcdef")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestRequireAuth_InvalidToken 覆盖 token 格式错误/签名不合法的场景。
func TestRequireAuth_InvalidToken(t *testing.T) {
	router := newRouterWithAuth(newTestTokenService(), nil)
	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if !jsonContains(w.Body.String(), `"error":"invalid_token"`) {
		t.Fatalf("expected invalid_token error, got %s", w.Body.String())
	}
}

// TestRequireAuth_ValidToken_NoBlacklist 覆盖最基本的成功路径：blacklist=nil
// 时（未启用登出黑名单能力）鉴权逻辑应与此前完全一致。
func TestRequireAuth_ValidToken_NoBlacklist(t *testing.T) {
	tokenSvc := newTestTokenService()
	token, err := tokenSvc.GenerateToken("user-123", false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	router := newRouterWithAuth(tokenSvc, nil)
	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if !jsonContains(w.Body.String(), `"user_id":"user-123"`) {
		t.Fatalf("expected user_id=user-123 in context, got %s", w.Body.String())
	}
}

// stubBlacklist 是可控制返回值的 TokenBlacklist 假实现（用真实 context.Context 签名）。
type stubBlacklist struct {
	blacklisted bool
	err         error
}

func (s *stubBlacklist) IsBlacklisted(_ context.Context, _ string) (bool, error) {
	return s.blacklisted, s.err
}

// TestRequireAuth_ValidToken_NotBlacklisted 覆盖注入了黑名单但该 token 未被登出的场景。
func TestRequireAuth_ValidToken_NotBlacklisted(t *testing.T) {
	tokenSvc := newTestTokenService()
	token, _ := tokenSvc.GenerateToken("user-456", false)

	router := newRouterWithAuth(tokenSvc, &stubBlacklist{blacklisted: false})
	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestRequireAuth_ValidToken_Blacklisted 覆盖能力补齐项的核心场景：已登出的 token
// 即使签名/格式仍然合法，也应被拒绝（401 token_revoked）。
func TestRequireAuth_ValidToken_Blacklisted(t *testing.T) {
	tokenSvc := newTestTokenService()
	token, _ := tokenSvc.GenerateToken("user-789", false)

	router := newRouterWithAuth(tokenSvc, &stubBlacklist{blacklisted: true})
	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if !jsonContains(w.Body.String(), `"error":"token_revoked"`) {
		t.Fatalf("expected token_revoked error, got %s", w.Body.String())
	}
}

// TestRequireAuth_BlacklistCheckFails_FailsOpen 覆盖失败开放策略：黑名单查询本身
// 出错（如 Redis 抖动）时，不应导致合法 token 的请求被拒绝。
func TestRequireAuth_BlacklistCheckFails_FailsOpen(t *testing.T) {
	tokenSvc := newTestTokenService()
	token, _ := tokenSvc.GenerateToken("user-fail-open", false)

	router := newRouterWithAuth(tokenSvc, &stubBlacklist{err: errRedisDown})
	req := httptest.NewRequest("GET", "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected fail-open to still allow request (200), got %d", w.Code)
	}
}

// TestUserIDFromContext_NotSet 覆盖未经过 RequireAuth 的 context 查询用户 ID 的场景。
func TestUserIDFromContext_NotSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	_, ok := UserIDFromContext(c)
	if ok {
		t.Fatal("expected ok=false when user id was never set in context")
	}
}

func jsonContains(body, substr string) bool {
	for i := 0; i+len(substr) <= len(body); i++ {
		if body[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
