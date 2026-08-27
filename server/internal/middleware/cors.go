package middleware

import "github.com/gin-gonic/gin"

// CORS 是 Task8（系统安全性）范围内的跨域配置。此前硬编码为
// `Access-Control-Allow-Origin: *`，任何域都能带着用户 token 发起跨域请求，
// 是真实的安全缺口（能力补齐项）。现在支持通过 `allowedOrigins` 传入白名单：
//   - 传入空切片或恰好为 `["*"]`：仅在开发模式下反射发起请求的 Origin，
//     以支持跨端口的 HttpOnly Cookie；生产环境由 Config 拒绝 `*`；
//   - 传入具体域名列表：仅反射请求 `Origin` 头命中白名单的请求，其余请求
//     不设置 `Access-Control-Allow-Origin`（浏览器会因跨域校验失败拒绝读取
//     响应），生产环境部署应配置为真实域名列表。
//
// 与 `ws.Handler` 的 `CheckOrigin` 放开处理方式保持一致的已知简化点。
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowAll := len(allowedOrigins) == 0
	allowedSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			continue
		}
		allowedSet[o] = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		switch {
		case origin != "" && (allowAll || allowedSet[origin]):
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		// Idempotency-Key 是写命令安全重放协议的一部分；未列入 CORS 允许头会导致
		// 前端跨域部署时预检失败，即使后端业务层已经实现了幂等保护也无法使用。
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
