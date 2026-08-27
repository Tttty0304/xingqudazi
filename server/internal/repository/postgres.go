package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPostgresPool 建立 PostgreSQL 连接池；带超时与显式健康检查（Ping），
// 避免"连接对象创建成功"被误判为"数据库真实可用"。
// maxConns/minConns 由调用方（config.Config）从环境变量传入，此前是硬编码的
// 10/1（Task10 性能与成本落地复核时发现：demo 场景够用，但连接数上限完全不
// 可配置，压测/扩容时无法调整，属于真实的可配置性缺口，本次补齐为可配置项，
// 默认值保持不变以不改变现有行为）。
func NewPostgresPool(ctx context.Context, dsn string, maxConns, minConns int32) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres dsn: %w", err)
	}
	poolCfg.MaxConns = maxConns
	poolCfg.MinConns = minConns
	poolCfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping failed: %w", err)
	}
	return pool, nil
}
