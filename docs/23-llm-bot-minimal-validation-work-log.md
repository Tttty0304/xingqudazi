# LLM 驱动机器人最小验证（工作日志，2026-08-19）

> 背景：项目设计之初（`docs/00-brainstorm-and-plan.md` ★13/★14 + Part3「AI-native
> 二期扩展设计」）就认真讨论过"训练替身机器人代替用户进行前期社交"的远期方向，
> 包括机器人透明度披露（★13：强制标识）、opt-in 机制（★14：默认关闭）等产品
> 伦理决策。但落地时只做了 schema 预留（`users.is_bot`/`proxy_for_user_id`/
> `messages.sender_type`/`bot_action_log` 等），业务代码从未真正读写过这些字段，
> 也从未产生过一条真实的机器人消息——这个设计此前只停留在"文档里认真讨论过"
> 的程度，用户明确要求补一个最小的、真实可运行的验证闭环。

## 一、范围（用户明确限定为"简单"）

1. 一个标记为 `is_bot=true` 的机器人账号加入一个房间；
2. 用真实调用通义千问（Qwen，DashScope 兼容 OpenAI 协议）LLM API 生成的内容，
   作为该机器人在房间里发的消息——这是"用 LLM 驱动机器人行为"的落地点；
3. 机器人向两个已注册的真实用户（`moonteng`、`lili`）发起好友请求。

**明确不做**（超出"最小验证"范围，与 Plan 原有的 P2 排除原则一致）：机器人
自主决策/多轮长期社交、真实 embedding/语义相似度匹配、机器人"察觉深度互动"
后自动触发推荐、批量/多机器人调度、运营管理后台。

## 二、安全处理：真实 API Key 不入库/不入文档

用户在对话中直接给出了真实的 DashScope API Key。处理原则：

- 真实 Key 只写入本地 `server/.env`（已被 `.gitignore` 覆盖的既有规则
  `server/.env` 命中，与项目此前的密钥管理方式一致）；
- `.env.example` 只保留变量名（`LLM_API_KEY`/`DASHSCOPE_API_KEY` 等），值留空；
- 本文档、README、testcase 等任何会被长期保留/展示的文件，均只引用环境变量
  名，不包含真实 Key 明文。

## 三、改动清单

### 1. `model.User` 补齐 `IsBot` 字段的真实读写

此前 `model.User` 结构体故意不包含 `is_bot`/`proxy_for_user_id`（注释写着
"避免引入未使用的死代码"）。本轮新增 `IsBot bool` 字段，`ProxyForUserID`
仍保持未接入（本次验证的机器人是独立身份，不代理任何具体用户，避免超出
最小验证范围）。

### 2. `UserRepository` 新增 `SetIsBot`/`IsBot`

- `SetIsBot(ctx, userID, isBot bool) error`：显式标记/取消标记机器人身份。
  **故意不通过任何公开 HTTP 接口暴露**——机器人身份是一个需要被信任的标签
  （据此决定消息 `sender_type`，对应 ★13 强制披露机制），若任意已登录用户
  能自我标记为机器人，等同于给了每个用户一个绕过透明度要求的开关。只保留
  为内部工具（`cmd/bot` 启动时直连数据库调用）可用的仓储方法。
- `IsBot(ctx, userID) (bool, error)`：供 WS 握手鉴权查询，结构化匹配
  `ws.BotChecker` 接口（依赖倒置：谁使用就由谁定义接口，与项目一贯风格
  一致）。
- `scanUser`/`FindByUsername`/`FindByID`/`FindByIDs` 的 SQL 与 `Scan` 目标同步
  补充 `is_bot` 列。

### 3. `bot_action_log` 首次真正被写入

新增 `model.BotActionLog` + `repository.BotActionLogRepository`
（`Create`/`ListByBotUser`）。这张表从 `migrations/0001_init_schema.up.sql`
建表起就存在，此前的注释写着"本次不产生机器人消息，故暂不写入，仅预留表
结构"——本轮首次让它承载真实数据：机器人每次由 LLM 驱动发出一条消息后，
落一条决策记录（含 LLM 使用的模型名与 token 统计），兼顾未来训练数据/可
解释性需求。

### 4. WS 层：`sender_type` 由服务端权威判定，不信任客户端

- `ws.Client` 新增 `isBot bool` 字段：握手成功后由服务端查询账号身份写入，
  此后只读不写，不存在客户端在消息里自称机器人的协议路径（`ClientMessage`
  结构体本身也没有这个字段）。
- `ws.Handler` 新增 `BotChecker` 接口（可为 nil，未注入时行为不变），
  `ServeWS` 在 Upgrade 成功后立即查一次，查询失败时按非机器人处理
  （fail-safe：宁可让机器人消息误显示为 human，也不因一次查询抖动拒绝
  整个连接升级）。
- `Hub.handleSendMessage`：`senderType` 从硬编码 `"human"` 改为
  `if c.isBot { "bot" } else { "human" }`，同步应用到落库的 `model.Message`
  与广播的 `ServerMessage`。`handleSendDirectMessage`（私聊）保持不变——
  本次验证范围不包含机器人私聊，不做超出范围的改动。

### 5. 新增 `pkg/llm`：Qwen（DashScope）客户端

`llm.Client` 接口 + `llm.QwenClient` 实现，通过 DashScope 的 OpenAI 兼容模式
端点（`{baseURL}/chat/completions`）调用，用标准库 `net/http` 直接实现协议，
不引入官方 SDK 依赖——与本项目 Web Push（`pkg/webpush`）"自己理解并实现协议"
的一贯风格保持一致，且 OpenAI 兼容协议本身足够简单。返回内容与 usage 统计
摘要（`prompt_tokens`/`completion_tokens`/`total_tokens`），供上层打印/留痕，
证明这条内容确实来自一次真实的网络调用而非硬编码模板。

### 6. 新增独立命令 `server/cmd/bot`

一个一次性运行的验证工具（非常驻服务），完整流程：

1. 登录优先，失败则注册（幂等，多次运行复用同一机器人账号）；
2. 直连数据库调用 `SetIsBot(true)`；
3. `GET /api/rooms` 选定一个房间；
4. 调用 Qwen LLM 生成一条开场白（system prompt 显式要求"以机器人身份发言，
   符合透明度披露要求"）；
5. 真实建立 WS 连接、`join_room`、`send_message`，并等待服务端的
   `message_received` 广播回执确认消息确实被处理（不是"发出去就假设成功"）；
6. 写一条 `bot_action_log`；
7. 依次对 `BOT_TARGET_USERNAMES`（默认 `moonteng,lili`）先 `GET
   /api/users/lookup` 查 ID，再 `POST /api/friends/requests` 发起好友请求
   （409 已存在视为"目标状态已达成"，不算失败，支持重复运行）。

复用 `internal/ws` 包导出的 `ClientMessage`/`ServerMessage`/事件常量构造 WS
协议消息，而非手写重复的 JSON 结构体，保证协议一致性由编译器保证而非约定。

### 7. 配置项

`config.Config` 新增 `LLMProvider`/`LLMAPIKey`/`LLMBaseURL`/`LLMModel`；新增
`getEnvFirst` 辅助函数支持 `LLM_API_KEY`/`DASHSCOPE_API_KEY` 两种环境变量名
兼容读取（后者是通义千问/百炼平台的习惯命名）。核心 `server` 进程本身不
依赖 LLM，未配置不影响主服务启动，只有 `cmd/bot` 会在缺失时提前退出。

## 四、真实运行结果（非模拟）

```
$ cd server && set -a && source .env && set +a && go run ./cmd/bot

机器人账号已就绪：username=ai_zhidai_bot user_id=17544430-...
已将账号 ... 标记为机器人身份（is_bot=true）
已选定房间：数码（room_id=d606e1b4-...，话题：数码产品与科技话题）
LLM(qwen-plus) 生成内容: "（机器人）哈喽～欢迎来数码天地！一起聊聊新机测评、黑科技和使用小技巧吧～"
LLM 调用统计: model=qwen-plus prompt_tokens=130 completion_tokens=25 total_tokens=155
WS 已连接，落在实例：server1
消息已发送并收到服务端广播确认，msg_id=253b1e28-...
已写入 bot_action_log 决策记录
已向 moonteng（user_id=f91af880-...）发起好友请求
已向 lili（user_id=1bd9d295-...）发起好友请求
机器人最小验证流程执行完毕。
```

一个有意思的细节：LLM 在生成内容时自主在文本开头加上了"（机器人）"字样——
说明它确实理解并遵循了 system prompt 里"以机器人身份发言，符合透明度披露
要求"的指示，这是意外但正向的验证信号（不代表前端展示依赖这一点，前端的
"（机器人）"标识完全来自 `sender_type` 字段，与消息文本内容无关，双重保障）。

### 数据库真实落地验证

```sql
SELECT id, username, is_bot FROM users WHERE username='ai_zhidai_bot';
--  17544430-... | ai_zhidai_bot | t

SELECT sender_id, sender_type, content FROM messages WHERE sender_type='bot';
--  17544430-... | bot | （机器人）哈喽～欢迎来数码天地！...

SELECT bot_user_id, room_id, decision_reason FROM bot_action_log;
--  17544430-... | d606e1b4-... | LLM(qwen-plus)生成开场白并发送：...

SELECT requester_id, target_id, status FROM friendships WHERE requester_id='17544430-...';
--  -> f91af880-...（moonteng） | pending
--  -> 1bd9d295-...（lili）     | pending
```

### 前端真机验证（`playwright-cli`）

访客登录进入"数码"房间，快照确认发言人正确展示为：

```
ai_zhidai_bot（机器人）
（机器人）哈喽～欢迎来数码天地！一起聊聊新机测评、黑科技和使用小技巧吧～
```

`RoomChatPage.tsx` 里"若 `senderType==='bot'` 则追加显示'（机器人）'"这段
代码此前被标注为"机器人当前实际未启用，属防御性处理"——本轮是这段代码第一次
被真实的机器人消息触发，而不是死代码。

### 真机验证过程中顺带发现并修复的一个基础设施问题

重建 `server` 容器后，`client` 容器的 nginx 反代出现 502（`client` 启动时
已解析并缓存了旧的 `server` 容器 IP，容器重建后 IP 变化但 nginx 未重新解析）。
这与本轮 LLM 机器人改动本身无关，属于"重建单个后端容器后前端反代需要一并
重启"的既有基础设施行为，重启 `client` 容器即恢复正常，不需要代码改动。

## 五、单测覆盖

- `internal/repository`：`TestUserRepository_SetIsBot_And_IsBot`、
  `TestUserRepository_SetIsBot_UnknownUser_ReturnsNotFound`、
  `TestUserRepository_IsBot_UnknownUser_ReturnsFalseNoError`、
  `TestBotActionLogRepository_CreateAndListByBotUser`、
  `TestBotActionLogRepository_Create_WithRoomID`（新增 `createTestRoom`
  测试夹具，供需要真实房间外键的测试复用）。
- `internal/ws`：`TestHub_HandleSendMessage_BotSenderTypeBroadcast`（机器人
  发消息广播/落库 `sender_type=bot`）、
  `TestHub_HandleSendMessage_HumanSenderTypeUnaffected`（对照组，确认新增
  分支未影响既有人类发言路径）。
- `internal/config`：`TestGetEnvFirst_TriesKeysInOrder`、
  `TestLoad_LLMDefaults`、`TestLoad_LLMAPIKey_FallsBackToDashScopeKey`。
- `pkg/llm`：`TestQwenClient_GenerateBotMessage_Success`（用
  `httptest.Server` 验证请求体构造正确、正确解析响应）、
  `TestQwenClient_GenerateBotMessage_EmptyAPIKey`、
  `TestQwenClient_GenerateBotMessage_APIErrorResponse`、
  `TestQwenClient_GenerateBotMessage_NoChoices`、
  `TestNewQwenClient_DefaultsWhenEmpty`。

`cmd/bot` 本身（0% 覆盖率）未写单测——它是一个薄的编排层（登录/查库/调
LLM/建WS连接/发HTTP请求），核心逆用逻辑均已在被调用的仓储/WS/LLM 客户端层
充分覆盖，`cmd/bot` 的正确性由本次真机运行结果验证，与 `cmd/server`（同样
0% 覆盖率，理由一致）的判断保持一致。

## 六、验证汇总

- `gofmt -l .`：无输出；`go vet ./...`：通过；`golangci-lint run ./...`：全绿。
- `go test ./... -cover`：全部包通过（`internal/repository` 22.8%→27.2%，
  `pkg/llm` 新增 82.4%，其余包不变）。
- 真机运行 `cmd/bot`：完整链路成功，数据库+前端双重验证。
- 重建容器后 `scripts/integration_test.py`（54项）全部通过，确认本轮改动
  对现有功能无回归。

## 七、如实标注的局限

- 机器人是"一次性运行的独立工具"，不是常驻服务——不会自主决定"什么时候该
  发言""该不该继续社交"，每次运行都是一次确定性的、由人手动触发的流程。
  这与 Plan 原始设想的"机器人自主在房间内进行前期社交"仍有本质差距，本轮
  只是把"从0到1完全没跑通"变成"有一条可复现的最小闭环"。
- `proxy_for_user_id`（机器人代理哪个真人用户）仍未接入任何业务逻辑。
- `interaction_events`（行为事件日志）与 `messages.embedding`/
  `interest_embedding`（向量语义）仍是纯 schema 预留，未参与本轮验证。
- 好友请求的目标用户名（`moonteng`/`lili`）是通过环境变量指定的固定值，
  不是机器人基于某种"判断"自主选择的对象——这一步骤本质上仍是确定性脚本
  行为，不构成"机器人自主决策"。
