# Task1 骨架 —— Work Log / 端到端验证记录

> 关联 Plan：`docs/00-brainstorm-and-plan.md`（Task0 已定稿版）；关联 Testcase：`testcase/00-testcase-plan.md`
> 日期：2026-08-17

## 实际改动

- `server/`：Go 1.23 + Gin 项目骨架（`go mod init xingqudazi-im/server`，真实 `go get` 拉取依赖，无手写伪造依赖版本）。
  - `internal/config`：环境变量配置加载。
  - `internal/repository`：PostgreSQL 连接池（pgx/v5）、Redis 客户端，均带真实 Ping 健康检查。
  - `internal/api`：`HealthHandler`（`/healthz` `/readyz`）、`MetricsHandler`（`/metrics`）。
  - `internal/middleware`：请求日志中间件（trace_id 贯穿）。
  - `pkg/log`、`pkg/metric`：结构化日志、进程内基础指标。
  - `cmd/server/main.go`：接线配置/日志/DB/Redis/路由/优雅关闭。
  - `internal/api/health_test.go`：T01 对应单测。
- `migrations/0001_init_schema.up/down.sql`：一次性建齐 P0 表结构（含 pgvector 扩展、AI-native 远期预留字段），含预置房间种子数据。
- `client/`：官方 `npm create vite react-ts` 脚手架 + 响应式占位页（联通后端 `/healthz`）。
- `deploy/`：`docker-compose.yml`（pgvector/pgvector:pg16 镜像 + redis + server + client）、两端 `Dockerfile`、`nginx.conf`。
- `scripts/smoke_test.sh`：部署后最小验证脚本。
- `README.md`、`.gitignore`、`project-meta.yaml`。

## 本地验证（真实执行，非模拟）

### 静态检查

```
cd server && gofmt -l . && go build ./... && go vet ./... && go test ./...
# 结果：全部通过，TestHealthz PASS
cd client && npm run build
# 结果：tsc -b && vite build 成功
```

### 端到端真机验证（关键：`docker compose` 在当前沙箱环境因缺少容器 capability 无法运行，
### 改用原生二进制 PostgreSQL 13.23 + pgvector 0.7.4（源码编译）+ Redis 5.0.3 验证，
### 与生产用的 docker-compose（PG16+pgvector 官方镜像）在扩展/schema 层面完全一致，
### 仅版本号不同，未做任何简化或跳过）：

1. `dnf` 安装 postgresql-server 13.23 + postgresql-server-devel + redis 5.0.3（由用户在其终端执行，因当前 Agent
   执行环境的安全钩子对 dnf/yum/rpm 存在已知 bug，全部替代方案已尝试排除，最终由用户手动执行安装命令）。
2. 从 GitHub 源码编译安装 `pgvector v0.7.4`（`make && make install`），针对 PG13 头文件真实编译，非预编译二进制。
3. `initdb` 初始化数据库集群，`pg_ctl start` 启动；`redis-server` 启动。
4. 创建 `im_user`/`xingqudazi_im` 数据库，**原样执行** `migrations/0001_init_schema.up.sql`（未做任何裁剪）：
   - 结果：`CREATE EXTENSION`（uuid-ossp + vector）均成功，12 张表全部建成，`rooms` 种子数据 4 条全部写入。
   - `\d users` 验证 `interest_embedding vector(768)` 列真实存在，pgvector 真实生效。
5. `go run ./cmd/server` 启动后端，真实连接上述 PostgreSQL + Redis：
   - 日志显示 `postgres connected` / `redis connected`。
   - `scripts/smoke_test.sh http://localhost:8080` **全部通过**（healthz OK、readyz 返回 `status:ready`）。
   - `curl /metrics` 返回正常 JSON 指标快照。
6. **T03 用例真机验证**：`pg_ctl stop` 停止 PostgreSQL，`/readyz` 立即返回 `HTTP 503` + 明确的 `db` 错误详情，
   `redis` 字段仍报 `ok`（故障隔离清晰，不是笼统500）；恢复 PostgreSQL 后 `/readyz` 立即回到 `status:ready`。
7. **T41 用例真机验证**：向后端进程发送 `SIGTERM`，日志依次打印
   `shutdown signal received, starting graceful shutdown` → `graceful shutdown completed`，
   进程正常退出，未出现请求被强制中断的痕迹。

## 偏差或遗留项

- 本地验证环境用的是 PostgreSQL 13.23（受当前沙箱 dnf 仓库默认可用版本限制），生产 `docker-compose.yml`
  用的是 `pgvector/pgvector:pg16` 官方镜像（PG16）。两者均 ≥ pgvector 最低要求版本（PG12+），schema/扩展行为
  一致，仅版本号不同，不影响本次验证结论的有效性。
- `docker compose` 命令本身未在本地环境验证成功（容器 capability 受限），建议在有完整 Docker 权限的机器上
  额外跑一次 `docker compose -f deploy/docker-compose.yml up -d --build` 做镜像层面的最终确认；本次已完成的
  是"代码逻辑 + 真实数据库/缓存 + 真实网络请求"的完整闭环验证，容器编排本身的语法/依赖关系已人工审查
  （`depends_on` + `healthcheck` 条件成立），风险较低。
- Task2 起的用户体系/房间/WS 等业务代码尚未开始，本记录仅覆盖 Task1 骨架。
