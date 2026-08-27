# Task10-13 收尾 Work Log（性能与成本 / 测试与代码质量 / 部署文档完善 / 演示准备）

> 日期：2026-08-18

## Task10：性能与成本落地

### 发现的真实问题

`friendships`/`conversations`/`match_candidates` 三张"双向关系表"的查询模式统一是
`WHERE (col_a = $1 OR col_b = $1)`，但 0001 建表时只给其中一侧建了索引：
- `friendships` 只有 `idx_friendships_target(target_id, status)`，缺 `requester_id` 侧
- `conversations` 只有 `UNIQUE(user_a_id, user_b_id)`（只对 `user_a_id` 侧友好），缺
  `user_b_id` 侧
- `match_candidates` 只有 `idx_match_candidates_user_a`，缺 `user_b_id` 侧

当前数据量下 `EXPLAIN` 尚未表现出显著代价，但这是真实的设计遗漏——按 `target_id`/
`user_b_id` 一侧命中的查询无法走索引，数据量增长后会退化为全表扫描。

PostgreSQL 连接池大小（`MaxConns=10, MinConns=1`）此前硬编码在 `NewPostgresPool` 内，
压测/扩容时完全无法调整。

### 改动

- `migrations/0003_perf_indexes.up/down.sql`：补齐 `idx_friendships_requester`、
  `idx_conversations_user_b`、`idx_match_candidates_user_b`；已在真实运行中的容器
  手动执行（`docker compose exec postgres psql ... < 0003_perf_indexes.up.sql`）
  并确认索引真实创建成功，同时同步进 `docker-compose.yml` 的初始化挂载列表，
  保证全新环境首次启动时自动生效。
- `server/internal/config/config.go` + `internal/repository/postgres.go` +
  `cmd/server/main.go`：连接池大小改为 `POSTGRES_MAX_CONNS`/`POSTGRES_MIN_CONNS`
  环境变量可配置，默认值不变（10/1），不改变现有行为。
- `deploy/nginx.conf`：为 Vite 构建产物的 `/assets/` 静态资源（文件名自带 content
  hash）加一年强缓存 `Cache-Control: public, max-age=31536000, immutable`，已用
  `curl -I` 验证响应头真实生效。
- 镜像体积复核：`docker images` 实测 `deploy-server` ≈80.7MB、`deploy-client`
  ≈76.3MB（均为多阶段构建 + alpine/nginx:alpine 轻量运行时），复核结论是**当前
  已属精简水平，未见进一步压缩空间**（Go 静态二进制 + alpine 基础镜像已是常规
  最佳实践，未发现可裁剪的多余依赖层）。
- 前端产物体积复核：`npm run build` 实测 JS ≈260KB（gzip 后 ≈82KB），当前页面
  数量下不需要引入代码分割，如实记录在 `docs/13-architecture-and-adr.md`。

## Task11：测试与代码质量

### 发现的真实问题

此前只靠 `gofmt -l .` + `go vet ./...` 兜底，没有统一的 lint 配置，也没有覆盖
`staticcheck`/`errcheck`/`unused` 等更严格的检查类别。接入
`server/.golangci.yml`（启用 errcheck/govet/staticcheck/unused/ineffassign/gofmt）
后，`golangci-lint run ./...` 首次运行即发现 3 类真实问题：

1. `internal/ws/client.go` 心跳超时期限设置（`SetReadDeadline`/`SetWriteDeadline`）
   与关闭帧发送（`WriteMessage(CloseMessage, ...)`）共 6 处返回值未检查——均为
   "最佳努力"操作（失败后续 `ReadMessage`/`WriteMessage` 会立即报错并触发既有
   清理逻辑），显式 `_ = ` 丢弃并加注释说明原因，不改变运行时行为。
2. `pkg/webpush/webpush.go`：`elliptic.Marshal` 在 Go 1.21 起已废弃（staticcheck
   `SA1019`），改用官方推荐的 `crypto/ecdh` 路径（`priv.ECDH().PublicKey().Bytes()`），
   输出格式与原函数完全等价（未压缩点 `0x04||X||Y`），已跑 `go test ./pkg/webpush/...`
   确认原有测试（含真实 RFC8291/8292 端到端加密/解密断言）全部仍通过。
3. `internal/service/friend_service_test.go`：`fakeFriendshipStore.seqID` 字段声明
   后从未被读写（真实死代码，大概率是从 `conversation_service_test.go` 的类似结构
   复制时误留），直接删除。

修复后 `golangci-lint run ./...` 退出码 0（全绿），`go test ./... -v` 全部仍通过。

### 新增集成测试脚本

`scripts/integration_test.py`：把此前多轮对话里临时写在 `/tmp/` 下、验证完即丢弃的
接口级验证脚本固化为仓库内可重复运行的黑盒集成测试，覆盖鉴权/房间/用户查找批量
查询/好友关系链/关注事项/AI推荐/私聊会话读路径/Web Push订阅/举报/未鉴权拦截，
共 45 项断言，对当前真实运行的 Docker 环境执行结果：

```text
==> 45 passed, 0 failed (against http://localhost:8080)
```

（过程中修复了脚本自身一处断言错误：访客登录接口实际返回 `200` 而非 `201`，已核对
`api/auth.go` 源码确认后修正。）

WebSocket 相关用例（群聊/私聊实时收发、限流、跨实例广播）未纳入本脚本——Python
标准库没有内置 WS 客户端，引入额外依赖对"零依赖即可跑"的诉求不划算，继续按
`testcase/00-testcase-plan.md` 记录的方式人工/一次性脚本验证。

## Task12：部署与运行说明完善

- 新增 `docs/13-architecture-and-adr.md`：整体架构 Mermaid 图 + 12 条关键技术决策
  ADR 汇总（从 Plan 文档 ★1-★15 与各 Task work log 中收敛整理）+ 5 项已知简化点
  如实标注 + Task10 性能与成本落地记录。
- `server/.env.example`：补齐此前遗漏的环境变量说明（`POSTGRES_MAX_CONNS/MIN_CONNS`、
  `MEDIA_UPLOAD_DIR`、`MAX_UPLOAD_SIZE_BYTES`、`SENSITIVE_WORDS`、`VAPID_*`），此前
  该文件从 Task1 起就再未同步过后续 Task 新增的配置项。
- `deploy/docker-compose.yml`：新增 `media_uploads` 具名卷挂载到 server 容器的
  `/app/uploads`——此前上传的图片直接落在容器内路径，未挂载任何卷，容器重建
  （`--build`）后旧图片全部丢失、留下 404 死链接（Task9 前端功能闭环真机验证时
  已实际观察到这个现象，当时未处理，本次补上）。

## Task13：演示准备与 AI 使用说明

- `scripts/demo_seed.py`：一键预置两个演示账号（`demo_alice`/`demo_bob`）、建立
  好友关系、设置匹配的关注事项并生成一轮 AI 推荐候选，全程幂等（重复运行不报错、
  不产生重复数据），已实际运行两次验证幂等性。
- `docs/14-demo-guide.md`：分四部分的完整演示脚本（核心聊天体验 / 好友与私聊 /
  内容安全与可靠性 / AI-native预留能力），含常见问题预案（图片404/推送授权失败/
  推荐列表为空的排查思路）。
- `docs/15-ai-usage-notes.md`：如实记录 AI 在本项目开发全过程中的具体工作方式
  （Brainstorm→Plan→Testcase→Work→Verify 循环、真实发现并修复的 3 类环境问题）
  与人工介入的关键节点（技术选型确认、验证节奏调整、方向性选择、范围边界确认），
  呼应 `project_introduction.md` 对"介绍 AI 使用方式"的要求。

## 验证汇总

- `gofmt -l .`：无输出
- `go vet ./...`：无输出
- `golangci-lint run ./...`：退出码 0
- `go test ./... -v`：全部 PASS
- 真实 Docker 环境重建（server+client）后 `docker compose ps`：4 个容器均
  healthy/running
- `./scripts/smoke_test.sh`：通过
- `python3 scripts/integration_test.py`：45 passed, 0 failed
- `python3 scripts/demo_seed.py`：两次运行均成功，幂等性确认
- 新增索引/连接池配置/nginx 缓存头均已在真实运行环境中确认生效（`\di`/`curl -I`
  实测）

至此，Plan 文档 Task0-Task20 全部完成。
