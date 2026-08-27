# 前端功能闭环 Work Log（好友/私聊/关注事项/AI推荐/Web Push UI + 群聊真实用户名修复）

> 日期：2026-08-18
> 背景：`docs/11-task9-work-log.md` 完成的是 Task9 **核心闭环**（登录注册/房间列表/群聊），
> 好友、私聊、关注事项、AI推荐、Web Push 对应的后端能力（Task14/15/17/19/20）此前均已
> 完成并真机验证，但**完全没有前端 UI**，属于"后端能力完整、前端消费方缺失"的典型半成品。
> 本轮目标：补齐这些前端页面，做到整体功能闭环，消除明显的半成品痕迹，再进入收尾阶段。

## 一、发现的功能缺口 & 处理方式

在设计前端页面前，梳理已有后端接口时发现三处真实的**接口缺口**（不只是前端缺失）：

1. **好友请求无法"事后查看"**：此前只有 WS `friend_request_received` 实时通知，接收方若
   当时离线，之后没有任何接口能查看"我有哪些待处理的好友请求"。新增
   `GET /api/friends/requests`（T120），区分 `incoming`/`outgoing` 方向。
2. **无法按用户名查找用户**：好友页"添加好友"场景需要先把用户名转成 user_id 才能调用
   `POST /api/friends/requests`，此前没有任何查找接口。新增 `GET /api/users/lookup?username=`（T121）。
3. **聊天页只能显示"用户ID前8位"**：群聊/私聊消息只有 `sender_id`，没有批量查用户名的接口，
   前端此前用占位展示（`用户${id.slice(0,8)}`）。新增 `GET /api/users?ids=`（T122）批量查询。

顺带补齐一处非阻塞但明显影响可用性的缺口：

4. **关注事项只能创建不能删除**：新增 `DELETE /api/watch-topics/:id`（T123），否则列表只会
   越堆越多，无法清理测试/过期数据。

## 二、后端改动（新增，非重构）

- `service/user_service.go`（新建）：薄封装 `UserStore`，供 `api.UserHandler` 复用
  `FindByUsername`/`FindByIDs`，避免 api 层直接依赖 repository。
- `repository/user_repository.go`：新增 `FindByIDs`（`WHERE id = ANY($1)`，避免 N+1）。
  **踩坑**：`id` 列是 UUID 类型，若传入非法格式字符串（如前端脏数据/已注销用户的陈旧引用），
  Postgres 做隐式类型转换时会直接报错而不是"查不到"，导致整个批量请求 500。修复：查询前用
  `uuid.Parse` 预过滤非法 ID，与"找不到静默忽略"语义保持一致。
- `api/user.go`（新建）：`UserHandler.Lookup`（`GET /api/users/lookup`）+
  `UserHandler.BatchGet`（`GET /api/users`），均需鉴权。
- `service/friend_service.go` + `repository/friendship_repository.go`：新增
  `FriendshipStore.ListPendingByUser` + `FriendService.ListPendingRequests`，
  按请求发起方计算 `direction`，并补全对端用户名。
- `api/friend.go`：新增 `FriendHandler.ListPendingRequests`（`GET /api/friends/requests`）。
- `service/watch_topic_service.go` + `repository/watch_topic_repository.go`：新增
  `WatchTopicStore.Delete` + `WatchTopicService.DeleteWatchTopic`（越权/不存在统一返回
  `ErrWatchTopicNotFound`）。
- `api/watch_topic.go`：新增 `WatchTopicHandler.Delete`（`DELETE /api/watch-topics/:id`）。
- `cmd/server/main.go`：接线上述 4 个新路由 + `UserService`/`UserHandler` 构造。
- 单测：`user_service_test.go`（新建）、`watch_topic_service_test.go`（新建，此前完全没有该
  service 的单测覆盖）、`friend_service_test.go` 新增 `TestFriendService_ListPendingRequests_T120`。
  `go test ./... -v` 全绿。

## 三、前端改动

### 架构调整：WebSocket 连接提升为应用级共享连接

此前 `useWebSocket` 只在 `RoomChatPage` 内建立连接，意味着好友请求/私聊消息等通知**只有
正好停留在聊天页才能收到**——这本身就是一种半成品体验。本轮引入 `context/SocketContext.tsx`，
把唯一的 WS 连接提升到 `AuthProvider` 之下、路由树之上，任意页面通过 `useSocket().subscribe(...)`
注册自己关心的事件，互不干扰。原 `hooks/useWebSocket.ts` 已删除（避免遗留死代码）。

### 新增文件

| 文件 | 作用 |
|---|---|
| `context/SocketContext.tsx` | 应用级共享 WS 连接 + 多路事件订阅 |
| `hooks/useUsernames.ts` | 批量解析 `user_id -> username`（带跨调用缓存，替代"用户ID前8位"占位展示） |
| `push.ts` + `public/sw.js` | Web Push 前端最小实现：Service Worker 注册、订阅/取消订阅、通知展示/点击跳转 |
| `components/NotificationToggle.tsx` | Layout 头部的 Web Push 订阅开关（🔔/🔕） |
| `pages/FriendsPage.tsx` | 好友页：按用户名添加 -> 收到的请求（接受/拒绝）-> 发出的请求（等待中）-> 好友列表（在线态+发消息+删除） |
| `pages/ConversationsPage.tsx` | 私聊会话列表：展示对方真实用户名 + 最近消息预览 |
| `pages/DirectChatPage.tsx` | 私聊聊天页：按 `peerId` 路由（不要求会话预先存在），双向实时收发 + 图片消息 |
| `pages/WatchTopicsPage.tsx` | 关注事项页：创建（关键词+可选房间+优先级）+ 列表 + 删除 |
| `pages/RecommendationsPage.tsx` | AI推荐页：生成/刷新 + 列表（匹配理由/分数）+ 感兴趣/忽略 + 确认后一键加好友 |

### 关键改动

- `RoomChatPage.tsx`：改用 `useSocket()` 替代原 `useWebSocket`；引入 `useUsernames` 解析
  历史消息与实时消息发送者的真实用户名（修复"用户ID前8位"半成品问题）；WS `error` 事件
  展示改用新增的 `translateCode` 中文提示，而非展示原始 code。
- `api/client.ts`：`ERROR_MESSAGES` 补齐新增接口 + WS 事件的错误码中文提示，导出
  `translateCode` 供 WS 错误场景复用。
- `App.tsx`：`SocketProvider` 包裹路由树；新增 5 个受保护路由。
- `Layout.tsx`：新增导航入口（私聊/好友/关注事项/AI推荐）+ 全局未读提醒（好友请求/私聊消息，
  订阅 `SocketContext`，离开对应页面后自动清除）+ `NotificationToggle`。

### 私聊路由设计说明

私聊页按**对方用户 ID**（`/messages/:peerId`）而非会话 ID 组织路由，不要求双方此前已经
创建过会话：进入页面时先查 `GET /api/conversations` 有没有匹配的 `peer_id`，有则拉取历史，
没有则视为空历史（非错误态），首次发送消息时后端会惰性创建会话，前端无需额外调用"创建会话"接口。

## 四、验证

### 后端接口验证（真实 Docker 环境，`docker compose up -d --build server`）

用临时脚本（`/tmp/verify_batch_a.py`，未入库）覆盖 T120-T123 全部用例，包含边界场景：
用户名不存在、批量查询混入非法 UUID、关注事项越权删除/重复删除。全部通过，详见
`testcase/00-testcase-plan.md` P0-14 章节。

### 前端真实浏览器端到端验证（`playwright-cli`，3 个独立浏览器会话：alice/bob/carol）

覆盖 `testcase/00-testcase-plan.md` P0-15 章节 T130-T139，重点验证：

1. 好友请求发起 -> 对方**停留在房间列表页**（非聊天页）时导航栏实时出现"好友•"提醒
   -> 打开好友页提醒消失、请求可接受。
2. 私聊双向实时收发：bob 发消息 -> alice（停留在好友页）导航栏"私聊•"提醒 -> 打开会话
   列表看到最新消息预览 -> 打开会话回复 -> bob 页面（一直开着）实时收到回复，无需刷新。
3. 群聊真实用户名：历史消息与 WS 实时消息两条路径均展示真实用户名（而非"用户ID前8位"）。
4. 关注事项创建/删除 UI 状态与后端一致。
5. AI推荐生成 -> 确认 -> 加为好友，形成"推荐 -> 好友关系"完整闭环（好友页"我发出的请求"
   可见对应记录）；已是好友的用户对不出现在候选中（复用 Task20 既有排除逻辑）。
6. Web Push 开关在自动化测试环境（无法真正授权通知权限）下优雅降级，不抛异常。

## 五、已知限制（如实说明，非本轮范围）

- Web Push 真实订阅需要用户在真实浏览器中主动授予通知权限（`Notification.requestPermission`），
  自动化测试环境默认拒绝，本轮仅验证了"权限被拒绝时优雅降级"，未验证"真实收到推送通知"的
  浏览器侧效果（后端推送发送链路已在 `docs/10-task17-20-work-log.md` 用宿主机 mock 服务
  验证过真实网络请求）。
- Service Worker 要求安全上下文（HTTPS 或 `localhost`），当前 docker-compose 部署走
  `http://localhost:8081`，满足条件；若未来部署到非 localhost 的 HTTP 域名，Web Push
  入口会因浏览器策略静默不可用（`isPushSupported()` 返回 false，按钮不渲染，非崩溃）。
- 好友列表/关注事项等页面未做分页（当前用户规模下数据量小，非阻塞项）。

## 六、状态

好友/私聊/关注事项/AI推荐/Web Push 五个此前缺失的前端页面全部补齐并真机验证通过；
群聊"用户ID前8位"半成品问题已修复；WebSocket 连接架构提升为应用级共享，通知类交互不再
局限于"必须停留在聊天页"。**Task9 全部前端页面（含扩展功能）与后端 Task1-8、14-20 已形成
完整闭环，无遗留的明显半成品页面。**

后续可推进方向：Task10-13（性能优化/测试收尾/部署文档完善/演示脚本）。
