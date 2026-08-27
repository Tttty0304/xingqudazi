package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/service"
)

func newAuthTestRouter(allowGuest bool) (*gin.Engine, *service.AuthService) {
	gin.SetMode(gin.TestMode)
	store := newFakeUserStore()
	tokenSvc := service.NewTokenService("test-secret", time.Hour)
	authSvc := service.NewAuthService(store, tokenSvc, allowGuest)
	h := &AuthHandler{AuthService: authSvc}

	r := gin.New()
	r.POST("/api/auth/register", h.Register)
	r.POST("/api/auth/login", h.Login)
	r.POST("/api/auth/guest", h.Guest)
	r.POST("/api/auth/logout", h.Logout)
	r.GET("/api/auth/session", func(c *gin.Context) {
		c.Set("auth_user_id", "session-user")
		c.Set("auth_is_guest", false)
		h.Session(c)
	})
	return r, authSvc
}

func doJSONRequest(router *gin.Engine, method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// TestAuthRegister_Success 覆盖 T10：合法用户名+密码注册成功，返回 201。
func TestAuthRegister_Success(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	w := doJSONRequest(router, "POST", "/api/auth/register", `{"username":"alice_h1","password":"abcd1234"}`, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"username":"alice_h1"`) {
		t.Fatalf("expected username echoed back, got %s", w.Body.String())
	}
}

// TestAuthRegister_InvalidRequestBody 覆盖缺少必填字段（binding:"required"）时的 400。
func TestAuthRegister_InvalidRequestBody(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	w := doJSONRequest(router, "POST", "/api/auth/register", `{"username":"onlyusername"}`, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"invalid_request_body"`) {
		t.Fatalf("expected invalid_request_body, got %s", w.Body.String())
	}
}

// TestAuthRegister_InvalidUsername 覆盖用户名格式不合法（如含特殊字符/过短）时的
// 400 invalid_username 映射（对应 service.ErrInvalidUsername）。
func TestAuthRegister_InvalidUsername(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	w := doJSONRequest(router, "POST", "/api/auth/register", `{"username":"a","password":"abcd1234"}`, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"invalid_username"`) {
		t.Fatalf("expected invalid_username, got %s", w.Body.String())
	}
}

// TestAuthRegister_InvalidPassword 覆盖密码复杂度校验失败（能力补齐项：纯数字密码）
// 时的 400 invalid_password 映射。
func TestAuthRegister_InvalidPassword(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	w := doJSONRequest(router, "POST", "/api/auth/register", `{"username":"bob_h1","password":"12345678"}`, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"invalid_password"`) {
		t.Fatalf("expected invalid_password, got %s", w.Body.String())
	}
}

// TestAuthRegister_UsernameTaken 覆盖重复用户名注册时的 400 username_taken。
func TestAuthRegister_UsernameTaken(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	first := doJSONRequest(router, "POST", "/api/auth/register", `{"username":"carol_h1","password":"abcd1234"}`, nil)
	if first.Code != http.StatusCreated {
		t.Fatalf("first registration should succeed, got %d", first.Code)
	}

	second := doJSONRequest(router, "POST", "/api/auth/register", `{"username":"carol_h1","password":"efgh5678"}`, nil)
	if second.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate username, got %d", second.Code)
	}
	if !contains(second.Body.String(), `"error":"username_taken"`) {
		t.Fatalf("expected username_taken, got %s", second.Body.String())
	}
}

// TestAuthLogin_Success 覆盖注册后立即登录成功，返回 token+user_id。
func TestAuthLogin_Success(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	doJSONRequest(router, "POST", "/api/auth/register", `{"username":"dave_h1","password":"abcd1234"}`, nil)

	w := doJSONRequest(router, "POST", "/api/auth/login", `{"username":"dave_h1","password":"abcd1234"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"token":"`) {
		t.Fatalf("expected token in response, got %s", w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Name != "im_session" {
		t.Fatalf("expected HttpOnly im_session cookie, got %+v", cookies)
	}
}

func TestAuthSession_ReturnsAuthenticatedIdentity(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	w := doJSONRequest(router, "GET", "/api/auth/session", "", nil)
	if w.Code != http.StatusOK || !contains(w.Body.String(), `"user_id":"session-user"`) {
		t.Fatalf("expected session identity, got code=%d body=%s", w.Code, w.Body.String())
	}
}

// TestAuthLogin_WrongPassword_And_UnknownUser_SameErrorCode 覆盖 T14 硬性要求：
// 用户不存在与密码错误必须归并为同一个 invalid_credentials，不能让攻击者通过
// 错误码区分"用户名是否存在"（防止用户名枚举）。
func TestAuthLogin_WrongPassword_And_UnknownUser_SameErrorCode(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	doJSONRequest(router, "POST", "/api/auth/register", `{"username":"erin_h1","password":"abcd1234"}`, nil)

	wrongPassword := doJSONRequest(router, "POST", "/api/auth/login", `{"username":"erin_h1","password":"wrongpass1"}`, nil)
	unknownUser := doJSONRequest(router, "POST", "/api/auth/login", `{"username":"no_such_user_h1","password":"whatever1"}`, nil)

	if wrongPassword.Code != http.StatusUnauthorized || unknownUser.Code != http.StatusUnauthorized {
		t.Fatalf("expected both to be 401, got wrong_password=%d unknown_user=%d", wrongPassword.Code, unknownUser.Code)
	}
	if wrongPassword.Body.String() != unknownUser.Body.String() {
		t.Fatalf("expected identical error body to prevent username enumeration, got %q vs %q",
			wrongPassword.Body.String(), unknownUser.Body.String())
	}
	if !contains(wrongPassword.Body.String(), `"error":"invalid_credentials"`) {
		t.Fatalf("expected invalid_credentials, got %s", wrongPassword.Body.String())
	}
}

// TestAuthGuest_Success 覆盖访客模式登录成功（is_guest=true）。
func TestAuthGuest_Success(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	w := doJSONRequest(router, "POST", "/api/auth/guest", `{}`, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"is_guest":true`) {
		t.Fatalf("expected is_guest=true, got %s", w.Body.String())
	}
}

// TestAuthGuest_Disabled 覆盖访客模式被配置关闭时返回 403 guest_mode_disabled。
func TestAuthGuest_Disabled(t *testing.T) {
	router, _ := newAuthTestRouter(false)
	w := doJSONRequest(router, "POST", "/api/auth/guest", `{}`, nil)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"guest_mode_disabled"`) {
		t.Fatalf("expected guest_mode_disabled, got %s", w.Body.String())
	}
}

// TestAuthLogout_AlwaysReturns200 覆盖登出接口的幂等设计：即使没有配置黑名单
// （blacklist=nil，AuthService.Logout 静默跳过），登出请求仍应返回 200，
// 不应因为"没有真正撤销 token"而向前端暴露内部实现细节报错。
func TestAuthLogout_AlwaysReturns200(t *testing.T) {
	router, _ := newAuthTestRouter(true)
	regResp := doJSONRequest(router, "POST", "/api/auth/register", `{"username":"frank_h1","password":"abcd1234"}`, nil)
	if regResp.Code != http.StatusCreated {
		t.Fatalf("registration failed: %d", regResp.Code)
	}
	loginResp := doJSONRequest(router, "POST", "/api/auth/login", `{"username":"frank_h1","password":"abcd1234"}`, nil)

	var token string
	body := loginResp.Body.String()
	idx := indexOf(body, `"token":"`)
	if idx < 0 {
		t.Fatalf("no token in login response: %s", body)
	}
	start := idx + len(`"token":"`)
	end := start
	for end < len(body) && body[end] != '"' {
		end++
	}
	token = body[start:end]

	w := doJSONRequest(router, "POST", "/api/auth/logout", `{}`, map[string]string{"Authorization": "Bearer " + token})
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
