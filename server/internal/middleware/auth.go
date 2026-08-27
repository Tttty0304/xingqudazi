package middleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/service"
	"xingqudazi-im/server/internal/session"
	logger "xingqudazi-im/server/pkg/log"
)

// contextKeyUserID / contextKeyIsGuest 是鉴权中间件写入 gin.Context 的键，
// 供后续 handler 通过 UserIDFromContext 读取当前请求身份。
const (
	contextKeyUserID  = "auth_user_id"
	contextKeyIsGuest = "auth_is_guest"
)

// TokenBlacklist 供 RequireAuth 查询"这个 token 是否已被登出"（能力补齐项，
// 真实实现见 repository.RedisTokenBlacklist）。定义在 middleware 包而非直接
// 依赖 repository 包，遵循本项目一致的依赖倒置风格（谁使用就由谁定义接口）。
type TokenBlacklist interface {
	IsBlacklisted(ctx context.Context, token string) (bool, error)
}

// RequireAuth 是 HTTP 接口的鉴权中间件：解析 `Authorization: Bearer <jwt>`，
// 校验通过后把 user_id/is_guest 写入 context；校验失败返回 401，不放行后续 handler。
// Task4 的 WebSocket 握手鉴权将复用同一个 TokenService.ParseToken，逻辑保持一致。
// blacklist 可为 nil（未启用登出黑名单能力时，鉴权逻辑与此前完全一致）。
func RequireAuth(tokenSvc *service.TokenService, blacklist TokenBlacklist) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenString := session.TokenFromRequest(c.Request)
		if tokenString == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing_token"})
			return
		}

		claims, err := tokenSvc.ParseToken(tokenString)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid_token"})
			return
		}

		if blacklist != nil {
			blacklisted, err := blacklist.IsBlacklisted(c.Request.Context(), tokenString)
			if err != nil {
				// 失败开放：黑名单查询本身故障不应导致全部已登录用户被拒绝，
				// 与 LoginRateLimiter 的 fail-open 策略保持一致，只记录日志。
				logger.FromContext(c.Request.Context()).Error("check token blacklist failed, failing open", "error", err)
			} else if blacklisted {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "token_revoked"})
				return
			}
		}

		c.Set(contextKeyUserID, claims.UserID)
		c.Set(contextKeyIsGuest, claims.IsGuest)
		c.Next()
	}
}

// UserIDFromContext 从已通过 RequireAuth 的请求 context 中取出当前用户 ID。
func UserIDFromContext(c *gin.Context) (string, bool) {
	v, ok := c.Get(contextKeyUserID)
	if !ok {
		return "", false
	}
	userID, ok := v.(string)
	return userID, ok
}

// IsGuestFromContext 返回已鉴权身份的访客标识，供会话恢复等只需读取 JWT claims
// 的处理器使用，避免再次解析 token 或依赖未导出的 context key。
func IsGuestFromContext(c *gin.Context) (bool, bool) {
	v, ok := c.Get(contextKeyIsGuest)
	if !ok {
		return false, false
	}
	isGuest, ok := v.(bool)
	return isGuest, ok
}
