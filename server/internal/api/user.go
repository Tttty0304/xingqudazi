package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
	logger "xingqudazi-im/server/pkg/log"
)

// UserHandler 补齐"按用户名查找用户"与"批量查询用户基础信息"两个此前缺失的接口
// （Task9 前端功能闭环：好友页"添加好友"输入用户名、会话列表/群聊消息展示真实用户名）。
type UserHandler struct {
	UserService *service.UserService
}

type userLookupResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type profileResponse struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	IsGuest   bool   `json:"is_guest"`
	AvatarURL string `json:"avatar_url"`
	Bio       string `json:"bio"`
}

type updateProfileRequest struct {
	AvatarURL string `json:"avatar_url"`
	Bio       string `json:"bio"`
}

func profileFromUser(user *model.User) profileResponse {
	return profileResponse{ID: user.ID, Username: user.Username, IsGuest: user.IsGuest, AvatarURL: user.AvatarURL, Bio: user.Bio}
}

// Lookup godoc: GET /api/users/lookup?username=xxx（需鉴权）
func (h *UserHandler) Lookup(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	username := strings.TrimSpace(c.Query("username"))
	if username == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}
	if err := service.ValidateUsername(username); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_username"})
		return
	}

	user, err := h.UserService.FindByUsername(c.Request.Context(), username)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
			return
		}
		log.Error("lookup user failed", "username", username, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	c.JSON(http.StatusOK, userLookupResponse{ID: user.ID, Username: user.Username})
}

// BatchGet godoc: GET /api/users?ids=id1,id2,id3（需鉴权）
func (h *UserHandler) BatchGet(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	_, _ = middleware.UserIDFromContext(c) // 仅要求已登录，不做额外越权限制（查基础信息不涉及隐私）

	raw := strings.TrimSpace(c.Query("ids"))
	if raw == "" {
		c.JSON(http.StatusOK, []userLookupResponse{})
		return
	}

	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			ids = append(ids, p)
		}
	}
	if len(ids) == 0 {
		c.JSON(http.StatusOK, []userLookupResponse{})
		return
	}
	// 避免单次请求 ids 参数被恶意构造得过长（demo 场景的最小防护，非严格限流）。
	if len(ids) > 200 {
		ids = ids[:200]
	}

	users, err := h.UserService.FindByIDs(c.Request.Context(), ids)
	if err != nil {
		log.Error("batch get users failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	resp := make([]userLookupResponse, 0, len(users))
	for _, u := range users {
		resp = append(resp, userLookupResponse{ID: u.ID, Username: u.Username})
	}
	c.JSON(http.StatusOK, resp)
}

// GetMyProfile godoc: GET /api/me/profile（需鉴权）
func (h *UserHandler) GetMyProfile(c *gin.Context) {
	userID, _ := middleware.UserIDFromContext(c)
	user, err := h.UserService.GetProfile(c.Request.Context(), userID)
	if err != nil {
		logger.FromContext(c.Request.Context()).Error("get profile failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	c.JSON(http.StatusOK, profileFromUser(user))
}

// UpdateMyProfile godoc: PUT /api/me/profile（需鉴权，支持 Idempotency-Key）
func (h *UserHandler) UpdateMyProfile(c *gin.Context) {
	var req updateProfileRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}
	// 头像必须来自本服务的已验证上传路径；避免把任意 URL 作为持久化内容再次
	// 注入页面，也避免服务端被动成为第三方图片跟踪的跳板。
	if req.AvatarURL != "" && !strings.HasPrefix(req.AvatarURL, "/uploads/") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_avatar_url"})
		return
	}
	userID, _ := middleware.UserIDFromContext(c)
	user, err := h.UserService.UpdateProfile(c.Request.Context(), userID, req.AvatarURL, req.Bio)
	if err != nil {
		if errors.Is(err, service.ErrInvalidBio) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_bio"})
			return
		}
		logger.FromContext(c.Request.Context()).Error("update profile failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	c.JSON(http.StatusOK, profileFromUser(user))
}
