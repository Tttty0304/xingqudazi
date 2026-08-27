# Task6+7+8 工作记录：可靠性、可观测性、系统安全性（批量合并推进）

> 日期：2026-08-17
> 说明：应用户要求"降低验证密度"，本轮将 Task6（可靠性）+ Task7（可观测性核实）+
> Task8（系统安全性）三个小 Task 合并在一轮内完成开发+验证，而非逐个 Task 单独走
> 一轮完整流程。

## 范围

- **Task6 可靠性**：
  - 发言限流（T40）：WS 连接级双层限流，2 秒突发窗口（≤10条）+ 每分钟长期配额
    （`cfg.RateLimitPerMinute`），群聊 `send_message` 与私聊 `send_direct_message`
    共用同一计数器。
  - Graceful shutdown（T41）：`Hub.Shutdown()` 在 SIGTERM 时向所有本地 WS 连接
    发送真实 Close 帧（`code=1000, reason=server_shutting_down`），而不是让连接被
    进程退出强制掐断——WS 是 hijacked 连接，`http.Server.Shutdown()` 不会等待/通知
    它们，因此必须显式处理。
- **Task7 可观测性**：核实确认 Task1 已完成的 `/healthz` `/readyz` `/metrics` +
  trace_id 结构化日志已达标，本轮无新增代码。
- **Task8 系统安全性**：
  - XSS 转义（T50）：群聊历史消息、私聊历史消息、私聊会话列表 `last_message` 预览
    三处 HTTP 输出边界统一用 `html.EscapeString` 转义；WS 广播（`message_received`/
    `direct_message_received`）同样在发布前转义。存储层保留原文，只在对外输出边界
    转义（方便未来更换转义/审核策略而不用回填历史数据）。
  - CORS（补充项）：新增 `middleware.CORS()`，为 Task9 前端跨域调用做准备。
  - 越权/未鉴权（T51/T52）：Task14 已有的 `middleware.RequireAuth` + service 层
    操作人校验逻辑，本轮做真机回归确认，无新增代码。

## 代码改动清单

- `internal/ws/client.go`：新增 `allowMessage(limit int) bool`，双层限流（突发窗口
  常量 `burstWindow=2s`/`burstLimit=10` + 按分钟长期配额）。
- `internal/ws/hub.go`：
  - `handleSendMessage`/`handleSendDirectMessage` 接入 `c.allowMessage()` 校验；
  - 广播前对 `Content` 做 `html.EscapeString`；
  - 新增 `Shutdown()` 方法，遍历 `h.users` 向所有本地连接的 `conn.WriteControl`
    发送 Close 帧。
- `cmd/server/main.go`：
  - 接入 `wsHub.Shutdown()`（在 `srv.Shutdown()` 之前调用）；
  - 接入 `middleware.CORS()`；
  - `ws.NewHub` 新增 `rateLimitPerMinute` 参数（读取 `cfg.RateLimitPerMinute`）。
- `internal/middleware/cors.go`（新建）：基础 CORS 中间件（放开全部 Origin，demo/
  评估项目简化处理，已在注释中标注生产环境应收紧为白名单）。
- `internal/api/room.go`、`internal/api/conversation.go`：历史消息响应 `Content`
  字段转义；`conversation.go` 额外修复了 `ListConversations` 的 `last_message`
  预览字段漏转义的问题（首轮验证脚本发现）。
- `internal/ws/client_test.go`（新建）：`allowMessage` 纯逻辑单测，覆盖不限流/
  超限拒绝/窗口恢复/突发限流四种场景。

## 验证结果（真实 Docker 容器环境）

| 项 | 结果 |
|---|---|
| 编译 + `go vet` + 全部单元测试（`-count=1`） | ✅ 通过 |
| T40 限流（1秒内连发20条） | ✅ 通过：`ok_count=10` 后精确触发 `rate_limited`，与 `burstLimit=10` 对应 |
| T41 Graceful shutdown | ✅ 通过：`docker kill --signal=TERM` 后，真机 WS 客户端收到 `close_code=1000, reason=server_shutting_down`；容器日志含 `shutdown signal received` → `ws_shutdown_close_frames_sent(client_count=1)` → `graceful shutdown completed`，进程退出码 0 |
| T50 XSS 转义（群聊历史/私聊历史/会话列表预览三处） | ✅ 全部通过（首轮验证发现会话列表预览漏转义，已修复并二次验证通过） |
| T51 越权（非接收方 accept 好友请求） | ✅ 通过（403，回归确认） |
| T52 未鉴权访问 `/api/friends` | ✅ 通过（401，回归确认） |
| CORS 响应头存在性 | ✅ 通过 |
| Task4 群聊、Task14 好友、Task15 私聊回归 | ✅ 通过（私聊回归脚本命中固定 `msg_id` 幂等去重跳过广播，属预期行为，非回归） |

## 已知简化点（如实记录，非遗漏）

- CORS 当前放开全部 Origin（`*`），生产环境应改为白名单校验，已在代码注释标注。
- 限流为 per-connection 粒度（内存计数器），非跨实例统一限流；重连会重置计数器。
  对 demo/评估场景已足够，若未来需要跨实例统一限流需迁移到 Redis 计数器（如
  `INCR` + `EXPIRE`），本轮不在范围内。
- Task7 未新增代码，Task1 的可观测性基础设施已达标，本轮仅做核实确认。

## 遗留

无新增遗留项；`docs/05-handover.md` 中原有的限流缺失风险项已解决。
