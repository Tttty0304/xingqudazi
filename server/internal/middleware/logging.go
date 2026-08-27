package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	logger "xingqudazi-im/server/pkg/log"
	"xingqudazi-im/server/pkg/metric"
)

// RequestLogging 为每个 HTTP 请求生成/透传 trace_id，并在请求开始与结束时打印
// 结构化日志（方法/路径/状态码/耗时/trace_id），覆盖「入口必须打印日志」的硬性要求。
func RequestLogging() gin.HandlerFunc {
	return func(c *gin.Context) {
		traceID := c.GetHeader("X-Trace-Id")
		if traceID == "" {
			traceID = uuid.NewString()
		}
		ctx := logger.WithTraceID(c.Request.Context(), traceID)
		c.Request = c.Request.WithContext(ctx)
		c.Writer.Header().Set("X-Trace-Id", traceID)

		start := time.Now()
		metric.Global.IncHTTPRequests()

		log := logger.FromContext(ctx)
		log.Info("http_request_start",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
		)

		c.Next()

		log.Info("http_request_end",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}
