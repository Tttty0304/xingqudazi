# 兴趣搭子在线聊天室 —— Testcase：接口级验收用例清单

> 关联 Plan 版本：`docs/00-brainstorm-and-plan.md`（Task0 已定稿版）
> ctl 状态：not-required（独立评估项目，轻量路径，用例仅落盘本地 `testcase/`，不生成 `acceptance.yaml`）
> 说明：本项目非 QQ 内部 OIDB/协议对外服务，不适用 `qq-utest`/SSO 包头相关规范；
> 全部接口为标准 HTTP REST + WebSocket JSON 帧，用例按通用 P0/P1/P2 分层验收模板构造。

## Part 0　元信息

- 覆盖范围：本轮聚焦 **P0 核心闭环**（Task1-5, 14, 15, 18基础, 19schema）对应的接口；
  Task16（媒体）/Task17（推送）/Task20（AI推荐）等后续 Task 的用例将在对应实现前补充，
  不在本轮阻塞首次编码启动。
- 日期：2026-08-17

## Part 1　复用检索结论

全新 greenfield 项目，无历史代码/用例/拨测记录可复用；全部用例从已确认的接口设计
（Plan Part 3「接口/数据/事件映射」）直接生成。

## Part 2　测试分层与矩阵

| Plan Task | 接口/事件 | 正向 | 负向 | 边界 |
|---|---|---|---|---|
| Task1 | `GET /healthz` `GET /readyz` | 200+健康标识 | DB/Redis 不可达时 `/readyz` 返回非200 | — |
| Task2 | `POST /api/auth/register` | 201+用户创建 | 用户名已存在 400 | 密码长度不足/用户名含非法字符 400 |
| Task2 | `POST /api/auth/login` | 200+JWT | 密码错误 401 | 用户不存在 401（不泄露"不存在"与"密码错"的区别，防止用户名枚举） |
| Task2 | WS 握手鉴权 `ws://.../ws?token=` | 200 升级成功 | 无 token/无效 token 拒绝升级(401/403) | token 过期拒绝升级 |
| Task3 | `GET /api/rooms` | 200+房间列表 | — | 未登录也可浏览（房间列表公开，验证产品定位） |
| Task3 | `GET /api/rooms/{id}/messages` | 200+历史消息分页 | 房间不存在 404 | `id` 非法格式 400 |
| Task4 | WS `join_room` | 收到 `room_user_count_update` 广播 | 房间不存在 → 错误事件 | 重复 join 同一房间幂等（不重复计数） |
| Task4 | WS 多实例广播（Redis Pub/Sub） | 两个不同进程实例上的用户互相收到广播消息 | Redis 连接中断时优雅降级（单实例内广播仍可用，日志记录降级） | — |
| Task5 | WS `send_message` | 全房间用户收到 `message_received`，消息落库 | 内容超长/空内容拒绝 | 相同 `msgId` 重复发送去重（幂等） |
| Task6 | 发言限流 | 正常频率放行 | 超过限流阈值返回 429/拒绝事件 | 恢复正常频率后自动放行 |
| Task8 | XSS 输入 | 正常文本正常展示 | 消息含 `<script>` 标签存储/展示时被转义 | — |
| Task14 | `POST /api/friends/requests` | 201+请求创建，对方收到 `friend_request_received` | 对方不存在 404 | 重复发起同一好友请求幂等/拒绝 |
| Task14 | `PUT /api/friends/requests/{id}` | 200+状态变更为 accepted/rejected | 非本人操作 403 | 已处理过的请求重复操作 409 |
| Task15 | WS `send_direct_message` | 双方收到 `direct_message_received`，落库 | 非好友关系时拒绝（若产品要求好友才能私聊，见 Part5 待确认） | 相同 `msgId` 幂等去重 |
| Task18 | `POST /api/reports` | 201+举报记录落库 | 目标消息/用户不存在 404 | 重复举报同一对象记录但不重复计数（按业务口径） |

## Part 3　具体用例清单与范围映射

> 环境相关值（如 `BASE_URL`、Redis/DB 连接串）用占位符，业务参数与断言不占位。

### P0-1 健康检查

| 用例ID | 接口 | 请求 | 预期响应 | 通过标准 |
|---|---|---|---|---|
| T01 | `GET /healthz` | 无参数 | `200 {"status":"ok"}` | `status_code==200` 且 `body.status=="ok"` |
| T02 | `GET /readyz`（依赖健康） | 无参数 | `200 {"status":"ready","db":"ok","redis":"ok"}` | `status_code==200`，`body.db=="ok"`，`body.redis=="ok"` |
| T03 | `GET /readyz`（DB 断连场景，Work阶段用 docker stop db 模拟） | 无参数 | 非200，`body.db!="ok"` | `status_code in [503]`，明确暴露故障组件而非笼统500 |

### P0-2 用户注册/登录

| 用例ID | 接口 | 请求 | 预期响应 | 通过标准 |
|---|---|---|---|---|
| T10 | `POST /api/auth/register` | `{"username":"alice","password":"Passw0rd!"}` | `201 {"user_id":"...","username":"alice"}` | `status_code==201`，`body.user_id` 非空 |
| T11 | `POST /api/auth/register`（重复用户名） | 同上用户名再注册一次 | `400 {"error":"username_taken"}` | `status_code==400`，`body.error=="username_taken"` |
| T12 | `POST /api/auth/register`（密码过短） | `{"username":"bob","password":"123"}` | `400 {"error":"invalid_password"}` | `status_code==400` |
| T13 | `POST /api/auth/login`（正确凭证） | `{"username":"alice","password":"Passw0rd!"}` | `200 {"token":"<jwt>","user_id":"..."}` | `status_code==200`，`body.token` 非空且为合法 JWT 格式 |
| T14 | `POST /api/auth/login`（密码错误） | `{"username":"alice","password":"wrong"}` | `401 {"error":"invalid_credentials"}` | `status_code==401`，错误信息不区分"用户不存在"与"密码错误" |
| T15 | `POST /api/auth/guest`（访客模式） | `{}` | `200 {"token":"<jwt>","user_id":"guest_xxx","is_guest":true}` | `status_code==200`，`body.is_guest==true` |

### P0-3 房间

| 用例ID | 接口 | 请求 | 预期响应 | 通过标准 |
|---|---|---|---|---|
| T20 | `GET /api/rooms` | 无需鉴权 | `200 [{"id":"...","name":"数码","online_count":N},...]` | `status_code==200`，返回数组含种子房间（至少4个预置主题） |
| T21 | `GET /api/rooms/{id}/messages?page=1&size=20` | 合法房间ID | `200 {"messages":[...],"has_more":bool}` | `status_code==200`，`messages` 按时间倒序 |
| T22 | `GET /api/rooms/{invalid_id}/messages` | 不存在的房间ID | `404 {"error":"room_not_found"}` | `status_code==404` |

### P0-4 WebSocket 群聊核心闭环

| 用例ID | 事件 | 请求（客户端发） | 预期响应/广播 | 通过标准 |
|---|---|---|---|---|
| T30 | 建连 | `ws://<host>/ws?token=<jwt>` | 升级成功，服务端推 `{"type":"connected","user_id":"..."}` | 握手 101，随后收到 `connected` 事件 |
| T31 | 建连（无 token） | `ws://<host>/ws` | 拒绝升级 | HTTP 401/403，不建立连接 |
| T32 | `join_room` | `{"type":"join_room","room_id":"r1"}` | 本人收到 `{"type":"joined","room_id":"r1"}`；房间广播 `room_user_count_update` | 广播的 `online_count` 较之前 +1 |
| T33 | `send_message` | `{"type":"send_message","room_id":"r1","msg_id":"m1","content":"hello"}` | 房间内全部在线用户（含发送者）收到 `{"type":"message_received","msg_id":"m1","content":"hello","sender_id":"...","sender_type":"human"}` | 所有在场用户都收到，`content` 与发送一致，消息已落库（可通过 T21 历史接口验证） |
| T34 | `send_message`（重复 msg_id） | 相同 `msg_id":"m1"` 再发一次 | 服务端识别重复，不重复广播/不重复落库 | 历史消息表中 `msg_id="m1"` 只有一条记录 |
| T35 | `send_message`（超长内容） | `content` 超过配置上限（如1000字符） | 服务端拒绝，返回 `{"type":"error","code":"content_too_long"}` | 不广播、不落库 |
| T36 | 多实例广播（需起2个server进程共享同一Redis） | 用户A连实例1、用户B连实例2，同在房间r1；A发消息 | B（连在实例2）仍能收到 `message_received` | 验证 Redis Pub/Sub 广播确实跨实例生效，非仅进程内内存广播 |
| T37 | `leave_room` | `{"type":"leave_room","room_id":"r1"}` | 房间广播 `room_user_count_update`（-1） | 用户不再收到该房间后续消息 |
| T38 | 断线重连 | 强制断开 WS 后用相同 token 重连 | 重连成功，若客户端携带 `last_msg_id` 可选择性补拉丢失的消息（P1可选） | 重连后不重复收到已处理过的消息 |

### P0-5 可靠性（已实现验证，2026-08-17，见 `docs/08-task6-7-8-work-log.md`）

| 用例ID | 场景 | 操作 | 预期 | 通过标准 | 结果 |
|---|---|---|---|---|---|
| T40 | 发言限流 | 同一连接1秒内连续发送 20 条消息（双层限流：2秒突发窗口上限10条 + 每分钟长期配额） | 超过突发阈值后的消息被拒绝 `{"type":"error","code":"rate_limited"}` | 超限消息不落库不广播；窗口过期后自动恢复放行 | ✅ 真机验证：`ok_count=10` 后精确触发 `rate_limited` |
| T41 | Graceful shutdown | 向容器内进程发 SIGTERM，同时有活跃 WS 连接 | 进程等待正在处理的请求完成后退出，WS 连接收到关闭帧而非强制断连 | 进程退出码为0，日志记录"graceful shutdown completed"，WS 客户端收到 Close 帧 | ✅ 真机验证：`close_code=1000 reason=server_shutting_down`，日志含 `ws_shutdown_close_frames_sent`+`graceful shutdown completed` |

### P0-6 安全（已实现验证，2026-08-17）

| 用例ID | 场景 | 操作 | 预期 | 通过标准 | 结果 |
|---|---|---|---|---|---|
| T50 | XSS 转义 | 发送消息 `content: "<script>alert(1)</script>"`（群聊+私聊两条路径） | 存储原文，API 返回时已转义 | 消息内容在 API 响应中被转义为 `&lt;script&gt;`（群聊历史/私聊历史/会话列表 `last_message` 预览三处输出边界均覆盖） | ✅ 真机验证三处均已转义 |
| T51 | 越权访问 | 用户A尝试 `PUT /api/friends/requests/{属于B的请求id}` | 拒绝 | `status_code==403` | ✅ 真机验证（Task14 已有逻辑，本轮回归确认） |
| T52 | 未鉴权访问受保护接口 | 不带 token 请求 `GET /api/friends` | 拒绝 | `status_code==401` | ✅ 真机验证 |

### P0-7 好友关系链（Task14）

| 用例ID | 接口 | 请求 | 预期响应 | 通过标准 |
|---|---|---|---|---|
| T60 | `POST /api/friends/requests` | `{"target_user_id":"bob_id"}`（alice发起） | `201 {"request_id":"...","status":"pending"}`；bob 收到 WS `friend_request_received` | `status_code==201` |
| T61 | `PUT /api/friends/requests/{id}` | `{"action":"accept"}`（bob操作） | `200 {"status":"accepted"}`；双方好友列表各新增一条 | `status_code==200`，`GET /api/friends` 双向可见 |
| T62 | `PUT /api/friends/requests/{id}`（重复操作） | 已 accepted 的请求再次 `accept` | `409 {"error":"already_resolved"}` | `status_code==409` |
| T63 | `GET /api/friends` | alice 查询 | `200 [{"user_id":"bob_id","online":bool},...]` | 包含 bob，且 `online` 字段随 bob 在线状态实时变化 |
| T64 | `DELETE /api/friends/{peer_id}`（Task14 实现时补充，Plan Part3 已确认接口） | alice 删除好友 bob | `204`；再次删除同一好友关系 `404 {"error":"not_friends"}` | `status_code==204`，删除后 `GET /api/friends` 不再包含对方 |

### P0-8 私聊（Task15）

| 用例ID | 事件/接口 | 请求 | 预期 | 通过标准 |
|---|---|---|---|---|
| T70 | WS `send_direct_message` | `{"type":"send_direct_message","target_user_id":"bob_id","msg_id":"d1","content":"hi"}` | bob 收到 `{"type":"direct_message_received",...}` | 双方均可通过 `GET /api/conversations/{id}/messages` 查到该消息 |
| T71 | `GET /api/conversations` | alice 查询 | `200 [{"conversation_id":"...","peer_id":"bob_id","last_message":"hi"}]` | 返回按最近消息时间倒序 |
| T72 | 非好友私聊限制（**已于 2026-08-17 与用户确认，口径：仅好友可私聊**） | 非好友用户尝试发起私聊 | WS `{"type":"error","code":"friend_required"}`，不落库不广播 | 非好友发起 `send_direct_message` 必须被拒绝，好友关系需为 `accepted` 状态 |

### P0-9 内容安全基础（Task18，已实现验证，2026-08-17，见 `docs/09-task16-18-19-work-log.md`）

| 用例ID | 接口 | 请求 | 预期 | 通过标准 | 结果 |
|---|---|---|---|---|---|
| T80 | `POST /api/reports` | `{"target_type":"message"\|"direct_message"\|"user","target_id":"...","reason":"spam"}` | `201 {"report_id":"..."}` | `status_code==201`，记录落库；目标不存在 `404`；同一举报人重复举报同一目标幂等返回相同 `report_id` | ✅ 真机验证：举报不存在消息 404、举报真实用户 201、重复举报幂等 |
| T81 | 敏感词过滤 | 发送包含预置敏感词库中词汇的消息（群聊/私聊） | 消息被拦截，`{"type":"error","code":"content_blocked"}` | 敏感词命中的消息不落库不广播 | ✅ 真机验证：拦截生效，历史消息接口确认未落库 |

### P0-10 多媒体消息（Task16/P0图片，已实现验证，2026-08-17）

| 用例ID | 接口/事件 | 请求 | 预期 | 通过标准 | 结果 |
|---|---|---|---|---|---|
| T90 | `POST /api/media/upload`（multipart，字段名 `file`） | 合法图片文件（jpeg/png/gif/webp） | `201 {"media_id":"...","url":"/uploads/..."}` | `status_code==201`，返回 URL 可直接访问 | ✅ 真机验证：PNG 上传成功，`GET` 该 URL 返回 200 `image/png` |
| T91 | `POST /api/media/upload` | 非图片类型文件（如 `text/plain`） | `400 {"error":"unsupported_media_type"}` | `status_code==400` | ✅ 真机验证 |
| T92 | `POST /api/media/upload` | 超过大小上限（默认5MB）的文件 | `400 {"error":"file_too_large"}` | `status_code==400` | ✅ 真机验证（6MB 文件被拒绝） |
| T93 | WS `send_message`（`content_type:"image"`） | `{"type":"send_message","room_id":"r1","content":"<T90返回的url>","content_type":"image"}` | 房间内用户收到 `{"type":"message_received","content_type":"image","content":"<url>",...}`，URL 不被转义 | `content` 与上传返回的 URL 完全一致 | ✅ 真机验证 |

### P0-11 关注事项（Task19/P1，已实现验证，2026-08-17）

| 用例ID | 接口 | 请求 | 预期 | 通过标准 | 结果 |
|---|---|---|---|---|---|
| T94 | `POST /api/watch-topics` | `{"keywords":"数码,摄影","room_id":"r1","priority":5}` | `201 {"topic_id":"..."}` | `status_code==201`，记录落库 | ✅ 真机验证 |
| T95 | `GET /api/watch-topics` | 无参数（当前登录用户） | `200 [{"id":"...","keywords":"数码,摄影",...}]` | 返回数组含刚创建的记录；未鉴权访问 `401` | ✅ 真机验证 |

### P0-12 Web Push 离线通知（Task17，已实现验证，2026-08-18，见 `docs/10-task17-20-work-log.md`）

| 用例ID | 接口/触发 | 请求 | 预期 | 通过标准 | 结果 |
|---|---|---|---|---|---|
| T100 | `POST /api/push/subscriptions` | `{"endpoint":"...","keys":{"p256dh":"...","auth":"..."}}` | `201` | `status_code==201`，记录落库；未鉴权 `401` | ✅ 真机验证 |
| T101 | `DELETE /api/push/subscriptions` | `{"endpoint":"..."}` | `204` | `status_code==204` | ✅ 真机验证 |
| T102 | 好友请求触发（目标离线时） | alice 向离线的 bob 发起好友请求 | bob 收到浏览器推送通知；主流程（好友请求创建）不受推送成败影响 | 好友请求接口仍正常返回 201；**已用宿主机 mock 推送服务确认容器内进程真实发起了符合 RFC8291/8292 的加密 HTTP 请求** | ✅ 真机验证（含端到端真实网络验证） |
| T103 | 私聊消息触发（目标离线时） | alice 给离线的 bob 发私聊消息 | bob 收到浏览器推送通知；目标在线时不重复推送（WS已实时送达） | 双方在线时 WS 正常送达，不触发额外推送 | ✅ 真机验证 |

### P0-13 AI 推荐规则化匹配演示（Task20，已实现验证，2026-08-18）

| 用例ID | 接口 | 请求 | 预期 | 通过标准 | 结果 |
|---|---|---|---|---|---|
| T110 | `POST /api/recommendations/generate` | 无参数 | `200 {"created":N}` | 扫描全部关注事项，两两用户按关键词交集+共同房间打分生成候选；已是好友的用户对排除；重复调用幂等不产生重复候选 | ✅ 真机验证 |
| T111 | `GET /api/recommendations` | 无参数（当前登录用户） | `200 [{"candidate_id":"...","peer_id":"...","match_score":N,...}]` | 返回含匹配对象与打分理由；未鉴权 `401` | ✅ 真机验证 |
| T112 | `PUT /api/recommendations/{id}` | `{"action":"confirm"\|"dismiss"}` | `200` | 非候选双方之一操作 `403`；候选不存在 `404`；已处理过的候选重复操作 `409`；非法 action `400` | ✅ 真机验证 |

### P0-14 用户查找/批量查询/好友请求列表补齐（本轮新增，已实现验证，2026-08-18，见前端功能闭环 work log）

> 背景：前端要做到"好友/私聊/关注事项/AI推荐"功能闭环，发现三处此前缺失的后端能力缺口——
> 好友请求只能靠 WS 实时通知得知（离线错过就永久看不到）、无法按用户名查找用户发起好友请求、
> 聊天页只能显示"用户ID前8位"而非真实用户名。本节为补齐这些缺口新增的接口用例。

| 用例ID | 接口 | 请求 | 预期 | 通过标准 | 结果 |
|---|---|---|---|---|---|
| T120 | `GET /api/friends/requests` | 无参数（当前登录用户） | `200 [{"request_id":...,"peer_id":...,"peer_username":...,"direction":"incoming"\|"outgoing",...}]` | 收到的请求标记为 `incoming`，发出的标记为 `outgoing`；请求被接受/拒绝后不再出现在列表中；未鉴权 `401` | ✅ 真机验证 |
| T121 | `GET /api/users/lookup?username=` | 用户名 | `200 {"id":...,"username":...}` | 用户不存在 `404 user_not_found`；`username` 为空 `400 invalid_request` | ✅ 真机验证 |
| T122 | `GET /api/users?ids=id1,id2,...` | 逗号分隔的用户ID列表 | `200 [{"id":...,"username":...}, ...]` | 不存在/非法格式（非 UUID）的 ID 静默忽略（不报错、不 500）；`ids` 为空返回空数组 | ✅ 真机验证（含边界：混入非 UUID 的 `no-such-id` 不触发 Postgres 报错） |
| T123 | `DELETE /api/watch-topics/:id` | 无请求体 | `204` | 非本人所有的关注事项视为不存在 `404 watch_topic_not_found`；重复删除同一 ID `404` | ✅ 真机验证 |

### P0-15 前端功能闭环（Task9 续，本轮新增，已实现验证，2026-08-18，见前端功能闭环 work log）

> 用 `playwright-cli` 驱动 3 个真实浏览器会话（alice/bob/carol）联动验证，覆盖好友、私聊、
> 关注事项、AI推荐、群聊真实用户名展示、Web Push 开关六个页面的端到端交互（非纯接口调用）。

| 用例ID | 场景 | 验证方式 | 结果 |
|---|---|---|---|
| T130 | 好友页：按用户名添加好友 -> 对方视角实时收到导航栏"好友•"未读提醒（无需刷新页面） | alice 发起请求，bob 浏览器（停留在 /rooms）实时出现提醒 | ✅ 真机验证 |
| T131 | 好友页：查看收到的请求并接受；接受后双方好友列表互相可见，在线状态正确 | bob 在 /friends 接受 alice 的请求 | ✅ 真机验证 |
| T132 | 私聊：从好友列表"发消息"进入私聊页（会话不存在时历史为空，不是错误态） | bob 点击"发消息" -> `/messages/:peerId` 显示"还没有聊天记录" | ✅ 真机验证 |
| T133 | 私聊：双向实时收发消息 + 对方视角导航栏"私聊•"未读提醒 + 进入会话列表后提醒清除 | bob 发消息 -> alice 导航栏出现提醒 -> alice 打开会话列表看到最新消息预览 -> 打开会话回复 -> bob 页面实时显示回复 | ✅ 真机验证 |
| T134 | 群聊：消息发送者展示真实用户名而非"用户ID前8位"（历史消息 + WS 实时消息两条路径） | alice 打开数码房间看到历史消息显示真实用户名；bob 实时发消息，alice 侧显示"bob_ui_a1" | ✅ 真机验证（修复此前已知半成品问题） |
| T135 | 关注事项：创建 -> 列表展示 -> 删除，UI 与后端状态一致 | alice 创建"摄影,爬山"关注事项后删除，列表变回空态 | ✅ 真机验证 |
| T136 | AI推荐：生成 -> 列表展示匹配理由与分数 -> 确认("感兴趣") -> 出现"加为好友"按钮 -> 点击后自动发起好友请求并在好友页可见待处理状态 | alice 与 carol（关键词均含"摄影"）匹配成功，确认后加好友，好友页"我发出的请求"出现 carol | ✅ 真机验证（AI推荐到好友关系的闭环路径） |
| T137 | AI推荐：已是好友的用户对不出现在候选列表中 | alice/bob 已互为好友，生成推荐后候选列表不含 bob | ✅ 真机验证（复用 Task20 既有排除逻辑，前端展示侧确认未破坏） |
| T138 | Web Push 开关：点击订阅按钮在浏览器拒绝通知权限（headless 自动化环境默认行为）时优雅降级，不抛出未捕获异常、不崩溃 | alice 点击 🔕，因测试环境无法真正授予通知权限，按钮保持未订阅状态，控制台无报错 | ✅ 真机验证（真实用户主动授权场景需要真实浏览器交互，非自动化环境限制，非产品缺陷） |
| T139 | 全局 WebSocket 连接提升为应用级共享连接（`SocketContext`）：任意页面均可收到好友请求/私聊消息通知，不再要求"必须停留在聊天页才能收到通知" | T130/T133 均在停留于 /rooms、/friends 等非聊天页时收到实时提醒，验证生效 | ✅ 真机验证 |

### 缺陷修复记录（用户报告，2026-08-18）

| 用例ID | 缺陷描述 | 根因 | 修复 | 结果 |
|---|---|---|---|---|
| T140 | 用户反馈"注册账户时并不支持纯数字输入，但是没有正确提示" | 复现后确认：**纯数字用户名本身后端/前端均支持**（`123`→201，已用curl直接验证）；真实根因是**密码长度提示不一致**——UI 占位文案与错误提示文案均写"至少6位"，但后端 `minPasswordLength=8`，用户按提示输入6-7位密码（常见测试习惯是简单数字）被拒绝，错误提示本身还错误地重复"至少6位"，等于告诉用户"你的输入已经满足要求"，造成困惑，与用户名是否纯数字无关 | 统一为"至少8位"（`AuthPage.tsx` 占位文案 + `client.ts` `invalid_password` 错误文案）；新增 `minLength={8}`（仅注册模式）触发浏览器原生前置校验，避免无效请求打到后端；新增用户名规则提示"3-32 位，支持字母、数字、下划线（纯数字也可以）"消除"纯数字是否支持"的疑虑 | ✅ 已用 `playwright-cli` 真实浏览器复现问题（6位密码报错文案自相矛盾）→ 修复 → 复验通过（6位密码被原生校验拦截不发请求；8位纯数字用户名+8位密码注册成功） |

### 能力补齐（用户差距分析反馈，2026-08-18）：WS 自动重连 / 登录暴力破解防护 / 前端自动化测试

| 用例ID | 场景 | 验证方式 | 结果 |
|---|---|---|---|
| T141 | WS 断线自动重连：连接被非主动关闭（网络抖动/服务端重启）后，`SocketContext` 按指数退避（1s→2s→4s→8s→封顶10s）自动重建连接，无需用户手动刷新页面；重连成功后已注册的 `subscribe` 监听器/页面自身的"加入房间"逻辑自动生效 | 单测：`SocketContext.test.tsx`（假 WebSocket + fake timers，验证重连时机与退避递增，3项）；真机：`docker stop`/`docker start` 后端容器模拟真实断线，`playwright-cli` 观察聊天页文案从"已连接"→"连接已断开，正在自动重连…"（输入框同步禁用）→ 服务端恢复健康后自动变回"已连接"，且重连后发消息成功（证明连接真实可用，非仅UI假显示） | ✅ 单测 3/3 通过 + 真机验证通过 |
| T142 | 登录接口暴力破解防护：`POST /api/auth/login` 按客户端IP做60秒固定窗口限流（默认 `LOGIN_RATE_LIMIT_PER_MINUTE=10`），超过阈值返回 `429 login_rate_limited`，不影响 `/register`、`/guest` | `curl` 连续发起15次错误登录请求，前10次 `401`（凭证错误），第11次起 `429`；`scripts/integration_test.py` 追加断言（放在脚本最后一步，避免影响前面的正常登录用例） | ✅ 真机验证（curl + 集成测试脚本共46项全部通过） |
| T143 | 前端自动化测试基础设施补齐：此前前端零测试资产，只靠人工/一次性 `playwright-cli` 会话验证 | 新增 `vitest` + `@testing-library/react`，覆盖 `api/client.ts` 纯函数（9项）、`AuthPage` 密码长度/用户名规则回归测试（3项，对应 T140 缺陷修复）、`SocketContext` 重连逻辑（3项，对应 T141），共 15 项，`npm run test` 全部通过 | ✅ 15/15 通过，且 `npm run build`/`npm run lint` 确认测试文件不影响生产构建与既有 lint 规则 |

### 能力补齐（用户体验反馈驱动，2026-08-18）：房间聊天页点击发言人用户名加好友

> 背景：群聊本身不要求好友关系即可发言（产品设计上的合理选择），但这意味着"认识一个聊得来的人后加好友"的
> 路径此前是"记住/复制用户名 -> 离开聊天室 -> 打开好友页 -> 手动输入用户名查找 -> 发请求"，链路长、易半路放弃。
> 不涉及任何后端/数据库改动，纯前端复用既有的 `GET /api/friends`、`GET /api/friends/requests`、
> `POST /api/friends/requests` 三个接口，新增 `UserActionPopover` 气泡组件。

| 用例ID | 场景 | 验证方式 | 结果 |
|---|---|---|---|
| C1 | 点击别人（非好友）的用户名 | alice 点击 bob 消息上的用户名 -> 弹出气泡，展示"加为好友"按钮 | ✅ 真机验证 |
| C2 | 点击自己发的消息上的用户名（"我"） | alice 自己的消息渲染为纯文本"我"（非按钮），不可点击 | ✅ 真机验证（快照确认为 `generic` 而非 `button`） |
| C3 | 点击已是好友的人的用户名 | bob 接受好友请求后，alice 再次点击 bob 用户名 -> 气泡展示"已经是好友"，无按钮 | ✅ 真机验证 |
| C4 | 点击"加为好友"发起请求 | alice 点击后按钮消失，气泡内文案变为"已发出好友请求，等待对方处理"，即为成功反馈 | ✅ 真机验证 |
| C5 | 已发出待处理请求时的状态展示 / `friend_request_already_exists` 边界 | 逻辑上与 C4 后再次打开气泡为同一分支（`direction==='outgoing'`），已随 C4/C8 链路验证；重复请求场景复用既有错误码翻译 | ✅ 代码路径确认（依赖既有已验证的错误码映射） |
| C6 | 气泡打开状态下点击气泡外部区域 | alice 点击输入框（气泡外部），气泡自动消失 | ✅ 真机验证 |
| C7 | 气泡打开状态下收到新消息 | alice 打开对历史发言人的气泡后，bob 发来新消息，气泡自动关闭 | ✅ 真机验证 |
| C8 | 端到端闭环：房间内点击加好友 -> 对方好友页收到 -> 接受 -> 双方互为好友 | alice 在数码房间点击 bob 用户名发起请求，bob 好友页"收到的好友请求"正确出现并接受成功 | ✅ 真机验证（alice/bob 双浏览器会话联动） |

### 能力补齐（诚实评估驱动，2026-08-19）：安全性/可靠性/测试/部署运行方式差距盘点后的修复

> 背景：结合课题9个考察方向对当前代码做了一次诚实评估（区分"已确认的设计边界简化"
> 与"真实缺口"），随后按方向逐一修复以下 9 项真实缺口，均已真机/自动化测试验证。

| 用例ID | 方向 | 场景 | 验证方式 | 结果 |
|---|---|---|---|---|
| G1 | 系统安全性 | 密码复杂度校验：注册密码必须同时包含字母与数字，纯数字/纯字母均被拒绝（此前只检查长度） | `TestValidatePassword` 单测新增4个用例；`integration_test.py` 新增3项断言（纯数字拒绝/纯字母拒绝/混合通过） | ✅ 单测+集成测试通过 |
| G2 | 系统安全性 | CORS 白名单可配置：`CORS_ALLOWED_ORIGINS` 环境变量，默认 `*` 保持原行为不变，可配置为具体域名列表 | 代码审查 + `go build`/`go vet` 通过；未破坏现有跨域访问（前端容器构建后正常访问） | ✅ 编译通过，行为兼容 |
| G3 | 系统安全性 | JWT 登出黑名单：`POST /api/auth/logout` 后旧 token 立即失效（HTTP 与 WS 两条鉴权路径均生效），此前"登出"只是前端清 localStorage | `integration_test.py` 新增断言：登出前 token 可正常访问 -> 调用登出 -> 登出后同一 token 返回 `401 token_revoked`；真机验证访客登出流程无异常 | ✅ 集成测试 + 真机验证通过 |
| G4 | 服务可靠性 | Web Push 发送失败重试：网络错误/5xx 最多重试3次（含首次），4xx（含410订阅失效）不重试直接判定终态 | 新增3个单测：410响应清理僵尸订阅（回归）、前两次503后第三次成功、持续503最终放弃且尝试次数精确等于上限 | ✅ 3/3 单测通过 |
| G5 | 测试与代码质量 | repository 层补充真实数据库单测：此前 `internal/repository` 覆盖率 0%，只能靠黑盒集成测试脚本兜底 | 新增 `user_repository_test.go`（5项，含 T122 修复的 UUID 边界回归）、`watch_topic_repository_test.go`（2项）、`token_blacklist_test.go`（2项），共9项，均对真实 Postgres/Redis 容器执行 | ✅ 9/9 通过 |
| G6 | 性能与成本 | 首次真实压测：此前完全没有任何并发性能基准数据 | 新增 `scripts/load_test.py`，实测 HTTP `GET /api/rooms` 50/200并发吞吐与延迟分布，WS 端到端消息往返延迟（50/200并发连接），数据见 `docs/17-gap-closure-work-log.md` 第七节 | ✅ 已获得真实数据（非专业压测工具，已如实标注局限性） |
| G7 | 测试与代码质量 | CI 配置：此前无任何 CI，全靠手动执行 | 新增 `.github/workflows/ci.yml`（后端 gofmt/govet/golangci-lint/go test 含真实 Postgres+Redis service；前端 build/lint/vitest），如实标注"未在真实 CI 环境跑过一次"（本项目无远程 git 仓库） | ✅ 配置资产已生成，本地无法验证真实 CI 触发 |
| G8 | 部署及运行方式 | 数据库备份/恢复策略：此前完全没有任何说明 | 新增 `scripts/db_backup.sh`/`db_restore.sh` + `docs/18-backup-and-restore.md`；真实执行一轮"备份→清空覆盖恢复→校验数据一致"（恢复前后 users=365/rooms=4 完全一致）+ smoke test 通过 | ✅ 真机验证通过（含修复真实踩坑：`pg_dump` 需加 `--clean --if-exists` 才能幂等恢复） |
| G9 | 产品功能与用户体验 | 未读消息精确数字：好友请求/私聊未读此前只有一个圆点，现展示精确数字（好友请求数从后端拉取真实基准值+WS事件累加；私聊未读为"本次会话期间收到的新消息数"，如实标注不含历史累积未读） | 真机验证（alice/bob/carol三方）：好友请求计数从1累加到2 -> 访问好友页清零 -> 离开重进后端重新拉取仍为2（验证非临时假数字）；私聊消息计数从1累加到2 -> 访问私聊清零 -> 验证自己发消息不误增自己的计数（sender_id过滤边界） | ✅ 真机验证通过 |

### 测试覆盖率专项补齐（2026-08-19）

针对"测试与代码质量"方向此前指出的"覆盖率分布很不均衡"，新增60+单测用例：
`internal/api` 0.3%→75.5%、`internal/middleware` 0%→94.7%、`internal/ws` 13.6%→61.1%、
`internal/config`/`pkg/metric`/`pkg/log` 0%→97.4%/100%/100%，全项目加权总体
0%→**58.0%**（`go tool cover -func` 实测）。详见 `docs/19-test-coverage-work-log.md`。

### 能力补齐（更丰富的部署与运行时测试，2026-08-19）

> 背景：架构文档一直声称"Redis Pub/Sub 多实例扇出为强制设计"，但此前所有测试/演示
> 场景永远只跑一个 server 容器，这个设计从未被真正的第二个独立进程验证过。本轮
> 新增 `deploy/docker-compose.multi-instance.yml`（叠加 overlay，起 server2 + nginx
> 负载均衡器 `lb`，不影响/替代默认单实例部署）+ 两个验证脚本，用真实容器操作
> （非模拟/纸面推演）验证多实例横向扩展与运行时可靠性。

| 用例ID | 场景 | 验证方式 | 结果 |
|---|---|---|---|
| M1 | 负载均衡真实分流到不同物理实例 | 连续10次请求 `lb:8082/healthz`，收集响应中的 `instance_id`（能力补齐新增字段），验证确实观测到 ≥2 个不同实例 | ✅ 真机验证（`scripts/multi_instance_test.py`） |
| M2 | 跨实例群聊广播（核心） | 两个 WS 客户端分别连接负载均衡器，落在两个不同的物理 `server`/`server2` 进程上，A 发消息，验证 B（落在另一实例）能通过 Redis Pub/Sub 真实收到广播，而非"同进程自己发布自己订阅" | ✅ 真机验证，两连接确认落在不同 instance_id |
| M3 | 跨实例私聊投递 | 同 M2 拓扑，A、B 互加好友后由落在不同实例的连接发起私聊，验证跨实例投递同样生效 | ✅ 真机验证 |
| M4 | 实例故障自动降级 | `docker stop server2`，连续20次请求负载均衡器，验证服务整体不中断（≥15/20成功，允许故障检测窗口内个别请求失败）；恢复 server2 后15秒内重新加入负载均衡池 | ✅ 真机验证（`scripts/resilience_test.sh`），20/20成功+重新加入 |
| M5 | Redis 故障自动恢复 | `docker stop redis`，验证 `/readyz` 立即报告 `not_ready` 且明确指出 `redis` 故障；`docker start redis` 后20秒内 `/readyz` 自动变回 `ready`（无需重启应用进程） | ✅ 真机验证 |
| M6 | Postgres 故障自动恢复 | 同 M5，验证 `/readyz` 明确指出 `db` 故障，恢复后30秒内自动变回 `ready` | ✅ 真机验证 |

已知边界（如实标注）：仍是同一台机器上的多个容器，非真正多机/多可用区部署；
未引入 K8s 等编排系统的自动扩缩容/健康检查驱逐能力。详见
`docs/20-deployment-runtime-testing-work-log.md`。

### 能力补齐（LLM 驱动机器人最小验证，2026-08-19）

> 背景：设计之初就讨论过"训练替身机器人代替用户进行前期社交"的远期方向
> （`docs/00-brainstorm-and-plan.md` ★13/★14 + Part3「AI-native 二期扩展设计」），
> 但此前只落地了 schema 预留（`users.is_bot`/`bot_action_log` 等），业务代码
> 从未真正读写过这些字段，也从未产生过一条真实的机器人消息。本轮补一个最小的、
> 真实可运行的验证闭环，用通义千问（Qwen/DashScope）LLM API 驱动机器人生成
> 消息内容，而非硬编码模板。

| 用例ID | 场景 | 验证方式 | 结果 |
|---|---|---|---|
| B1 | 机器人账号标记 | `cmd/bot` 通过 `UserRepository.SetIsBot` 标记账号后，直查数据库 `users.is_bot` | ✅ 真机验证（`is_bot=t`） |
| B2 | 机器人消息 sender_type 正确广播/落库 | 机器人通过真实 WS 协议加入房间发消息，验证广播事件与 `messages` 表行的 `sender_type` 均为 `bot`，且判定完全由服务端根据账号身份决定（不信任客户端字段） | ✅ 真机验证 + 单测（`TestHub_HandleSendMessage_BotSenderTypeBroadcast`） |
| B3 | 消息内容确实由 LLM 生成 | 打印 LLM 原始 usage 统计（`prompt_tokens`/`completion_tokens`/`total_tokens`）证明发生过真实网络调用；内容非代码里硬编码的模板字符串 | ✅ 真机验证（真实调用 Qwen API，模型自主生成"（机器人）哈喽～欢迎来数码天地！…"） |
| B4 | 前端正确展示机器人标识 | 真实浏览器进入房间，验证发言人展示为 `ai_zhidai_bot（机器人）` | ✅ 真机验证（`playwright-cli`），此前该前端防御性分支从未被真实触发过 |
| B5 | `bot_action_log` 首次真正写入 | 机器人发消息后落一条决策记录，直查数据库 | ✅ 真机验证（此前该表从建表起 0 行） |
| B6 | 向真实存在的用户发起好友请求 | 机器人分别向 `moonteng`、`lili`（用户已注册的真实账号）发起好友请求，直查 `friendships` 表 | ✅ 真机验证（两条 `pending` 记录） |

已知边界（如实标注）：机器人是独立身份，不代理任何具体真人用户
（`proxy_for_user_id` 仍未接入业务逻辑）；本次只验证"发一条消息+发好友请求"
这一最小链路，不涉及机器人自主决策/多轮长期社交/基于 embedding 的语义匹配。
详见 `docs/23-llm-bot-minimal-validation-work-log.md`。

### 能力补齐（用户行为数据训练管道最小验证，2026-08-19）

> 背景：`interaction_events` 表此前从建表起从未被写入过——"未来能否把用户
> 行为数据投喂给模型训练用户替身"这个设想缺少最基础的行为原始数据支撑。
> 本轮让 `join_room`/`send_message`/`add_friend_request` 三类事件真实写入，
> 并新增最小导出工具验证"格式上确实能被整理成结构化训练语料"。

| 用例ID | 场景 | 验证方式 | 结果 |
|---|---|---|---|
| D1 | `join_room` 事件真实写入 | 机器人首次加入房间后直查 `interaction_events`；重复 join 不重复记录（幂等） | ✅ 真机验证 + 单测（`TestHub_HandleJoinRoom_RecordsInteractionEvent`） |
| D2 | `send_message` 事件真实写入 | 成功发消息后事件 Payload 含 `msg_id`/`content_type`（不重复存储正文）；命中敏感词拦截/重复消息等失败路径不产生事件 | ✅ 真机验证 + 单测（`TestHub_HandleSendMessage_RecordsInteractionEvent`、`TestHub_HandleSendMessage_BlockedContent_DoesNotRecordEvent`） |
| D3 | `add_friend_request` 事件真实写入 | 两个全新注册账号发起好友请求，直查事件表 `target_user_id` 正确；重复发起（已存在）不重复记录 | ✅ 真机验证 + 单测（`TestFriendService_SendRequest_RecordsInteractionEvent`） |
| D4 | 未注入 EventRecorder 不影响主流程 | `EventRecorder` 为 nil（默认状态）时 join/send 均正常工作 | ✅ 单测（`TestHub_EventRecorder_Nil_DoesNotPanic`） |
| D5 | 最小导出：数据库到结构化 JSON 的格式验证 | `cmd/export_training_data` 对机器人账号、两个全新真人账号分别导出，输出含 `format_version`/`user`/`watch_topics`/`interaction_events` 的结构化 JSON，覆盖全部 3 种事件类型 | ✅ 真机验证 |

已知边界（如实标注）：这只是让"从0到1完全没有行为数据"变成"有基础事件流+
一条可复现的导出链路"；未接入结构化标签解析（`user_watch_topics.keywords`
仍是自由文本）、未加数据使用授权/opt-in 字段、未做账号删除前的归档策略、
`view_profile`/`long_dwell` 等更细粒度事件类型仍未接入。详见
`docs/24-training-data-pipeline-work-log.md`。

## Part 4　固定部署后最小 trace 草案

本项目为独立 Docker Compose 部署（非内部 STKE），"部署后最小 trace"简化为**本地/CI 环境一键验证脚本**，覆盖顺序：

1. `docker compose up -d` 启动 app+db+redis
2. 轮询 `GET /healthz` 直到 200（超时告警而非死等）
3. `GET /readyz` 确认 db/redis 均 ok
4. 走 T10→T13（注册+登录，拿到 token）
5. 走 T20（拉房间列表，确认种子数据已灌入）
6. 走 T30→T33（WS 建连+加房+发消息，验证核心闭环）

### Part 4.5　黑盒接口回归固化（Task11，2026-08-18 新增）

上方 T10-T139 中不依赖 WebSocket 的 REST 接口用例（鉴权/房间/好友/用户查找/关注事项/
AI推荐/私聊会话读路径/Web Push订阅/举报/未鉴权拦截）已固化为可重复运行的脚本
`scripts/integration_test.py`，不再依赖临时 `/tmp/` 脚本：

```bash
python3 scripts/integration_test.py http://localhost:8080
# ==> 54 passed, 0 failed
```

WebSocket 相关用例（群聊/私聊实时收发、限流、跨实例广播等）仍按本文档记录的方式
人工/一次性脚本验证，因 Python 标准库无内置 WS 客户端，未纳入本脚本。

以上脚本作为 Work 阶段交付物之一（如 `scripts/smoke_test.sh` 或集成测试文件），失败即视为本次改动未通过最小验证。

## Part 5　失败回退标准 / 监控观测点 / 自动化与人工验证边界

### 命令格式、重放与错误隔离（2026-08-26 新增）

| 用例ID | 场景 | 预期 | 本机实测（2026-08-27） |
|---|---|---|---|
| V1 | 任一 JSON 写接口携带未知字段、错误字段类型、空 body 或两个 JSON 值 | `400 invalid_request_body`，无写入副作用 | ✅ 严格绑定单测 + HTTP 集成 |
| V2 | 房间/会话历史携带非法 `page`/`size` | `400 invalid_page` 或 `invalid_size`，不静默回退 | ✅ HTTP 集成 |
| V3 | 同一鉴权用户以相同 `Idempotency-Key` 重放任一成功 HTTP 写命令 | 返回第一次的原始 2xx 响应；handler/写库/通知只执行一次 | ✅ Redis 单测 + HTTP 集成 |
| V4 | 相同 key 的第一个请求尚未完成时并发重放 | `409 idempotency_in_progress`，不产生第二次副作用 | ✅ 真实 Redis 并发单测 |
| V5 | 带 key 时 Redis 不可用 | `503 idempotency_unavailable`，拒绝不确定的写入 | ✅ Redis 不可达单测 |
| V6 | WS 帧存在未知字段、非法 UUID、多个 JSON 值或未知事件 | 收到 `error` 帧，连接保持可用，下一条合法命令仍可处理 | ✅ 严格解码/分发单测 |

**失败回退标准**：P0 任一用例未通过（T01-T81 中标记为 P0 的），视为该 Task 未完成，回到 Work 阶段修复，不得进入下一 Task 或声称"验证通过"。

**监控观测点**：
- 日志关键字：`trace_id`/`room_id`/`user_id` 应出现在每条业务日志中；错误日志需包含具体 `error_code`。
- 指标：在线连接数、房间人数分布、消息QPS、WS错误率、限流触发次数。

**自动化与人工验证边界**：
- 自动化：T01-T81 全部可写成集成测试自动执行（Go 的 `net/http/httptest` + WS 客户端库）。
- 人工验证：T36（多实例广播）需要人工起两个进程实例并观察，建议写成脚本辅助但首次验证人工确认结果。

**待确认/待补充事项（不阻塞首次编码启动，随对应 Task 实现前补齐）**：
- ~~T72：非好友私聊限制口径，待 Task15 实现前与用户确认。~~ **已于 2026-08-17 确认：仅好友可私聊**，
  见 `docs/07-task15-work-log.md`。
- ~~Task16/20 对应用例将在各自实现前补充到本文件。~~ **Task16（T90-T93）已于 2026-08-17 补充并
  验证通过**，见上方「P0-10」与 `docs/09-task16-18-19-work-log.md`；**Task20（T110-T112）已于
  2026-08-18 补充并验证通过**，见上方「P0-13」。
- ~~Task17（Web Push）对应用例将在实现前补充到本文件。~~ **已于 2026-08-18 补充并验证通过
  （含端到端真实网络验证）**，见上方「P0-12」与 `docs/10-task17-20-work-log.md`。
- 至此，Plan Part5 Task 清单中的全部 P0/P1 用例均已补充完毕；Task9（前端页面）、
  Task10-13（收尾类）不产出独立接口用例。
