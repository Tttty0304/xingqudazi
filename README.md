# 兴趣搭子在线聊天室

基于不同兴趣主题的在线聊天室 Web 产品。用户可浏览不同兴趣的聊天室、进入房间与在线用户实时聊天，并支持好友、私聊等 IM 基础能力。

> **最终交付文档**（考核提交主文档：产品体验 + 整体架构 + 关键技术方案 + 后端工程质量
> 细节【优雅报错/数据校验/幂等重放/测试覆盖率】+ 云端部署 + 压测结论 + AI 协作方式，
> 一份文档看完全貌）：
> [`docs/29-final-delivery-document.md`](docs/29-final-delivery-document.md)。

> 需求扩写、开放问题确认与任务拆解详见 [`docs/00-brainstorm-and-plan.md`](docs/00-brainstorm-and-plan.md)；
> 接口级验收用例详见 [`testcase/00-testcase-plan.md`](testcase/00-testcase-plan.md)。
>
> **课题验收/提交交接文档**（运行说明 + 逐项对照9个考察方向的诚实评估 + 架构
> 摘要 + AI使用方式摘要，一份文档看完全貌）：
> [`docs/21-submission-handover.md`](docs/21-submission-handover.md)。
>
> **最终演示与答辩脚本**（可直接照读的演示流程 + 架构讲解口径 + 关键技术方案
> 串讲 + AI使用方式介绍 + 常见追问预案）：
> [`docs/22-final-demo-and-defense-script.md`](docs/22-final-demo-and-defense-script.md)。
>
> **当前运行状态与云端部署/压测计划**（明确当前尚无公网部署，含推荐规格、远程
> 演示、独立压测与跨PC迁移说明）：
> [`docs/25-current-status-and-cloud-deployment-plan.md`](docs/25-current-status-and-cloud-deployment-plan.md)。

## 当前进度

本仓库**全部计划内 Task（Task1-20）均已完成**并在真实 Docker 环境验证通过（含跨容器
多实例 Redis Pub/Sub 广播、端到端真实 Web Push 网络、真实浏览器多用户联动端到端验证，
见 `docs/05-handover.md`）：

- **Task1 项目骨架**：Go + Gin，已接线配置加载 / 结构化日志（含 trace_id）/ PostgreSQL(pgvector)
  连接池 / Redis 连接 / `/healthz` `/readyz` `/metrics` 三个基础端点 / 优雅关闭骨架；
  前端 Vite + React + TypeScript 响应式布局骨架；数据库一次性建齐 P0 表结构，见
  `migrations/0001_init_schema.up.sql`；`deploy/docker-compose.yml` 一键起 postgres(pgvector) +
  redis + server + client。
- **Task2 用户体系与鉴权**：注册/登录/访客模式、JWT 签发与校验、鉴权中间件。
- **Task3 房间管理**：房间列表（无需鉴权）、历史消息分页查询。
- **Task4 WS Gateway**：连接鉴权、join/leave 房间、心跳、**Redis Pub/Sub 多实例广播**（已在真实
  跨容器环境验证跨进程广播生效）、消息发送与落库、`msg_id` 幂等去重（已吸收 Task5 消息写入路径）。
- **Task6 可靠性**：WS 连接级发言限流（突发窗口+分钟配额双层）、Graceful Shutdown（SIGTERM 时向
  全部活跃 WS 连接发送真实 Close 帧，已用 `docker kill --signal=TERM` 真机验证）。
- **Task7 可观测性**：沿用 Task1 的 `/healthz` `/readyz` `/metrics` + trace_id 结构化日志，已核实达标。
- **Task8 系统安全性**：XSS 转义（群聊/私聊历史消息、会话列表预览三处输出边界）、基础 CORS 中间件、
  越权/未鉴权访问拦截（回归确认）。
- **Task14 好友关系链**：发起/接受/拒绝好友请求（对方实时收到 WS `friend_request_received`，跨实例
  推送复用 Task4 的 Redis Pub/Sub 扇出设计）、好友列表（`online` 字段基于 Redis 在线态引用计数实时
  计算，非静态假数据）、删除好友。
- **Task15 私聊**：WS `send_direct_message`（**仅好友可私聊**，已与用户确认口径，非好友被拒绝并返回
  `friend_required`）、会话惰性创建、`GET /api/conversations` 会话列表（含最近一条消息摘要）、
  `GET /api/conversations/{id}/messages` 历史消息查询（仅会话参与者可访问）。
- **Task16 多媒体消息（P0图片）**：`POST /api/media/upload` 图片上传（类型/大小校验，本地磁盘存储
  + 静态路由访问）、WS 消息扩展 `content_type` 支持图片消息（URL 不受 XSS 转义处理）。
- **Task18 内容安全**：敏感词过滤（命中即拦截，不落库不广播）、`POST /api/reports` 举报消息/私聊
  消息/用户（目标存在性校验 + 同举报人重复举报幂等）。
- **Task19 关注事项（P1）**：`POST/GET /api/watch-topics`，是 Task20 AI 推荐规则化匹配演示的
  直接输入源。
- **Task17 Web Push 离线通知**：纯 Go 实现 RFC8291（消息加密）+ RFC8292（VAPID）协议，
  好友请求/私聊消息目标离线时触发推送（在线用户仅走 WS，不重复推送）；**已用宿主机 mock
  推送服务验证容器内进程真实发起符合协议的加密 HTTP 请求**，非仅代码逻辑自证。
- **Task20 AI 推荐规则化匹配演示**：基于 Task19 关注事项关键词交集+共同房间打分生成候选
  （已是好友的用户对自动排除），`POST /api/recommendations/generate`+
  `GET /api/recommendations`+`PUT /api/recommendations/:id`（confirm/dismiss）。
- **Task9 前端页面（核心闭环）**：React Router 多页面应用——登录/注册/访客快速进入、
  房间列表、聊天页（历史消息+WS实时收发+图片上传，覆盖加载态/空态/错误态）。**已用
  `playwright-cli` 驱动真实浏览器完整走通注册→登录→进房间→发消息→发图片→离开→退出
  全链路**，过程中发现并修复了 3 个只有真实浏览器环境才能暴露的部署配置 bug（`/api`
  双重前缀、WS 端点 URL 拼接、`/uploads` 静态资源未被 nginx 代理），详见
  `docs/11-task9-work-log.md`。
- **Task9 续：好友/私聊/关注事项/AI推荐/Web Push 前端功能闭环**：WebSocket 连接提升为
  应用级共享连接（任意页面均可实时收到好友请求/私聊消息通知，不再局限于"必须停留在聊天页"）；
  新增好友页（按用户名添加、待处理请求收发双向展示、在线态、一键私聊）、私聊会话列表 + 聊天页
  （按对方用户ID路由，无需预先存在会话）、关注事项页（创建/删除）、AI推荐页（生成/确认/忽略，
  确认后可一键转化为好友请求形成完整闭环）、Web Push 订阅开关；同时补齐后端此前缺失的
  `GET /api/friends/requests`（好友请求可事后查看）、`GET /api/users/lookup`、
  `GET /api/users`（批量查用户名，修复群聊/私聊"用户ID前8位"占位展示问题）、
  `DELETE /api/watch-topics/:id` 四个接口。**已用 `playwright-cli` 驱动 3 个真实浏览器
  会话（alice/bob/carol）联动验证全部交互链路**，详见 `docs/12-frontend-closure-work-log.md`。
- **Task10 性能与成本落地**：补齐 `friendships`/`conversations`/`match_candidates`
  三张双向关系表缺失的反向索引（`migrations/0003_perf_indexes.up.sql`）、PostgreSQL
  连接池大小改为可配置（`POSTGRES_MAX_CONNS`/`POSTGRES_MIN_CONNS`）、nginx 为 Vite
  产物的 `/assets/` 静态资源加一年强缓存，镜像体积（server≈80MB/client≈76MB）与
  前端产物体积（JS gzip 后≈82KB）复核确认已属精简水平。
- **Task11 测试与代码质量**：新增 `server/.golangci.yml` 统一 lint 配置（`golangci-lint
  run ./...` 全绿，覆盖 errcheck/govet/staticcheck/unused/ineffassign/gofmt），修复
  其发现的真实问题（WS 心跳/关闭帧场景漏检查的 error 返回值、废弃的 `elliptic.Marshal`
  改为 `crypto/ecdh` 官方推荐路径、测试文件中的一处死代码字段）；新增
  `scripts/integration_test.py` 把此前分散在多轮对话里的临时验证脚本固化为仓库内
  可重复运行的黑盒接口集成测试（45 项断言，覆盖鉴权/房间/好友/私聊/关注事项/AI推荐/
  Web Push/未鉴权拦截的主路径与关键边界）。
- **Task12 部署与运行说明完善**：新增 `docs/13-architecture-and-adr.md`（架构图 +
  关键技术决策 ADR 汇总 + 已知简化点如实标注）；`.env.example`/`docker-compose.yml`
  补全此前遗漏的环境变量说明与 `media_uploads` 具名卷（避免容器重建后历史图片丢失）。
- **Task13 演示准备与 AI 使用说明**：新增 `scripts/demo_seed.py`（一键预置演示账号/
  好友关系/关注事项/AI推荐候选，幂等可重复运行）+ `docs/14-demo-guide.md`（分四部分、
  含常见问题预案的完整演示脚本）+ `docs/15-ai-usage-notes.md`（如实记录 AI 在本项目
  开发全过程中的具体工作方式与人工介入节点）。

至此 Plan 文档 Task0-Task20 全部完成。

### 能力补齐（2026-08-18，用户验收反馈驱动）

- **缺陷修复**：用户反馈"注册纯数字用户名无正确提示"，复现后确认真实根因是密码长度
  提示前后不一致（UI 写"至少6位"，后端实际要求8位），而非用户名限制；已修复文案并
  统一为"至少8位"，同步补充用户名规则说明。
- **WS 断线自动重连**：`SocketContext` 此前断线后需用户手动刷新页面才能恢复，现按
  指数退避（1s→2s→4s→8s→封顶10s）自动重建连接，已用真实容器停止/启动模拟断线场景
  验证生效（无需手动刷新）。
- **登录接口暴力破解防护**：`/api/auth/login` 新增按IP的固定窗口限流（默认
  10次/分钟，Redis实现，与项目多实例扇出架构一致），超限返回 `429`。
- **前端自动化测试基础设施**：引入 `vitest` + `@testing-library/react`，补齐此前
  零测试资产的缺口，新增 15 项单测（覆盖 API 客户端纯函数、上述缺陷修复的回归测试、
  WS重连逻辑）。
- **房间聊天页：点击发言人用户名直接加好友**：群聊本身不要求好友关系即可发言，此前
  "认识一个聊得来的人后加好友"的路径是"记住用户名 -> 离开聊天室 -> 好友页手动查找 ->
  发请求"，链路长、易放弃。新增 `UserActionPopover` 气泡组件（点击用户名弹出，自动
  判定当前关系态：已是好友/请求待处理/可发起），不涉及任何后端/数据库改动，纯复用
  既有的好友三件套接口。已用双浏览器会话（alice/bob）联动验证完整闭环，见
  `testcase/00-testcase-plan.md` C1-C8。

详见 [`docs/17-gap-closure-work-log.md`](docs/17-gap-closure-work-log.md)。

### 能力补齐（2026-08-19，结合课题9个考察方向的诚实差距盘点）

对照课题列出的产品/架构/实时通信/可靠性/可观测性/安全性/性能/测试/部署9个方向，
逐条对照真实代码做了一次诚实评估后，修复以下真实缺口（区分"已确认的设计边界"与
"未做到位"，评估细节与修复记录见 `docs/17-gap-closure-work-log.md` 第七节）：

- **系统安全性**：密码复杂度校验（须同时含字母数字，此前只查长度）；CORS 白名单
  可配置（`CORS_ALLOWED_ORIGINS`，默认行为不变）；**JWT 登出黑名单**——`POST
  /api/auth/logout` 后旧 token 立即失效（此前登出只是前端清 localStorage，服务端
  在自然过期前依然认可），HTTP 与 WS 两条鉴权路径均生效。
- **服务可靠性**：Web Push 发送失败重试（网络错误/5xx 最多重试3次，4xx 不重试）。
- **测试与代码质量**：repository 层补充9项真实数据库单测（此前覆盖率0%）；新增
  `.github/workflows/ci.yml`（后端lint+test+真实Postgres/Redis service，前端
  build+lint+vitest，如实标注尚未在真实CI环境跑过，因本项目无远程git仓库）；
  `scripts/integration_test.py` 扩充到 **54 项**。
- **性能与成本**：新增 `scripts/load_test.py` 首次获得真实并发数据（HTTP 100并发
  吞吐2486 req/s，WS端到端消息往返 p50≈170ms/p99≈347ms，如实标注非专业压测工具）。
- **部署及运行方式**：新增数据库备份/恢复脚本 `scripts/db_backup.sh`/`db_restore.sh`
  + `docs/18-backup-and-restore.md`，已真机验证"备份→覆盖恢复→数据一致"完整闭环
  （含真实踩坑修复：`pg_dump` 需 `--clean --if-exists` 才能幂等恢复）。
- **产品功能与用户体验**：好友请求/私聊未读从"一个圆点"升级为精确数字（好友请求数
  从后端拉取真实基准值，私聊未读如实标注为"本次会话期间收到的新消息数"而非声称
  覆盖历史累积未读）。

### 测试覆盖率大幅提升（2026-08-19，针对"测试与代码质量"方向的专项补齐）

此前诚实评估指出测试覆盖率"分布很不均衡"——`internal/api`（HTTP handler 层）
仅 0.3%、`internal/middleware` 为 0%、`internal/ws`（WebSocket 核心逻辑）仅
13.6%，基本靠黑盒集成测试脚本兜底。本轮新增 60+ 个单测用例后：

| 包 | 改动前 | 改动后 |
|---|---|---|
| `internal/api` | 0.3% | **75.5%** |
| `internal/middleware` | 0% | **94.7%** |
| `internal/ws` | 13.6% | **61.2%** |
| `internal/config` | 0% | **97.6%** |
| `internal/repository` | 0% | **30.4%** |
| `internal/service` | 67.4% | **67.6%** |
| `pkg/llm` | 新增 | **82.4%** |
| `pkg/metric` / `pkg/log` | 0% | **100%** |
| **全项目加权总体** | 约15-20%（早期估算） | **52.8%**（`go tool cover -func` 当前实测） |

`internal/api`层用fake repository + 真实service组合验证HTTP层路由/鉴权/错误码
映射/XSS转义；`internal/ws`层用真实Redis + 无需真实WebSocket连接的裸Client
验证Hub核心广播/私聊/限流，并补充机器人`sender_type`与行为事件记录测试；
`internal/repository`补充机器人身份/行为事件真实DB测试，`pkg/llm`补充Qwen
客户端测试。总体覆盖率从早期58.0%显示为当前52.8%，是后续新增了两个0%覆盖率
的薄命令编排层（`cmd/bot`、`cmd/export_training_data`）导致分母扩大，并非测试
删除或质量回退；不以旧数字掩盖最新真实结果。详见
[`docs/19-test-coverage-work-log.md`](docs/19-test-coverage-work-log.md)。

### 更丰富的部署与运行时测试（2026-08-19，针对"部署及运行方式"方向的专项补齐）

此前架构文档一直声称"Redis Pub/Sub 多实例扇出为强制设计"，但所有测试/演示场景
永远只跑一个 `server` 容器，这个设计从未被真正的第二个独立进程验证过。本轮新增：

- **instance_id 可观测性**：`/healthz`、`/readyz`、WS `connected` 事件均新增
  `instance_id` 字段（回落到容器 hostname 或 `INSTANCE_ID` 环境变量），可从外部
  区分某次响应/某条连接落在哪个物理实例上。
- **多实例部署 overlay**：`deploy/docker-compose.multi-instance.yml` +
  `deploy/nginx.lb.conf`，叠加在默认单实例部署之上（不修改/替代默认部署），
  一键起第二个独立后端进程 + nginx 负载均衡器：
  ```bash
  docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.multi-instance.yml up -d --build
  python3 scripts/multi_instance_test.py http://localhost:8082   # 验证跨实例广播/私聊
  ./scripts/resilience_test.sh http://localhost:8082              # 验证实例/依赖故障自动恢复
  ```
- **真实验证结果**：负载均衡器确认分流到2个不同实例；两个 WS 连接落在不同物理
  进程上仍能通过 Redis Pub/Sub 收到彼此的广播/私聊（不再是"同进程自己发布自己
  订阅"）；`docker stop server2` 后服务整体不中断（20/20请求成功）且恢复后自动
  重新加入负载均衡池；`docker stop redis`/`postgres` 后 `/readyz` 正确报告
  `not_ready` 并指出具体故障组件，恢复后无需重启应用进程自动变回 `ready`。
  已知边界：仍是同机多容器，非真正多机部署；未引入 K8s 编排能力。详见
  [`docs/20-deployment-runtime-testing-work-log.md`](docs/20-deployment-runtime-testing-work-log.md)。

### LLM 驱动机器人最小验证（2026-08-19，AI-native 远期方向的首次真实验证）

设计之初讨论过"训练替身机器人代替用户进行前期社交"的远期方向（★13/★14 已
确认机器人透明度披露/opt-in 决策），但此前只落地了 schema 预留
（`users.is_bot`/`bot_action_log` 等），业务代码从未真正读写过这些字段，
也从未产生过一条真实的机器人消息。本轮新增最小验证闭环：

- **新增**：`server/cmd/bot`（独立验证工具）+ `server/pkg/llm`（Qwen/DashScope
  客户端，OpenAI 兼容协议，标准库直接实现不引入 SDK 依赖）；`UserRepository.
  SetIsBot`/`IsBot`（故意不通过任何公开 HTTP 接口暴露，避免用户自我标记为
  机器人绕过透明度要求）；`BotActionLogRepository`（首次真正写入这张此前
  0 行数据的表）；WS 层 `sender_type` 改为由服务端根据账号 `is_bot` 权威判定
  （不信任客户端字段）。
- **真实运行结果**：机器人账号登录 → 标记 `is_bot=true` → 调用 Qwen LLM 真实
  生成一条开场白（"（机器人）哈喽～欢迎来数码天地！…"，非硬编码模板）→
  通过真实 WS 协议发到"数码"房间并收到广播确认 → 写入 `bot_action_log` →
  向已注册的真实用户 `moonteng`/`lili` 成功发起好友请求。数据库直查 +
  `playwright-cli` 真机验证前端正确展示 `ai_zhidai_bot（机器人）` 标识
  （此前该前端防御性分支从未被真实触发过）。
- **用法**：
  ```bash
  export LLM_API_KEY=sk-xxx        # 或 DASHSCOPE_API_KEY，写入 server/.env（已 gitignore）
  cd server && go run ./cmd/bot
  ```
- **已知局限**（如实标注）：这是一次性运行的确定性工具，不是自主决策的常驻
  服务；`interaction_events`现已记录基础事件但仍未参与真实语义匹配，
  `proxy_for_user_id`/`embedding`同样未接入；好友请求目标是环境变量指定的
  固定用户名，不构成"机器人自主判断"。详见
  [`docs/23-llm-bot-minimal-validation-work-log.md`](docs/23-llm-bot-minimal-validation-work-log.md)。

### 用户行为数据训练管道最小验证（2026-08-19）

诚实检查发现 `interaction_events` 表此前从建表起从未被写入过——"未来能否把
用户行为数据投喂给模型训练用户替身"缺少最基础的行为原始数据支撑。本轮：

- 让 `join_room`/`send_message`/`add_friend_request` 三类事件真实写入
  （`ws.Hub`/`service.FriendService` 新增可选 `EventRecorder` 注入，nil-safe，
  只在行为真正成功发生时记录，失败/拦截/重复等分支不产生噪音信号）；
- 新增 `cmd/export_training_data` 最小导出工具，把某用户的账号信息+关注
  事项+行为事件历史组装成带版本号（`format_version`）的结构化 JSON，验证
  "数据库原始行到可投喂训练格式"这条链路确实可行。
- **真机验证**：机器人触发 join_room/send_message → 导出确认事件正确落库
  并可结构化输出；两个全新注册账号发起好友请求 → 导出确认 `add_friend_
  request` 事件正确记录。
- **已知局限**（如实标注）：历史数据依然永久缺失（本轮之前的行为无法补
  采）；`user_watch_topics.keywords` 仍是非结构化自由文本；没有数据使用
  授权/opt-in 字段；没有周期性画像聚合（只是原始事件流水账）；账号删除
  仍是级联硬删除，没有"删除前归档"流程。详见
  [`docs/24-training-data-pipeline-work-log.md`](docs/24-training-data-pipeline-work-log.md)。

### 当前部署状态与云端演示/压测计划（2026-08-19）

当前Docker Compose默认环境健康，支持本机演示与同机多实例验证；但**尚未部署到
公网云服务器，当前没有可供评委直接访问的公网URL**。此前查询 Lighthouse
`ap-guangzhou`未发现运行中实例。建议获得可用实例后按4核8GB、80GB SSD、10Mbps
公网规格部署，并从独立压测端执行50→100→200并发梯度压测、采集服务端CPU/内存/
网络曲线；不要把本机快照压测包装成云端容量结论。详细状态、部署步骤、远程访问
与跨PC数据迁移说明见
[`docs/25-current-status-and-cloud-deployment-plan.md`](docs/25-current-status-and-cloud-deployment-plan.md)。

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go 1.23 + Gin + gorilla/websocket |
| 数据库 | PostgreSQL 16（pgvector 扩展，预留 AI-native 向量检索能力） |
| 缓存/广播 | Redis（在线态 + Pub/Sub 多实例扇出，强制设计，非可选） |
| 前端 | React 19 + TypeScript + Vite |
| 部署 | Docker Compose |

## 本地运行

### 方式一：Docker Compose 一键启动（推荐）

```bash
docker compose -f deploy/docker-compose.yml up -d --build

# 等待启动后验证：
./scripts/smoke_test.sh http://localhost:8080

# 更完整的接口级黑盒回归（54项断言，覆盖鉴权/房间/好友/私聊/关注事项/AI推荐/Web Push/登录限流/登出黑名单）：
python3 scripts/integration_test.py http://localhost:8080

# 预置演示账号/好友关系/关注事项数据，方便现场演示（幂等，可重复运行）：
python3 scripts/demo_seed.py http://localhost:8080

# 简易压测（HTTP并发 + WS端到端消息往返延迟，非专业压测工具，见下方说明）：
python3 scripts/load_test.py http://localhost:8080

# 数据库备份 / 恢复（见 docs/18-backup-and-restore.md）：
./scripts/db_backup.sh
./scripts/db_restore.sh backups/<备份文件>.sql.gz

# 前端访问：http://localhost:8081
# 后端 API：http://localhost:8080
```

演示脚本详见 [`docs/14-demo-guide.md`](docs/14-demo-guide.md)；架构图与关键技术决策见
[`docs/13-architecture-and-adr.md`](docs/13-architecture-and-adr.md)；AI 使用方式说明见
[`docs/15-ai-usage-notes.md`](docs/15-ai-usage-notes.md)；数据库备份恢复策略见
[`docs/18-backup-and-restore.md`](docs/18-backup-and-restore.md)。

### 方式二：本地分别启动（开发调试用）

需要本地已有 PostgreSQL（启用 `vector` 扩展）与 Redis，或用 `docker compose -f deploy/docker-compose.yml up postgres redis -d` 只起依赖。

```bash
# 后端
cd server
cp .env.example .env   # 按需修改
go run ./cmd/server

# 前端（另一个终端）
cd client
cp .env.example .env
npm install
npm run dev

# 前端单测
npm run test
```

## 项目结构

```text
/client                     前端（React + Vite + TS）
/server
  /cmd/server               程序入口
  /internal/api             HTTP handlers
  /internal/ws              WebSocket gateway（后续 Task 接入）
  /internal/service         业务逻辑
  /internal/repository      数据访问层
  /internal/middleware      鉴权/限流/日志中间件
  /internal/model           数据模型
  /internal/config          配置加载
  /pkg/log                  结构化日志
  /pkg/metric               基础运行指标
/migrations                 数据库迁移脚本
/deploy                     docker-compose.yml、nginx.conf
/docs                       需求扩写、任务规划、架构说明、演示脚本、AI 使用说明
/testcase                   接口级验收用例清单
/scripts                    部署后最小验证脚本、集成测试、演示数据预置脚本
```

## 架构设计要点（AI 使用方式与关键技术决策见 `docs/` 目录）

- **实时通信**：WebSocket 为主链路，广播统一走 Redis Pub/Sub（发布到房间频道 → 各实例订阅 →
  推给本地连接），即使当前单实例部署，架构上已具备加机器水平扩展的能力（不是"以后需要再重构"）。
- **AI-native 远期扩展预留**：`users.interest_embedding`、`messages.embedding`（pgvector 类型）、
  `users.is_bot`/`proxy_for_user_id`、`messages.sender_type`、`interaction_events`、
  `user_watch_topics`、`match_candidates`、`bot_action_log` 均已在 schema 中预留，用于支撑
  未来"训练替身机器人代替用户进行前期社交"的产品方向；当前版本的 AI 推荐（Task20）为
  **规则化简单实现**（基于关注事项关键词重合 + 共同房间），不涉及模型训练，可平滑升级为
  embedding 驱动的语义匹配与真实机器人代理。
