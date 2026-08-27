package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// testRedisClient 返回真实 Redis 客户端用于登录限流中间件测试（与 repository 层
// 测试保持一致的策略：连真实 Redis 而非 mock，因为限流逻辑的正确性依赖 Redis
// 真实的 INCR/EXPIRE 原子语义，mock 容易与真实行为脱节）。连接失败时跳过。
func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("skip login rate limiter test: cannot connect to test redis (%v)", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newLoginRateLimiterRouter(client *redis.Client, limit int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/login", LoginRateLimiter(client, limit), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

// uniqueTestIP 为每个子测试生成一个唯一的伪造客户端 IP（通过 X-Forwarded-For 生效，
// 需配合 gin 的 TrustedProxies 配置；这里直接用 RemoteAddr 更简单可靠），避免不同
// 测试用例之间共享同一个 Redis 限流计数器 key 造成互相干扰。
var testIPCounter int

func nextTestIP() string {
	testIPCounter++
	return "10.0.0." + string(rune('0'+testIPCounter%10)) + ":12345"
}

// TestLoginRateLimiter_AllowsWithinLimit 覆盖未超过阈值时正常放行。
func TestLoginRateLimiter_AllowsWithinLimit(t *testing.T) {
	client := testRedisClient(t)
	router := newLoginRateLimiterRouter(client, 3)
	ip := nextTestIP()
	t.Cleanup(func() { _ = client.Del(context.Background(), "login_rate:"+ipOnly(ip)) })

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/login", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 within limit, got %d", i+1, w.Code)
		}
	}
}

// TestLoginRateLimiter_BlocksOverLimit 覆盖超过阈值后返回 429（能力补齐项核心行为）。
func TestLoginRateLimiter_BlocksOverLimit(t *testing.T) {
	client := testRedisClient(t)
	router := newLoginRateLimiterRouter(client, 2)
	ip := nextTestIP()
	t.Cleanup(func() { _ = client.Del(context.Background(), "login_rate:"+ipOnly(ip)) })

	var lastCode int
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/login", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		lastCode = w.Code
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding limit of 2, got %d on final request", lastCode)
	}
}

// TestLoginRateLimiter_DisabledWhenLimitNonPositive 覆盖 limit<=0 时完全不限流
// （配置显式关闭该能力的场景）。
func TestLoginRateLimiter_DisabledWhenLimitNonPositive(t *testing.T) {
	client := testRedisClient(t)
	router := newLoginRateLimiterRouter(client, 0)
	ip := nextTestIP()

	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("POST", "/login", nil)
		req.RemoteAddr = ip
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200 (rate limiting disabled), got %d", i+1, w.Code)
		}
	}
}

// TestLoginRateLimiter_DifferentIPs_IndependentCounters 覆盖限流按 IP 独立计数，
// 一个 IP 被限流不应影响另一个 IP。
func TestLoginRateLimiter_DifferentIPs_IndependentCounters(t *testing.T) {
	client := testRedisClient(t)
	router := newLoginRateLimiterRouter(client, 1)
	ipA := nextTestIP()
	ipB := nextTestIP()
	t.Cleanup(func() {
		_ = client.Del(context.Background(), "login_rate:"+ipOnly(ipA))
		_ = client.Del(context.Background(), "login_rate:"+ipOnly(ipB))
	})

	reqA1 := httptest.NewRequest("POST", "/login", nil)
	reqA1.RemoteAddr = ipA
	wA1 := httptest.NewRecorder()
	router.ServeHTTP(wA1, reqA1)
	if wA1.Code != http.StatusOK {
		t.Fatalf("ipA first request: expected 200, got %d", wA1.Code)
	}

	reqA2 := httptest.NewRequest("POST", "/login", nil)
	reqA2.RemoteAddr = ipA
	wA2 := httptest.NewRecorder()
	router.ServeHTTP(wA2, reqA2)
	if wA2.Code != http.StatusTooManyRequests {
		t.Fatalf("ipA second request: expected 429, got %d", wA2.Code)
	}

	// ipB 是全新 IP，即使 ipA 已被限流，ipB 仍应正常放行。
	reqB1 := httptest.NewRequest("POST", "/login", nil)
	reqB1.RemoteAddr = ipB
	wB1 := httptest.NewRecorder()
	router.ServeHTTP(wB1, reqB1)
	if wB1.Code != http.StatusOK {
		t.Fatalf("ipB first request: expected 200 (independent counter from ipA), got %d", wB1.Code)
	}
}

func ipOnly(remoteAddr string) string {
	for i := len(remoteAddr) - 1; i >= 0; i-- {
		if remoteAddr[i] == ':' {
			return remoteAddr[:i]
		}
	}
	return remoteAddr
}
