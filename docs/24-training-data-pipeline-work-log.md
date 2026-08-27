# 用户行为数据训练管道最小验证（工作日志，2026-08-19）

> 背景：用户提问"在数据存储方式和整理的格式方面，我们有没有考虑到未来直接
> 能够将用户痕迹和数据投喂给模型训练用户替身呢？"，诚实检查后发现：
> `interaction_events` 表（Plan 原始设计里"训练信号的核心来源，历史数据无法
> 回溯补采，必须从上线第一天开始记录"）从建表起到现在一行都没写过；
> `messages.is_blocked` 字段同样是死字段（命中敏感词的消息从未插入过，负样本
> 不存在）；且完全没有一条能把数据库原始行整理成"可投喂给模型"格式的管道。
> 本轮范围（用户明确聚焦在两点）：① 让行为事件真正开始被记录（越晚做，
> 错过的历史数据越多）；② 补一个最小的导出验证，证明格式上确实可行。

## 一、范围

**做的**：
1. `interaction_events` 首批接入 3 类事件的真实写入：`join_room`（WS）、
   `send_message`（WS）、`add_friend_request`（REST 触发的 service 层行为），
   覆盖"实时协议行为"与"REST触发行为"两类来源，证明机制不局限于某一条路径。
2. 新增 `cmd/export_training_data` 最小导出工具：把某用户的账号信息、关注
   事项、行为事件历史，组装成一份带版本号的结构化 JSON。

**明确不做**（如实标注，避免范围失控）：
- `user_watch_topics.keywords` 结构化标签解析（仍是自由文本字符串）；
- 数据使用授权/opt-in 字段（呼应★14 机器人opt-in精神的"姐妹决策"，本轮未做）；
- 账号删除前的训练数据归档策略；
- `view_profile`/`long_dwell` 等更细粒度事件类型（Plan 里提到但本轮聚焦在
  已有代码路径能自然产生的 3 类核心事件，不为了"多凑几个事件类型"而新增
  本不存在的功能触点）；
- `messages.is_blocked` 死字段问题（这是另一个独立的诚实缺口，本轮未处理）。

## 二、改动清单

### 1. `model.InteractionEvent`

对应 `interaction_events` 表的 Go 结构体。`Payload` 用 `json.RawMessage`，
故意不在 Payload 里重复存储消息正文——原始内容已完整落在 `messages` 表，
行为事件通过 `msg_id` 关联即可，避免同一份内容维护两份拷贝的一致性风险。

新增事件类型常量 `EventTypeJoinRoom`/`EventTypeSendMessage`/
`EventTypeAddFriendRequest`，命名与 Plan 文档列出的事件类型保持一致。

### 2. `InteractionEventRepository`（`Create`/`ListByUser`）

真实 PostgreSQL 实现，这张表第一次有代码真正往里写数据。

### 3. `ws.Hub` 接入 `EventRecorder`（可选注入，nil-safe）

- `handleJoinRoom`：只在**首次**加入（`!alreadyJoined`）记一条事件，重复
  join（前端重连/切页面）不产生噪音信号，与 T32 幂等口径保持一致。
- `handleSendMessage`：只在消息**真正落库+广播成功**之后才记，内容校验
  失败/限流/敏感词拦截/重复消息（msgId 已存在）等分支均不记——这些分支
  没有产生真实的"发消息"行为，训练数据不应该混入噪音。
- `handleSendDirectMessage`（私聊）本轮未接入，保持原样，避免范围扩大。

### 4. `service.FriendService` 接入 `EventRecorder`（可选注入，nil-safe）

- `SendRequest`：只在好友请求**真正创建成功**（`inserted=true`）之后才记，
  自己加自己/对方不存在/已存在请求/并发竞态命中等分支均不记。

两处均采用与 `PushNotifier`/`SetPushNotifier` 完全一致的可选注入模式
（`SetEventRecorder`，不改变已有构造函数签名，不影响任何既有调用点/单测）。

### 5. `cmd/export_training_data`

独立的一次性运行工具，输入 `EXPORT_USERNAME`，直连数据库读取
`UserRepository.FindByUsername` + `WatchTopicRepository.ListByUser` +
`InteractionEventRepository.ListByUser`，组装成：

```json
{
  "format_version": "v1-minimal",
  "generated_at": "...",
  "user": {...},
  "watch_topics": [...],
  "interaction_events": [...]
}
```

`format_version` 显式标注格式版本号——训练数据管道的格式一旦被下游消费，
后续变更需要走版本兼容策略，从第一版就带上版本号是基本的工程习惯。这层
"导出结构体"与 `model` 包的内部字段命名故意解耦（如 `model.WatchTopic.
RoomID` 是 `string` 空值表示"全局关注"，导出层用 `omitempty` 更明确地
表达"这个字段是否存在"），这正是"数据库表结构不等于训练数据格式，中间需要
一层显式转换"这个问题本身要回答的部分。

## 三、真实运行结果（非模拟）

重建 `server` 容器后，先运行 `cmd/bot`（触发 join_room + send_message）：

```
$ go run ./cmd/bot
...
消息已发送并收到服务端广播确认，msg_id=fa649840-...
```

导出机器人账号数据：

```json
{
  "format_version": "v1-minimal",
  "user": {"username": "ai_zhidai_bot", "is_bot": true, ...},
  "watch_topics": [],
  "interaction_events": [
    {"event_type": "send_message", "room_id": "d606e1b4-...",
     "payload": {"msg_id": "fa649840-...", "content_type": "text"}, ...},
    {"event_type": "join_room", "room_id": "d606e1b4-...", ...}
  ]
}
```

再注册两个全新真人账号验证 `add_friend_request`（用全新账号是为了避开此前
已存在的好友请求导致的幂等跳过，确保这是一次"真正创建成功"的事件）：

```json
{
  "user": {"username": "evt_test_d_...", "is_bot": false, ...},
  "interaction_events": [
    {"event_type": "add_friend_request",
     "target_user_id": "fda98351-...", ...}
  ]
}
```

三种事件类型（`join_room`/`send_message`/`add_friend_request`）均已在真实
运行环境下验证：数据库真实写入 → 导出工具真实读取 → 格式化为结构化 JSON。

## 四、单测覆盖

- `internal/repository`：`TestInteractionEventRepository_CreateAndListByUser`、
  `TestInteractionEventRepository_Create_WithTargetUserID`、
  `TestInteractionEventRepository_ListByUser_OrderedByCreatedAtDesc`。
- `internal/ws`：`TestHub_HandleJoinRoom_RecordsInteractionEvent`（含重复
  join 去重验证）、`TestHub_HandleSendMessage_RecordsInteractionEvent`、
  `TestHub_HandleSendMessage_BlockedContent_DoesNotRecordEvent`（拦截路径
  不产生事件）、`TestHub_EventRecorder_Nil_DoesNotPanic`（可选注入边界）。
- `internal/service`：`TestFriendService_SendRequest_RecordsInteractionEvent`、
  `TestFriendService_SendRequest_DuplicatePending_DoesNotRecordEvent`。
- `cmd/export_training_data` 本身（0% 覆盖率）未写单测——与 `cmd/bot`/
  `cmd/server` 同款判断：薄编排层，核心逻辑均已在被调用的仓储层充分覆盖，
  正确性由本次真机运行结果验证。

## 五、验证汇总

- `gofmt -l .`：无输出；`go vet ./...`：通过；`golangci-lint run ./...`：全绿。
- `go test ./... -cover`：全部包通过（`internal/ws` 61.1%→61.2%，
  `internal/service` 67.4%→67.6%，`internal/repository` 27.2%→30.4%）。
- 真机运行：`cmd/bot` 触发事件 → `cmd/export_training_data` 导出验证，
  覆盖全部 3 种事件类型；新注册两个全新账号补充验证 `add_friend_request`。
- 重建容器后 `scripts/integration_test.py`（54项）全部通过，确认无回归。

## 六、如实标注的局限（回应原始问题的完整边界）

这份工作把"完全没有任何行为数据管道"变成"有基础事件流 + 一条可复现的
导出链路"，但距离"真正能直接投喂给模型训练用户替身"仍有明确差距：

1. **历史数据依然缺失**：此前上线到现在积累的全部真实用户行为（在本轮
   之前发生的每一次 join/发消息/加好友）都没有被记录，永久无法回溯补采——
   这正是 Plan 原始设计里强调"必须从第一天开始记录"的原因，现在只是让
   "从今天起不再继续错过"成立，历史窟窿补不回来。
2. **`user_watch_topics.keywords` 仍是非结构化自由文本**：要用于训练前的
   特征提取，还需要额外的解析/分词/归一化步骤，导出的 `keywords` 字段目前
   原样传递，不是"可直接向量化"的格式。
3. **没有数据使用授权机制**：项目在★13/★14已经认真讨论过"机器人代理行为
   需要用户opt-in"，但**没有对等的"我的数据是否可用于训练我的替身"字段**，
   这是与★13/★14同一套伦理框架下本该存在但缺失的姐妹决策。
4. **没有周期性画像聚合**：现在导出的是原始事件流水账，没有任何地方把这些
   事件汇总成"这个用户过去N天呈现出的兴趣/行为特征"这类更适合直接喂给模型
   的中间表示——导出工具只证明了"格式上能取出结构化数据"，不等于"已经是
   训练就绪的特征"。
5. **账号删除仍是级联硬删除**：对隐私是对的默认行为，但意味着如果未来真的
   要建训练数据集，需要在用户主动同意的前提下单独设计"删除前归档"流程，
   现在完全没有这一步——`ON DELETE CASCADE` 会让这些事件随账号删除一起
   永久消失，不会进入任何独立于活跃账号的训练语料库。
6. **`messages.is_blocked`/`bot_action_log`（此前已在能力补齐4中真正写入）
   之外，`interaction_events` 只接入了 3 类最基础的事件**，`view_profile`/
   `long_dwell`（长时间停留）等更细粒度的信号仍未接入，因为当前代码路径里
   根本不存在能自然产生这些事件的功能触点（"查看资料"这个功能目前不存在，
   "长时间停留"需要额外的前端埋点，均超出本轮"最小验证"范围）。
