package repository

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// presenceCountsKey 是全局用户在线态引用计数 Hash 的键名（Plan Part3 提及的
// `user:{id}:session` 概念在实现上收敛为一个 Hash，field=userID，value=当前活跃连接数，
// 避免单个用户多端同时在线/其中一端断开时被误判为离线）。
const presenceCountsKey = "presence:online_user_counts"

// RedisUserPresence 同时实现：
//   - ws.PresenceTracker（MarkOnline/MarkOffline，供 WS Hub 在连接建立/断开时调用）
//   - service.PresenceChecker（IsOnline，供 FriendService 查询好友在线态，T63）
//
// 两个接口分别定义在各自的包中（依赖倒置：谁使用就由谁定义接口），
// 本类型通过方法签名结构化匹配两者，无需相互引用 ws/service 包。
type RedisUserPresence struct {
	client *redis.Client
}

func NewRedisUserPresence(client *redis.Client) *RedisUserPresence {
	return &RedisUserPresence{client: client}
}

// MarkOnline 在用户建立一条新的 WS 连接时调用，引用计数 +1。
func (p *RedisUserPresence) MarkOnline(ctx context.Context, userID string) error {
	if err := p.client.HIncrBy(ctx, presenceCountsKey, userID, 1).Err(); err != nil {
		return fmt.Errorf("presence mark online: %w", err)
	}
	return nil
}

// MarkOffline 在用户的一条 WS 连接断开时调用，引用计数 -1；计数归零或以下时清理该字段，
// 避免 Hash 无限增长残留已下线用户的 0 值字段。
func (p *RedisUserPresence) MarkOffline(ctx context.Context, userID string) error {
	newVal, err := p.client.HIncrBy(ctx, presenceCountsKey, userID, -1).Result()
	if err != nil {
		return fmt.Errorf("presence mark offline: %w", err)
	}
	if newVal <= 0 {
		if err := p.client.HDel(ctx, presenceCountsKey, userID).Err(); err != nil {
			return fmt.Errorf("presence cleanup: %w", err)
		}
	}
	return nil
}

// IsOnline 查询用户当前是否有活跃 WS 连接（T63：好友在线态随连接实时变化）。
func (p *RedisUserPresence) IsOnline(ctx context.Context, userID string) (bool, error) {
	val, err := p.client.HGet(ctx, presenceCountsKey, userID).Int()
	if err != nil {
		if err == redis.Nil {
			return false, nil
		}
		return false, fmt.Errorf("presence is online: %w", err)
	}
	return val > 0, nil
}
