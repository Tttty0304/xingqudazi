# Task17+Task20 工作记录：Web Push 离线通知、AI 推荐规则化匹配演示

> 日期：2026-08-18
> 说明：按用户"降低验证密度"的要求，本轮将 Task17（Web Push）+ Task20（AI推荐）合并在
> 一轮内完成开发+验证。Task20 依赖 Task19（已完成）的关注事项数据与 Task14（已完成）
> 的好友关系数据，Task17 复用 Task14/Task15 已建立的 Redis Pub/Sub 跨实例推送基础设施
> （用户级频道），两者都是"演示型"收尾功能，适合合并推进。

## 范围

- **Task17 Web Push 离线通知**：
  - 后端实现 RFC8291（消息加密，aes128gcm）+ RFC8292（VAPID 身份声明）协议的最小必要
    子集（`pkg/webpush`），不依赖任何第三方 web-push 库，纯 Go 标准库 + `golang-jwt` +
    `golang.org/x/crypto/hkdf` 实现。
  - `GET /api/push/vapid-public-key`（无需鉴权）+ `POST/DELETE /api/push/subscriptions`
    （订阅管理）。
  - 触发时机：好友请求（Task14）/私聊消息（Task15）目标用户**离线**（无活跃 WS 连接）
    时才发送 Web Push；在线用户已通过 WS 实时收到，不重复推送。
  - 订阅失效（推送服务返回 404/410）时自动清理该条订阅记录。
  - 未通过环境变量配置 VAPID 密钥时，进程启动自动生成一对临时密钥（已在配置层打印
    WARN 日志提示这一简化点）。
  - 未做：前端 Service Worker 代码（Task9 尚未开始）、通知点击跳转等前端交互。
- **Task20 AI 推荐规则化匹配演示**：
  - 规则：两用户关注事项关键词交集（每个共同关键词 +2 分）+ 是否在同一房间维度都设置
    了关注事项（+1 分）；已是好友的用户对不生成候选。
  - `POST /api/recommendations/generate`（触发全量重新生成，demo 场景任一登录用户可
    触发，非管理员专属/非定时任务）、`GET /api/recommendations`（查询待确认候选）、
    `PUT /api/recommendations/:id`（confirm/dismiss）。
  - 未做：真实 AI/LLM 语义匹配（Plan 明确是"规则化匹配演示"，不是真实 AI 推荐系统）、
    后台定时任务触发生成（本次为手动触发接口）。

## 代码改动清单

- **migrations**：新增 `0002_task17_push_subscriptions.up/down.sql`（Web Push 订阅表；
  `match_candidates` 表已在 0001 建好，Task20 无需新迁移）。
- **model**：新增 `push_subscription.go`、`match_candidate.go`。
- **pkg/webpush**：新增 `webpush.go`（RFC8291/8292 实现）+ `webpush_test.go`
  （端到端加解密自验证单测，模拟浏览器生成真实 ECDH 密钥，用本包 `Send()` 加密发送到
  本地 `httptest.Server`，再用测试内的逆向解密实现验证明文完全还原，并断言协议头
  正确）。
- **repository**：新增 `push_subscription_repository.go`、`match_candidate_repository.go`；
  `watch_topic_repository.go` 新增 `ListAll`（供 Task20 全量扫描）。
- **service**：
  - 新增 `push_service.go`+`push_errors.go`（`PushService.Subscribe/Unsubscribe/
    NotifyOfflineUser`）。
  - 新增 `recommendation_service.go`+`recommendation_errors.go`
    （`RecommendationService.GenerateCandidates/ListCandidates/RespondCandidate`，
    纯逻辑函数 `matchTopics`/`candidateAction` 独立可单测）。
  - `friend_service.go`：新增 `PushNotifier` 接口 + `SetPushNotifier`（可选注入，
    不改变 `NewFriendService` 签名），`SendRequest` 内接入离线推送触发。
- **ws/hub.go**：新增 `PushNotifier` 接口 + `SetPushNotifier`，`handleSendDirectMessage`
  内接入离线推送触发（不含发送者用户名的通用文案，保持 ws 包依赖最小化）。
- **api**：新增 `push.go`、`recommendation.go`。
- **config**：新增 `MediaUploadDir`（已存在）之外新增 `VAPIDPublicKey`/
  `VAPIDPrivateKey`/`VAPIDSubject`，未配置时自动生成临时密钥并打印 WARN。
- **main.go**：接线 `pushSvc`/`recommendationSvc` 及对应路由；`wsHub.SetPushNotifier`/
  `friendSvc.SetPushNotifier` 注入。
- **deploy/docker-compose.yml**：postgres 服务新增第二个初始化 SQL 挂载
  （`0002_task17_push_subscriptions.up.sql`）。
- **测试**：`recommendation_service_test.go`（8 个测试函数，覆盖 matchTopics 纯逻辑、
  生成/排除好友/幂等、查询/响应/越权/已处理/非法action）、`push_service_test.go`
  （5 个测试函数，覆盖订阅管理、在线跳过、离线发送、失效订阅清理）。

## 验证结果（真实 Docker 容器环境）

| 项 | 结果 |
|---|---|
| 编译 + `go vet` + 全部单元测试（含 webpush 包端到端加解密自验证） | ✅ 通过 |
| T100 订阅推送 | ✅ 201 |
| T101 取消订阅 | ✅ 204 |
| T102 好友请求触发离线推送（主流程不受影响） | ✅ 201 |
| T103 私聊消息触发离线推送（双方在线时 WS 正常送达） | ✅ 通过 |
| **端到端真实网络验证**：容器内进程向宿主机 mock 推送服务发起真实 HTTP 请求 | ✅ 通过：`Content-Encoding: aes128gcm`，`Authorization: vapid t=<真实JWT>`，body 181字节加密载荷，完全符合 RFC8291/8292 |
| T110 生成推荐候选 | ✅ 200 |
| T111 查询推荐列表（含未鉴权401） | ✅ 通过 |
| T112 确认/忽略候选（含非参与者403、重复响应409、非法action 400） | ✅ 通过 |
| 已是好友的用户对排除在推荐外 | ✅ 通过 |
| Task4群聊、Task14好友、Task6/8可靠性安全、Task16/18/19回归 | ✅ 全部通过，无回归 |

### 关于"端到端真实网络验证"的说明

首次验证时使用手工构造的非法 ECDH 公钥字节（格式合法但不是曲线上的合法点），
`crypto/ecdh.NewPublicKey()` 正确拒绝了这一非法输入，导致在发起网络请求前就返回错误
——这是代码按预期工作（参数校验先行），但确实说明第一次验证方法有误，不能因此断言
"网络发送环节没有被真实验证"。改用真实生成的 P-256 密钥重测后，在宿主机监听一个
极简 mock HTTP 服务，触发好友请求后，**确认容器内进程真实发起了跨网络的 HTTP POST
请求**，携带正确的协议头与加密体，这是不依赖真实浏览器/FCM/Mozilla Push Service
也能给出的最强正确性证据。

## 已知简化点（如实记录，非遗漏）

- VAPID 密钥未配置环境变量时自动生成临时密钥，进程重启后旧浏览器订阅的 VAPID 校验
  会失效，需要用户重新订阅；生产环境应固定配置。
- Task20 的"AI 推荐"是纯规则化打分（关键词交集+共同房间），不涉及任何机器学习/LLM
  语义理解，与 Plan 中"规则化匹配演示"的定位一致，非过度承诺。
- `POST /api/recommendations/generate` 是手动触发接口，非后台定时任务，且 demo 场景
  下未做管理员权限限制（任一登录用户均可触发全量重新生成）。
- Web Push 前端 Service Worker 代码、通知权限请求 UI、点击通知后的页面跳转均未实现
  （属于 Task9 前端范围，Task17 本次只交付后端契约）。

## 遗留

无新增遗留项。
