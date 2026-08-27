package logger

import (
	"context"
	"testing"
)

// TestTraceIDFromContext_EmptyWhenNotSet 覆盖未绑定过 trace_id 的 context 应
// 返回空字符串，而不是 panic 或返回错误类型断言的默认零值以外的内容
// （能力补齐项：此前 pkg/log 覆盖率为 0%）。
func TestTraceIDFromContext_EmptyWhenNotSet(t *testing.T) {
	if got := TraceIDFromContext(context.Background()); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

// TestWithTraceID_RoundTrip 覆盖 WithTraceID 写入 + TraceIDFromContext 读出的
// 基本往返正确性，这是贯穿全项目 trace_id 链路追踪能力的最基础保证。
func TestWithTraceID_RoundTrip(t *testing.T) {
	ctx := WithTraceID(context.Background(), "trace-abc-123")
	if got := TraceIDFromContext(ctx); got != "trace-abc-123" {
		t.Fatalf("expected trace-abc-123, got %q", got)
	}
}

// TestFromContext_WithoutTraceID_ReturnsDefaultLogger 覆盖没有 trace_id 时
// FromContext 应能正常返回一个可用的 logger（不 panic），而不强制要求所有
// 调用点都必须先设置 trace_id。
func TestFromContext_WithoutTraceID_ReturnsDefaultLogger(t *testing.T) {
	log := FromContext(context.Background())
	if log == nil {
		t.Fatal("expected non-nil logger even without trace_id in context")
	}
}

// TestFromContext_WithTraceID_ReturnsUsableLogger 覆盖设置了 trace_id 后
// FromContext 返回的 logger 仍然可用（Enabled 检查不会因为附加了字段而失效）。
func TestFromContext_WithTraceID_ReturnsUsableLogger(t *testing.T) {
	ctx := WithTraceID(context.Background(), "trace-xyz")
	log := FromContext(ctx)
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
	if !log.Enabled(ctx, 0) {
		t.Fatal("expected logger to remain enabled for info level after attaching trace_id")
	}
}

// TestNew_ReturnsUsableLogger 覆盖 New() 构造的全局 logger 可正常使用，且
// 会被设为 slog 默认 logger（FromContext 依赖 slog.Default()）。
func TestNew_ReturnsUsableLogger(t *testing.T) {
	log := New()
	if log == nil {
		t.Fatal("expected non-nil logger from New()")
	}
	log.Info("smoke test log line", "key", "value")
}
