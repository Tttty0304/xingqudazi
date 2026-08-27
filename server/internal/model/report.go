package model

import "time"

// Report 对应 migrations 中的 reports 表（Task18，基础内容安全：举报入口）。
type Report struct {
	ID         string
	ReporterID string
	TargetType string // message | direct_message | user
	TargetID   string
	Reason     string
	Status     string // open | reviewed | dismissed
	CreatedAt  time.Time
}
