package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
)

// bindJSONStrict 统一拒绝未知字段、类型不匹配、空 body 和尾随 JSON。业务接口不能
// 靠 json decoder 的默认宽松行为“猜测”客户端意图，否则拼错字段会被静默忽略。
func bindJSONStrict(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple json values")
		}
		return err
	}
	// 严格解码不会经过 Gin 的 ShouldBindJSON；显式恢复原有 binding 标签（例如
	// binding:"required"）校验，避免缺失字段被误当作后续业务校验错误。
	if err := binding.Validator.ValidateStruct(target); err != nil {
		return err
	}
	return nil
}

func validUUID(value string) bool { _, err := uuid.Parse(value); return err == nil }

func requireUUID(c *gin.Context, value, field string) bool {
	if validUUID(value) {
		return true
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_" + field})
	return false
}

// parsePage 让无效分页明确失败，而不是悄悄回退到第一页，便于调用方修正命令。
func parsePage(c *gin.Context) (int, int, bool) {
	page, size := 1, 20
	var err error
	if raw, ok := c.GetQuery("page"); ok {
		page, err = strconv.Atoi(raw)
		if err != nil || page < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_page"})
			return 0, 0, false
		}
	}
	if raw, ok := c.GetQuery("size"); ok {
		size, err = strconv.Atoi(raw)
		if err != nil || size < 1 || size > 100 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_size"})
			return 0, 0, false
		}
	}
	return page, size, true
}

func normalizedNonEmpty(value string, max int) (string, bool) {
	v := strings.TrimSpace(value)
	return v, v != "" && len(v) <= max
}
