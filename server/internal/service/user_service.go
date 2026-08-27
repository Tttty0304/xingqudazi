package service

import (
	"context"
	"errors"
	"fmt"

	"xingqudazi-im/server/internal/model"
)

// UserService 是一个薄封装：把 UserStore 的只读查询能力（单条/批量/按用户名）
// 暴露给 api 层复用，避免 api.UserHandler 直接依赖 repository 具体实现
// （与 FriendService/ConversationService 等既有分层原则一致）。本次新增，
// 用于补齐"根据用户ID批量查用户名"这一此前缺失的能力（见 Task9 前端功能闭环）。
type UserService struct {
	store UserStore
}

type UserProfileUpdater interface {
	UpdateProfile(ctx context.Context, userID, avatarURL, bio string) (*model.User, error)
}

func NewUserService(store UserStore) *UserService {
	return &UserService{store: store}
}

// FindByUsername 对应 `GET /api/users/lookup?username=`：按用户名精确查找，
// 供前端"添加好友"场景先把用户名转成 user_id 再调用
// `POST /api/friends/requests`。
func (s *UserService) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	user, err := s.store.FindByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrRepositoryUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("find user by username: %w", err)
	}
	return user, nil
}

// FindByIDs 对应 `GET /api/users?ids=`：批量查询用户基础信息，供前端展示
// 会话列表/群聊消息发送者等场景的真实用户名（替代此前"用户ID前8位"的占位展示）。
func (s *UserService) FindByIDs(ctx context.Context, ids []string) ([]model.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	users, err := s.store.FindByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("find users by ids: %w", err)
	}
	return users, nil
}

func (s *UserService) GetProfile(ctx context.Context, userID string) (*model.User, error) {
	user, err := s.store.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, ErrRepositoryUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("find profile: %w", err)
	}
	return user, nil
}

func (s *UserService) UpdateProfile(ctx context.Context, userID, avatarURL, bio string) (*model.User, error) {
	if len([]rune(bio)) > 280 {
		return nil, ErrInvalidBio
	}
	updater, ok := s.store.(UserProfileUpdater)
	if !ok {
		return nil, fmt.Errorf("user store does not support profile update")
	}
	user, err := updater.UpdateProfile(ctx, userID, avatarURL, bio)
	if err != nil {
		if errors.Is(err, ErrRepositoryUserNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("update profile: %w", err)
	}
	return user, nil
}
