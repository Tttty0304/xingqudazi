# Task4 实时通信 WS Gateway —— Work Log / 验证记录

> 关联 Testcase：T30-T37（及 Plan Part2 矩阵中 Task4/Task5 相关行）
> 关联开放问题：★3（Redis Pub/Sub 多实例扇出——**强制要求，非可选**）
> 日期：2026-08-17

## 范围调整说明

Plan 原本把"消息落库 + msgId 幂等去重"划在 Task5，但 WS `send_message` 事件天然需要落库才能验证
T33/T34，因此 Task4 一并吸收了消息持久化的**写入路径**（Task3 已完成读路径：历史消息查询）。
Task5 后续只需补充历史消息相关的收尾项（如有），不存在缺口。

## 实际改动

- `internal/ws/message.go`：WS 客户端消息（`ClientMessage`）与服务端广播信封（`ServerMessage`）
  的结构体定义，事件类型：`join_room`/`leave_room`/`send_message`/`message_received`/
  `user_online`/`user_offline`/`room_user_count_update`/`heartbeat`/`error`。
- `internal/ws/client.go`：`Client` 结构，管理单条 WS 连接的读/写协程与心跳（ping/pong）。
- `internal/ws/hub.go`：`Hub`——核心网关逻辑：
  - 连接注册/注销、房间 join/leave、在线人数广播；
  - **Redis Pub/Sub 广播模式（★3 强制要求）**：发消息统一走"发布到 `room:{id}` 频道 → 所有实例
    各自订阅该频道 → 收到后检查本地连接池 → 推给本地在线用户"，不存在任何"进程内直接查找连接"
    的捷径实现，因此代码架构层面天然支持水平扩展到多实例；
  - `handleSendMessage`：内容校验（抽成纯函数 `validateSendMessage` 便于单测）→ 落库
    （`MessageRepository.Create`，唯一约束 `(room_id, msg_id)` 做幂等去重）→ 发布广播。
- `internal/ws/handler.go`：`ServeWS` HTTP 升级处理器，握手阶段先鉴权（校验 `?token=` 的 JWT）
  再升级为 WebSocket 连接（对应 T30/T31：无 token/无效 token 直接拒绝升级）。
- `internal/repository/message_repository.go`：新增 `Create` 方法（写入路径），
  `INSERT ... ON CONFLICT (room_id, msg_id) DO NOTHING`，返回值区分"新插入"与"重复消息"。
- `cmd/server/main.go`：接线 `wsHub`/`wsHandler`，挂载 `GET /ws` 路由。
- 单测：`internal/ws/hub_test.go`，覆盖频道命名规则、消息校验纯函数、序列化逻辑。

## 本地验证

### 单元测试：全部通过（`go test ./... -v`，含本次新增用例）

### 真机端到端验证（单进程，`localhost:8080`）

用 Python（`websockets`/`aiohttp`）编写真实 WS 客户端脚本，覆盖：

- **T30**：合法 token 握手成功，升级为 WS 连接 ✅
- **T31**：无 token / 无效 token 握手被拒绝（升级失败）✅
- **T32**：`join_room` 后收到 `room_user_count_update` 广播；重复 join 同一房间不重复计数（幂等）✅
- **T33**：`send_message` 后房间内所有在线用户收到 `message_received`，且消息真实落库
  （用真实 `psql` 查询验证）✅
- **T34**：相同 `msg_id` 重复发送 → 只落库一次（唯一约束生效），不重复广播 ✅
- **T35**：超长内容 / 空内容 → 返回 `error` 事件，不落库不广播 ✅
- 心跳（ping/pong）与 `leave_room` 正常工作 ✅

### 跨进程多实例验证（★3 强制要求的确定性证明）

在不同端口（`:8090`）另起一个**完全独立的 OS 进程**（不同 PID，仅共享同一个 Redis/PostgreSQL），
两个用户分别连接到不同实例，验证 A 在实例1发消息，B 在实例2上通过 Redis Pub/Sub 真实收到广播——
不是同进程内模拟，是货真价实的跨进程验证：

- 用户 A 连接实例1（`:8080`），加入房间；用户 B 连接实例2（`:8090`），加入同一房间；
- A 发送消息 → 实例1 发布到 Redis 频道 `room:{id}` → 实例2 的订阅循环收到 → 推送给 B ✅
- 验证脚本：`/data/tmp/ws_multi_instance.py`（临时脚本，未随代码提交，如需复现可参考本记录
  重新编写：两个进程分别监听不同端口、指向同一 Redis/PostgreSQL，用 WS 客户端分别连接后互发消息）。
- 验证完成后已清理临时实例进程，只保留主实例（`:8080`）继续开发。

## 偏差或遗留项

- 验证用的 Python WS 脚本（`ws_smoke.py`/`ws_multi_instance.py`）是临时脚本，写在 `/data/tmp/`
  （沙箱临时目录），**未纳入代码仓库**。如需在新环境复现多实例验证，建议参考上文验证步骤重新编写，
  或改用 `docker compose up --scale server=2`（需要调整 compose 端口映射避免冲突）来验证。
- 限流（Task6）尚未接入，`send_message` 当前没有频率限制，仅有内容长度限制；Testcase 中
  "发言限流"相关用例（对应 Task6 矩阵行）留给 Task6 实现时验证。
- WS 层暂无自动化集成测试（仅手工脚本真机验证 + 单元测试覆盖纯函数逻辑），如时间允许可在
  Task11（测试与代码质量）阶段补充基于 `httptest` + 真实 WS client 的集成测试。
