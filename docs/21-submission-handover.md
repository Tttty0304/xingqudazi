# 提交交接文档：兴趣搭子在线聊天室

> 文档定位：本文档面向**课题验收/评审**场景，把散落在 `docs/00`~`docs/25`、
> `README.md`、`testcase/00-testcase-plan.md` 中的信息，按题目要求的结构重新
> 组织为一份可独立阅读的交接材料——包含运行说明、逐项对照9个考察方向的诚实
> 评估、整体架构、AI 使用方式、演示建议、已知局限与当前云端部署计划。
>
> 撰写时间：2026-08-19（项目当前状态的快照）。
> 定位声明：这是一个**功能完整、经过真机验证、在有限时间内对后台工程质量做了
> 有意识取舍的 demo 级全栈产品**，不是生产级架构——文中会明确标注哪些方向做到
> 了什么程度、哪些是主动确认的设计边界、哪些是清楚知道但还没来得及补的真实差距。

---

## 一、课题要求回顾

- **课题**：兴趣搭子在线聊天室——基于不同兴趣主题的在线聊天室，支持浏览房间、
  进入房间与在线用户实时聊天；用户体系/交互方式/产品功能可自行设计扩展。
- **产出要求**：可运行、可体验的完整 Web 产品（前端+后台服务+必要数据存储）+
  完整代码 + 运行说明。
- **9个重点考察方向**：产品功能与用户体验完整性 / 后台服务及整体架构设计 /
  实时通信方案 / 服务可靠性与异常处理 / 日志监控及可观测性 / 系统安全性 /
  性能资源使用及成本 / 测试与代码质量 / 部署及运行方式。
- **AI 使用要求**：不限制 AI 生成代码比例，但要能理解并对最终代码/方案/质量
  负责；最终需完整演示产品 + 介绍架构/技术方案/AI 使用方式。

本文档逐项回应以上要求。

---

## 二、运行说明

### 前置依赖

- Docker + Docker Compose（推荐方式，无需本地装 Go/Node/Postgres/Redis）
- 如需本地分别启动：Go 1.23+、Node 18+、PostgreSQL 16（`vector` 扩展）、Redis 7

### 方式一：Docker Compose 一键启动（推荐，用于演示/验收）

```bash
cd v2
docker compose -f deploy/docker-compose.yml up -d --build

# 前端访问：http://localhost:8081
# 后端 API：http://localhost:8080

# 健康检查：
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz   # {"status":"ready","db":"ok","redis":"ok","instance_id":"server1"}
```

首次构建（含 Postgres 镜像拉取 + Go/前端依赖编译）约需 3-5 分钟，取决于网络状况。

### 一键验证脚本（建议按顺序执行，确认环境健康）

```bash
./scripts/smoke_test.sh http://localhost:8080          # 最小可用性验证（healthz/readyz）
python3 scripts/integration_test.py http://localhost:8080   # 接口级黑盒回归，54项断言
python3 scripts/demo_seed.py http://localhost:8080          # 预置演示账号/好友关系/关注事项数据（幂等）
```

### 演示专用数据与脚本

- `scripts/demo_seed.py`：预置好几组演示账号（含已建立好友关系、关注事项、AI推荐
  候选），避免现场演示时要从空白状态一步步注册/建关系。
- `docs/14-demo-guide.md`：分四部分的完整演示脚本（含常见问题预案），建议演示前
  过一遍。

### 进阶验证（体现"更丰富的部署/运行时测试"，非日常演示必需）

```bash
# 简易压测（HTTP并发 + WS端到端消息往返延迟）：
python3 scripts/load_test.py http://localhost:8080

# 多实例横向扩展验证（起第二个独立后端进程 + nginx负载均衡，叠加在默认部署之上）：
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.multi-instance.yml up -d --build
python3 scripts/multi_instance_test.py http://localhost:8082
./scripts/resilience_test.sh http://localhost:8082      # 真实故障注入（docker stop/start）验证自动恢复
# 验证完毕恢复默认单实例状态：
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.multi-instance.yml stop server2 lb
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.multi-instance.yml rm -f server2 lb

# 数据库备份/恢复：
./scripts/db_backup.sh
./scripts/db_restore.sh backups/<备份文件>.sql.gz
```

### 方式二：本地分别启动（开发调试用）

```bash
# 仅起依赖：
docker compose -f deploy/docker-compose.yml up postgres redis -d

# 后端：
cd server && cp .env.example .env && go run ./cmd/server

# 前端（另一终端）：
cd client && cp .env.example .env && npm install && npm run dev

# 后端/前端测试：
cd server && go test ./... -cover
cd client && npm run test
```

### 常用测试账号（`demo_seed.py` 预置，若已运行过该脚本可直接登录体验）

具体账号名单见 `docs/14-demo-guide.md`；也可以直接点"访客模式"免注册体验核心
聊天功能。

---

## 三、产出物清单

| 类别 | 位置 |
|---|---|
| 后端代码 | `server/`（Go + Gin，分层：`api`/`service`/`repository`/`ws`/`middleware`） |
| 前端代码 | `client/`（React 19 + TypeScript + Vite） |
| 数据库迁移 | `migrations/`（6个 `.sql` 文件） |
| 部署配置 | `deploy/`（`docker-compose.yml` 默认单实例 + `docker-compose.multi-instance.yml` 多实例overlay + nginx配置） |
| 需求/任务/架构文档 | `docs/00-brainstorm-and-plan.md`（需求扩写+★1-★15开放问题决策+Task0-20清单）、`docs/13-architecture-and-adr.md`（架构图+ADR） |
| 各阶段工作日志 | `docs/01`~`docs/12`、`docs/16`~`docs/25`（记录各开发批次的改动、真机验证、机器人最小验证、训练数据管道与云端部署计划） |
| 演示与AI使用说明 | `docs/14-demo-guide.md`、`docs/15-ai-usage-notes.md`、`docs/22-final-demo-and-defense-script.md` |
| 验收用例清单 | `testcase/00-testcase-plan.md`（T01起接口级用例 + C1-C8气泡功能用例 + G1-G9能力补齐用例 + M1-M6多实例/可靠性用例 + B1-B6 LLM机器人用例 + D1-D5训练数据管道用例） |
| 自动化验证脚本与工具 | `scripts/`（smoke_test / integration_test / demo_seed / load_test / multi_instance_test / resilience_test / db_backup / db_restore）+ `server/cmd/bot`（LLM机器人最小验证）+ `server/cmd/export_training_data`（训练数据JSON导出） |
| CI 配置 | `.github/workflows/ci.yml`（如实标注：因项目无远程git仓库，尚未在真实CI环境跑过一次） |
| 当前云端部署状态/计划 | `docs/25-current-status-and-cloud-deployment-plan.md`（当前未部署公网；给出Lighthouse规格、部署与独立压测计划） |

---

## 四、逐项对照9个考察方向

以下均基于**真实代码 + 真机验证结果**，区分"已确认的设计边界简化"与"清楚知道
但尚未做到位的真实差距"，不做无根据的溢美。

### 1. 产品功能与用户体验完整性 —— 较好满足

**已实现**（均经真实浏览器多用户联动验证，非纸面功能列表）：
- 注册/登录/访客模式三种进入方式
- 4个固定兴趣主题房间的群聊（历史消息分页 + WS实时收发 + 图片消息）
- 好友关系链（发起/接受/拒绝/删除，实时WS通知）
- 私聊（仅好友可私聊，会话列表+历史消息）
- 关注事项（用户主动声明兴趣关键词）+ 规则化 AI 推荐（基于关注事项交集与共同
  房间打分，确认后一键转化为好友请求）
- Web Push 离线通知（好友请求/私聊消息，用户离线时触发）
- 房间内点击发言人用户名快捷加好友（缩短"聊得来 -> 加好友"链路）
- 好友请求/私聊未读精确数字提醒（非仅一个圆点）
- 敏感词过滤 + 举报机制

**真实缺口**（如实标注）：无找回密码流程；无头像/个人资料页（仅用户名文字）；
私聊未读数不含历史累积（仅统计本次会话期间新消息，因后端无持久化已读游标）；
房间不支持用户自建（产品设计范围内的主动选择，见 ADR，非缺陷）。

### 2. 后台服务及整体架构设计 —— 较好满足

Go + Gin 三层架构：`api`（HTTP handler）→ `service`（业务逻辑）→ `repository`
（数据访问），依赖通过构造函数显式注入（`main.go` 手工装配 ~20个 service/repo）。
12条 ADR 记录关键决策（PostgreSQL+pgvector 预留 AI 向量检索能力、Redis 作为
多实例横向扩展的**强制**基座而非可选项等），详见 `docs/13-architecture-and-adr.md`。

**真实缺口**：无 API 文档（OpenAPI/Swagger），只有 testcase 文档+代码注释；
无 API 版本化（`/api/v1`）；无 DI 框架，规模再大时手工装配会变得难维护。

### 3. 实时通信方案 —— 较好满足，已用真实多实例验证

WebSocket（gorilla/websocket）+ Redis Pub/Sub 跨实例扇出：连接鉴权、join/leave
房间、心跳、消息落库+`msg_id`幂等去重、Graceful Shutdown（SIGTERM时向全部活跃
连接发送真实Close帧）。前端断线后指数退避自动重连（1s→2s→4s→8s→封顶10s）。

**本轮新增的关键验证**：此前"多实例扇出"一直只是架构文档里的声明，从未有真正
的第二个独立进程参与验证过。本轮搭建了真实的多实例拓扑（`docker-compose.
multi-instance.yml`：两个独立后端进程 + nginx负载均衡），用两条分别落在**不同
物理实例**的 WS 连接实测验证了跨实例群聊广播与私聊投递均真实生效，不再是
"同进程自己发布自己订阅"。详见 `docs/20-deployment-runtime-testing-work-log.md`。

**真实缺口**：无消息 ACK/重发队列，网络丢包时前端无法感知"消息其实没发出去"。

### 4. 服务可靠性与异常处理 —— 部分满足，边界清晰

- WS 连接级双层限流（突发窗口+分钟配额）
- 登录接口暴力破解防护（按IP Redis固定窗口限流，**fail-open**：Redis故障时
  放行而非拒绝所有登录，避免限流器自身成为单点故障）
- Graceful Shutdown 覆盖 HTTP 与 WS 两条路径
- 启动期依赖不可用时明确 `os.Exit(1)`，不会"看起来启动成功但实际半残"
- Web Push 发送失败重试（网络错误/5xx最多重试3次，4xx不重试）
- **本轮新增的真实故障注入验证**：用真实 `docker stop`/`start` 制造实例故障
  与依赖（Redis/Postgres）故障，验证负载均衡自动降级（实例故障期间服务整体
  不中断）+ 依赖故障后 `/readyz` 正确报告并在恢复后自动变回 `ready`（无需重启
  应用进程），共6项场景全部通过真机验证，详见 `docs/20-deployment-runtime-
  testing-work-log.md`。

**真实缺口**：无熔断器；数据库/Redis 长期故障时只有健康检查变红，没有更细粒度
的降级策略；无通用重试机制（本轮仅补齐了Web Push一处）。

### 5. 日志、监控及可观测性 —— 部分满足，需明确边界

结构化日志（`slog`）+ 请求级 `trace_id`（透传 `X-Trace-Id`，记录请求耗时）；
`/healthz`、`/readyz`（依赖健康聚合）、`/metrics`（进程内原子计数器：在线连接数/
消息总量/WS错误数/各房间在线人数）；本轮新增 `instance_id` 字段（多实例部署下
可区分日志/响应来自哪个物理进程）。

**需要如实指出的本质局限**：`/metrics` 是自实现的极简JSON输出，**不是
Prometheus格式**（ADR已明确标注为主动选择，非遗漏），没有时序数据库、没有
仪表盘、没有告警——这意味着"监控"目前只能靠人肉看日志/curl `/metrics`，**不
具备生产意义上的可观测性**，是9个方向里成色最需要主动说明的一项。

### 6. 系统安全性 —— 从薄弱提升到及格，非"强"

- JWT鉴权（HMAC签名+过期校验）
- 密码复杂度校验（须同时含字母数字，非纯长度检查）
- CORS 白名单可配置（`CORS_ALLOWED_ORIGINS`，默认保持demo场景的完全放开）
- **JWT登出黑名单**：`POST /api/auth/logout`后旧token立即失效，HTTP与WS两条
  鉴权路径均生效
- XSS转义（`html.EscapeString`，覆盖群聊/私聊/会话预览5处输出边界）
- 敏感词过滤、举报机制、登录接口限流

**真实缺口**：**没有HTTPS/TLS**（真实部署裸奔HTTP/WS，token明文传输，这是若
被追问"能否直接上线生产"时唯一无法绕过的短板）；无refresh token机制（只有
黑名单撤销，没有短期access token+长期refresh的标准方案）；无账号锁定策略
（限流只挡单IP高频爆破速度，不挡分布式多IP场景）。

### 7. 性能、资源使用及成本 —— 从空白提升到有基准数据，样本仍单薄

反向索引补齐（`friendships`/`conversations`/`match_candidates`三张双向关系
表，避免`WHERE a=$1 OR b=$1`全表扫描）；PostgreSQL连接池大小可配置；nginx对
静态资源加一年强缓存；镜像体积 server≈80MB/client≈76MB，前端JS gzip后≈83KB。

**本轮新增真实压测数据**（`scripts/load_test.py`，本机单机环境，非专业压测
工具，仅供数量级参考）：HTTP `GET /api/rooms` 100并发共500请求，100%成功，
avg=36.8ms/p50=32.9ms/p95=79.4ms/p99=99.5ms，吞吐2486 req/s；WS端到端消息
往返（100并发连接）100%成功，p50≈170ms/p99≈347ms。

**真实缺口**：只测了单一并发档位/单机快照，没有梯度加压找拐点、没有资源曲线
（CPU/内存随并发变化）、没有长稳测试。这份数据的价值是"从0到1"，不是"性能
报告"。

### 8. 测试与代码质量 —— 本轮重点补齐，覆盖率大幅提升

2026-08-26 又补齐了统一命令防护层：全部 HTTP 写接口支持 `Idempotency-Key` 安全重放，
严格拒绝未知 JSON 字段/类型错误/尾随 JSON；WS 坏帧只返回协议错误、不关闭连接，确保
后续命令可继续处理。详见 `docs/26-command-validation-idempotency-work-log.md`。2026-08-27
已在本机 Docker Desktop（Linux/x86_64）实测：后端 `go test ./...` 全绿，真实 HTTP
集成脚本为 **57 passed, 0 failed**，前端 Vitest 为 **16 passed**；同时发现并修复严格
解码遗漏 `binding:\"required\"` 标签校验的问题，并将前端构建基镜像从 Node 20 升至
满足锁定测试依赖要求的 Node 22。

`golangci-lint`全绿（覆盖errcheck/govet/staticcheck/unused/ineffassign）；
`scripts/integration_test.py` 54项接口级黑盒回归；前端 `vitest` 15项单测。

**本轮新增：单测覆盖率专项补齐**（实测数据，`go test ./... -cover`）：

| 包 | 改动前 | 改动后 |
|---|---|---|
| `internal/api`（HTTP handler） | 0.3% | **75.5%** |
| `internal/middleware` | 0% | **94.7%** |
| `internal/ws`（WS核心逻辑） | 13.6% | **61.2%** |
| `internal/config` | 0% | **97.6%** |
| `pkg/metric` / `pkg/log` | 0% | **100%** |
| `internal/service` | 67.4% | **67.6%**（新增行为事件记录测试） |
| `internal/repository` | 0% | **30.4%**（补充机器人/行为事件真实DB测试） |
| `pkg/llm` | 新增 | **82.4%**（Qwen OpenAI兼容客户端） |
| `pkg/webpush` | 78.8% | 78.8%（未变） |
| `cmd/bot` / `cmd/export_training_data` / `cmd/server` | — | 0%（薄编排层，依赖真机/集成验证；不为覆盖率强行拆分） |
| **全项目加权总体** | 约15-20%（早期估算） | **52.8%**（`go tool cover -func` 当前实测） |

新增60+单测用例，方法上：`api`层用fake repository+真实service组合验证HTTP层
路由/鉴权/错误码映射/XSS转义；`ws`层用真实Redis+裸Client（无需真实WebSocket
连接）验证Hub核心广播/私聊/限流逻辑，过程中修复了一个真实的异步时序竞态测试
bug。详见 `docs/19-test-coverage-work-log.md`。

CI配置：新增 `.github/workflows/ci.yml`（后端lint+test含真实Postgres/Redis
service container，前端build+lint+vitest）。**如实标注局限**：本项目未接入
任何远程git仓库，这份配置目前只是"资产本身"，尚未在真实CI环境实际触发跑过
一次，不等同于"已验证通过的CI"。

### 9. 部署及运行方式 —— 从单机demo扩展到已验证的多实例横向扩展能力

Docker Compose一键起postgres(pgvector)+redis+server+client；多阶段构建
Dockerfile；`.env.example`覆盖全部环境变量；`media_uploads`具名卷避免容器
重建丢图片；数据库备份/恢复脚本（`db_backup.sh`/`db_restore.sh`，已真机验证
"备份→覆盖恢复→数据一致"完整闭环，含真实踩坑修复：`pg_dump`需加
`--clean --if-exists`才能幂等恢复）。

**本轮新增：更丰富的部署与运行时测试**（详见 `docs/20-deployment-runtime-
testing-work-log.md`、testcase M1-M6）：
- 新增 `deploy/docker-compose.multi-instance.yml` overlay，起第二个独立后端
  进程+nginx负载均衡（叠加在默认单实例部署之上，不影响默认交付形态）
- 用两个**真实独立进程**验证了此前只停留在文档声明层面的"Redis Pub/Sub多
  实例扇出"设计：负载均衡真实分流到2个实例、跨实例群聊广播/私聊投递均实测
  通过
- 用真实 `docker stop`/`start`（非模拟）验证：实例故障期间服务整体不中断
  （20/20请求成功）+ 故障恢复后自动重新加入负载均衡池；Redis/Postgres故障后
  `/readyz`正确报告并在恢复后20-30秒内自动变回`ready`（无需重启应用进程）

**当前公网交付状态与真实缺口**（如实标注）：当前默认交付仍是本地Docker
Compose，**尚未部署到任何公网云服务器，也没有可给评委访问的公网URL**；此前
查询的 Lighthouse `ap-guangzhou` 地域没有运行中实例，尚未选择其他可用实例。
项目已有云端部署/独立压测计划（推荐4核8GB、80GB SSD、10Mbps公网；服务端与
压测端分离；50→100→200并发梯度+资源曲线+稳态WS测试），详见
`docs/25-current-status-and-cloud-deployment-plan.md`。此外仍是同一台机器上的
多个容器，非真正多机/多可用区部署；未引入Kubernetes等编排系统的自动扩缩容/
健康检查驱逐能力；媒体文件用本地磁盘存储，生产环境应换对象存储。

---

## 五、整体架构与关键技术方案（摘要）

详细架构图、数据模型、全部ADR见 `docs/13-architecture-and-adr.md`；这里只列
最关键的几条：

- **实时通信**：WebSocket为主链路，广播统一走 `发布到房间/用户频道 → 各实例
  Redis订阅 → 推给本地连接`，即使当前默认单实例部署，架构上已具备水平扩展
  能力（本轮已用真实多实例拓扑证实，非"以后需要再重构"的空话）。
- **数据存储**：PostgreSQL 16 + pgvector 扩展（预留AI-native向量检索能力，
  当前AI推荐为规则化实现，未接入embedding，可平滑升级）；Redis承担在线态/
  Pub/Sub广播/各类计数器（限流/黑名单），均设计为"可重建/有TTL"，不做持久化。
- **AI-native远期扩展与数据管道**：`users.interest_embedding`、`messages.embedding`、
  `users.is_bot`/`proxy_for_user_id`、`match_candidates`等字段已在schema中预留，
  用于支撑未来"训练替身机器人代替用户进行前期社交"的产品方向；
  `is_bot`/`bot_action_log`已通过`server/cmd/bot`+`server/pkg/llm`最小验证
  工具真实跑通（真实调用Qwen LLM生成消息、发到房间、发好友请求）。本轮还让
  `interaction_events`首次真实记录`join_room`/`send_message`/
  `add_friend_request`，并通过`server/cmd/export_training_data`导出带
  `format_version`的结构化JSON，证明"原始行为数据→训练格式"链路可行（详见
  `docs/23-llm-bot-minimal-validation-work-log.md`、
  `docs/24-training-data-pipeline-work-log.md`）。`embedding`/
  `proxy_for_user_id`仍未接入真实语义匹配，且没有训练数据授权/opt-in、画像
  聚合或删除前归档机制。
- **鉴权**：JWT（HMAC签名）+ Redis黑名单实现登出即时失效，HTTP与WS两条鉴权
  路径复用同一套逻辑保持一致性。

---

## 六、AI 在开发过程中的使用方式（摘要）

完整记录见 `docs/15-ai-usage-notes.md`，核心要点：

- **工作模式**：需求澄清（Brainstorm）→ 任务拆解（Plan，Task0-20）→ 验收
  用例先行（Testcase）→ 编码+真机验证（Work+Verify）的循环，每个功能模块
  完整走一遍，而非一次性生成整个仓库再统一测。
- **AI承担的工作**：全部代码编写（后端Go/前端React/数据库迁移/Docker配置）、
  架构与技术选型建议、测试设计与执行（单测/集成测试/`playwright-cli`驱动的
  真实浏览器多用户端到端测试）、真实环境问题排查（多个bug是在真实Docker/
  浏览器环境验证时才暴露、由AI自主定位修复的，如前端API路径双重前缀、
  PostgreSQL UUID类型边界500错误等）；近期还完成了真实Qwen API驱动机器人
  的最小闭环，以及从`interaction_events`到版本化训练JSON的最小数据管道验证。
- **人工介入的关键节点**：全部技术选型确认（★1-★15开放问题决策）、验证节奏
  调整、产品范围边界确认（如"是否开放用户自建房间"），均由用户明确拍板，
  AI不擅自替用户做主。
- **局限性如实说明**：AI生成代码比例很高，但每处关键设计决策都有对应人工
  确认记录；验证手段（真实Docker环境+真实浏览器多用户测试）比纯单元测试更
  接近生产场景，但UI细节的美观度/交互流畅度仍更多依赖AI的训练知识而非针对性
  用户调研。

---

## 七、演示建议

1. 用 `scripts/demo_seed.py` 预置数据后，按 `docs/14-demo-guide.md` 的四部分
   脚本走一遍（账号体系→群聊→好友/私聊→AI推荐闭环）。
2. 如需展示架构设计能力，重点讲：Redis Pub/Sub多实例扇出设计 + 本轮真实
   多实例验证结果（`docs/20-deployment-runtime-testing-work-log.md`）、
   LLM机器人与行为数据导出最小闭环（`docs/23`/`docs/24`）、ADR决策记录
   （`docs/13-architecture-and-adr.md`）。
3. 如被追问"测试覆盖率"，可直接展示 `docs/19-test-coverage-work-log.md` 的
   对比表；当前总体加权覆盖率为**52.8%**（`go tool cover -func`实测），而非
   早期的58.0%——原因是后续新增了两个0%覆盖率的薄命令编排层，不能隐瞒
   分母扩大后的真实结果。
4. 评委需要直接体验时，当前没有公网URL；应先按
   `docs/25-current-status-and-cloud-deployment-plan.md` 选择可用云实例部署，
   部署完成后再在本文档补充正式演示URL与云端压测数据。
5. **不建议**把项目包装成"生产级架构"，建议按本文档第四节的诚实定位回答——
   "知道差距在哪、为什么现在没做、做了会是什么代价"本身就是本课题"体现对
   后台工程质量的思考"这一考察点想看到的。

---

## 八、如果继续投入，优先级建议（供参考，非当前范围）

按性价比排序：
1. **部署到可用云服务器并形成公网演示URL**：当前最直接影响评委现场体验的
   缺口；部署完成后应使用独立压测端执行梯度压测并采集服务端资源曲线（计划见
   `docs/25-current-status-and-cloud-deployment-plan.md`）。
2. HTTPS/TLS（系统安全性最后一块短板，绑定真实域名后优先补齐）。
3. 接入真实Prometheus+Grafana（可观测性从"演示级"到"生产级"的关键一步，
   但是新增基础设施组件级别的工作量）。
4. 消息ACK+重发队列（实时通信可靠性的关键补强，但改动面较大）。
5. 训练数据使用授权/opt-in + 结构化标签/画像聚合/删除前归档（LLM替身方向从
   "最小数据管道可行"走向真正可合法训练的必要补强）。
6. `internal/repository` 层继续补充单测覆盖率（当前30.4%仍偏低）。
