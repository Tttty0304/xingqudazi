package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/service"
	logger "xingqudazi-im/server/pkg/log"
)

// RecommendationHandler 对应 Testcase T110-T112：AI 推荐规则化匹配演示（Task20）。
type RecommendationHandler struct {
	RecommendationService *service.RecommendationService
}

// Generate godoc: POST /api/recommendations/generate（需鉴权；demo 场景下允许任一
// 已登录用户触发全量重新生成，不是管理员专属接口——Plan 未要求做成后台定时任务，
// 这是最小化实现的合理简化，已在工作记录中如实标注）。
func (h *RecommendationHandler) Generate(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	created, err := h.RecommendationService.GenerateCandidates(c.Request.Context())
	if err != nil {
		log.Error("generate recommendation candidates failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"created": created})
}

type recommendationResponse struct {
	CandidateID  string  `json:"candidate_id"`
	PeerID       string  `json:"peer_id"`
	PeerUsername string  `json:"peer_username"`
	SharedTopic  string  `json:"shared_topic,omitempty"`
	RoomID       string  `json:"room_id,omitempty"`
	MatchReason  string  `json:"match_reason"`
	MatchScore   float64 `json:"match_score"`
}

// List godoc: GET /api/recommendations（需鉴权）
func (h *RecommendationHandler) List(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	candidates, err := h.RecommendationService.ListCandidates(c.Request.Context(), userID)
	if err != nil {
		log.Error("list recommendation candidates failed", "user_id", userID, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}

	resp := make([]recommendationResponse, 0, len(candidates))
	for _, cand := range candidates {
		resp = append(resp, recommendationResponse{
			CandidateID:  cand.CandidateID,
			PeerID:       cand.PeerID,
			PeerUsername: cand.PeerUsername,
			SharedTopic:  cand.SharedTopic,
			RoomID:       cand.RoomID,
			MatchReason:  cand.MatchReason,
			MatchScore:   cand.MatchScore,
		})
	}
	c.JSON(http.StatusOK, resp)
}

type respondCandidateRequest struct {
	Action string `json:"action" binding:"required"` // confirm | dismiss
}

// Respond godoc: PUT /api/recommendations/:id（需鉴权）
func (h *RecommendationHandler) Respond(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)
	candidateID := c.Param("id")

	var req respondCandidateRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}

	err := h.RecommendationService.RespondCandidate(c.Request.Context(), candidateID, userID, req.Action)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCandidateAction):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_action"})
		case errors.Is(err, service.ErrCandidateNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "candidate_not_found"})
		case errors.Is(err, service.ErrForbiddenCandidateRespond):
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		case errors.Is(err, service.ErrCandidateAlreadyResolved):
			c.JSON(http.StatusConflict, gin.H{"error": "already_resolved"})
		default:
			log.Error("respond recommendation candidate failed", "candidate_id", candidateID, "user_id", userID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		}
		return
	}

	c.Status(http.StatusOK)
}
