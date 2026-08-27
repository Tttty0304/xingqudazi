package service

import "errors"

// 关注事项相关的语义化错误（Task19/P1）。
var (
	ErrInvalidWatchTopic = errors.New("invalid_watch_topic")
	// ErrWatchTopicNotFound 对应 T123（本轮新增）：删除不存在/非本人所有的关注事项。
	ErrWatchTopicNotFound = errors.New("watch_topic_not_found")
)
