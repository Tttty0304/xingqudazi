package api

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"xingqudazi-im/server/pkg/metric"
)

// MetricsHandler 暴露 Prometheus text exposition format，供 Prometheus 原生抓取。
func MetricsHandler(c *gin.Context) {
	c.Data(http.StatusOK, "text/plain; version=0.0.4; charset=utf-8", []byte(metric.Global.PrometheusText()))
}

// MetricsJSONHandler 保留旧 JSON 快照路径，方便已有演示/排障脚本读取。
func MetricsJSONHandler(c *gin.Context) {
	c.JSON(http.StatusOK, metric.Global.Snapshot())
}
