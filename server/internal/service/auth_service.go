package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"xingqudazi-im/server/internal/model"
)

// UserStore 是 AuthService 依赖的最小数据访问接口，由 repository.UserRepository
// 实现（真实 PostgreSQL），单测中用内存假实现替代，不需要连真实数据库即可覆盖
// register/login/guest 的核心分支逻辑。
type UserStore interface {
	Create(ctx context.Context, u *model.User) error
	FindByUsername(ctx context.Context, username string) (*model.User, error)
	FindByID(ctx context.Context, id string) (*model.User, error)
	// FindByIDs 批量查询用户基础信息（前端展示用户名场景的低成本补充，
	// 替代原先各处 N+1 循环单条查询，同时供新增的 `GET /api/users?ids=` 使用）。
	FindByIDs(ctx context.Context, ids []string) ([]model.User, error)
}

// ErrRepositoryUserNotFound 由 UserStore 实现在查不到用户时返回；
// AuthService 据此与"密码错误"归并为同一个 ErrInvalidCredentials（T14 要求）。
var ErrRepositoryUserNotFound = errors.New("repository_user_not_found")

// TokenBlacklist 由 AuthService.Logout 使用，真实实现见
// repository.RedisTokenBlacklist（能力补齐项：登出后 token 立即失效）。
type TokenBlacklist interface {
	Add(ctx context.Context, token string, ttl time.Duration) error
}

type AuthService struct {
	store      UserStore
	tokenSvc   *TokenService
	allowGuest bool
	// blacklist 可为 nil（未注入时 Logout 静默跳过，不影响注册/登录/访客模式
	// 主流程；单测里大多不需要构造真实 Redis 依赖）。
	blacklist TokenBlacklist
}

func NewAuthService(store UserStore, tokenSvc *TokenService, allowGuest bool) *AuthService {
	return &AuthService{store: store, tokenSvc: tokenSvc, allowGuest: allowGuest}
}

// SetTokenBlacklist 注入登出黑名单实现（与 SetPushNotifier 等其它可选依赖的
// 注入方式保持一致：不改变已有构造函数签名，避免所有单测/调用点都被迫适配）。
func (s *AuthService) SetTokenBlacklist(b TokenBlacklist) {
	s.blacklist = b
}

// Register 对应 Testcase T10-T12：校验用户名/密码格式、检查重名、bcrypt 哈希后落库。
func (s *AuthService) Register(ctx context.Context, username, password string) (*model.User, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	existing, err := s.store.FindByUsername(ctx, username)
	if err != nil && !errors.Is(err, ErrRepositoryUserNotFound) {
		return nil, fmt.Errorf("check existing username: %w", err)
	}
	if existing != nil {
		return nil, ErrUsernameTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &model.User{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: string(hash),
		IsGuest:      false,
	}
	if err := s.store.Create(ctx, user); err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return user, nil
}

// Login 对应 Testcase T13-T14：**不区分**"用户不存在"与"密码错误"，
// 统一返回 ErrInvalidCredentials，防止用户名枚举攻击。
func (s *AuthService) Login(ctx context.Context, username, password string) (*model.User, string, error) {
	user, err := s.store.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrRepositoryUserNotFound) {
			return nil, "", ErrInvalidCredentials
		}
		return nil, "", fmt.Errorf("find user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, "", ErrInvalidCredentials
	}

	token, err := s.tokenSvc.GenerateToken(user.ID, user.IsGuest)
	if err != nil {
		return nil, "", fmt.Errorf("generate token: %w", err)
	}
	return user, token, nil
}

// GuestLogin 对应 Testcase T15：访客模式，无需用户名密码，随机生成访客身份。
func (s *AuthService) GuestLogin(ctx context.Context) (*model.User, string, error) {
	if !s.allowGuest {
		return nil, "", ErrGuestModeDisabled
	}

	user := &model.User{
		ID:       uuid.NewString(),
		Username: fmt.Sprintf("guest_%s", uuid.NewString()[:8]),
		IsGuest:  true,
	}
	if err := s.store.Create(ctx, user); err != nil {
		return nil, "", fmt.Errorf("create guest user: %w", err)
	}

	token, err := s.tokenSvc.GenerateToken(user.ID, user.IsGuest)
	if err != nil {
		return nil, "", fmt.Errorf("generate token: %w", err)
	}
	return user, token, nil
}

// Logout 使当前 token 立即失效（能力补齐项，见 TokenBlacklist 注释）。
// 幂等设计：token 为空、已经过期/格式错误、或未注入黑名单实现时均静默返回
// nil（视为"登出成功"，不向前端暴露内部实现细节，也不因为一次 token 已经
// 自然失效就报错阻塞用户的登出操作）。
func (s *AuthService) Logout(ctx context.Context, tokenString string) error {
	if s.blacklist == nil || tokenString == "" {
		return nil
	}
	claims, err := s.tokenSvc.ParseToken(tokenString)
	if err != nil {
		// token 本身已经不合法（过期/伪造/格式错误），没有必要拉黑一个反正
		// 已经无法通过校验的 token。
		return nil
	}
	var ttl time.Duration
	if claims.ExpiresAt != nil {
		ttl = time.Until(claims.ExpiresAt.Time)
	}
	if err := s.blacklist.Add(ctx, tokenString, ttl); err != nil {
		return fmt.Errorf("blacklist token on logout: %w", err)
	}
	return nil
}
