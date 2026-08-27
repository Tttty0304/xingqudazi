package model

import "time"

// MediaAsset 对应 migrations 中的 media_assets 表（Task16，P0 图片消息）。
type MediaAsset struct {
	ID        string
	OwnerID   string
	MediaType string // image | voice | file（P0 仅接受 image，voice/file 为 P1，暂不接受上传）
	URL       string
	MimeType  string
	SizeBytes int64
	CreatedAt time.Time
}
