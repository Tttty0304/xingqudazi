package service

import (
	"context"
	"testing"

	"xingqudazi-im/server/internal/model"
)

// TestUserService_FindByUsername_T121 对应 T121（本轮新增）：按用户名查找，
// 供前端"添加好友"场景先把用户名转成 user_id。
func TestUserService_FindByUsername_T121(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice-id", Username: "alice"})
	svc := NewUserService(users)

	user, err := svc.FindByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("FindByUsername failed: %v", err)
	}
	if user.ID != "alice-id" {
		t.Errorf("expected id=alice-id, got %s", user.ID)
	}

	_, err = svc.FindByUsername(context.Background(), "no-such-user")
	if err != ErrUserNotFound {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}

// TestUserService_FindByIDs_T122 对应 T122（本轮新增）：批量查询用户基础信息，
// 供前端展示会话列表/群聊消息发送者的真实用户名（替代"用户ID前8位"占位展示）。
func TestUserService_FindByIDs_T122(t *testing.T) {
	users := newFakeUserStore()
	users.Create(context.Background(), &model.User{ID: "alice-id", Username: "alice"})
	users.Create(context.Background(), &model.User{ID: "bob-id", Username: "bob"})
	svc := NewUserService(users)

	result, err := svc.FindByIDs(context.Background(), []string{"alice-id", "bob-id", "no-such-id"})
	if err != nil {
		t.Fatalf("FindByIDs failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 users (missing id silently ignored), got %+v", result)
	}

	empty, err := svc.FindByIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("FindByIDs(nil) failed: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected empty result for nil ids, got %+v", empty)
	}
}
