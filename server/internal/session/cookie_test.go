package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenFromRequestPrefersBearerThenCookie(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	request.AddCookie(&http.Cookie{Name: CookieName, Value: "cookie-token"})
	if got := TokenFromRequest(request); got != "cookie-token" {
		t.Fatalf("expected cookie token, got %q", got)
	}
	request.Header.Set("Authorization", "Bearer header-token")
	if got := TokenFromRequest(request); got != "header-token" {
		t.Fatalf("expected bearer token to take priority, got %q", got)
	}
}

func TestSetAndClearTokenUseSafeCookieAttributes(t *testing.T) {
	recorder := httptest.NewRecorder()
	SetToken(recorder, "token-value", true, 900)
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != CookieName || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge != 900 {
		t.Fatalf("unexpected session cookie: %+v", cookie)
	}

	cleared := httptest.NewRecorder()
	ClearToken(cleared, true)
	if got := cleared.Result().Cookies()[0].MaxAge; got != -1 {
		t.Fatalf("expected clear cookie max age -1, got %d", got)
	}
}
