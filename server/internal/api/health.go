package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// HealthHandler 提供 /healthz（进程存活）与 /readyz（依赖健康）两个端点，
// 对应 Testcase T01-T03。两者语义不同：
//   - /healthz：进程本身是否还活着（不依赖外部资源），用于容器存活探针；
//   - /readyz：进程 + 依赖（DB/Redis）是否都可以正常处理请求，用于流量接入判断。
type HealthHandler struct {
	DB    *pgxpool.Pool
	Redis *redis.Client
	// InstanceID 标识当前进程实例（能力补齐项：多实例部署下的可观测性 +
	// 支撑更丰富的部署/运行时测试）。此前架构文档一直声称"Redis Pub/Sub
	// 多实例扇出为强制设计"，但从未有任何手段能从外部区分"这个响应究竟是
	// 哪一个物理实例处理的"——负载均衡器背后到底真的分流到了几个实例、
	// 广播消息是否真的跨越了两个独立进程，此前都无法验证，只能停留在
	// "理论上应该是这样"。真实值来自 `os.Hostname()`（Docker 默认把容器 ID
	// 短哈希设为 hostname），未设置时留空不影响其它功能。
	InstanceID string
}

// Healthz godoc: GET /healthz
func (h *HealthHandler) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "instance_id": h.InstanceID})
}

// Readyz godoc: GET /readyz
// 对应 T02（依赖均健康）/T03（DB 或 Redis 不可达时返回 503 并明确指出故障组件）。
func (h *HealthHandler) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	dbStatus := "ok"
	if err := h.DB.Ping(ctx); err != nil {
		dbStatus = "error: " + err.Error()
	}

	redisStatus := "ok"
	if err := h.Redis.Ping(ctx).Err(); err != nil {
		redisStatus = "error: " + err.Error()
	}

	if dbStatus != "ok" || redisStatus != "ok" {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":      "not_ready",
			"db":          dbStatus,
			"redis":       redisStatus,
			"instance_id": h.InstanceID,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ready",
		"db":          dbStatus,
		"redis":       redisStatus,
		"instance_id": h.InstanceID,
	})
}
