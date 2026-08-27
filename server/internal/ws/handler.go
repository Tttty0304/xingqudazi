package ws

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"

	"xingqudazi-im/server/internal/service"
	"xingqudazi-im/server/internal/session"
)

// TokenBlacklist 供 WS 握手鉴权查询"这个 token 是否已被登出"（能力补齐项，
// 与 middleware.TokenBlacklist 是同一份真实实现 repository.RedisTokenBlacklist，
// 这里单独定义接口是为了不让 ws 包反向依赖 middleware 包）。
type TokenBlacklist interface {
	IsBlacklisted(ctx context.Context, token string) (bool, error)
}

// BotChecker 供 WS 握手鉴权查询"这个连接对应的账号是否为机器人身份"（能力
// 补齐项：LLM 驱动机器人最小验证），真实实现见 repository.UserRepository.IsBot。
// 服务端在握手成功后立即查一次并写入 Client.isBot，此后 Hub 的 sender_type
// 判定完全依赖这个服务端权威结果，不存在客户端在消息里自称机器人的路径。
type BotChecker interface {
	IsBot(ctx context.Context, userID string) (bool, error)
}

// Handler 提供 WS 升级入口：`GET /ws?token=<jwt>`（对应 T30/T31）。
type Handler struct {
	hub        *Hub
	tokenSvc   *service.TokenService
	blacklist  TokenBlacklist // 可为 nil：未启用登出黑名单能力时行为不变
	botChecker BotChecker     // 可为 nil：未注入时所有连接均视为非机器人，行为不变
	upgrader   websocket.Upgrader
}

func NewHandler(hub *Hub, tokenSvc *service.TokenService, blacklist TokenBlacklist, botChecker BotChecker) *Handler {
	return NewHandlerWithOrigins(hub, tokenSvc, blacklist, botChecker, []string{"*"})
}

// NewHandlerWithOrigins 按部署配置校验 WebSocket Origin。默认构造函数仅用于兼容
// 本地演示/已有测试；生产入口必须显式传入非通配的 CORS 白名单。
func NewHandlerWithOrigins(hub *Hub, tokenSvc *service.TokenService, blacklist TokenBlacklist, botChecker BotChecker, allowedOrigins []string) *Handler {
	return &Handler{
		hub:        hub,
		tokenSvc:   tokenSvc,
		blacklist:  blacklist,
		botChecker: botChecker,
		upgrader:   websocket.Upgrader{CheckOrigin: originAllowed(allowedOrigins)},
	}
}

func originAllowed(allowedOrigins []string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" { // 非浏览器探针/CLI 客户端没有 Origin，仍可使用显式 token 鉴权。
			return true
		}
		for _, allowed := range allowedOrigins {
			if allowed == "*" || strings.EqualFold(origin, allowed) {
				return true
			}
		}
		return false
	}
}

// ServeWS 是 gin handler：先鉴权（复用 HTTP 鉴权同一个 TokenService.ParseToken，
// 保证鉴权语义一致），鉴权失败直接拒绝升级并返回 401（T31），成功才执行 Upgrade。
func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	token := session.TokenFromRequest(r)
	if token == "" {
		// 兼容非浏览器客户端；浏览器前端已改为同源 HttpOnly Cookie，避免 JWT 出现在 URL。
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		http.Error(w, "missing_token", http.StatusUnauthorized)
		return
	}

	claims, err := h.tokenSvc.ParseToken(token)
	if err != nil {
		http.Error(w, "invalid_token", http.StatusUnauthorized)
		return
	}

	// 能力补齐项：已登出（黑名单命中）的 token 不允许再建立新的 WS 连接。
	// 已知边界：这只拦住"用旧 token 发起新连接"，不会强制断开在此之前已经
	// 建立成功的活跃连接（Hub 没有为每个 client 单独维护 token 过期定时器，
	// 强行加会显著增加复杂度，且收益有限——旧连接本身仍会随正常心跳超时或
	// 客户端主动断开而结束）。
	if h.blacklist != nil {
		if revoked, err := h.blacklist.IsBlacklisted(r.Context(), token); err == nil && revoked {
			http.Error(w, "token_revoked", http.StatusUnauthorized)
			return
		}
	}

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Default().Error("ws_upgrade_failed", "error", err)
		return
	}

	// 能力补齐项：握手成功后立即查一次账号是否为机器人身份，写入 Client，
	// 后续该连接发的全部消息 sender_type 均据此权威判定（见 Client.isBot 注释）。
	// 查询失败时按非机器人处理（fail-safe：宁可让机器人消息误显示为"human"，
	// 也不应该因为一次查询抖动就拒绝整个连接升级）。
	isBot := false
	if h.botChecker != nil {
		if b, err := h.botChecker.IsBot(r.Context(), claims.UserID); err == nil {
			isBot = b
		} else {
			slog.Default().Error("ws_bot_checker_failed", "user_id", claims.UserID, "error", err)
		}
	}

	client := newClient(h.hub, conn, claims.UserID, isBot)
	h.hub.register(client)

	go client.writePump()
	go client.readPump()
}
