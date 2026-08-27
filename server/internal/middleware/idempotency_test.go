package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestIdempotencyKeyPattern(t *testing.T) {
	for _, key := range []string{"replay-001", "abc_def.123", "12345678"} {
		if !idempotencyKeyPattern.MatchString(key) {
			t.Errorf("expected %q to be valid", key)
		}
	}
	for _, key := range []string{"short", "contains space", "", "bad/slash"} {
		if idempotencyKeyPattern.MatchString(key) {
			t.Errorf("expected %q to be invalid", key)
		}
	}
}

// 使用真实 Redis 验证“第一次执行、第二次原样重放、不再次执行业务处理器”。环境
// 不提供 Redis 时按项目已有约定跳过，不用 mock 伪造原子 SetNX 语义。
func TestIdempotencyReplaysSuccessfulCommand(t *testing.T) {
	redisClient := testRedisClient(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	count := 0
	r.POST("/command", Idempotency(redisClient, func(*gin.Context) string { return "test-user" }), func(c *gin.Context) {
		count++
		c.JSON(http.StatusCreated, gin.H{"run": count})
	})
	key := "replay-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/command", nil)
		req.Header.Set(IdempotencyKeyHeader, key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated || w.Body.String() != `{"run":1}` {
			t.Fatalf("run %d got %d %s", i, w.Code, w.Body.String())
		}
	}
	if count != 1 {
		t.Fatalf("handler executed %d times, want 1", count)
	}
}

func TestIdempotencyRejectsConcurrentReplay(t *testing.T) {
	redisClient := testRedisClient(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	entered := make(chan struct{})
	release := make(chan struct{})
	r.POST("/command", Idempotency(redisClient, func(*gin.Context) string { return "test-user" }), func(c *gin.Context) {
		close(entered)
		<-release
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	key := "concurrent-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/command", nil)
		req.Header.Set(IdempotencyKeyHeader, key)
		r.ServeHTTP(w, req)
		firstDone <- w
	}()
	<-entered

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/command", nil)
	secondReq.Header.Set(IdempotencyKeyHeader, key)
	r.ServeHTTP(second, secondReq)
	if second.Code != http.StatusConflict || second.Body.String() != `{"error":"idempotency_in_progress"}` {
		t.Fatalf("concurrent replay got %d %s", second.Code, second.Body.String())
	}

	close(release)
	if first := <-firstDone; first.Code != http.StatusCreated {
		t.Fatalf("first request got %d %s", first.Code, first.Body.String())
	}
}

func TestIdempotencyAllowsRetryAfterFailedCommand(t *testing.T) {
	redisClient := testRedisClient(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	attempts := 0
	r.POST("/command", Idempotency(redisClient, func(*gin.Context) string { return "test-user" }), func(c *gin.Context) {
		attempts++
		if attempts == 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_command"})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"attempt": attempts})
	})

	key := "retry-after-error-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	for i, wantCode := range []int{http.StatusBadRequest, http.StatusCreated} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/command", nil)
		req.Header.Set(IdempotencyKeyHeader, key)
		r.ServeHTTP(w, req)
		if w.Code != wantCode {
			t.Fatalf("request %d got %d %s, want %d", i+1, w.Code, w.Body.String(), wantCode)
		}
	}
	if attempts != 2 {
		t.Fatalf("handler executed %d times, want retry after failure", attempts)
	}
}

func TestIdempotencyFailsClosedWhenRedisUnavailable(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 50 * time.Millisecond,
		MaxRetries:  -1,
	})
	t.Cleanup(func() { _ = redisClient.Close() })
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/command", Idempotency(redisClient, func(*gin.Context) string { return "test-user" }), func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"should_not": "run"})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/command", nil)
	req.Header.Set(IdempotencyKeyHeader, "redis-unavailable-"+strconv.FormatInt(time.Now().UnixNano(), 10))
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != `{"error":"idempotency_unavailable"}` {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if err := redisClient.Ping(context.Background()).Err(); err == nil {
		t.Fatal("test address unexpectedly has Redis")
	}
}
