package repository

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testDB 返回一个真实 Postgres 连接池，供 repository 层测试直接对真实数据库做
// 验证（能力补齐项：此前 repository 层测试覆盖率为 0%，`internal/api` 层同样很
// 低，只靠黑盒集成测试脚本 `scripts/integration_test.py` 兜底，单元测试完全
// 覆盖不到 SQL 语句本身是否正确）。
//
// 之所以选择连真实数据库而不是引入 sqlmock：本项目大量 SQL 用了 Postgres 特有
// 语法（`= ANY($1)`、`::text` 类型转换、`ON DELETE CASCADE` 级联删除等），
// mock 出的期望断言容易与真实执行行为脱节，价值有限；真实数据库测试更贴近
// 实际运行环境，且当前 `docker compose` 已经在跑一个真实 Postgres 实例，
// 复用它做测试成本几乎为零。
//
// 通过 TEST_POSTGRES_DSN 环境变量指定测试数据库连接串（未设置时回落到与
// `deploy/docker-compose.yml` 默认配置一致的本地地址，即 `docker compose up`
// 后可直接跑）；连接失败时跳过（不是失败），避免在没有起 Docker 环境的机器上
// （如某些未来接入的 CI 场景）阻塞整个测试套件——`go test` 的默认行为决不能
// 是"必须先手动起数据库才能跑通全部测试"，跳过并给出清晰提示才是正确姿势。
func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		dsn = "postgres://im_user:im_password@localhost:5432/xingqudazi_im?sslmode=disable"
	}
	pool, err := NewPostgresPool(context.Background(), dsn, 5, 1)
	if err != nil {
		t.Skipf("skip repository test: cannot connect to test postgres (%v); "+
			"start `docker compose -f deploy/docker-compose.yml up -d postgres` "+
			"or set TEST_POSTGRES_DSN to point at a reachable Postgres instance", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// createTestRoom 插入一个带随机名称的真实房间行，返回其 ID，并注册 t.Cleanup
// 在测试结束后删除。与 createTestUser（watch_topic_repository_test.go）同款
// 设计，供需要一个真实房间外键（如 bot_action_log.room_id）的测试复用。
func createTestRoom(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	roomID := uuid.NewString()
	if _, err := db.Exec(context.Background(),
		`INSERT INTO rooms (id, name, topic, is_preset) VALUES ($1, $2, $3, true)`,
		roomID, "repotest_room_"+uuid.NewString()[:8], "单测用途"); err != nil {
		t.Fatalf("create test room failed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), `DELETE FROM rooms WHERE id = $1`, roomID)
	})
	return roomID
}
