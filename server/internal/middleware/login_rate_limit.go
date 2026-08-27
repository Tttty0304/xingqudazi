package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	logger "xingqudazi-im/server/pkg/log"
)

// LoginRateLimiter 是登录接口的暴力破解防护（能力补齐项，此前 `/api/auth/login`
// 只做用户名密码校验，没有任何频率限制——WS 消息发送在 Task6 就有双层限流，登录
// 接口反而完全没有，攻击者可以对同一账号无限次尝试猜密码，是真实的安全缺口）。
//
// 按客户端 IP 做固定窗口限流（Redis INCR + 首次命中时 EXPIRE 60s），超过阈值直接
// 拒绝、不再进入真实的用户名密码校验逻辑。用 Redis 而非进程内 map 计数，是为了与
// 项目既有架构保持一致：Redis 已是强制依赖（★3 多实例横向扩展设计），进程内计数器
// 在多实例部署下会各自独立计数、防护形同虚设，Redis 计数天然可在实例间共享。
//
// 失败开放（fail-open）：Redis 出错时放行请求但记录错误日志，避免限流器自身故障
// 导致核心登录功能不可用——Redis 已是 `/readyz` 的依赖项，真实故障时整体已处于
// not-ready 状态，这里的 fail-open 只是"不让限流器成为额外的单点故障放大器"。
func LoginRateLimiter(redisClient *redis.Client, limitPerMinute int) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limitPerMinute <= 0 {
			c.Next()
			return
		}

		log := logger.FromContext(c.Request.Context())
		key := fmt.Sprintf("login_rate:%s", c.ClientIP())

		count, err := redisClient.Incr(c.Request.Context(), key).Result()
		if err != nil {
			log.Error("login rate limiter redis incr failed, failing open", "error", err)
			c.Next()
			return
		}
		if count == 1 {
			// 首次命中该窗口，设置过期时间形成固定窗口。这不是最精确的滑动窗口算法
			// （窗口边界附近可能出现 2 倍突发），但实现简单、无额外依赖，对"拦截高频
			// 暴力破解"这个目标已经足够；`Expire` 失败不影响本次放行/拒绝判断，
			// 只记录日志（最坏情况是这个 key 不会自动过期，下次请求仍会走到这里
			// 重新尝试设置）。
			if expErr := redisClient.Expire(c.Request.Context(), key, time.Minute).Err(); expErr != nil {
				log.Error("login rate limiter set expire failed", "error", expErr)
			}
		}

		if int(count) > limitPerMinute {
			c.JSON(http.StatusTooManyRequests, gin.H{"error": "login_rate_limited"})
			c.Abort()
			return
		}
		c.Next()
	}
}
