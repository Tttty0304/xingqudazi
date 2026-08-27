package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisTokenBlacklist 实现"登出后 token 立即失效"（能力补齐项）。
//
// JWT 本身是无状态签名令牌，服务端默认无法撤销已签发的 token——此前"登出"
// 只是前端清空 localStorage，服务端在 token 自然过期（默认24小时）前依然
// 认可其合法性；如果 token 泄露，用户点"登出"实际上什么都没做。
//
// 用 Redis 存一个"已登出 token"黑名单：key 是 token 字符串本身，value 只是
// 占位符，TTL 设为该 token 距离自然过期的剩余时长——到期后 Redis 自动清理，
// 不需要额外的定时清理任务，黑名单大小也不会无限增长（同一时刻最多存在
// "全部活跃用户在 JWT_EXPIRY 时间窗口内登出过的 token 数量"这么多条目）。
type RedisTokenBlacklist struct {
	client *redis.Client
}

func NewRedisTokenBlacklist(client *redis.Client) *RedisTokenBlacklist {
	return &RedisTokenBlacklist{client: client}
}

func tokenBlacklistKey(token string) string {
	return "token_blacklist:" + token
}

// Add 把 token 加入黑名单。ttl 应为该 token 距离自然过期的剩余时长；
// ttl<=0（token 已经过期或即将过期）时不写入，避免产生毫无意义的数据。
func (b *RedisTokenBlacklist) Add(ctx context.Context, token string, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	if err := b.client.Set(ctx, tokenBlacklistKey(token), "1", ttl).Err(); err != nil {
		return fmt.Errorf("blacklist token: %w", err)
	}
	return nil
}

// IsBlacklisted 查询 token 是否已被登出。Redis 故障时按"未被拉黑"处理
// （失败开放，与 LoginRateLimiter 的 fail-open 策略保持一致：黑名单是登出
// 体验的增强能力，不应因为 Redis 抖动导致全部已登录用户被误判为登出状态；
// Redis 本身已是 `/readyz` 的强依赖，真实故障时整体已处于 not-ready）。
func (b *RedisTokenBlacklist) IsBlacklisted(ctx context.Context, token string) (bool, error) {
	n, err := b.client.Exists(ctx, tokenBlacklistKey(token)).Result()
	if err != nil {
		return false, fmt.Errorf("check token blacklist: %w", err)
	}
	return n > 0, nil
}
