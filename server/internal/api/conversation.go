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

// ConversationHandler 对应 Testcase T70-T72：私聊会话列表 / 历史消息。
// 私聊消息的**发送**走 WS `send_direct_message`（见 ws.Hub.handleSendDirectMessage），
// 这里只负责 HTTP 侧的会话列表与历史消息查询（读路径），与 Task3/Task4 群聊读写分离的
// 设计保持一致。
type ConversationHandler struct {
	ConversationService *service.ConversationService
}

type conversationResponse struct {
	ConversationID string `json:"conversation_id"`
	PeerID         string `json:"peer_id"`
	LastMessage    string `json:"last_message"`
	LastMessageAt  string `json:"last_message_at"`
	UnreadCount    int64  `json:"unread_count"`
}

// ListConversations godoc: GET /api/conversations（对应 T71，需鉴权）
func (h *ConversationHandler) ListConversations(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	summaries, err := h.ConversationService.ListConversations(c.Request.Context(), userID)
	if err != nil {
		log.Error("list conversations failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	resp := make([]conversationResponse, 0, len(summaries))
	for _, s := range summaries {
		resp = append(resp, conversationResponse{
			ConversationID: s.ConversationID,
			PeerID:         s.PeerID,
			LastMessage:    html.EscapeString(s.LastMessage), // T50：XSS 转义
			LastMessageAt:  s.LastMessageAt.Format("2006-01-02T15:04:05Z07:00"),
			UnreadCount:    s.UnreadCount,
		})
	}
	c.JSON(http.StatusOK, resp)
}

type directMessageResponse struct {
	ID          string `json:"id"`
	MsgID       string `json:"msg_id"`
	SenderID    string `json:"sender_id"`
	SenderType  string `json:"sender_type"`
	Content     string `json:"content"`
	ContentType string `json:"content_type"`
	CreatedAt   string `json:"created_at"`
}

// ListMessages godoc: GET /api/conversations/:id/messages?page=&size=（对应 T70，需鉴权，
// 仅会话参与者可查看）
func (h *ConversationHandler) ListMessages(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)
	conversationID := c.Param("id")

	if _, err := uuid.Parse(conversationID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_conversation_id"})
		return
	}

	page, size, ok := parsePage(c)
	if !ok {
		return
	}

	messages, hasMore, err := h.ConversationService.ListMessages(c.Request.Context(), conversationID, userID, page, size)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrConversationNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "conversation_not_found"})
		case errors.Is(err, service.ErrForbiddenConversationAccess):
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		default:
			log.Error("list conversation messages failed", "conversation_id", conversationID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		}
		return
	}

	resp := make([]directMessageResponse, 0, len(messages))
	for _, m := range messages {
		content := m.Content
		if m.ContentType == "text" {
			content = html.EscapeString(content) // T50：XSS 转义（存储保留原文，仅对外输出转义；
			// Task16 起图片消息 Content 为媒体 URL，不做转义，避免破坏引用）
		}
		resp = append(resp, directMessageResponse{
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
