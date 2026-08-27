// Package session 统一管理浏览器会话 Cookie，避免 JWT 长期暴露给前端 JavaScript。
package session

import (
	"net/http"
	"strings"
)

const CookieName = "im_session"

// TokenFromRequest 优先兼容 Bearer token，随后读取 HttpOnly Cookie。Bearer 仅为
// 非浏览器客户端和既有接口脚本保留；Web 前端默认只使用 Cookie。
func TokenFromRequest(r *http.Request) string {
	if authorization := r.Header.Get("Authorization"); strings.HasPrefix(authorization, "Bearer ") {
		return strings.TrimPrefix(authorization, "Bearer ")
	}
	if cookie, err := r.Cookie(CookieName); err == nil {
		return cookie.Value
	}
	return ""
}

func SetToken(w http.ResponseWriter, token string, secure bool, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearToken(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}
