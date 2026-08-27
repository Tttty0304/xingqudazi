package service

import "errors"

// 鉴权相关的语义化错误。api 层据此翻译成对应的 HTTP 状态码与错误码
// （见 Testcase T10-T15：username_taken/invalid_password/invalid_credentials）。
var (
	ErrUsernameTaken      = errors.New("username_taken")
	ErrInvalidPassword    = errors.New("invalid_password")
	ErrInvalidUsername    = errors.New("invalid_username")
	ErrInvalidCredentials = errors.New("invalid_credentials")
	ErrGuestModeDisabled  = errors.New("guest_mode_disabled")
	ErrUserNotFound       = errors.New("user_not_found")
	ErrInvalidRoomName    = errors.New("invalid_room_name")
	ErrInvalidRoomTopic   = errors.New("invalid_room_topic")
	ErrInvalidBio         = errors.New("invalid_bio")
)
