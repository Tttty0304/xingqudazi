# Task3 兴趣聊天室管理 —— Work Log / 验证记录

> 关联 Testcase：T20-T22 及 Plan Part2 矩阵中的边界场景
> 日期：2026-08-17

## 实际改动

- `internal/model/room.go`：`Room`/`RoomWithOnlineCount`/`Message` 数据模型。
- `internal/service/room_errors.go`：`ErrRoomNotFound`/`ErrRepositoryRoomNotFound`。
- `internal/service/room_service.go`：`RoomService`（ListRooms/GetRoom），依赖 `RoomStore`+`OnlineCounter` 接口；
  在线人数查询失败时优雅降级为 0，不让核心浏览功能因非核心依赖异常而挂掉。
- `internal/service/message_service.go`：`MessageService`（ListRoomMessages），依赖 `RoomStore`+`MessageStore`；
  非法分页参数（page<1/size越界）自动规范化，不直接透传给数据库层。
- `internal/repository/room_repository.go`、`message_repository.go`：真实 PostgreSQL 实现。
- `internal/repository/room_online_counter.go`：`RedisRoomOnlineCounter`，基于 `room:{id}:online_users` Set
  （复用 Plan Part3 已定义的 Redis 键结构，为 Task4 的 WS Gateway 维护该 Set 预留统一接口）。
- `internal/api/room.go`：`RoomHandler`（`GET /api/rooms`、`GET /api/rooms/:id/messages`），房间 ID 做 UUID 格式
  前置校验（非法格式 400，与"合法格式但不存在"404 区分）。
- `cmd/server/main.go`：接线房间路由（`/api/rooms` 无需鉴权，符合 T20 要求）。
- 单测：`room_service_test.go`，覆盖 T20/T21/T22 及分页参数规范化。

## 本地验证

### 单元测试：全部通过（`go test ./... -v`，含本次新增 5 个测试）

### 真机端到端验证

- **T20**：`GET /api/rooms` → `200`，返回 4 个预置种子房间（数码/追番/运动/美食），`online_count` 均为 0
  （真实状态：Task4 WS 尚未接入在线连接维护，不是伪造数据）✅
- **T21**：`GET /api/rooms/{真实房间ID}/messages?page=1&size=20` → `200 {"messages":[],"has_more":false}`
  （真实状态：messages 表当前无数据，因为 Task5 的写入路径尚未接入）✅
- **T22**：`GET /api/rooms/{合法UUID但不存在}/messages` → `404 {"error":"room_not_found"}` ✅
- **边界**（Plan矩阵）：`GET /api/rooms/not-a-uuid/messages` → `400 {"error":"invalid_room_id"}` ✅

## 偏差或遗留项

- T21 目前只能验证"空列表+分页字段正确"，完整的"有数据时正确排序/分页"要等 Task5 接入 send_message
  写入路径后才能端到端验证；单测已用假 MessageStore 覆盖了排序/分页/has_more 逻辑本身。
- 无。
