package service

import (
	"regexp"
)

// 用户名/密码校验规则（★2 已确认：用户名+密码注册登录 + 访客模式）。
// 规则从简但明确，避免"看似有校验但实际什么都放过"的假阳性。
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

// hasLetterPattern/hasDigitPattern 用于密码复杂度校验（能力补齐项）：此前
// `ValidatePassword` 只检查长度，纯数字（如 "12345678"）也能通过，是真实的
// 安全缺口——弱密码空间过小，更容易被暴力破解（即使已有登录接口限流，密码
// 强度本身仍是独立的一道防线）。
var hasLetterPattern = regexp.MustCompile(`[a-zA-Z]`)
var hasDigitPattern = regexp.MustCompile(`[0-9]`)

const minPasswordLength = 8

// ValidateUsername 对应 Testcase 校验：用户名 3-32 位，仅允许字母/数字/下划线。
func ValidateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return ErrInvalidUsername
	}
	return nil
}

// ValidatePassword 对应 Testcase T12：密码过短（如 "123"）应被拒绝；
// 能力补齐后新增要求：必须同时包含字母与数字（纯数字/纯字母均不合格），
// 不要求特殊符号——在"防弱密码"与"不过度增加注册门槛"之间取一个平衡点，
// 与本项目 demo/评估场景的定位一致（不强制要求大小写混合/特殊字符）。
func ValidatePassword(password string) error {
	if len(password) < minPasswordLength {
		return ErrInvalidPassword
	}
	if !hasLetterPattern.MatchString(password) || !hasDigitPattern.MatchString(password) {
		return ErrInvalidPassword
	}
	return nil
}
