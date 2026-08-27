package repository

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// RedisRoomOnlineCounter 实现 service.OnlineCounter：房间在线人数以
// Redis Set `room:{id}:online_users` 的元素个数表示（键结构见 Plan Part3）。
// Task4 起，WS Gateway 会在 join_room/leave_room 时维护这个 Set；
// 在此之前（Task3 阶段）该 Set 通常为空，Count 返回 0，这是符合预期的真实状态，
// 不是伪造数据。
type RedisRoomOnlineCounter struct {
	client *redis.Client
}

func NewRedisRoomOnlineCounter(client *redis.Client) *RedisRoomOnlineCounter {
	return &RedisRoomOnlineCounter{client: client}
}

func (c *RedisRoomOnlineCounter) Count(ctx context.Context, roomID string) (int64, error) {
	key := fmt.Sprintf("room:%s:online_users", roomID)
	count, err := c.client.SCard(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("scard %s: %w", key, err)
	}
	return count, nil
}
