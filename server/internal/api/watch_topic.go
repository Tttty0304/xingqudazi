package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/service"
	logger "xingqudazi-im/server/pkg/log"
)

// WatchTopicHandler 对应 Testcase T94-T95：关注事项（Task19/P1）。
type WatchTopicHandler struct {
	WatchTopicService *service.WatchTopicService
}

type createWatchTopicRequest struct {
	RoomID    string  `json:"room_id"` // 可空：全局关注
	Keywords  string  `json:"keywords" binding:"required"`
	Priority  int     `json:"priority"`
	ExpiresAt *string `json:"expires_at"` // RFC3339，可空表示不过期
}

// Create godoc: POST /api/watch-topics（需鉴权）
func (h *WatchTopicHandler) Create(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	var req createWatchTopicRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}
	var ok bool
	if req.Keywords, ok = normalizedNonEmpty(req.Keywords, 500); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_watch_topic"})
		return
	}
	if req.Priority == 0 {
		req.Priority = 3
	}
	if req.Priority < 1 || req.Priority > 5 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_priority"})
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_expires_at"})
			return
		}
		expiresAt = &t
	}

	topic, err := h.WatchTopicService.CreateWatchTopic(c.Request.Context(), userID, req.RoomID, req.Keywords, req.Priority, expiresAt)
	if err != nil {
		log.Error("create watch topic failed", "user_id", userID, "error", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_watch_topic"})
		return
	}

	log.Info("watch_topic_created", "topic_id", topic.ID, "user_id", userID)
	c.JSON(http.StatusCreated, gin.H{"topic_id": topic.ID})
}

type watchTopicResponse struct {
	ID        string  `json:"id"`
	RoomID    string  `json:"room_id,omitempty"`
	Keywords  string  `json:"keywords"`
	Priority  int     `json:"priority"`
	ExpiresAt *string `json:"expires_at,omitempty"`
}

// List godoc: GET /api/watch-topics（需鉴权）
func (h *WatchTopicHandler) List(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	topics, err := h.WatchTopicService.ListWatchTopics(c.Request.Context(), userID)
	if err != nil {
		log.Error("list watch topics failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	resp := make([]watchTopicResponse, 0, len(topics))
	for _, t := range topics {
		item := watchTopicResponse{ID: t.ID, RoomID: t.RoomID, Keywords: t.Keywords, Priority: t.Priority}
		if t.ExpiresAt != nil {
			s := t.ExpiresAt.Format(time.RFC3339)
			item.ExpiresAt = &s
		}
		resp = append(resp, item)
	}
	c.JSON(http.StatusOK, resp)
}

// Delete godoc: DELETE /api/watch-topics/:id（对应 T123，本轮新增，需鉴权，仅本人可删）
func (h *WatchTopicHandler) Delete(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)
	topicID := c.Param("id")

	if err := h.WatchTopicService.DeleteWatchTopic(c.Request.Context(), topicID, userID); err != nil {
		if errors.Is(err, service.ErrWatchTopicNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "watch_topic_not_found"})
			return
		}
		log.Error("delete watch topic failed", "topic_id", topicID, "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	log.Info("watch_topic_deleted", "topic_id", topicID, "user_id", userID)
	c.Status(http.StatusNoContent)
}
