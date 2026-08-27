package ws

import "testing"

// TestClientAllowMessage 覆盖 Task6/T40 的纯限流逻辑：固定窗口计数器。
func TestClientAllowMessage(t *testing.T) {
	t.Run("unlimited_when_limit_non_positive", func(t *testing.T) {
		c := &Client{}
		for i := 0; i < 100; i++ {
			if !c.allowMessage(0) {
				t.Fatalf("expected unlimited (limit=0) to always allow, blocked at i=%d", i)
			}
		}
	})

	t.Run("blocks_after_exceeding_limit", func(t *testing.T) {
		c := &Client{}
		limit := 3
		for i := 0; i < limit; i++ {
			if !c.allowMessage(limit) {
				t.Fatalf("expected message %d within limit %d to be allowed", i+1, limit)
			}
		}
		if c.allowMessage(limit) {
			t.Fatalf("expected message beyond limit %d to be rejected", limit)
		}
	})

	t.Run("burst_limit_blocks_rapid_fire_even_under_per_minute_quota", func(t *testing.T) {
		c := &Client{}
		limit := 60 // 按分钟配额充裕，不应是拦截原因
		allowedCount := 0
		blocked := false
		for i := 0; i < 20; i++ {
			if c.allowMessage(limit) {
				allowedCount++
			} else {
				blocked = true
			}
		}
		if !blocked {
			t.Fatalf("expected burst of 20 rapid messages to eventually be blocked by burst limit, allowedCount=%d", allowedCount)
		}
		if allowedCount > burstLimit {
			t.Fatalf("expected at most burstLimit=%d messages allowed before blocking, got %d", burstLimit, allowedCount)
		}
	})

	t.Run("recovers_after_window_reset", func(t *testing.T) {
		c := &Client{}
		limit := 1
		if !c.allowMessage(limit) {
			t.Fatal("expected first message to be allowed")
		}
		if c.allowMessage(limit) {
			t.Fatal("expected second message within same window to be rejected")
		}
		// 模拟两个窗口都已过期（回拨窗口起点，等价于时间流逝超过窗口长度）。
		c.rateWindowStart = c.rateWindowStart.Add(-2 * 60 * 1e9) // -2分钟（纳秒单位）
		c.burstWindowStart = c.burstWindowStart.Add(-5 * 1e9)    // -5秒（纳秒单位），大于 burstWindow
		if !c.allowMessage(limit) {
			t.Fatal("expected message after window reset to be allowed again")
		}
	})
}
