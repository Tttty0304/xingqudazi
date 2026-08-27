package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/service"
	"xingqudazi-im/server/internal/session"
	logger "xingqudazi-im/server/pkg/log"
)

// AuthHandler 对应 Testcase T10-T15：注册 / 登录 / 访客模式。
type AuthHandler struct {
	AuthService         *service.AuthService
	SessionCookieSecure bool
	// OmitTokenResponse 仅在生产 Cookie 会话模式启用；默认保留 token 字段以兼容
	// 既有自动化脚本和非浏览器 API 客户端。
	OmitTokenResponse bool
}

type registerRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register godoc: POST /api/auth/register（对应 T10-T12）
func (h *AuthHandler) Register(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())

	var req registerRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}

	user, err := h.AuthService.Register(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUsernameTaken):
			c.JSON(http.StatusBadRequest, gin.H{"error": "username_taken"})
		case errors.Is(err, service.ErrInvalidPassword):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_password"})
		case errors.Is(err, service.ErrInvalidUsername):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_username"})
		default:
			log.Error("register failed", "username", req.Username, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		}
		return
	}

	log.Info("user_registered", "user_id", user.ID, "username", user.Username)
	c.JSON(http.StatusCreated, gin.H{"user_id": user.ID, "username": user.Username})
}

// Login godoc: POST /api/auth/login（对应 T13-T14）
func (h *AuthHandler) Login(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())

	var req loginRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}

	user, token, err := h.AuthService.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			// 故意不区分"用户不存在"与"密码错误"，防止用户名枚举（T14 硬性要求）。
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials"})
			return
		}
		log.Error("login failed", "username", req.Username, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	log.Info("user_logged_in", "user_id", user.ID)
	session.SetToken(c.Writer, token, h.SessionCookieSecure, int((24 * time.Hour).Seconds()))
	response := gin.H{"user_id": user.ID}
	if !h.OmitTokenResponse {
		response["token"] = token
	}
	c.JSON(http.StatusOK, response)
}

// Guest godoc: POST /api/auth/guest（对应 T15）
func (h *AuthHandler) Guest(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())

	user, token, err := h.AuthService.GuestLogin(c.Request.Context())
	if err != nil {
		if errors.Is(err, service.ErrGuestModeDisabled) {
			c.JSON(http.StatusForbidden, gin.H{"error": "guest_mode_disabled"})
			return
		}
		log.Error("guest login failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	log.Info("guest_logged_in", "user_id", user.ID)
	session.SetToken(c.Writer, token, h.SessionCookieSecure, int((24 * time.Hour).Seconds()))
	response := gin.H{"user_id": user.ID, "is_guest": true}
	if !h.OmitTokenResponse {
		response["token"] = token
	}
	c.JSON(http.StatusOK, response)
}

// Session 返回当前 Cookie/Bearer 会话的最小身份信息。前端刷新页面时用它校验
// HttpOnly Cookie 是否仍有效，避免只凭 localStorage 中的展示信息误判为已登录。
func (h *AuthHandler) Session(c *gin.Context) {
	userID, _ := middleware.UserIDFromContext(c)
	isGuest, _ := middleware.IsGuestFromContext(c)
	c.JSON(http.StatusOK, gin.H{"user_id": userID, "is_guest": isGuest})
}

// Logout godoc: POST /api/auth/logout（能力补齐项：登出后 token 立即失效，
// 而不是仅前端清 localStorage、服务端在自然过期前依然认可该 token）。
// 挂载在 authedGroup 下（需要携带一个当前仍合法的 token 才能调用），
// 幂等：重复调用/token 已过期均返回 200，不额外区分错误场景。
func (h *AuthHandler) Logout(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())

	tokenString := session.TokenFromRequest(c.Request)
	if err := h.AuthService.Logout(c.Request.Context(), tokenString); err != nil {
		// 黑名单写入失败（如 Redis 抖动）不应阻塞用户登出的本地体验——前端
		// 无论如何都会清空本地登录态，这里只记录日志供排查，仍返回 200。
		log.Error("logout failed", "error", err)
	}
	session.ClearToken(c.Writer, h.SessionCookieSecure)
	c.JSON(http.StatusOK, gin.H{})
}
