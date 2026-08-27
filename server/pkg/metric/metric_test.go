package metric

import (
	"strings"
	"testing"
)

// TestMetrics_CountersIncrementAndDecrement 覆盖各计数器的基础加减语义
// （能力补齐项：此前 pkg/metric 覆盖率为 0%，`/metrics` 端点输出的数据来源
// 完全没有单测验证过其计数逻辑本身是否正确）。用独立的 *Metrics 实例而非
// 全局 Global 单例，避免与其它测试/包内代码共享状态产生互相干扰。
func TestMetrics_CountersIncrementAndDecrement(t *testing.T) {
	m := &Metrics{roomOnlineCount: make(map[string]int64)}

	m.IncOnlineConnections()
	m.IncOnlineConnections()
	m.DecOnlineConnections()
	if got := m.Snapshot().OnlineConnections; got != 1 {
		t.Fatalf("expected OnlineConnections=1, got %d", got)
	}

	m.IncMessagesSent()
	m.IncMessagesSent()
	m.IncMessagesSent()
	if got := m.Snapshot().TotalMessagesSent; got != 3 {
		t.Fatalf("expected TotalMessagesSent=3, got %d", got)
	}

	m.IncWSErrors()
	if got := m.Snapshot().WSErrors; got != 1 {
		t.Fatalf("expected WSErrors=1, got %d", got)
	}

	m.IncHTTPRequests()
	m.IncHTTPRequests()
	if got := m.Snapshot().HTTPRequestsTotal; got != 2 {
		t.Fatalf("expected HTTPRequestsTotal=2, got %d", got)
	}
}

func TestMetrics_PrometheusTextIsStableAndEscaped(t *testing.T) {
	m := &Metrics{roomOnlineCount: make(map[string]int64)}
	m.IncHTTPRequests()
	m.SetRoomOnlineCount(`room"quoted`, 2)
	text := m.PrometheusText()
	for _, expected := range []string{
		"# TYPE interest_chat_http_requests_total counter",
		"interest_chat_http_requests_total 1",
		`interest_chat_room_online{room_id="room\"quoted"} 2`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("metrics text missing %q: %s", expected, text)
		}
	}
}

// TestMetrics_SetRoomOnlineCount_TracksPerRoom 覆盖按房间维度记录在线人数，
// 且互不干扰。
func TestMetrics_SetRoomOnlineCount_TracksPerRoom(t *testing.T) {
	m := &Metrics{roomOnlineCount: make(map[string]int64)}

	m.SetRoomOnlineCount("room-a", 5)
	m.SetRoomOnlineCount("room-b", 3)
	m.SetRoomOnlineCount("room-a", 7) // 覆盖更新，而不是累加

	snap := m.Snapshot()
	if snap.RoomOnlineCount["room-a"] != 7 {
		t.Fatalf("expected room-a=7 after overwrite, got %d", snap.RoomOnlineCount["room-a"])
	}
	if snap.RoomOnlineCount["room-b"] != 3 {
		t.Fatalf("expected room-b=3, got %d", snap.RoomOnlineCount["room-b"])
	}
}

// TestMetrics_Snapshot_ReturnsIndependentCopy 覆盖 Snapshot 返回的
// RoomOnlineCount 是独立拷贝，调用方修改快照不应影响 Metrics 内部真实状态
// （否则并发场景下会有数据竞争/意外污染的风险）。
func TestMetrics_Snapshot_ReturnsIndependentCopy(t *testing.T) {
	m := &Metrics{roomOnlineCount: make(map[string]int64)}
	m.SetRoomOnlineCount("room-a", 1)

	snap := m.Snapshot()
	snap.RoomOnlineCount["room-a"] = 999 // 修改快照
	snap.RoomOnlineCount["room-injected"] = 123

	fresh := m.Snapshot()
	if fresh.RoomOnlineCount["room-a"] != 1 {
		t.Fatalf("expected internal state to remain 1 despite snapshot mutation, got %d", fresh.RoomOnlineCount["room-a"])
	}
	if _, exists := fresh.RoomOnlineCount["room-injected"]; exists {
		t.Fatal("expected mutation on returned snapshot to not leak into internal state")
	}
}

// TestGlobalMetrics_IsUsable 覆盖包级单例 Global 本身初始化正确、可直接调用
// （防止未来重构时不小心遗漏 roomOnlineCount 的初始化导致 nil map panic）。
func TestGlobalMetrics_IsUsable(t *testing.T) {
	Global.IncHTTPRequests()
	Global.SetRoomOnlineCount("smoke-test-room", 1)
	snap := Global.Snapshot()
	if snap.HTTPRequestsTotal < 1 {
		t.Fatal("expected Global.HTTPRequestsTotal to be at least 1 after increment")
	}
}
