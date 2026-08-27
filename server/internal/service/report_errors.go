package service

import "errors"

// 内容安全（举报）相关的语义化错误（Task18/T80）。
var (
	ErrReportTargetNotFound    = errors.New("report_target_not_found")
	ErrInvalidReportTargetType = errors.New("invalid_report_target_type")
)
