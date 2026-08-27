package api

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestHealthz 对应 Testcase T01：GET /healthz 应返回 200 + {"status":"ok"}。
// /healthz 不依赖任何外部资源，所以这里不需要真实的 DB/Redis 连接。
func TestHealthz(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &HealthHandler{}
	router.GET("/healthz", h.Healthz)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if body := w.Body.String(); body == "" || !contains(body, `"status":"ok"`) {
		t.Fatalf("expected body to contain status ok, got %s", body)
	}
}

// TestReadyz_MissingDependencies 覆盖 Readyz 在 DB/Redis 均未初始化（nil）时的行为。
// HealthHandler.DB/Redis 均为 nil 时调用其方法会 panic，因此这个测试的真实价值是
// 通过真实 Docker 环境（TEST_POSTGRES_DSN/TEST_REDIS_ADDR）验证 T02/T03 的两种分支，
// 与 repository 层测试保持一致的策略：真实依赖不可达时跳过而非用 nil 制造假成功。
func TestReadyz_AllHealthy(t *testing.T) {
	pool := testPostgresPool(t)
	redisClient := testRedisClient(t)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &HealthHandler{DB: pool, Redis: redisClient}
	router.GET("/readyz", h.Readyz)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200 when both deps healthy, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"status":"ready"`) {
		t.Fatalf("expected status=ready, got %s", w.Body.String())
	}
}

// TestReadyz_RedisDown 覆盖 T03：Redis 不可达时返回 503 并明确指出故障组件，
// 而 DB 仍然健康（用真实 Postgres + 一个指向不存在地址的 Redis 客户端模拟）。
func TestReadyz_RedisDown(t *testing.T) {
	pool := testPostgresPool(t)
	brokenRedis := brokenRedisClient()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &HealthHandler{DB: pool, Redis: brokenRedis}
	router.GET("/readyz", h.Readyz)

	req := httptest.NewRequest("GET", "/readyz", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Fatalf("expected 503 when redis unreachable, got %d, body=%s", w.Code, w.Body.String())
	}
	if !contains(w.Body.String(), `"status":"not_ready"`) {
		t.Fatalf("expected status=not_ready, got %s", w.Body.String())
	}
	if !contains(w.Body.String(), `"db":"ok"`) {
		t.Fatalf("expected db to still report ok while redis is down, got %s", w.Body.String())
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && indexOf(s, substr) >= 0
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
