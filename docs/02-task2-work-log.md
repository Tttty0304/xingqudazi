# Task2 用户体系与鉴权 —— Work Log / 验证记录

> 关联 Plan：`docs/00-brainstorm-and-plan.md`（Task0 已定稿版）；关联 Testcase：`testcase/00-testcase-plan.md` T10-T15
> 日期：2026-08-17

## 实际改动

- `internal/model/user.go`：`User` 数据模型。
- `internal/service/errors.go`：语义化 sentinel error（`ErrUsernameTaken`/`ErrInvalidPassword`/`ErrInvalidCredentials` 等）。
- `internal/service/validation.go`：用户名（3-32位字母数字下划线）/密码（≥8位）校验规则。
- `internal/service/token.go`：`TokenService`，JWT 签发/校验，`Claims` 含 `user_id`/`is_guest`，供 Task4 WS 鉴权复用。
- `internal/service/auth_service.go`：`AuthService`（Register/Login/GuestLogin），依赖 `UserStore` 接口（依赖倒置，单测无需真实 DB）；
  bcrypt 哈希密码；Login **不区分**"用户不存在"与"密码错误"，统一返回 `ErrInvalidCredentials`（T14 硬性要求）。
- `internal/repository/user_repository.go`：`UserRepository`，`UserStore` 的真实 PostgreSQL 实现。
- `internal/api/auth.go`：`AuthHandler`（`POST /api/auth/register|login|guest`）。
- `internal/middleware/auth.go`：`RequireAuth` HTTP 鉴权中间件，解析 `Authorization: Bearer <jwt>`，为后续 Task 的鉴权路由分组提供基础设施。
- `cmd/server/main.go`：接线 Task2 鉴权路由；预留 `authedGroup`（已挂 `RequireAuth` 中间件）供后续 Task 挂载。
- 单测：`validation_test.go`（用户名/密码规则）、`token_test.go`（JWT 签发/校验/过期/密钥不匹配/格式错误）、
  `auth_service_test.go`（内存假 `UserStore`，覆盖 Register/Login/GuestLogin 全部分支，含 T14 的"错误归一"专项验证）。

## 本地验证

### 单元测试

```
cd server && gofmt -l . && go build ./... && go vet ./... && go test ./... -v
```

结果：全部通过（`internal/service` 17 个子测试全绿，`internal/api` T01 单测保持通过）。

### 真机端到端验证（复用 Task1 已启动的 PostgreSQL 13.23+pgvector / Redis 5.0.3）

- 排查记录：重启后端时曾遗留一个孤儿旧编译进程（`go-build` 缓存二进制，PPID=1）占用 8080 端口，
  导致一开始请求打到旧版本二进制返回 404；确认后 `kill -9` 清理旧进程并重新 `go run` 后恢复正常，
  此后请求均命中新代码。

- **T10**（注册成功）：`POST /api/auth/register {"username":"alice","password":"Passw0rd!"}` → `201 {"user_id":"...","username":"alice"}` ✅
- **T11**（重复用户名）：相同请求再发一次 → `400 {"error":"username_taken"}` ✅
- **T12**（密码过短）：`{"username":"bob","password":"123"}` → `400 {"error":"invalid_password"}` ✅
- **T13**（登录成功）：`POST /api/auth/login {"username":"alice","password":"Passw0rd!"}` → `200` + 合法 JWT ✅
- **T14**（密码错误）：`{"username":"alice","password":"wrong"}` → `401 {"error":"invalid_credentials"}` ✅
- **T14b**（用户不存在，硬性要求验证）：`{"username":"nosuchuser","password":"whatever"}` → `401 {"error":"invalid_credentials"}`，
  与 T14 **完全相同**的错误响应，证明未泄露"用户是否存在"信息 ✅
- **T15**（访客模式）：`POST /api/auth/guest {}` → `200 {"is_guest":true,"token":"...","user_id":"guest_xxx"}` ✅
- **数据库落库验证**：`SELECT username, is_guest, LEFT(password_hash,10) FROM users` 显示 alice 密码已 bcrypt
  哈希（`$2a$10$...`，非明文），访客用户 `password_hash` 为空 ✅

## 偏差或遗留项

- 已鉴权路由分组（`authedGroup`，挂了 `RequireAuth` 中间件）当前尚未挂载任何真实业务路由（好友/私聊/房间管理
  在后续 Task 中接入），本次仅验证了中间件基础设施可编译、单测覆盖 token 解析逻辑，未做真实 HTTP 层面的
  401/200 集成测试（因为没有真实受保护路由可测）——将在 Task3/14/15 接入对应路由后一并补充集成验证。
- Task4（WebSocket 鉴权）将复用本次的 `TokenService.ParseToken`，待接入时验证。
