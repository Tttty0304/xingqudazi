package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"xingqudazi-im/server/internal/model"
	"xingqudazi-im/server/internal/service"
)

// createTestUser 插入一个带随机用户名的真实用户行，返回其 ID，并注册
// t.Cleanup 在测试结束后删除（`ON DELETE CASCADE` 会一并清理引用它的
// watch_topics 等外键行，见 migrations/0001_init_schema.up.sql）。用随机
// 用户名而不是固定字符串，避免并发跑多个测试/多次重复运行时撞上
// `username` 唯一约束。
func createTestUser(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	userID := uuid.NewString()
	username := "repotest_" + uuid.NewString()[:8]
	if _, err := db.Exec(context.Background(),
		`INSERT INTO users (id, username, is_guest) VALUES ($1, $2, true)`, userID, username); err != nil {
		t.Fatalf("create test user failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	return userID
}

func TestUserRepository_CreateAndFindByUsername(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	userID := uuid.NewString()
	username := "repotest_" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	user := &model.User{ID: userID, Username: username, PasswordHash: "hash123", IsGuest: false}
	if err := repo.Create(ctx, user); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	found, err := repo.FindByUsername(ctx, username)
	if err != nil {
		t.Fatalf("FindByUsername failed: %v", err)
	}
	if found.ID != userID || found.Username != username || found.IsGuest {
		t.Fatalf("FindByUsername returned unexpected user: %+v", found)
	}
}

func TestUserRepository_FindByUsername_NotFound(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)

	_, err := repo.FindByUsername(context.Background(), "no_such_user_"+uuid.NewString())
	if !errors.Is(err, service.ErrRepositoryUserNotFound) {
		t.Fatalf("expected ErrRepositoryUserNotFound, got %v", err)
	}
}

func TestUserRepository_FindByID(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	userID := uuid.NewString()
	username := "repotest_" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})

	if err := repo.Create(ctx, &model.User{ID: userID, Username: username, IsGuest: true}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	found, err := repo.FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if !found.IsGuest {
		t.Fatalf("expected IsGuest=true (访客用户 password_hash 为空), got false")
	}
	// 访客用户 password_hash 为空字符串（NULL 落库，查询时 COALESCE 回退为空字符串）。
	if found.PasswordHash != "" {
		t.Fatalf("expected empty PasswordHash for guest user, got %q", found.PasswordHash)
	}
}

// TestUserRepository_FindByIDs_IgnoresInvalidUUIDs 是此前修复过的真实缺陷的
// 回归测试：混入非 UUID 格式的 ID（如前端传入的脏数据）不应导致整个批量查询
// 报错（Postgres 对 `id = ANY($1)` 做隐式类型转换失败会 500），而是静默忽略。
func TestUserRepository_FindByIDs_IgnoresInvalidUUIDs(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()

	userID := uuid.NewString()
	username := "repotest_" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID)
	})
	if err := repo.Create(ctx, &model.User{ID: userID, Username: username}); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	users, err := repo.FindByIDs(ctx, []string{userID, "not-a-valid-uuid", "another-bad-id"})
	if err != nil {
		t.Fatalf("FindByIDs should not error on invalid UUID mixed in, got: %v", err)
	}
	if len(users) != 1 || users[0].ID != userID {
		t.Fatalf("expected exactly 1 valid user returned, got %+v", users)
	}
}

func TestUserRepository_FindByIDs_AllInvalid_ReturnsEmpty(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)

	users, err := repo.FindByIDs(context.Background(), []string{"bad-1", "bad-2"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected empty result, got %d users", len(users))
	}
}

// TestUserRepository_SetIsBot_And_IsBot 覆盖能力补齐项（LLM 驱动机器人最小
// 验证）：新建账号默认 is_bot=false；SetIsBot(true) 后 FindByID/IsBot 均应
// 反映最新状态；这条路径故意不通过任何公开 HTTP 接口，只在仓储层验证。
func TestUserRepository_SetIsBot_And_IsBot(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)
	ctx := context.Background()
	userID := createTestUser(t, db)

	isBot, err := repo.IsBot(ctx, userID)
	if err != nil {
		t.Fatalf("IsBot failed: %v", err)
	}
	if isBot {
		t.Fatal("expected newly created account to default is_bot=false")
	}

	if err := repo.SetIsBot(ctx, userID, true); err != nil {
		t.Fatalf("SetIsBot(true) failed: %v", err)
	}

	found, err := repo.FindByID(ctx, userID)
	if err != nil {
		t.Fatalf("FindByID failed: %v", err)
	}
	if !found.IsBot {
		t.Fatal("expected FindByID to reflect is_bot=true after SetIsBot")
	}

	isBot, err = repo.IsBot(ctx, userID)
	if err != nil {
		t.Fatalf("IsBot failed: %v", err)
	}
	if !isBot {
		t.Fatal("expected IsBot to return true after SetIsBot(true)")
	}
}

// TestUserRepository_SetIsBot_UnknownUser_ReturnsNotFound 覆盖边界：对不存在
// 的用户 ID 调用 SetIsBot 应返回 ErrRepositoryUserNotFound，而不是静默成功
// （RowsAffected=0 时的显式判断）。
func TestUserRepository_SetIsBot_UnknownUser_ReturnsNotFound(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)

	err := repo.SetIsBot(context.Background(), uuid.NewString(), true)
	if !errors.Is(err, service.ErrRepositoryUserNotFound) {
		t.Fatalf("expected ErrRepositoryUserNotFound, got %v", err)
	}
}

// TestUserRepository_IsBot_UnknownUser_ReturnsFalseNoError 覆盖边界：查询不
// 存在的用户时返回 false+nil（不视为错误——见 IsBot 方法注释）。
func TestUserRepository_IsBot_UnknownUser_ReturnsFalseNoError(t *testing.T) {
	db := testDB(t)
	repo := NewUserRepository(db)

	isBot, err := repo.IsBot(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("expected no error for unknown user, got: %v", err)
	}
	if isBot {
		t.Fatal("expected false for unknown user")
	}
}
