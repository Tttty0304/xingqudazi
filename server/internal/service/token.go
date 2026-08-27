package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 是本项目 JWT 的自定义声明，供 HTTP 中间件与 WS 握手鉴权共用（Task2 已确认
// 的"WS 鉴权中间件"基础设施，Task4 接入 WebSocket 时直接复用 ParseToken）。
type Claims struct {
	UserID  string `json:"user_id"`
	IsGuest bool   `json:"is_guest"`
	jwt.RegisteredClaims
}

// ErrInvalidToken 统一表示"token 缺失/格式错误/签名不合法/已过期"这一大类问题，
// 不向调用方泄露具体是哪种失败原因（避免给伪造 token 的攻击者调试线索）。
var ErrInvalidToken = errors.New("invalid_token")

// TokenService 封装 JWT 的签发与校验。
type TokenService struct {
	secret []byte
	expiry time.Duration
}

func NewTokenService(secret string, expiry time.Duration) *TokenService {
	return &TokenService{secret: []byte(secret), expiry: expiry}
}

// GenerateToken 为指定用户签发 JWT，`is_guest` 会一并编码进 claims，
// 供后续鉴权中间件/业务逻辑判断是否为访客身份。
func (s *TokenService) GenerateToken(userID string, isGuest bool) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:  userID,
		IsGuest: isGuest,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiry)),
			Subject:   userID,
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("sign token: %w", err)
	}
	return signed, nil
}

// ParseToken 校验并解析 JWT，返回其中的 Claims。
// 覆盖：签名不合法、已过期、格式错误 —— 均统一返回 ErrInvalidToken。
func (s *TokenService) ParseToken(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
