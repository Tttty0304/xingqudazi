package api

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/service"
	logger "xingqudazi-im/server/pkg/log"
)

// PushHandler 对应 Testcase T100-T101：Web Push 订阅管理（Task17）。
type PushHandler struct {
	PushService *service.PushService
}

// VAPIDPublicKey godoc: GET /api/push/vapid-public-key（无需鉴权：前端在展示"是否已授权
// 通知权限"的界面时可能尚未登录/刚登录，提前拿到公钥不涉及隐私信息）。
func (h *PushHandler) VAPIDPublicKey(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"public_key": h.PushService.VAPIDPublicKey()})
}

type subscribeRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
	Keys     struct {
		P256dh string `json:"p256dh" binding:"required"`
		Auth   string `json:"auth" binding:"required"`
	} `json:"keys" binding:"required"`
}

// Subscribe godoc: POST /api/push/subscriptions（需鉴权，body 与浏览器
// `PushSubscription.toJSON()` 输出结构一致，方便前端 Task9 直接透传）。
func (h *PushHandler) Subscribe(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	var req subscribeRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}
	endpoint, err := url.ParseRequestURI(req.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_push_subscription"})
		return
	}

	if err := h.PushService.Subscribe(c.Request.Context(), userID, req.Endpoint, req.Keys.P256dh, req.Keys.Auth); err != nil {
		if errors.Is(err, service.ErrInvalidPushSubscription) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_push_subscription"})
			return
		}
		log.Error("subscribe push failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	log.Info("push_subscribed", "user_id", userID, "endpoint", req.Endpoint)
	c.Status(http.StatusCreated)
}

type unsubscribeRequest struct {
	Endpoint string `json:"endpoint" binding:"required"`
}

// Unsubscribe godoc: DELETE /api/push/subscriptions（需鉴权）
func (h *PushHandler) Unsubscribe(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	var req unsubscribeRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}
	endpoint, err := url.ParseRequestURI(req.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_push_subscription"})
		return
	}

	if err := h.PushService.Unsubscribe(c.Request.Context(), userID, req.Endpoint); err != nil {
		log.Error("unsubscribe push failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	c.Status(http.StatusNoContent)
}
