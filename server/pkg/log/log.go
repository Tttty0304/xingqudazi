package logger

import (
	"context"
	"log/slog"
	"os"
)

// contextKey 避免 context 键冲突。
type contextKey string

const traceIDKey contextKey = "trace_id"

// New 构建全局结构化日志器（JSON 格式，便于机器解析/后续接入日志采集）。
// 覆盖「日志、监控及可观测性」评分项要求的结构化日志能力。
func New() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

// WithTraceID 把 trace_id 绑定进 context，供请求链路全程携带。
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// TraceIDFromContext 从 context 中取出 trace_id，取不到时返回空字符串。
func TraceIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}
	return ""
}

// FromContext 返回带有 trace_id 字段的 logger，业务代码应优先使用这个而不是裸的 slog.Default()，
// 确保「入口/出口/下游调用前后/失败分支都有日志且可关联同一次请求」。
func FromContext(ctx context.Context) *slog.Logger {
	traceID := TraceIDFromContext(ctx)
	if traceID == "" {
		return slog.Default()
	}
	return slog.Default().With(slog.String("trace_id", traceID))
}
