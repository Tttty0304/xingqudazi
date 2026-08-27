package service

import "errors"

// 房间/消息相关的语义化错误（对应 Testcase T22：房间不存在 404）。
var (
	ErrRoomNotFound = errors.New("room_not_found")
)
