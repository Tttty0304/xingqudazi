package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/internal/middleware"
	"xingqudazi-im/server/internal/service"
	logger "xingqudazi-im/server/pkg/log"
)

// MediaHandler 对应 Testcase T90-T92：图片消息上传（Task16/P0）。
type MediaHandler struct {
	MediaService *service.MediaService
}

// Upload godoc: POST /api/media/upload（multipart/form-data，字段名 "file"，需鉴权）
func (h *MediaHandler) Upload(c *gin.Context) {
	log := logger.FromContext(c.Request.Context())
	userID, _ := middleware.UserIDFromContext(c)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing_file"})
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		log.Error("open uploaded file failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		return
	}
	defer f.Close()

	mimeType := fileHeader.Header.Get("Content-Type")
	asset, err := h.MediaService.UploadImage(c.Request.Context(), userID, mimeType, fileHeader.Size, f)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrUnsupportedMediaType):
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_media_type"})
		case errors.Is(err, service.ErrFileTooLarge):
			c.JSON(http.StatusBadRequest, gin.H{"error": "file_too_large"})
		default:
			log.Error("upload image failed", "user_id", userID, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error"})
		}
		return
	}

	log.Info("media_uploaded", "media_id", asset.ID, "owner_id", userID, "size_bytes", asset.SizeBytes)
	c.JSON(http.StatusCreated, gin.H{"media_id": asset.ID, "url": asset.URL})
}
