# 能力补齐 Work Log：WS自动重连 / 登录暴力破解防护 / 前端自动化测试

> 日期：2026-08-18
> 背景：用户报告"注册纯数字用户名无正确提示"的缺陷后，进一步结合课题 9 个考察方向做了
> 一次全面差距盘点（见对话记录），识别出优先级最高的 3 项真实缺口并按优先级补齐：
> 1. WS 断线无自动重连（最影响真实使用体验）
> 2. 登录接口无暴力破解防护（安全类缺口，实现成本低）
> 3. 前端零自动化测试（长期维护风险最大）

## 一、缺陷修复：T140 密码长度提示不一致

用户反馈"注册账户时并不支持纯数字输入，但是没有正确提示"。复现后确认**纯数字用户名
本身从未受限**（`curl` 直测 `123` 三位纯数字用户名注册返回 `201`）。真实根因：
`AuthPage.tsx` 密码输入框占位文案与 `client.ts` 的 `invalid_password` 错误文案都写
"至少6位"，但后端 `minPasswordLength` 实际是 **8**。用户很可能顺手用了 6-7 位纯数字
密码测试，被后端拒绝，前端错误提示却还在重复"至少6位"——等于告诉用户"你的输入已经
符合要求"，是自相矛盾的提示，跟用户名是否纯数字无关。

修复：
- 统一文案为"至少8位"（占位符 + 错误提示）
- 注册模式下密码框加 `minLength={8}` 触发浏览器原生前置校验，不满足直接拦截
- 用户名输入框下新增提示"3-32 位，支持字母、数字、下划线（纯数字也可以）"，消除疑虑

已用 `playwright-cli` 真实浏览器复现问题（6位密码报错文案自相矛盾）→ 修复 → 复验通过。

## 二、能力补齐 1：WS 断线自动重连（T141）

### 问题

`SocketContext`（应用级共享 WS 连接）此前断线（`onclose`）后直接把 `status` 设为
`closed`，不会自己恢复——用户网络抖动一下、或后端容器重启一次，就必须手动刷新页面
才能继续收发消息。这是目前最影响真实使用体验的缺口。

### 改动

- `ConnectionStatus` 新增 `'reconnecting'` 状态。
- 非主动关闭（区分"组件卸载/effect重跑导致的主动关闭"与"网络抖动/服务端重启导致的
  意外断连"，用 `intentionalClose` 标志位区分）触发后，用指数退避（1s→2s→4s→8s→封顶
  10s）自动重建连接。
- 重连成功后**不需要**任何显式的"重放"逻辑：
  - `listenersRef`（`subscribe` 注册的监听器）与具体连接对象解耦，重连后依然有效。
  - 各页面自己对 `status` 的 `useEffect` 依赖（如 `RoomChatPage` 里"WS连接建立后加入
    房间"）会随 `status` 变为 `open` 自动重新执行，天然完成"重连后自动重新加入房间"。
- `RoomChatPage.tsx`/`DirectChatPage.tsx`：状态文案补充 `reconnecting` 分支
  （"连接已断开，正在自动重连…"），输入框在此状态下继续禁用。

### 验证

**单测**（`SocketContext.test.tsx`，用假 WebSocket + `vi.useFakeTimers()`）：
1. 非主动断线后 1s 触发第一次重连尝试
2. 连续断线时退避延迟指数增长（1s→2s）
3. 组件卸载（主动关闭）时不会触发自动重连

**真机验证**（`playwright-cli` + 真实 Docker 容器）：
1. 访客登录进入房间聊天页，确认 `已连接 · N 人在线`
2. `docker stop deploy-server-1`：页面**未手动刷新**，自动变为
   "连接已断开，正在自动重连…"，输入框同步禁用（📷/发送按钮/文本框全部 disabled）
3. `docker start deploy-server-1`，等待健康检查通过后，页面**未手动刷新**自动恢复为
   "已连接 · 2 人在线"
4. 恢复后发送一条消息成功（证明重连后的连接是真实可用的，不是仅 UI 状态假显示）

## 三、能力补齐 2：登录接口暴力破解防护（T142）

### 问题

`/api/auth/login` 只做用户名密码校验，没有任何频率限制——WS 消息发送在 Task6 就有
双层限流，登录接口反而完全没有，攻击者可以对同一账号无限次尝试猜密码，是真实的安全
缺口。

### 改动

- 新增 `server/internal/middleware/login_rate_limit.go`：`LoginRateLimiter` 中间件，
  按客户端 IP 做 Redis 固定窗口限流（`INCR` + 首次命中 `EXPIRE 60s`），超过阈值
  （默认 `LOGIN_RATE_LIMIT_PER_MINUTE=10`）直接返回 `429 {"error":"login_rate_limited"}`，
  不再进入真实的用户名密码校验逻辑。
- 用 Redis 而非进程内 map 计数：与项目既有架构保持一致（Redis 已是 ★3 强制多实例
  横向扩展设计的一部分），进程内计数器在多实例部署下形同虚设。
- **失败开放**（fail-open）：Redis 出错时放行请求但记录错误日志，避免限流器自身
  故障导致核心登录功能不可用（Redis 已是 `/readyz` 依赖项，真实故障时整体已
  not-ready，这里只是不让限流器成为额外的单点放大器）。
- 只挂载在 `/api/auth/login`，不影响 `/register`、`/guest`——批量注册测试账号/
  访客模式是当前项目正常使用场景，限流针对的是"对同一账号高频猜密码"这一具体风险。

### 验证

`curl` 连续发起 15 次错误登录请求：前 10 次 `401`（凭证错误），第 11 次起 `429`，
响应体 `{"error":"login_rate_limited"}` 与预期一致。`scripts/integration_test.py`
追加了对应断言（放在脚本**最后一步**，避免影响脚本前面对 `/login` 的正常断言），
连续两次完整跑通（46 项全部通过）。

## 四、能力补齐 3：前端自动化测试基础设施（T143）

### 问题

前端此前零测试资产——`playwright-cli` 驱动的多用户联动验证虽然真实、有效，但都是
"一次性会话"，验证完即结束，不能像后端 `go test ./...` 一样在每次改动后快速重跑做
回归检查，长期维护风险最大。

### 改动

- 引入 `vitest` + `@testing-library/react` + `@testing-library/jest-dom` +
  `@testing-library/user-event` + `jsdom`，新增独立的 `vitest.config.ts`（不与
  `vite.config.ts` 生产构建配置耦合）与 `src/test/setup.ts`（jest-dom 断言扩展 +
  统一 `afterEach(cleanup)`）。
- `package.json` 新增 `test`/`test:watch` 脚本。
- 新增 3 个测试文件，共 15 项断言：
  - `api/client.test.ts`（9项）：`translateCode`/`errorMessage`/`resolveMediaUrl`/
    `resolveWsUrl`/`serverOrigin` 纯函数行为。
  - `pages/AuthPage.test.tsx`（3项）：**直接固化 T140 缺陷修复的回归测试**——
    注册模式密码框 `minLength=8`、登录模式不限制、纯数字用户名可正常输入。
  - `context/SocketContext.test.tsx`（3项）：**固化 T141 重连逻辑**——见上节。

### 踩坑记录

写 `AuthPage.test.tsx` 时最初想用 `checkValidity()`/`.validity.tooShort` 断言
"密码不足8位时浏览器会拦截"，用 `fireEvent.change` 和 `userEvent.type` 都失败。
排查后发现 **jsdom 明确将 `tooShort` 硬编码为 `() => false`**（见
`node_modules/jsdom/lib/jsdom/living/nodes/HTMLInputElement-impl.js` 源码注释：
"jsdom has no way at the moment to emulate a user interaction, so tooLong/tooShort
have to be set to false"）——这是 jsdom 的已知限制，不是应用代码或测试代码的问题。
改为直接断言 `passwordInput.minLength === 8` 这个 DOM 属性值本身，同样能覆盖
"注册模式要求8位、登录模式不做限制"的真实行为，且不依赖 jsdom 未实现的能力。

### 验证

`npm run test`：15/15 通过；`npm run build`（`tsc -b && vite build`）确认测试文件
不影响生产构建产物与类型检查；`npm run lint`（oxlint）确认无新增 lint 问题（仍是此前
已知的 2 条 fast-refresh 最佳实践提示，与本次改动无关）。

## 五、验证汇总

- 后端：`gofmt -l .` 无输出、`go vet ./...` 无输出、`go test ./... -v` 全部 PASS、
  `golangci-lint run ./...` 退出码 0
- 前端：`npm run test` 15/15 通过、`npm run build` 成功、`npm run lint` 无新增问题
- 真实 Docker 环境重建（server+client）后 `docker compose ps`：全部 healthy
- `scripts/integration_test.py`：46 passed, 0 failed（含新增的登录限流断言）
- 真机验证：WS 断线自动重连（`docker stop`/`start` 服务端容器模拟真实断线）、
  登录暴力破解限流（`curl` 连续请求触发 429）均已用真实环境验证生效

至此，此前差距分析中识别出的 3 项高优先级缺口（WS自动重连/登录限流/前端测试）均已
补齐并验证通过。

## 六、能力补齐 4：房间聊天页点击发言人用户名加好友（用户体验反馈驱动，同日追加）

### 问题

群聊本身不要求好友关系即可发言（产品设计上的合理选择：陌生人也能先聊起来），但这
意味着"认识一个聊得来的人后加好友"的路径此前是：记住/复制对方用户名 -> 离开聊天室
-> 打开好友页 -> 手动输入用户名查找 -> 发起请求。链路又长又容易半路放弃，属于社交类
产品核心转化路径上真实存在的体验缺口。

### 工作量评估

不涉及任何后端/数据库改动——后端能力早在 Task14 就完整存在（`POST /api/friends/
requests` 支持直接传 `target_user_id`，`GET /api/friends`、`GET /api/friends/
requests` 也都已具备）。纯前端新增一个小组件 + 交互逻辑，与此前"AI推荐页确认后一键
加好友"是同一量级的工作。

### 改动

- 新增 `client/src/components/UserActionPopover.tsx` + `.css`：轻量气泡卡片组件，
  点击外部区域（`mousedown` 监听）或 `Esc` 自动关闭，视口边界钳制避免溢出屏幕。
- `RoomChatPage.tsx`：
  - 非"我自己"且非机器人（`senderType !== 'bot'`，机器人当前实际未启用，属防御性
    处理）的发言人用户名从纯文本改为可点击 `<button>`（保留可访问性/键盘导航）。
  - 点击时并行请求 `GET /api/friends` + `GET /api/friends/requests`，据此判定
    关系态：已是好友 / 已发出待处理请求 / 对方已发来待处理请求 / 可发起请求，
    分别展示对应文案或"加为好友"按钮。
  - 用自增 `popoverRequestId` token 防止"快速切换点击不同发言人"时，前一次未完成
    的异步查询以过期结果覆盖后一次已打开的气泡（经典竞态问题的轻量处理，请求量小
    没必要引入 `AbortController`）。
  - 新消息到达（`message_received`）或切换房间（`roomId` 变化）时自动关闭气泡，
    避免气泡引用的发言人状态过期或停留在错误位置。

### 验证

`testcase/00-testcase-plan.md` C1-C8，双浏览器会话（alice/bob）真机联动验证：
点击非好友用户名弹出气泡（C1）、自己的消息不可点击（C2）、已是好友展示纯文案无按钮
（C3）、点击加好友后气泡内即时反馈"已发出好友请求，等待对方处理"（C4）、点击外部区域
自动关闭（C6）、收到新消息自动关闭（C7）、alice 发起 -> bob 好友页收到并接受的完整
闭环（C8）全部通过。前端 `npm run build`/`npm run lint` 通过，无新增问题；后端
`scripts/integration_test.py` 46 项无回归。

## 七、能力补齐 5-13：结合课题9个方向的诚实差距盘点后的批量修复（2026-08-19）

### 背景

用户要求逐条对照课题列出的9个考察方向，结合真实代码给出诚实评估（区分"已确认的
设计边界简化"与"未被意识到/未做的真实缺口"）。评估后识别出以下真实缺口并逐一修复：

### G1 系统安全性：密码复杂度校验

- `service/validation.go`：`ValidatePassword` 从"只检查长度≥8"升级为"长度≥8 且
  同时包含字母与数字"，不要求特殊符号（在防弱密码与不过度提高注册门槛之间取平衡，
  与本项目 demo/评估场景定位一致）。
- 前端 `AuthPage.tsx` 密码占位文案同步更新为"至少8位，需同时包含字母和数字"。
- 验证：`TestValidatePassword` 新增4个用例；`integration_test.py` 新增3项断言。

### G2 系统安全性：CORS 白名单可配置

- `middleware/cors.go` 从硬编码 `Access-Control-Allow-Origin: *` 改为支持
  `CORS_ALLOWED_ORIGINS`（逗号分隔）配置白名单，命中才反射该 Origin；默认值仍是
  `*`（保持 demo/评估项目默认行为不变，生产部署应配置为真实域名）。
- `.env.example` 新增对应说明。

### G3 系统安全性：JWT 登出黑名单

此前"登出"只是前端清 `localStorage`，服务端在 token 自然过期（默认24小时）前
依然认可其合法性——如果 token 泄露，用户点"登出"实际上什么都没做。

- 新增 `repository.RedisTokenBlacklist`：登出时把 token 写入 Redis，TTL 设为该
  token 距离自然过期的剩余时长，到期自动清理，无需额外定时任务。
- `AuthService.Logout`：解析 token 拿到真实过期时间后调用黑名单 `Add`；token
  本身已不合法（过期/伪造）时静默跳过，不阻塞用户登出的本地体验。
- HTTP 鉴权中间件 `middleware.RequireAuth` 与 WS 握手鉴权 `ws.Handler.ServeWS`
  均在解析 token 成功后追加一次黑名单查询，两条鉴权路径行为保持一致；黑名单查询
  失败时**失败开放**（fail-open，与 `LoginRateLimiter` 策略一致，避免 Redis 抖动
  导致全部已登录用户被误判登出）。
- 新增 `POST /api/auth/logout` 接口（需携带当前仍合法的 token），前端
  `AuthContext.logout()` 采用 fire-and-forget 方式调用（不阻塞本地登出体验）。
- 已知边界（如实标注）：只拦截"用已登出 token 发起新 WS 连接"，不会强制断开在
  登出前已经建立成功的活跃 WS 连接（未额外维护每连接的 token 过期定时器，收益
  相对复杂度不高——旧连接仍会随心跳超时/客户端主动断开自然结束）。
- 验证：`integration_test.py` 新增断言（登出前可访问 -> 登出 -> 登出后同一 token
  返回 `401 token_revoked`）；真机验证访客登出流程正常跳转登录页、控制台无异常。

### G4 服务可靠性：Web Push 发送失败重试

此前单条订阅发送失败（网络抖动/推送服务瞬时 5xx）直接放弃，不做任何重试。

- `PushService.sendWithRetry`：网络错误/5xx 最多重试到 3 次（含首次），退避
  `[0, 200ms, 500ms]`；4xx（含 404/410 订阅失效）语义明确，直接判定终态不重试，
  410 场景保留原有的"清理僵尸订阅"逻辑。
- 新增3个单测：410 清理订阅（既有回归）、前两次 503 后第三次成功、持续 503 最终
  放弃且尝试次数精确等于上限（用 `httptest.Server` + 原子计数器模拟真实网络交互，
  而非纯 mock 断言调用参数）。

### G5 测试与代码质量：repository 层补充真实数据库单测

此前诚实评估中发现 `internal/repository` 覆盖率为 0%——数据访问层的 SQL 正确性
完全没有单测覆盖，只能靠黑盒集成测试脚本间接兜底。

- 新增 `testdb_test.go`：提供连接真实 Postgres 的测试夹具（`TEST_POSTGRES_DSN`
  环境变量可配置，未设置时回落到 `docker-compose.yml` 默认地址；连接失败时
  `t.Skip` 而非失败，不阻塞没有起 Docker 环境的场景）。选择连真实数据库而非
  `sqlmock`：本项目大量 SQL 用了 Postgres 特有语法（`= ANY($1)`、级联删除等），
  mock 出的期望容易与真实执行行为脱节。
- `user_repository_test.go`（5项）：含 T122 缺陷修复的真实回归测试
  （`FindByIDs` 混入非 UUID 格式 ID 不应触发 500）。
- `watch_topic_repository_test.go`（2项）：含 T123 的所有权校验（非本人无法
  删除他人记录）、AI 推荐候选生成依赖的"已过期关注事项应被排除"逻辑。
- `token_blacklist_test.go`（2项，配套 G3 新增代码）：真实 Redis 往返验证。
- 全部9项均对真实运行中的 Docker 容器执行并通过。

### G6 性能与成本：首次真实压测

此前诚实评估明确指出"没有做过任何压测，1000并发下延迟多少完全未知"。

新增 `scripts/load_test.py`（标准库 `urllib`/`threading` 做 HTTP 压测零依赖，
`websockets`/`aiohttp` 做 WS 端到端往返延迟压测），如实标注**不是专业压测工具**
（非 wrk/k6 级别的精细负载模型），只是把"完全空白"变成"至少有一份真实数据"。

**实测数据**（本机 Docker Compose 单机部署环境，非独立压测机，仅供数量级参考）：

| 场景 | 并发 | 结果 |
|---|---|---|
| `HTTP GET /api/rooms` | 100并发，共500请求 | 100% 成功，avg=36.8ms / p50=32.9ms / p95=79.4ms / p99=99.5ms，吞吐 2486 req/s |
| WS 端到端消息往返（发送→收到自己的广播回执） | 100并发连接，共300条消息 | 100% 成功，avg=180.9ms / p50=169.7ms / p95=308.9ms / p99=346.5ms |

诚实说明局限性：未做长时间稳态压测、未监控服务端 CPU/内存曲线随并发的变化、
未做梯度加压寻找性能拐点、压测环境与生产部署环境不同（本机而非独立压测节点）。
这份数据的价值在于"从完全没有基准数据，变成有一个可复现的最小基准"，而非
"生产级 SLA 保证"。

### G7 测试与代码质量：CI 配置

此前测试/lint/构建全部依赖手动执行，没有"提交后自动跑一遍"的兜底机制。

新增 `.github/workflows/ci.yml`：后端流水线（`gofmt -l` 检查、`go vet`、
`golangci-lint`、`go test ./... -cover`，并起 Postgres+Redis service container
供 repository 层真实集成测试使用）+ 前端流水线（`tsc` 类型检查+构建、
`oxlint`、`vitest`）。**如实标注局限**：本项目未接入任何真实的远程 git 仓库
（`project-meta.yaml` 已确认），因此这份配置目前只是"资产本身"——尚未在真实
CI 环境实际触发跑过一次，不能等同于"已验证通过的 CI"；若推送到真实 GitHub
仓库可直接生效。

### G8 部署及运行方式：数据库备份/恢复策略

此前对"数据库备份/恢复"完全没有任何说明。

- 新增 `scripts/db_backup.sh` / `scripts/db_restore.sh` + 详细文档
  `docs/18-backup-and-restore.md`。
- **真实踩坑记录**：初版 `pg_dump` 未加 `--clean --if-exists`，在真机验证"备份
  -> 恢复到已有数据的同一数据库"时产生大量 `relation already exists` /
  `duplicate key` 报错——这正是恢复脚本最常见的真实使用场景（目标库通常已经
  跑过 migrations 初始化，而不是恢复到一个空库）。加上这两个参数后恢复变为
  幂等操作，重新验证通过。
- 真机验证：恢复前记录基准（`users`=365行，`rooms`=4行）-> 执行备份 -> 用刚
  生成的备份覆盖恢复同一数据库 -> 恢复后重新查询完全一致 -> `smoke_test.sh`
  确认服务无需重启即可正常对外提供服务。
- 已知限制（如实标注）：仅支持恢复到某次备份的时间点（无 PITR）；Redis 数据
  （在线态/限流计数器/登出黑名单）不在备份范围内——设计上这些数据本身可重建
  /有 TTL 自动过期，不属于需要持久化保护的业务数据。

### G9 产品功能与用户体验：未读消息精确数字

此前好友请求/私聊未读只有一个圆点提醒，看不出具体有多少条。

- `Layout.tsx`：
  - 好友请求计数：进入应用时先用 `GET /api/friends/requests` 拉取真实的
    "收到的待处理请求"数量作为基准（不是凭空从0开始），此后每收到一次
    `friend_request_received` WS 事件 `+1`。
  - 私聊未读计数：后端当前没有持久化的"已读游标"（`direct_messages` 表未记录
    阅读状态），因此**如实只统计"本次会话期间收到的新消息数"**，不包含历史
    累积的未读——避免打着"精确数字"的旗号展示一个实际没有数据支撑的假精确值。
    每收到一次他人发来的 `direct_message_received` 事件 `+1`，用 `sender_id
    !== userId` 过滤掉自己发消息时收到的送达确认回执（这是真实存在的边界，
    WS 广播会把消息也发回发送者本身）。
  - 访问对应页面时清零，与此前圆点提醒的语义一致。
- 真机验证（alice/bob/carol 三方联动）：好友请求计数从1累加到2 -> 访问好友页
  清零 -> 用 `goto` 触发整页刷新重新挂载组件后仍能从后端正确拉回真实基准值2
  （证明不是纯前端临时计数）；私聊消息计数从1累加到2 -> 访问私聊清零 -> 验证
  自己发消息不会误增自己的计数。

### 验证汇总

- 后端：`gofmt -l`/`go vet`/`golangci-lint run ./...` 全绿；`go test ./...`
  含新增的 repository 真实数据库测试与 push_service 重试测试全部通过。
- 前端：`npm run build`/`npm run lint`/`npm run test` 全部通过。
- 集成测试：`scripts/integration_test.py` 从 46 项扩充到 **54 项**，全部通过
  （新增密码复杂度3项 + 登出黑名单1项 + 沿用已有46项无回归，具体断言数以脚本
  实际输出为准）。
- 真机验证：`playwright-cli` 三用户联动验证未读精确数字（G9）+ 登出流程（G3）。
- 数据库备份/恢复：真实执行一轮完整"备份→恢复→数据校验→服务可用性"验证（G8）。
- 已重建全部 Docker 容器，最终状态即为本次改动后的运行状态。
