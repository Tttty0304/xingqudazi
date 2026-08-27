package api

import (
	"errors"
	"html"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/service"
	logger "xingqudazi-im/server/pkg/log"
)

// RoomHandler 对应 Testcase T20-T22：房间列表 / 历史消息。
type RoomHandler struct {
	RoomService    *service.RoomService
	MessageService *service.MessageService
}

type roomResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Topic       string `json:"topic"`
	OnlineCount int64  `json:"online_count"`
}

type createRoomRequest struct {
	Name  string `json:"name"`
	Topic string `json:"topic"`
}

// ListRooms godoc: GET /api/rooms（对应 T20，无需鉴权）
func (h *RoomHandler) ListRooms(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())

	rooms, err := h.RoomService.ListRooms(c.Request.Context())
	if err != nil {
		log.Error("list rooms failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	resp := make([]roomResponse, 0, len(rooms))
	for _, r := range rooms {
		resp = append(resp, roomResponse{
			ID:          r.ID,
			Name:        r.Name,
			Topic:       r.Topic,
			OnlineCount: r.OnlineCount,
		})
	}
	c.JSON(http.StatusOK, resp)
}

// Create godoc: POST /api/rooms（需鉴权，支持 Idempotency-Key）
func (h *RoomHandler) Create(c *gin.Context) {
	var req createRoomRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}
	userID, _ := middleware.UserIDFromContext(c)
	room, err := h.RoomService.CreateRoom(c.Request.Context(), userID, req.Name, req.Topic)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidRoomName):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_room_name"})
		case errors.Is(err, service.ErrInvalidRoomTopic):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_room_topic"})
		default:
			logger.FromContext(c.Request.Context()).Error("create room failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		}
		return
	}
	c.JSON(http.StatusCreated, roomResponse{ID: room.ID, Name: room.Name, Topic: room.Topic, OnlineCount: 0})
}

type messageResponse struct {
	ID          string `json:"id"`
	MsgID       string `json:"msg_id"`
	SenderID    string `json:"sender_id"`
	SenderType  string `json:"sender_type"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
	CreatedAt   string `json:"created_at"`
}

// ListRoomMessages godoc: GET /api/rooms/:id/messages?page=&size=（对应 T21-T22）
func (h *RoomHandler) ListRoomMessages(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	roomID := c.Param("id")

	// 对应 Plan Part2 矩阵「id 非法格式 400」：房间 ID 必须是合法 UUID，
	// 格式错误在到达数据库前拦截，避免把校验职责下放给驱动层报错信息。
	if _, err := uuid.Parse(roomID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_room_id"})
		return
	}

	page, size, ok := parsePage(c)
	if !ok {
		return
	}

	messages, hasMore, err := h.MessageService.ListRoomMessages(c.Request.Context(), roomID, page, size)
	if err != nil {
		if errors.Is(err, service.ErrRoomNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "room_not_found"})
			return
		}
		log.Error("list room messages failed", "room_id", roomID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	resp := make([]messageResponse, 0, len(messages))
	for _, m := range messages {
		content := m.Content
		if m.ContentType == "text" {
			content = html.EscapeString(content) // T50：XSS 转义（存储保留原文，仅对外输出转义；
			// Task16 起图片消息 Content 为媒体 URL，不做转义，避免破坏引用）
		}
		resp = append(resp, messageResponse{
			ID:          m.ID,
			MsgID:       m.MsgID,
			SenderID:    m.SenderID,
			SenderType:  m.SenderType,
			Content:     content,
			ContentType: m.ContentType,
			CreatedAt:   m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	c.JSON(http.StatusOK, gin.H{"messages": resp, "has_more": hasMore})
}
