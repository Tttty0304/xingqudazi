# 业务命令校验、幂等与错误隔离（工作日志，2026-08-26）

## 目标与审计结论

本轮按 HTTP 与 WebSocket 的全部业务入口检查三件事：请求数据格式是否在进入业务层前
被明确校验、写命令在网络超时后的重放是否会造成重复副作用、错误命令是否只终止当前
命令而不污染后续处理。

原有基础并非空白：群聊/私聊的 `msg_id` 已由数据库唯一约束去重；重复加入房间、推送
订阅 upsert、举报去重、登出和取消推送订阅也已有业务级幂等语义。但仍存在两项系统性
缺口：HTTP 写接口没有通用重放令牌；不少 JSON 绑定依赖框架默认宽松解析，未知字段会
被静默忽略，分页参数也会悄悄回退。

## 本轮实现

### 1. 严格数据格式校验

- 新增 `api.bindJSONStrict`：所有 JSON 写接口（注册、登录、好友、推送订阅、举报、
  关注事项、推荐处理）统一拒绝未知字段、类型不匹配、空 body、尾随或多个 JSON 值；
- 房间与会话历史查询的 `page`/`size` 改为明确校验：`page >= 1`、`1 <= size <= 100`，
  格式错误返回 400，不再静默改写请求；
- 用户查找先按既有用户名规则校验；关注事项对空白关键词、超过 500 字符的关键词、
  非法优先级和非法时间戳明确拒绝；举报理由限制为非空且最多 500 字符；推送 endpoint
  必须是带主机名的 HTTPS URL；
- WebSocket 帧改为 `DisallowUnknownFields` 的严格解码，并校验 `join_room`、
  `leave_room`、`send_message` 的 `room_id` 及 `send_direct_message` 的
  `target_user_id` 为 UUID。

### 2. HTTP 命令可安全重放

所有会产生副作用的 HTTP 入口均支持可选 `Idempotency-Key`：注册、登录、访客登录、
登出、好友请求与处理、删除好友、推送订阅/取消订阅、图片上传、举报、关注事项创建/
删除、生成推荐、处理推荐。

- key 仅允许 8–128 个字母数字、`.`、`_`、`-`；
- 使用 Redis `SETNX` 原子占位；首次成功响应会被缓存 24 小时；同一调用方、HTTP 方法、
  路径与 key 的重放直接返回原始状态码与响应体，不二次写库、推送或落盘；
- 同一 key 的并发未完成请求返回 `409 idempotency_in_progress`；Redis 不可用时，对
  携带 key 的写命令返回 `503 idempotency_unavailable`，避免在“无法确认是否已执行”的
  情况下冒险重复执行；未携带 key 的兼容旧客户端请求仍保留原业务行为。

同时已把 `Idempotency-Key` 加入 CORS 允许头，确保前后端跨域部署时浏览器预检不会
拦截这一协议头。

鉴权前命令按客户端 IP 隔离，鉴权后命令按 `user_id` 隔离。现有领域级保障继续保留：
`msg_id` 去重、房间重复 join、订阅 upsert/取消订阅、举报去重等不依赖该 HTTP 头。

### 3. 错误命令隔离

HTTP 格式/业务错误均返回明确的 4xx，不进入写入路径；WebSocket 的坏 JSON、未知字段、
非法 UUID 和未知事件返回一条 `error` 事件而**不关闭连接**，后续合法帧仍会继续处理。
这避免了单个错误命令中断用户的实时会话。

删除好友也调整为状态幂等：目标已不在好友列表时仍返回 `204`，因为期望状态“已不是
好友”已经达成。

## 测试补充

- `internal/api/request_validation_test.go`：严格 JSON、未知字段、类型错误、多个 JSON
  值、空 body 以及非法分页边界；
- `internal/ws/hub_test.go`：严格 WS 解码、未知字段、非法 UUID、多 JSON 值，及未知
  事件仍可被安全分发为协议错误；
- `internal/middleware/idempotency_test.go`：key 格式、真实 Redis 下“首次执行一次、
  随后重放原响应且不再执行 handler”、并发重放返回 409、失败命令释放 key 后可重试；
  另以不可达 Redis 地址验证带 key 命令会 fail-closed 返回 503。2026-08-27 已通过
  `TEST_REDIS_ADDR=host.docker.internal:6379` 在真实 Docker Redis 上实际执行。

## 验证状态与下一步

前端 TypeScript 构建检查已通过；Vitest 因现有 `node_modules` 缺失 Rolldown 的可选
原生绑定而无法启动（未改动依赖目录，以免覆盖既有环境）。本机未安装 Go 工具链。

**2026-08-27 Docker 复核（已完成）**：已安装并启动 Docker Desktop，实际确认引擎为
Linux/`x86_64`（client/server 均为 29.7.2），且 Compose 5.4.0 可用。首次拉取 Docker
Hub 时因本机网络出口超时受阻；在用户启用 VPN 并补齐基础镜像后，未改写项目镜像来源，
成功构建并启动 PostgreSQL、Redis、server、client 四项服务。最终确认
`http://localhost:8080/healthz` 与 `http://localhost:8081/` 均返回 200，数据库、Redis、
server 健康检查均为 healthy。

- 在 Docker 内执行 `go test ./...`：**全部通过**。期间发现严格 JSON 解码替代
  `ShouldBindJSON` 后漏执行 `binding:\"required\"` 标签，已在 `bindJSONStrict` 中显式
  恢复结构体标签校验，并新增缺失必填字段的单测；
- 对运行中的服务执行 `scripts/integration_test.py http://localhost:8080`：**57 passed,
  0 failed**，包含新增的 JSON 未知字段拒绝、非法分页和同一 `Idempotency-Key` 重放；
- 在隔离 Linux 容器执行前端 Vitest：**3 个测试文件、16 项测试全部通过**。该验证发现
  lockfile 中的 jsdom/Vitest 依赖要求 Node 22+，而 Dockerfile 原先固定 Node 20；已将
  前端构建阶段升级为 `node:22-alpine`，重新构建也通过 TypeScript/Vite 生产构建。

若在另一台机器复验，可直接在项目 Docker Compose 环境执行：

```bash
cd v2/server
gofmt -w ./internal ./cmd
go test ./...
python3 ../scripts/integration_test.py http://localhost:8080
```

建议把 `Idempotency-Key` 写入前端 API 客户端：每次用户主动写操作生成 UUID 并在网络
重试时复用同一个 key；新操作必须生成新 key。媒体上传仍应特别注意客户端不要在未知
结果时改用新 key 重试，否则这是新的业务命令而不是重放。

已同步更新 `client/src/api/client.ts`：所有 POST/PUT/DELETE/上传默认生成 key，并允许
调用方以最后一个 `idempotencyKey` 参数传入并复用同一个 key 实施重试。
