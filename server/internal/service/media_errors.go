package service

import "errors"

// 多媒体消息相关的语义化错误（Task16/T90-T92：图片上传）。
var (
	ErrUnsupportedMediaType = errors.New("unsupported_media_type")
	ErrFileTooLarge         = errors.New("file_too_large")
)
