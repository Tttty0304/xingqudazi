package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/service"
	logger "xingqudazi-im/server/pkg/log"
)

// ReportHandler 对应 Testcase T80：举报消息/私聊消息/用户（Task18 基础内容安全）。
type ReportHandler struct {
	ReportService *service.ReportService
}

type createReportRequest struct {
	TargetType string `json:"target_type" binding:"required"`
	TargetID   string `json:"target_id" binding:"required"`
	Reason     string `json:"reason" binding:"required"`
}

// CreateReport godoc: POST /api/reports（需鉴权）
func (h *ReportHandler) CreateReport(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	var req createReportRequest
	if err := bindJSONStrict(c, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request_body"})
		return
	}
	var ok bool
	if req.Reason, ok = normalizedNonEmpty(req.Reason, 500); !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_reason"})
		return
	}

	report, err := h.ReportService.CreateReport(c.Request.Context(), userID, req.TargetType, req.TargetID, req.Reason)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidReportTargetType):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_target_type"})
		case errors.Is(err, service.ErrReportTargetNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "report_target_not_found"})
		default:
			log.Error("create report failed", "reporter_id", userID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		}
		return
	}

	log.Info("report_created", "report_id", report.ID, "reporter_id", userID, "target_type", req.TargetType, "target_id", req.TargetID)
	c.JSON(http.StatusCreated, gin.H{"report_id": report.ID})
}
