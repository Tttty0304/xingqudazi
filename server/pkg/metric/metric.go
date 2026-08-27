package metric

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Metrics 是一个极轻量的进程内指标收集器。它以 Prometheus text exposition format
// 暴露基础运行指标，避免为少量自定义指标引入额外 client 依赖，同时可被
// Prometheus/Grafana 原生抓取。
type Metrics struct {
	onlineConnections atomic.Int64
	totalMessagesSent atomic.Int64
	wsErrors          atomic.Int64
	httpRequestsTotal atomic.Int64

	mu              sync.RWMutex
	roomOnlineCount map[string]int64
}

// Global 是进程内单例，避免到处传参。
var Global = &Metrics{
	roomOnlineCount: make(map[string]int64),
}

func (m *Metrics) IncOnlineConnections() { m.onlineConnections.Add(1) }
func (m *Metrics) DecOnlineConnections() { m.onlineConnections.Add(-1) }
func (m *Metrics) IncMessagesSent()      { m.totalMessagesSent.Add(1) }
func (m *Metrics) IncWSErrors()          { m.wsErrors.Add(1) }
func (m *Metrics) IncHTTPRequests()      { m.httpRequestsTotal.Add(1) }

func (m *Metrics) SetRoomOnlineCount(roomID string, count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roomOnlineCount[roomID] = count
}

// Snapshot 返回当前指标快照，供 `/metrics` handler 渲染。
type Snapshot struct {
	OnlineConnections int64            `json:"online_connections"`
	TotalMessagesSent int64            `json:"total_messages_sent"`
	WSErrors          int64            `json:"ws_errors"`
	HTTPRequestsTotal int64            `json:"http_requests_total"`
	RoomOnlineCount   map[string]int64 `json:"room_online_count"`
}

func (m *Metrics) Snapshot() Snapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	roomCopy := make(map[string]int64, len(m.roomOnlineCount))
	for k, v := range m.roomOnlineCount {
		roomCopy[k] = v
	}
	return Snapshot{
		OnlineConnections: m.onlineConnections.Load(),
		TotalMessagesSent: m.totalMessagesSent.Load(),
		WSErrors:          m.wsErrors.Load(),
		HTTPRequestsTotal: m.httpRequestsTotal.Load(),
		RoomOnlineCount:   roomCopy,
	}
}

// PrometheusText 把进程快照渲染为 Prometheus text exposition format。项目的指标量
// 很小，手写稳定格式可避免额外 client 依赖，同时可被 Prometheus/Grafana 原生抓取。
func (m *Metrics) PrometheusText() string {
	snapshot := m.Snapshot()
	var builder strings.Builder
	writeMetric := func(name, help, kind string, value int64) {
		fmt.Fprintf(&builder, "# HELP %s %s\n# TYPE %s %s\n%s %d\n", name, help, name, kind, name, value)
	}
	writeMetric("interest_chat_online_connections", "Current open WebSocket connections.", "gauge", snapshot.OnlineConnections)
	writeMetric("interest_chat_messages_sent_total", "Total accepted chat messages.", "counter", snapshot.TotalMessagesSent)
	writeMetric("interest_chat_ws_errors_total", "Total WebSocket protocol errors.", "counter", snapshot.WSErrors)
	writeMetric("interest_chat_http_requests_total", "Total HTTP requests received.", "counter", snapshot.HTTPRequestsTotal)
	fmt.Fprintln(&builder, "# HELP interest_chat_room_online Current online users by room.\n# TYPE interest_chat_room_online gauge")
	roomIDs := make([]string, 0, len(snapshot.RoomOnlineCount))
	for roomID := range snapshot.RoomOnlineCount {
		roomIDs = append(roomIDs, roomID)
	}
	sort.Strings(roomIDs)
	for _, roomID := range roomIDs {
		fmt.Fprintf(&builder, "interest_chat_room_online{room_id=%s} %d\n", strconv.Quote(roomID), snapshot.RoomOnlineCount[roomID])
	}
	return builder.String()
}
