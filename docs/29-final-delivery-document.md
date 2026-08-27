# 兴趣搭子在线聊天室 —— 最终交付文档

> 本文档是面向课题考核的**最终交付说明**，整合产品使用体验、整体架构、关键技术
> 方案、后端工程质量细节（优雅报错 / 数据校验 / 幂等重放 / 测试覆盖率）、云端
> 部署方案、压测结论，以及 AI 在开发过程中的协作方式。是一份可独立阅读、直接
> 用于提交与答辩的主文档。
>
> 撰写时间：2026-08-27
> 交付形态：可运行、可体验的完整全栈 Web 产品（前端 + 后台服务 + 数据存储）

---

## 一、产品是什么：一句话 + 使用体验

**兴趣搭子在线聊天室**：用户可以按兴趣主题进入不同的群聊房间，与在线用户实时
聊天；认识聊得来的人后可以加好友、私聊；系统基于用户填写的"关注事项"做兴趣
匹配推荐。

### 1.1 核心使用流程

1. **进入**：注册 / 登录 / 访客快速体验三种方式进入。
2. **群聊**：浏览兴趣房间（数码 / 追番 / 运动 / 美食），进入房间实时收发文字与
   图片消息，在线人数实时更新。
3. **加好友**：房间内点击发言人用户名即可弹出"加为好友"气泡，无需跳出聊天室。
4. **私聊**：好友之间一对一私聊，会话列表带未读计数与最近消息摘要。
5. **关注与推荐**：填写"关注事项"关键词，系统生成 AI 推荐候选（带匹配理由与
   打分），确认后一键转化为好友请求，形成完整闭环。
6. **离线通知**：好友请求 / 私聊消息在目标离线时触发 Web Push（需 HTTPS）。

### 1.2 公网体验地址

- 部署地址：`http://111.230.227.135`（腾讯云 Lighthouse，2核2GB）
- 前端通过 nginx 反代，浏览器直接访问根路径即可体验。
- 演示账号可用 `python3 scripts/demo_seed.py http://111.230.227.135` 预置
  （`demo_alice` / `demo_bob`，密码见脚本内配置）。

> 说明：当前为 HTTP + 公网 IP 部署（无域名），因此 Web Push 离线通知需在
> HTTPS 下才可用；系统级通知功能依赖域名 + TLS，详见第六节部署方案。

---

## 二、整体架构

### 2.1 架构图

```mermaid
flowchart TB
    subgraph Client["浏览器"]
        UI["React 19 SPA<br/>(登录/房间/私聊/好友/关注事项/AI推荐)"]
        SW["Service Worker<br/>(Web Push 通知展示)"]
    end
    subgraph Edge["nginx (client 容器)"]
        Static["静态资源 + gzip + 长缓存"]
        ProxyAPI["/api/* 反代"]
        ProxyWS["/ws 反代 (Upgrade)"]
        ProxyUploads["/uploads/* 反代"]
    end
    subgraph Server["Go 后端 (server 容器，可水平扩展为多实例)"]
        HTTP["Gin HTTP API<br/>(鉴权/房间/好友/私聊/关注事项/AI推荐/举报/推送)"]
        WS["WS Gateway<br/>(gorilla/websocket)"]
        Push["Web Push 发送器<br/>(纯Go实现 RFC8291/8292)"]
    end
    subgraph Data["数据层"]
        PG[("PostgreSQL 16 + pgvector<br/>业务数据 + interaction_events")]
        Redis[("Redis<br/>Pub/Sub + 在线态 + 限流/黑名单/幂等占位")]
    end
    UI -- HTTP/WS --> Edge
    Edge --> HTTP
    Edge --> WS
    Edge --> ProxyUploads --> HTTP
    HTTP --> PG
    HTTP --> Redis
    WS --> PG
    WS -- 广播/在线态 --> Redis
    Redis -. Pub/Sub 跨实例扇出（已用真实多实例验证）.-> WS
    HTTP --> Push
    Push -- 加密 HTTP 请求 --> 浏览器 Push Service
    Push Service -.-> SW
```

### 2.2 分层与装配

后端是标准三层架构，模块边界清晰：

- **`internal/api`**：HTTP handler，负责路由、参数校验、错误码映射。
- **`internal/service`**：业务逻辑层，全部业务规则（鉴权、好友状态机、私聊校验、
  推荐打分、幂等语义）都在这里。
- **`internal/repository`**：数据访问层，PostgreSQL / Redis 的具体读写。
- **`internal/ws`**：WebSocket Gateway，只做"连接管理与协议分发"，业务逻辑不下沉。
- **`internal/middleware`**：鉴权 / 限流 / CORS / 幂等 / 日志中间件。
- 依赖通过构造函数**手工显式注入**（`main.go` 装配约 20 个 service/repo），
  各层通过接口依赖倒置（谁使用谁定义接口），便于单测替换。

### 2.3 最关键的架构决策：Redis Pub/Sub 多实例扇出

所有房间广播、私聊投递、在线人数统计**统一走 Redis Pub/Sub 中转**，WS 网关之间
不直连。这是从第一天就确定的**强制设计**（非可选），保证即使默认单实例部署，
代码也天然具备水平扩展能力——加机器只需改编排配置，不需改一行应用代码。

**这一点已用真实多实例拓扑验证**（非纸面声明）：叠加 `docker-compose.multi-
instance.yml` 起第二个独立后端进程 + nginx 负载均衡，实测两个落在不同物理进程的
WS 连接仍能通过 Redis 收到彼此的广播与私聊。

---

## 三、关键技术方案

| 技术点 | 方案 | 理由 |
|---|---|---|
| 实时通信 | WebSocket + Redis Pub/Sub 多实例扇出 | 架构从第一天具备水平扩展能力 |
| 数据存储 | PostgreSQL 16 + pgvector 扩展 | 关系数据 + 向量检索一套存储，预留 AI-native 升级路径 |
| 鉴权 | JWT（HMAC）+ Redis 黑名单撤销 | 无状态鉴权 + 补上"登出即时失效" |
| 离线推送 | 纯 Go 实现 Web Push（RFC8291/8292） | 不依赖第三方 SDK，理解并实现整套协议 |
| 内容安全 | 敏感词拦截 + 举报机制 | 保证"能拦截、能取证"的基础闭环 |
| 部署 | Docker Compose（单实例 / 多实例 overlay / 生产 / 可观测性四套） | 单机 demo 最优性价比 + 已验证横向扩展 |

---

## 四、后端工程质量（重点）

这是课题"岗位方向偏后台开发"的核心考察点，以下逐项说明我们如何保证后端质量。

### 4.1 优雅报错：分层错误码 + 语义化错误类型 + 边界不泄露

后端采用**三层错误处理**，保证"该报的错报得准，不该暴露的信息不泄露"：

1. **语义化错误类型（service 层）**：业务规则失败返回明确的 `errors.New` 哨兵
   错误，如 `ErrInvalidCredentials`、`ErrFriendRequiredForDirectMessage`、
   `ErrConversationNotFound` 等。每个错误语义单一、可被 `errors.Is` 精确匹配。

2. **错误码映射（api 层）**：handler 用 `errors.Is` 把语义错误翻译成对应的 HTTP
   状态码 + 机器可读错误码（如 `invalid_credentials` → 401、
   `friend_required` → 403、`conversation_not_found` → 404），前端据此显示中文
   提示。错误码统一，前端 `ERROR_MESSAGES` 映射表覆盖全部。

3. **安全边界（不泄露内部信息）**：
   - 登录接口**故意不区分**"用户不存在"与"密码错误"，统一返回
     `invalid_credentials`，防止用户名枚举攻击；
   - JWT 校验失败统一返回 `invalid_token`，不泄露"签名错误 / 已过期 / 格式错误"
     的具体原因，不给伪造 token 的攻击者调试线索；
   - 内部错误（数据库异常等）统一返回 `internal_error`，真实错误只写入结构化
     日志，不返回给客户端。

4. **失败分支不产生副作用**：消息发送在"内容校验失败 / 限流 / 敏感词拦截 /
   重复 msg_id"等分支**不落库、不广播、不记录行为事件**，只回一个 error 事件，
   保证失败不污染数据（训练数据里也不会混入噪音）。

### 4.2 数据中间校验：请求进入业务层前被明确校验

本轮（`docs/26`）对全部 HTTP 与 WS 入口做了**严格数据格式校验**，核心是
"数据在进入业务层前就被校验，而不是靠数据库约束 / 业务逻辑兜底"：

- **严格 JSON 解码（`bindJSONStrict`）**：所有 JSON 写接口统一拒绝未知字段、
  类型不匹配、空 body、尾随或多个 JSON 值；并显式恢复结构体 `binding:"required"`
  标签校验，缺失必填字段明确拒绝。
- **分页参数显式校验**：`page >= 1`、`1 <= size <= 100`，格式错误返回 400，
  不再静默改写请求。
- **字段级校验**：用户名规则、密码复杂度（须同时含字母数字）、关注事项关键词
  长度与非法优先级、举报理由长度、推送 endpoint 必须是 HTTPS URL 等，均有明确
  校验并拒绝非法输入。
- **WS 帧严格解码**：`DisallowUnknownFields` + 校验 `room_id` / `target_user_id`
  为合法 UUID；坏帧返回一条 `error` 事件但**不关闭连接**，后续合法帧继续处理，
  避免单个错误命令中断用户实时会话。

### 4.3 幂等与可安全重放：Idempotency-Key + 领域级幂等双保险

网络重试是真实风险，后端用**两层幂等**保证"重试不会产生重复副作用"：

1. **通用 HTTP 幂等层（`Idempotency-Key`）**：所有会产生副作用的写接口（注册、
   登录、好友、私聊、举报、关注事项、推荐处理、媒体上传等）支持可选的
   `Idempotency-Key`。Redis `SETNX` 原子占位，首次成功响应缓存 24 小时；同一
   调用方 + 方法 + 路径 + key 的重放直接返回原始响应，**不二次写库 / 推送 /
   落盘**。关键语义：
   - key 仅允许 `[A-Za-z0-9._-]{8,128}`，非法格式拒绝；
   - 同一 key 并发未完成请求返回 `409 idempotency_in_progress`；
   - Redis 不可用时 fail-closed 返回 `503 idempotency_unavailable`，避免在
     "无法确认是否已执行"的情况下冒险重复执行；
   - 鉴权前按客户端 IP 隔离、鉴权后按 user_id 隔离，防止跨用户 key 串用。

2. **领域级幂等（不依赖 HTTP 头）**：即使客户端不带 `Idempotency-Key`，核心
   写路径仍有业务级幂等——群聊/私聊 `msg_id` 由数据库唯一约束去重、房间重复
   join 幂等、推送订阅 upsert、举报去重、删除好友状态幂等（已不是好友仍返回
   204，因为期望状态已达成）。

3. **前端配合**：前端 API 客户端对所有写操作默认生成 `Idempotency-Key`，网络
   重试时复用同一个 key；新操作生成新 key，避免把重试误当成新命令。

### 4.4 可靠性：限流 + 优雅关闭 + 故障隔离 + 失败重试

- **双层限流**：WS 连接级"2 秒突发窗口 + 分钟配额"双层限流，分别拦截瞬时刷屏
  与长期高频骚扰；登录接口另有按 IP 的固定窗口限流（fail-open，Redis 抖动不
  阻断登录）。
- **优雅关闭**：SIGTERM 时先向全部活跃 WS 连接发送真实 Close 帧（协议层明确
  通知），再等待 HTTP 请求完成；已用 `docker kill --signal=TERM` 真机验证。
- **故障隔离与自动恢复**：真实 `docker stop` 故障注入验证——实例故障期间服务
  整体不中断、恢复后自动重新加入负载均衡池；Redis/Postgres 故障后 `/readyz`
  正确报告 `not_ready` 并指出具体组件，恢复后无需重启进程自动变回 `ready`。
- **Web Push 失败重试**：网络错误 / 5xx 最多重试 3 次，4xx（订阅失效等明确
  错误）不重试并清理僵尸订阅。

### 4.5 测试与代码质量：覆盖率 + 分层测试 + 真机验证

- **单测覆盖率**（`go tool cover -func` 实测，全项目加权 **52.8%**）：

  | 包 | 覆盖率 |
  |---|---|
  | `internal/api`（HTTP handler） | 75.5% |
  | `internal/middleware` | 94.7% |
  | `internal/ws`（WS 核心逻辑） | 61.2% |
  | `internal/config` | 97.6% |
  | `internal/service` | 67.6% |
  | `internal/repository` | 30.4% |
  | `pkg/log` / `pkg/metric` | 100% |
  | `pkg/llm` | 82.4% |
  | `pkg/webpush` | 78.8% |

  说明：总体从早期 58% 显示为当前 52.8%，是后续新增了两个 0% 覆盖率的薄命令
  编排层（`cmd/bot`、`cmd/export_training_data`）导致分母扩大，非测试删除或
  质量回退。`repository` 层 30.4% 是明确的短板（真实数据库单测成本高），如实
  标注。

- **分层测试方法**：`api` 层用 fake repository + 真实 service 组合验证 HTTP 层
  路由 / 鉴权 / 错误码映射 / XSS 转义；`ws` 层用真实 Redis + 裸 Client 验证 Hub
  核心广播 / 私聊 / 限流；`repository` 层用真实数据库补充机器人身份 / 行为事件
  测试。

- **黑盒集成测试**：`scripts/integration_test.py` **57 项断言**（鉴权 / 房间 /
  好友 / 私聊 / 关注事项 / AI 推荐 / Web Push / 登录限流 / 登出黑名单 / 严格
  JSON / 幂等重放），真实环境跑通。

- **真机端到端**：`playwright-cli` 驱动 2-3 个真实浏览器会话多用户联动，验证
  了多个只有真实浏览器环境才能暴露的 bug（如 `/api` 双重前缀、WS 端点 URL
  拼接、`/uploads` 未被 nginx 代理）。

- **静态检查**：`golangci-lint run ./...` 全绿（errcheck / govet / staticcheck /
  unused / ineffassign / gofmt），修复了其发现的真实问题（WS 心跳遗漏 error
  检查、废弃的 `elliptic.Marshal` 改为 `crypto/ecdh`、测试死代码）。

- **CI 配置**：`.github/workflows/ci.yml`（后端 lint + test 含真实 Postgres/Redis
  service，前端 build + lint + vitest）。如实标注：项目此前无远程 git 仓库，
  该 CI 尚未在真实 CI 环境触发跑过，当前已推送至 GitHub 具备触发条件。

---

## 五、性能、资源与成本 + 压测结论

### 5.1 云端压测（2026-08-27，独立压测端 → 公网服务器）

详见 `docs/28-load-test-and-feature-report.md`，核心数据：

**HTTP 梯度压测（`GET /api/rooms`）**：

| 并发 | 成功 | 失败 | p50 | p95 | 吞吐 |
|---|---:|---:|---:|---:|---:|
| 20 | 100% | 0 | 219ms | 2625ms | 30.3 req/s |
| 50 | 100% | 0 | 218ms | 2618ms | 63.2 req/s |
| 100 | 99.8% | 1 | 188ms | 2594ms | 49.9~125 req/s |

**WS 端到端压测（20 并发连接 × 3 消息）**：100% 成功，**p50=93ms**，p99=363ms。

**功能链路（注册→好友→关注→AI 推荐，18 项操作）**：全部 2xx 成功，0 失败；
AI 推荐正确生成候选（`peer=ft_c 分数=2 理由=你们都关注：徒步`）。

### 5.2 诚实结论

- **实时通信是强项**：WS 消息往返 p50=93ms，证明 Go 后端 + Redis Pub/Sub 广播
  链路性能健康。
- **2核2GB 是性能边界**：HTTP 100 并发触及该规格 CPU 上限，出现排队延迟
  （p95 约 2.6s）与偶发失败。
- **网络噪声**：p95 长尾含"本地跨运营商访问腾讯云"的间歇性网络延迟（单发
  请求 172-312ms，同操作有时 200ms 有时 2.5s），非服务器持续过载。
- **不可据此声称生产 SLA**：未做更大规格极限探测、未采集服务端资源曲线、
  未做长稳测试。更大规模结论需 4核8GB + 同地域压测端 + 专业工具（wrk/k6）。

### 5.3 资源与成本优化

- 反向索引补齐（`friendships`/`conversations`/`match_candidates` 双向关系表），
  避免 `WHERE a=$1 OR b=$1` 全表扫描。
- PostgreSQL 连接池大小可配置（`POSTGRES_MAX_CONNS`/`MIN_CONNS`）。
- nginx 对 Vite 产物 `/assets/` 加一年强缓存，gzip 压缩。
- 多阶段构建镜像精简：server ≈ 80MB、client ≈ 76MB；前端 JS gzip 后 ≈ 82KB。

---

## 六、部署方案

### 6.1 当前部署形态

- 腾讯云 Lighthouse（2核2GB，Ubuntu 24.04），Docker Compose 单实例部署。
- 四套编排文件：
  - `docker-compose.yml`：默认单实例（本地 / demo）。
  - `docker-compose.multi-instance.yml`：多实例 overlay（验证横向扩展）。
  - `docker-compose.production.yml` + `Caddyfile`：生产模式（不暴露 DB/Redis，
    Caddy 自动 TLS）。
  - `docker-compose.observability.yml`：Prometheus / Grafana / Alertmanager。

### 6.2 一键部署命令

```bash
# 克隆代码
git clone https://github.com/Tttty0304/xingqudazi.git v2 && cd v2

# 生成密钥
openssl rand -hex 32    # JWT_SECRET
openssl rand -base64 24 # 数据库密码

# 写 .env（JWT_SECRET / POSTGRES_PASSWORD / CORS 等）

# 启动
docker compose -f deploy/docker-compose.yml up -d --build
./scripts/smoke_test.sh http://localhost:8080
python3 scripts/integration_test.py http://localhost:8080   # 57 项断言
```

### 6.3 生产化（有域名时）

```bash
cp .env.production.example .env.production   # 填入 DOMAIN / 强密钥
docker compose --env-file .env.production -f deploy/docker-compose.production.yml up -d --build
# Caddy 自动申请 TLS，HTTP 自动跳转 HTTPS
```

---

## 七、AI 在开发过程中的使用方式（协作方式）

### 7.1 协作模式：不是"一句话黑盒生成"，而是四步循环

项目全程按「**需求澄清 → 任务拆解 → 用例先行 → 编码验证**」的循环推进，每个
功能模块完整走一遍，而非一次性生成整个仓库再统一测：

1. **Brainstorm（需求澄清）**：AI 把"做一个兴趣聊天室"这句模糊需求，扩写成
   15 个结构化技术决策问题（★1-★15，如后端语言、用户体系、实时通信协议、
   AI 推荐做到什么程度），逐项与用户确认后定稿，**避免 AI 自己猜方案**。
2. **Plan（任务拆解）**：拆成 Task0-Task20 显式清单，标注依赖与并行性。
3. **Testcase（验收用例先行）**：编码前先写好接口级验收用例，明确"怎样才算
   做完"。
4. **Work + Verify（编码 + 真机验证）**：每完成 1-3 个 Task，跑编译 + 单测 +
   重建 Docker + 真实浏览器多用户联动，把验证结果（**包括失败和踩坑过程**）
   如实写回 work log。

### 7.2 AI 具体承担的工作

- **架构与技术选型建议**：如"WS 广播必须走 Redis Pub/Sub 而非直连"、"私聊会话
  用规范化键避免方向不同产生重复记录"等设计，AI 提出并说明理由，用户确认后实施。
- **全部代码编写**：后端 Go、前端 React、数据库迁移、Docker/nginx 配置、纯 Go
  实现的 Web Push 协议栈。
- **测试设计与执行**：单测、集成测试脚本、真实浏览器多用户端到端测试用例。
- **真实环境问题自主排查**：多个 bug 是 AI 在真实 Docker/浏览器环境验证时自主
  定位并修复的，例如：
  - 前端 API 路径 `/api` 前缀叠加 → `/api/api/xxx`（仅真实浏览器网络请求可见）；
  - PostgreSQL 对 UUID 列做 `= ANY($1)` 查询时，非法 UUID 字符串直接 500；
  - `elliptic.Marshal` 废弃 API，接入 `golangci-lint` 才系统性发现；
  - 部署到公网后定位"微信内置浏览器 `crypto.randomUUID` 缺失"导致的发消息失败、
    头像上传后未持久化、手机切后台 WS 断连导致未读高亮丢失等真实问题。

### 7.3 人工介入的关键节点（human-in-loop）

AI **没有全自动**完成项目，以下节点由用户明确决策：

- **全部技术选型**：后端语言、数据库、实时通信协议、App 端形态、离线推送、
  内容审核完整度、机器人透明度披露等 ★1-★15 决策，用户在 AI 给出选项后拍板。
- **验证节奏**：用户中途要求"降低验证密度，攒 2-3 个 task 一起测"，AI 据此调整。
- **方向与范围边界**："前端优先补 UI 闭环还是先做收尾"、"是否开放用户自建房间"
  等多路径/边界选择，AI 列选项等用户表态，不自行决定。

### 7.4 如实说明局限性

- AI 生成代码比例很高（几乎全部业务代码），但**每处关键设计决策都有对应人工
  确认记录**，这是"理解并对最终代码 / 技术方案 / 产品质量负责"的依据。
- AI 的验证手段（真实 Docker + 真实浏览器多用户）比纯单测更接近生产，但仍不能
  替代真实用户可用性反馈；UI 美观度更多依赖 AI 训练知识而非针对性用户调研。
- 部分简化点（本地磁盘存储、固定敏感词表、规则化 AI 推荐）是时间预算下与用户
  共同确认的合理取舍，非隐藏技术债，均已在 `docs/13-architecture-and-adr.md`
  "已知简化点"中列出。

---

## 八、逐项对照课题 9 个考察方向的诚实自评

| 方向 | 自评 | 关键证据 |
|---|---|---|
| 产品功能与用户体验 | 较好满足 | 群聊/好友/私聊/关注/AI推荐/Web Push 全链路真机验证；近期修复头像持久化、未读高亮等真实体验问题 |
| 后台服务及整体架构 | 较好满足 | 三层分层 + 12 条 ADR + 手工依赖注入，模块边界清晰 |
| 实时通信方案 | **优秀** | Redis Pub/Sub 多实例扇出，真实多实例拓扑验证，WS 压测 p50=93ms |
| 服务可靠性与异常处理 | 中上，边界清晰 | 双层限流 + 优雅关闭 + 故障注入验证 + 失败重试；缺熔断器 |
| 日志监控可观测性 | 中上 | 结构化日志 + trace_id + `/metrics`(Prometheus 格式) + 可观测性编排；告警未接真实通知通道 |
| 系统安全性 | 及格偏上 | JWT + 黑名单撤销 + 密码复杂度 + CORS 白名单 + 严格校验 + 幂等 + XSS 转义；缺 HTTPS/TLS（生产编排已具备，需域名） |
| 性能资源成本 | 有基准，样本单薄 | 反向索引/连接池/镜像精简 + 首次真实压测；未做长稳/资源曲线 |
| 测试与代码质量 | **强项** | 覆盖率 52.8% + 57 项集成断言 + golangci-lint 全绿 + 真机端到端 |
| 部署及运行方式 | 中上 | Docker Compose 四套编排 + 公网已部署 + 备份恢复；未上 K8s/多机 |

**一句话总结**：这是一个功能完整、经过真机验证的全栈 demo 产品，重点在于对
后台工程质量的思考——实时通信的多实例架构是真实验证过的（非纸面声明），安全性
/ 可靠性 / 测试覆盖率 / 性能基准这些方向做了及格线以上的基础工作，同时我们
清楚知道离生产级还差什么（HTTPS、真实监控告警、更扎实的压测），这些差距和
原因都写在文档里，不是隐藏的技术债。

---

## 九、相关文档索引

| 内容 | 文档 |
|---|---|
| 需求澄清与任务清单 | `docs/00-brainstorm-and-plan.md` |
| 架构图 + 12 条 ADR | `docs/13-architecture-and-adr.md` |
| AI 使用方式完整记录 | `docs/15-ai-usage-notes.md` |
| 测试覆盖率提升过程 | `docs/19-test-coverage-work-log.md` |
| 多实例部署与运行时可靠性验证 | `docs/20-deployment-runtime-testing-work-log.md` |
| 提交交接（9 方向逐项评估） | `docs/21-submission-handover.md` |
| 最终演示与答辩脚本 | `docs/22-final-demo-and-defense-script.md` |
| 命令校验/幂等/错误隔离 | `docs/26-command-validation-idempotency-work-log.md` |
| 生产就绪七项缺口收敛 | `docs/27-production-readiness-work-log.md` |
| 云端压测与功能链路报告 | `docs/28-load-test-and-feature-report.md` |
| 接口级验收用例清单 | `testcase/00-testcase-plan.md` |
