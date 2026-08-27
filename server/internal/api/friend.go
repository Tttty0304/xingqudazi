package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/service"
	logger "xingqudazi-im/server/pkg/log"
)

// FriendHandler 对应 Testcase T60-T63：好友关系链（发起请求/接受拒绝/好友列表/删除好友）。
type FriendHandler struct {
	FriendService *service.FriendService
}

type sendFriendRequestRequest struct {
	TargetUserID string `json:"target_user_id" binding:"required"`
}

// SendRequest godoc: POST /api/friends/requests（对应 T60，需鉴权）
func (h *FriendHandler) SendRequest(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	var req sendFriendRequestRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}

	friendship, err := h.FriendService.SendRequest(c.Request.Context(), userID, req.TargetUserID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCannotFriendSelf):
			c.JSON(http.StatusBadRequest, gin.H{"error": "cannot_friend_self"})
			return
		case errors.Is(err, service.ErrUserNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "user_not_found"})
			return
		case errors.Is(err, service.ErrAlreadyFriends):
			c.JSON(http.StatusConflict, gin.H{"error": "already_friends"})
			return
		case errors.Is(err, service.ErrFriendRequestExists):
			c.JSON(http.StatusConflict, gin.H{"error": "friend_request_already_exists"})
			return
		case friendship != nil:
			// 走到这里说明好友请求本身已创建成功，err 只是 WS 通知推送失败
			// （如对方当前离线，属预期情况）。请求已落库的事实优先，仍按创建成功处理，
			// 仅记录日志，不影响接口返回。
			log.Info("friend_request_notify_failed_but_created", "request_id", friendship.ID, "error", err)
		default:
			log.Error("send friend request failed", "requester_id", userID, "target_id", req.TargetUserID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
			return
		}
	}

	log.Info("friend_request_created", "request_id", friendship.ID, "requester_id", userID, "target_id", req.TargetUserID)
	c.JSON(http.StatusCreated, gin.H{"request_id": friendship.ID, "status": friendship.Status})
}

type respondFriendRequestRequest struct {
	Action string `json:"action" binding:"required"` // accept | reject
}

// RespondRequest godoc: PUT /api/friends/requests/:id（对应 T61/T62，需鉴权）
func (h *FriendHandler) RespondRequest(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)
	requestID := c.Param("id")

	var req respondFriendRequestRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}

	friendship, err := h.FriendService.RespondRequest(c.Request.Context(), requestID, userID, req.Action)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidFriendAction):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_action"})
		case errors.Is(err, service.ErrFriendRequestNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "friend_request_not_found"})
		case errors.Is(err, service.ErrForbiddenFriendResponse):
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		case errors.Is(err, service.ErrFriendRequestResolved):
			c.JSON(http.StatusConflict, gin.H{"error": "already_resolved"})
		default:
			log.Error("respond friend request failed", "request_id", requestID, "actor_id", userID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		}
		return
	}

	log.Info("friend_request_resolved", "request_id", requestID, "actor_id", userID, "status", friendship.Status)
	c.JSON(http.StatusOK, gin.H{"status": friendship.Status})
}

type friendResponse struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Online   bool   `json:"online"`
}

// ListFriends godoc: GET /api/friends（对应 T63，需鉴权）
func (h *FriendHandler) ListFriends(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	friends, err := h.FriendService.ListFriends(c.Request.Context(), userID)
	if err != nil {
		log.Error("list friends failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	resp := make([]friendResponse, 0, len(friends))
	for _, f := range friends {
		resp = append(resp, friendResponse{UserID: f.UserID, Username: f.Username, Online: f.Online})
	}
	c.JSON(http.StatusOK, resp)
}

type pendingFriendRequestResponse struct {
	RequestID    string `json:"request_id"`
	PeerID       string `json:"peer_id"`
	PeerUsername string `json:"peer_username"`
	Direction    string `json:"direction"` // incoming | outgoing
	CreatedAt    string `json:"created_at"`
}

// ListPendingRequests godoc: GET /api/friends/requests（对应 T120，本轮新增，需鉴权）
// 列出当前用户涉及的全部待处理好友请求（收到的+发出的），补齐此前"离线错过 WS
// 通知后再无处查看好友请求"的功能缺口。
func (h *FriendHandler) ListPendingRequests(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	requests, err := h.FriendService.ListPendingRequests(c.Request.Context(), userID)
	if err != nil {
		log.Error("list pending friend requests failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	resp := make([]pendingFriendRequestResponse, 0, len(requests))
	for _, r := range requests {
		resp = append(resp, pendingFriendRequestResponse{
			RequestID:    r.RequestID,
			PeerID:       r.PeerID,
			PeerUsername: r.PeerUsername,
			Direction:    r.Direction,
			CreatedAt:    r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	c.JSON(http.StatusOK, resp)
}

// DeleteFriend godoc: DELETE /api/friends/:id（Plan Part3 已确认接口，:id 为对方 user_id）
func (h *FriendHandler) DeleteFriend(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)
	peerID := c.Param("id")

	if err := h.FriendService.RemoveFriend(c.Request.Context(), userID, peerID); err != nil {
		if errors.Is(err, service.ErrNotFriends) {
			// DELETE 的目标状态是“不是好友”；重放或并发删除已达成该状态，仍成功。
			c.Status(http.StatusNoContent)
			return
		}
		log.Error("delete friend failed", "user_id", userID, "peer_id", peerID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	log.Info("friend_removed", "user_id", userID, "peer_id", peerID)
	c.Status(http.StatusNoContent)
}
