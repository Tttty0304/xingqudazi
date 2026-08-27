package repository

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// testRedis 返回真实 Redis 客户端，用于 RedisTokenBlacklist 的真实往返测试
// （能力补齐项新增代码，理应有真实测试覆盖，而不是只靠人工 curl 验证）。
// 通过 TEST_REDIS_ADDR 环境变量指定测试 Redis 地址（未设置时回落到与
// `deploy/docker-compose.yml` 默认配置一致的本地地址）；连接失败时跳过。
func testRedis(t *testing.T) *redis.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		t.Skipf("skip redis test: cannot connect to test redis (%v); "+
			"start `docker compose -f deploy/docker-compose.yml up -d redis` "+
			"or set TEST_REDIS_ADDR to point at a reachable Redis instance", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRedisTokenBlacklist_AddAndIsBlacklisted(t *testing.T) {
	client := testRedis(t)
	blacklist := NewRedisTokenBlacklist(client)
	ctx := context.Background()

	token := "test-token-" + time.Now().Format("20060102150405.000000000")
	t.Cleanup(func() { _ = client.Del(ctx, tokenBlacklistKey(token)) })

	blacklisted, err := blacklist.IsBlacklisted(ctx, token)
	if err != nil {
		t.Fatalf("IsBlacklisted (before Add) failed: %v", err)
	}
	if blacklisted {
		t.Fatal("expected token to not be blacklisted before Add is called")
	}

	if err := blacklist.Add(ctx, token, time.Minute); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	blacklisted, err = blacklist.IsBlacklisted(ctx, token)
	if err != nil {
		t.Fatalf("IsBlacklisted (after Add) failed: %v", err)
	}
	if !blacklisted {
		t.Fatal("expected token to be blacklisted after Add is called")
	}
}

// TestRedisTokenBlacklist_Add_SkipsWhenTTLNonPositive 验证 ttl<=0（token 已经
// 过期）时不写入任何数据，避免产生毫无意义的 Redis key。
func TestRedisTokenBlacklist_Add_SkipsWhenTTLNonPositive(t *testing.T) {
	client := testRedis(t)
	blacklist := NewRedisTokenBlacklist(client)
	ctx := context.Background()

	token := "test-token-expired-" + time.Now().Format("20060102150405.000000000")
	t.Cleanup(func() { _ = client.Del(ctx, tokenBlacklistKey(token)) })

	if err := blacklist.Add(ctx, token, 0); err != nil {
		t.Fatalf("Add with ttl=0 should not error, got: %v", err)
	}

	exists, err := client.Exists(ctx, tokenBlacklistKey(token)).Result()
	if err != nil {
		t.Fatalf("Exists check failed: %v", err)
	}
	if exists != 0 {
		t.Fatal("expected no key to be written when ttl<=0")
	}
}
