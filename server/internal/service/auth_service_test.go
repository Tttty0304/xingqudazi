package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"xingqudazi-im/server/internal/model"
)

// fakeUserStore 是 UserStore 的内存假实现，仅用于单测，不依赖真实数据库。
type fakeUserStore struct {
	mu         sync.Mutex
	byID       map[string]*model.User
	byUsername map[string]*model.User
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{
		byID:       make(map[string]*model.User),
		byUsername: make(map[string]*model.User),
	}
}

func (f *fakeUserStore) Create(_ context.Context, u *model.User) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	copyUser := *u
	f.byID[u.ID] = &copyUser
	f.byUsername[u.Username] = &copyUser
	return nil
}

func (f *fakeUserStore) FindByUsername(_ context.Context, username string) (*model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byUsername[username]
	if !ok {
		return nil, ErrRepositoryUserNotFound
	}
	return u, nil
}

func (f *fakeUserStore) FindByID(_ context.Context, id string) (*model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.byID[id]
	if !ok {
		return nil, ErrRepositoryUserNotFound
	}
	return u, nil
}

func (f *fakeUserStore) FindByIDs(_ context.Context, ids []string) ([]model.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]model.User, 0, len(ids))
	for _, id := range ids {
		if u, ok := f.byID[id]; ok {
			result = append(result, *u)
		}
	}
	return result, nil
}

func newTestAuthService() *AuthService {
	tokenSvc := NewTokenService("test-secret", time.Hour)
	return NewAuthService(newFakeUserStore(), tokenSvc, true)
}

// TestAuthService_Register_Success 对应 T10：注册成功。
func TestAuthService_Register_Success(t *testing.T) {
	svc := newTestAuthService()
	user, err := svc.Register(context.Background(), "alice", "Passw0rd!")
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}
	if user.Username != "alice" {
		t.Errorf("expected username=alice, got %s", user.Username)
	}
	if user.ID == "" {
		t.Error("expected non-empty user id")
	}
	if user.PasswordHash == "Passw0rd!" {
		t.Error("password must be hashed, not stored in plaintext")
	}
}

// TestAuthService_Register_DuplicateUsername 对应 T11：重复用户名。
func TestAuthService_Register_DuplicateUsername(t *testing.T) {
	svc := newTestAuthService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "alice", "Passw0rd!"); err != nil {
		t.Fatalf("first register failed: %v", err)
	}
	_, err := svc.Register(ctx, "alice", "AnotherPass1!")
	if err != ErrUsernameTaken {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
}

// TestAuthService_Register_InvalidPassword 对应 T12：密码过短。
func TestAuthService_Register_InvalidPassword(t *testing.T) {
	svc := newTestAuthService()
	_, err := svc.Register(context.Background(), "bob", "123")
	if err != ErrInvalidPassword {
		t.Fatalf("expected ErrInvalidPassword, got %v", err)
	}
}

// TestAuthService_Login_Success 对应 T13：正确凭证登录成功。
func TestAuthService_Login_Success(t *testing.T) {
	svc := newTestAuthService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "alice", "Passw0rd!"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	user, token, err := svc.Login(ctx, "alice", "Passw0rd!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}
	if user.Username != "alice" {
		t.Errorf("expected username=alice, got %s", user.Username)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

// TestAuthService_Login_WrongPassword 对应 T14：密码错误。
func TestAuthService_Login_WrongPassword(t *testing.T) {
	svc := newTestAuthService()
	ctx := context.Background()
	if _, err := svc.Register(ctx, "alice", "Passw0rd!"); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, _, err := svc.Login(ctx, "alice", "wrong-password")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

// TestAuthService_Login_UserNotFound_SameErrorAsWrongPassword 对应 T14 的硬性要求：
// "用户不存在" 与 "密码错误" 必须返回完全相同的错误，防止用户名枚举。
func TestAuthService_Login_UserNotFound_SameErrorAsWrongPassword(t *testing.T) {
	svc := newTestAuthService()
	_, _, err := svc.Login(context.Background(), "no-such-user", "whatever")
	if err != ErrInvalidCredentials {
		t.Fatalf("expected ErrInvalidCredentials for nonexistent user (same as wrong password), got %v", err)
	}
}

// TestAuthService_GuestLogin_Success 对应 T15：访客模式。
func TestAuthService_GuestLogin_Success(t *testing.T) {
	svc := newTestAuthService()
	user, token, err := svc.GuestLogin(context.Background())
	if err != nil {
		t.Fatalf("GuestLogin failed: %v", err)
	}
	if !user.IsGuest {
		t.Error("expected is_guest=true")
	}
	if token == "" {
		t.Error("expected non-empty token")
	}

	tokenSvc := NewTokenService("test-secret", time.Hour)
	claims, err := tokenSvc.ParseToken(token)
	if err != nil {
		t.Fatalf("ParseToken failed: %v", err)
	}
	if !claims.IsGuest {
		t.Error("expected token claims is_guest=true")
	}
}

// TestAuthService_GuestLogin_Disabled 验证 allowGuest=false 时访客模式被拒绝。
func TestAuthService_GuestLogin_Disabled(t *testing.T) {
	tokenSvc := NewTokenService("test-secret", time.Hour)
	svc := NewAuthService(newFakeUserStore(), tokenSvc, false)

	_, _, err := svc.GuestLogin(context.Background())
	if err != ErrGuestModeDisabled {
		t.Fatalf("expected ErrGuestModeDisabled, got %v", err)
	}
}
