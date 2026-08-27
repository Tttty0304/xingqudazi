package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

func newUserTestRouter() (*gin.Engine, *fakeUserStore) {
	gin.SetMode(gin.TestMode)
	store := newFakeUserStore()
	svc := service.NewUserService(store)
	h := &UserHandler{UserService: svc}

	r := gin.New()
	r.Use(fakeAuthMiddleware("alice"))
	r.GET("/api/users/lookup", h.Lookup)
	r.GET("/api/users", h.BatchGet)
	r.GET("/api/me/profile", func(c *gin.Context) { c.Set("auth_user_id", "me"); h.GetMyProfile(c) })
	r.PUT("/api/me/profile", func(c *gin.Context) { c.Set("auth_user_id", "me"); h.UpdateMyProfile(c) })
	return r, store
}

func TestProfile_GetAndUpdate(t *testing.T) {
	router, store := newUserTestRouter()
	store.put(&model.User{ID: "me", Username: "alice"})

	update := httptest.NewRequest("PUT", "/api/me/profile", strings.NewReader(`{"avatar_url":"/uploads/avatar.png","bio":"喜欢桌游"}`))
	update.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	router.ServeHTTP(updateW, update)
	if updateW.Code != http.StatusOK || !contains(updateW.Body.String(), `"bio":"喜欢桌游"`) {
		t.Fatalf("expected profile update, got code=%d body=%s", updateW.Code, updateW.Body.String())
	}

	getW := httptest.NewRecorder()
	router.ServeHTTP(getW, httptest.NewRequest("GET", "/api/me/profile", nil))
	if getW.Code != http.StatusOK || !contains(getW.Body.String(), `"avatar_url":"/uploads/avatar.png"`) {
		t.Fatalf("expected profile get, got code=%d body=%s", getW.Code, getW.Body.String())
	}
}

// TestUserLookup_Success 覆盖按用户名查找用户成功。
func TestUserLookup_Success(t *testing.T) {
	router, store := newUserTestRouter()
	store.put(&model.User{ID: "bob-id", Username: "bob"})

	req := httptest.NewRequest("GET", "/api/users/lookup?username=bob", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"id":"bob-id"`) {
		t.Fatalf("expected id=bob-id, got %s", w.Body.String())
	}
}

// TestUserLookup_NotFound 覆盖用户名不存在返回 404。
func TestUserLookup_NotFound(t *testing.T) {
	router, _ := newUserTestRouter()

	req := httptest.NewRequest("GET", "/api/users/lookup?username=no_such_user", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"user_not_found"`) {
		t.Fatalf("expected user_not_found, got %s", w.Body.String())
	}
}

// TestUserLookup_EmptyUsername 覆盖 username 查询参数为空时的 400。
func TestUserLookup_EmptyUsername(t *testing.T) {
	router, _ := newUserTestRouter()

	req := httptest.NewRequest("GET", "/api/users/lookup?username=", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	if !contains(w.Body.String(), `"error":"invalid_request"`) {
		t.Fatalf("expected invalid_request, got %s", w.Body.String())
	}
}

// TestBatchGetUsers_IgnoresUnknownIDs 覆盖批量查询时未知/不存在的 ID 被静默忽略
// （不报错、不影响其它合法 ID 的查询结果）。
func TestBatchGetUsers_IgnoresUnknownIDs(t *testing.T) {
	router, store := newUserTestRouter()
	store.put(&model.User{ID: "id-1", Username: "user1"})

	req := httptest.NewRequest("GET", "/api/users?ids=id-1,no-such-id,id-2-also-missing", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"username":"user1"`) {
		t.Fatalf("expected user1 in response, got %s", w.Body.String())
	}
	if contains(w.Body.String(), "no-such-id") {
		t.Fatalf("expected unknown ID to not appear in response at all, got %s", w.Body.String())
	}
}

// TestBatchGetUsers_EmptyIDs 覆盖 ids 查询参数为空时返回空数组而非报错。
func TestBatchGetUsers_EmptyIDs(t *testing.T) {
	router, _ := newUserTestRouter()

	req := httptest.NewRequest("GET", "/api/users?ids=", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "[]" {
		t.Fatalf("expected empty array response, got %s", w.Body.String())
	}
}
