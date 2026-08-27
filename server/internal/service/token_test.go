package service

import (
	"testing"
	"time"
)

func TestTokenService_GenerateAndParse_RoundTrip(t *testing.T) {
	svc := NewTokenService("test-secret", time.Hour)

	token, err := svc.GenerateToken("user-123", false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := svc.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}
	if claims.UserID != "user-123" {
		t.Errorf("expected user_id=user-123, got %s", claims.UserID)
	}
	if claims.IsGuest {
		t.Error("expected is_guest=false")
	}
}

func TestTokenService_ParseToken_Expired(t *testing.T) {
	svc := NewTokenService("test-secret", -time.Hour) // 已过期

	token, err := svc.GenerateToken("user-123", false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = svc.ParseToken(token)
	if err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken for expired token, got %v", err)
	}
}

func TestTokenService_ParseToken_WrongSecret(t *testing.T) {
	svc1 := NewTokenService("secret-a", time.Hour)
	svc2 := NewTokenService("secret-b", time.Hour)

	token, err := svc1.GenerateToken("user-123", false)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	_, err = svc2.ParseToken(token)
	if err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken for wrong secret, got %v", err)
	}
}

func TestTokenService_ParseToken_Malformed(t *testing.T) {
	svc := NewTokenService("test-secret", time.Hour)

	_, err := svc.ParseToken("not-a-valid-jwt")
	if err != ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken for malformed token, got %v", err)
	}
}

func TestTokenService_GenerateToken_GuestFlag(t *testing.T) {
	svc := NewTokenService("test-secret", time.Hour)

	token, err := svc.GenerateToken("guest-abc", true)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := svc.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}
	if !claims.IsGuest {
		t.Error("expected is_guest=true to round-trip correctly")
	}
}
