# Task14 好友关系链 —— Work Log / 验证记录

> 关联 Testcase：T60-T63（新增 T64 补充用例）及 Plan Part2 矩阵中的边界场景
> 日期：2026-08-17

## 实际改动

- `internal/model/friendship.go`：`Friendship`/`Friend` 数据模型。
- `internal/service/friend_errors.go`：好友关系链语义化错误
  （`ErrCannotFriendSelf`/`ErrFriendRequestNotFound`/`ErrFriendRequestExists`/`ErrAlreadyFriends`/
  `ErrFriendRequestResolved`/`ErrForbiddenFriendResponse`/`ErrInvalidFriendAction`/`ErrNotFriends`）。
- `internal/service/friend_service.go`：`FriendService`（SendRequest/RespondRequest/ListFriends/RemoveFriend），
  依赖 `FriendshipStore`+`UserStore`（复用 Task2 已有接口）+`PresenceChecker`+`FriendNotifier`；
  `PresenceChecker`/`FriendNotifier` 均在 service 包定义为接口（依赖倒置），真实实现分别由
  `repository.RedisUserPresence`、`ws.Hub` 提供，两者与 service 包之间无 import 依赖，仅方法签名结构化匹配。
- `internal/repository/friendship_repository.go`：真实 PostgreSQL 实现，`Create` 用
  `ON CONFLICT (requester_id, target_id) DO NOTHING` + RowsAffected 判定唯一约束命中（复用 Task4
  消息去重的既有模式，不解析 pgconn 错误码）。
- `internal/repository/user_presence.go`：`RedisUserPresence`，基于 Hash 引用计数
  (`presence:online_user_counts`，field=userID) 实现"多端在线不误判离线"。同时满足 `ws.PresenceTracker`
  （MarkOnline/MarkOffline）与 `service.PresenceChecker`（IsOnline）两个接口。
- `internal/ws/hub.go`：
  - 新增 `users map[string]map[*Client]bool`（本地 userID → 连接集合），`register`/`unregister`
    时维护，并调用 `PresenceTracker` 更新全局在线态。
  - 新增点对点跨实例推送能力：`user:{id}:notify` Redis 频道 + `dispatchToLocalUser`/`publishToUser`，
    与房间广播（`room:{id}:broadcast`）复用同一条 ★3 强制多实例扇出设计，`subscribeLoop` 用一次
    `PSubscribe` 同时订阅两类频道模式，按频道前缀分发。
  - `NotifyFriendRequestReceived` 方法实现 `service.FriendNotifier`（结构化匹配，ws 包不 import
    service 包）。
- `internal/ws/message.go`：新增 `EventFriendRequestReceived` 事件类型、`ServerMessage.RequestID`/
  `FromUserID` 字段、`userNotifyEnvelope` 结构。
- `internal/api/friend.go`：`FriendHandler`（SendRequest/RespondRequest/ListFriends/DeleteFriend）。
- `cmd/server/main.go`：接线 `presenceTracker`（提前到 Hub 构造之前，Hub 与 FriendService 共用同一个
  实例）、好友相关 4 个路由（挂在已有的 `authedGroup` 下，复用 Task2 的鉴权中间件）。
- 单测：`friend_service_test.go`，覆盖 T60-T63 全部正向/边界场景（自建 `fakeFriendshipStore`/
  `fakePresenceChecker`/`fakeFriendNotifier`，复用已有的 `fakeUserStore`）。

## 本地验证

### 单元测试：全部通过（`go test ./...`，新增 9 个测试用例）

### 真机端到端验证（Docker 镜像模式，`docker compose up -d --build server`）

- **T60**：alice 发起好友请求 → `201 {"request_id":...,"status":"pending"}`；bob（WS 在线）实时收到
  `{"type":"friend_request_received","request_id":...,"from_user_id":"<alice_id>"}` ✅
- **T60 边界·重复发起**：alice 再次对 bob 发起 → `409 friend_request_already_exists` ✅
- **边界·对方不存在**：`target_user_id` 为不存在的 UUID → `404 user_not_found` ✅
- **边界·对自己发起**：`target_user_id==自己` → `400 cannot_friend_self` ✅
- **T51 同款越权**：非请求接收方（eve）尝试 `accept` → `403 forbidden` ✅
- **T61**：bob 接受请求 → `200 {"status":"accepted"}` ✅
- **T62**：bob 对已处理过的请求重复 `accept` → `409 already_resolved` ✅
- **T63**：alice 查询好友列表，`GET /api/friends` 返回 bob 且 `online:true`（bob 当前 WS 连接中）；
  bob 断开 WS 连接后（等待 ~1s 让 unregister 生效）再查询，`online` 变为 `false` —— **真实的
  Redis 在线态引用计数生效，非静态假数据** ✅
- **T64（补充，Plan 已确认接口但 Testcase 原未覆盖，本次补齐并同步更新 testcase 文档）**：
  `DELETE /api/friends/{peer_id}` 删除好友 → `204`；删除后好友列表不再包含对方 ✅；
  对不存在的好友关系再次删除 → `404 not_friends` ✅

以上共 14 项断言，全部通过（脚本化验证，覆盖 `testcase/00-testcase-plan.md` T60-T63 全部用例
及新增 T64）。

## 偏差或遗留项

- **新增 T64**：Plan Part3 已确认 `DELETE /api/friends/{id}` 为 P0 接口，但 Testcase 首版未单独列出
  对应用例（仅在接口列表中提及）。本次实现时补齐了该接口并同步在 `testcase/00-testcase-plan.md`
  Part3 P0-7 表格中新增 T64 一行，保持"接口清单"与"用例清单"一致，非未经确认的范围外新增功能。
- 用户在线态判断粒度为"是否有任意一条 WS 连接"，未区分"挂着但不活跃"与"真正活跃"，这与 Plan 设计
  的 `user:{id}:session` 概念一致，暂无需求要求更细粒度的活跃度判断。
- `subscribeLoop` 目前对房间广播和用户通知使用同一个 `PSubscribe` 订阅两个模式；随着后续 Task15
  私聊、Task17 离线推送等场景增多点对点通知类型，可以观察是否需要拆分成独立的 goroutine/连接，
  当前规模下未发现性能问题，留作后续观察项，不阻塞当前交付。
