package middleware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Idempotency-Key 是所有会产生业务副作用的 HTTP 命令可选支持的重放令牌。相同
// 调用方、方法、路径和 key 会收到第一次成功执行的原始响应，不会再次写库、发通知
// 或落盘。未携带该头时仍保留各业务自身的幂等保护（如 msg_id、upsert）。
const IdempotencyKeyHeader = "Idempotency-Key"

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,128}$`)

type idempotencyRecord struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
}

type responseCapture struct {
	gin.ResponseWriter
	body []byte
}

func (w *responseCapture) Write(data []byte) (int, error) {
	w.body = append(w.body, data...)
	return w.ResponseWriter.Write(data)
}
func (w *responseCapture) WriteString(s string) (int, error) {
	w.body = append(w.body, s...)
	return w.ResponseWriter.WriteString(s)
}

func Idempotency(redisClient *redis.Client, scope func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader(IdempotencyKeyHeader)
		if key == "" {
			c.Next()
			return
		}
		if !idempotencyKeyPattern.MatchString(key) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid_idempotency_key"})
			return
		}
		redisKey := "idempotency:" + scope(c) + ":" + c.Request.Method + ":" + c.FullPath() + ":" + key
		ctx := c.Request.Context()
		claimed, err := redisClient.SetNX(ctx, redisKey, "pending", 24*time.Hour).Result()
		if err != nil { // fail closed: an untracked replay may duplicate an irreversible command.
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "idempotency_unavailable"})
			return
		}
		if !claimed {
			stored, getErr := redisClient.Get(ctx, redisKey).Result()
			if getErr != nil || stored == "pending" {
				c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "idempotency_in_progress"})
				return
			}
			var record idempotencyRecord
			if json.Unmarshal([]byte(stored), &record) != nil {
				c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "idempotency_in_progress"})
				return
			}
			body, _ := base64.StdEncoding.DecodeString(record.Body)
			c.Data(record.Status, "application/json; charset=utf-8", body)
			c.Abort()
			return
		}
		writer := &responseCapture{ResponseWriter: c.Writer}
		c.Writer = writer
		c.Next()
		if c.Writer.Status() >= 200 && c.Writer.Status() < 300 {
			record, _ := json.Marshal(idempotencyRecord{Status: c.Writer.Status(), Body: base64.StdEncoding.EncodeToString(writer.body)})
			_ = redisClient.Set(context.Background(), redisKey, record, 24*time.Hour).Err()
		} else {
			_ = redisClient.Del(context.Background(), redisKey).Err()
		}
	}
}
