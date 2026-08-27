package api

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"xingqudazi-im/server/internal/repository"
)

// testPostgresPool / testRedisClient 与 repository 层测试用的同名工具保持一致的
// 策略：连真实 Docker Compose 中的 Postgres/Redis，不可达时跳过而非用 mock 制造
// 假成功（health.go 的 Readyz 逻辑本身就是"真实探测依赖"，mock 掉依赖会让测试
// 失去意义）。
func testPostgresPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://im_user:im_password@localhost:5432/xingqudazi_im?sslmode=disable"
	}
	pool, err := repository.NewPostgresPool(context.Background(), dsn, 5, 1)
	if err != nil {
		t.Skipf("skip api test: cannot connect to test postgres (%v)", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("skip api test: cannot connect to test redis (%v)", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// brokenRedisClient 返回一个指向不存在地址的 Redis 客户端，用于模拟"Redis 不可达"
// 场景（T03），不需要真实断网/停容器就能可靠复现。
func brokenRedisClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // 保留端口，本地不会有真实服务监听
		DialTimeout: 200 * time.Millisecond,
	})
}
