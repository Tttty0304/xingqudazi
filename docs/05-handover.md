# 开发交接文档

> 交接时间：2026-08-17
> 交接原因：当前沙箱环境是缺少 `cap_sys_admin` 等能力的 K8s Pod，无法运行嵌套容器（`docker compose`
> 走不通），需要转移到一个具备完整 Docker 权限的环境继续开发和验证。
> 本文档只做"状态快照 + 交接说明"，完整的需求/决策记录见 [`docs/00-brainstorm-and-plan.md`](00-brainstorm-and-plan.md)，
> 不在本文重复展开。

---

## 一、文档地图（先看这几份，按顺序）

| 文档 | 内容 |
|---|---|
| `docs/00-brainstorm-and-plan.md` | **最权威的信息源**：需求扩写、9轮开放问题澄清与最终决策（★1-★15）、技术架构、Task0-Task20 全部任务清单及依赖关系 |
| `testcase/00-testcase-plan.md` | 接口级验收用例清单（T01起），当前已实现功能均已用真机验证覆盖对应用例 |
| `docs/01-task1-work-log.md` ~ `04-task4-work-log.md` | Task1-4 各自的改动清单 + 真机验证记录 |
| `README.md` | 项目简介、技术栈、本地运行方式（**注意**：目前只写到 Task1 进度，未同步到 Task4，见下方"待办"） |
| 本文档 | 当前状态快照 + 转移到新环境的操作步骤 + 下一步任务顺序 |

---

## 二、当前完成情况总览

### 已确认的关键技术决策（Task0，详见 Plan ★1-★15，不再赘述）

- 后端：**Go + Gin + gorilla/websocket**
- 数据库：**PostgreSQL（启用 pgvector 扩展）+ Redis**
- 实时通信：**WebSocket + Redis Pub/Sub 多实例扇出（强制要求，非可选）**
- 部署：**Docker Compose 一键部署**
- 可观测性：**仅结构化日志 + 基础 metrics**（不接入 Prometheus/Grafana）
- App 端：**响应式网页**（非独立原生 App）
- 离线推送：**Web Push（已确认要做，非可选）**
- 功能范围：群聊 + 好友关系链 + 私聊 + 图片消息(P0)/语音文件(P1) + 基础内容安全 + AI-native 远期扩展预留（含 P1 规则化 AI 推荐简单演示）

### 已完成的 Task（Task0-Task4）

| Task | 状态 | 说明 |
|---|---|---|
| Task0 | ✅ 完成 | 全部开放问题已定稿，见 Plan 文档"Task 0 结论落定"章节 |
| Task1 | ✅ 完成+真机验证 | 项目骨架：配置/日志/DB+Redis连接/健康检查/metrics/优雅关闭；前端 Vite 脚手架；DB migration（一次性建齐全部 P0+预留字段）；Docker 部署文件 |
| Task2 | ✅ 完成+真机验证 | 用户体系与鉴权：注册/登录/访客模式/JWT签发校验/鉴权中间件（对应 T10-T15） |
| Task3 | ✅ 完成+真机验证 | 房间管理：房间列表（无需鉴权）+ 历史消息分页查询（对应 T20-T22） |
| Task4 | ✅ 完成+真机验证（含跨进程验证） | WS Gateway：连接鉴权、join/leave房间、心跳、Redis Pub/Sub 多实例广播、消息发送与落库、msgId 幂等去重（对应 T30-T37，**已吸收 Task5 的消息写入路径**，范围调整已记录在 Task4 work log） |

### 尚未开始的 Task（按 Plan 依赖顺序，供接手后规划）

| Task | 内容 | 依赖 | 备注 |
|---|---|---|---|
| Task6 | 可靠性与异常处理：限流、超时降级、优雅关闭（优雅关闭已在 Task1 做了骨架，限流/降级未做） | Task2-5 | `send_message` 目前无频率限制，只有内容长度限制 |
| Task7 | 可观测性完善：`/healthz`/`/readyz`/`/metrics`/结构化日志已在 Task1 做了基础版，Task7 需要补充 trace_id 全链路贯穿的完整性检查、更全的 metrics 指标 | Task1 | 已确认不接入 Prometheus/Grafana |
| Task8 | 系统安全性：XSS 转义、CORS、越权校验专项检查（目前只有鉴权中间件，未做系统性安全加固） | Task2-5 | |
| Task9 | 前端页面与交互：目前只有一个验证连通性的占位页，**登录/注册/房间列表/聊天界面等全部页面均未开发** | Task2,3,4,5接口 | 工作量较大，建议尽早并行启动 |
| Task10 | 性能与成本落地 | 收尾阶段 | |
| Task11 | 测试与代码质量：目前每个 Task 都补了单元测试，但没有统一的 lint 配置和集成测试 | 各功能Task | |
| Task12 | 部署与运行说明完善：docker-compose/Dockerfile 已写好，**但从未在真实 Docker 环境验证过**（沙箱无法跑嵌套容器），这是接手后**第一件事** | 全部功能Task | 见下方"三、转移到新环境的操作步骤" |
| Task13 | 演示准备与 AI 使用说明 | Task12 | |
| Task14 | 好友关系链（好友请求状态机/好友列表/在线态） | Task2 | **未开始**，是 Task15 私聊的前置依赖 |
| Task15 | 私聊（会话模型/私聊消息/WS私聊事件） | Task4,Task14 | **未开始** |
| Task16 | 多媒体消息（图片P0/语音文件P1） | Task5,Task15 | **未开始** |
| Task17 | 离线通知 Web Push（**已确认要做，非P1可选**） | Task9,Task14,Task15 | **未开始** |
| Task18 | 内容安全（敏感词/长度限制/举报） | Task5,Task15 | **未开始**（当前消息长度限制已有，敏感词/举报未做） |
| Task19 | AI-native schema 预留 + 关注事项功能 | Task2,3,5 | **schema 已在 migration 中建好**（`interaction_events`/`match_candidates`/`bot_action_log`/`is_bot`/`sender_type`/`embedding`等），但"关注事项"功能本身（UI+API）未开发 |
| Task20 | AI 推荐规则化简单演示 | Task19,Task14 | **未开始** |

> **建议的下一步顺序**：Task14（好友）→ Task15（私聊）→ 与此并行启动 Task9（前端页面，工作量大应尽早开始）
> → Task6/7/8（工程质量类，随功能推进持续叠加，不要拖到最后）→ Task16/17/18/19/20（P0/P1功能收尾）
> → Task10/11/12/13（收尾）。这与 Plan 文档"执行顺序与并行性"章节一致。

---

## 三、转移到新环境的操作步骤（有 Docker 权限后要做的第一批事）

### 1. 把代码转移过去

**当前项目未初始化 git 仓库**（`git status` 报错，说明连 `git init` 都还没做）。转移方式二选一：

- **方式A（推荐）**：直接把整个 `xingqudazi_im_project` 目录打包/rsync 到新环境（含 `.gitignore`，
  但注意排除 `client/node_modules`、`server` 编译产物等，`.gitignore` 已经配置好这些规则）。
- **方式B**：在新环境先 `git init`，`git add -A && git commit`，再 push 到远程仓库，之后走正常 git 流程。

无论哪种方式，转移后请确认这些文件都在：`server/`、`client/`（不含 `node_modules`，需要 `npm install`
重新生成）、`deploy/`、`migrations/`、`scripts/`、`docs/`、`testcase/`、`README.md`、`project-meta.yaml`、`.gitignore`。

### 2. 验证 Docker 环境本身可用

```bash
docker --version
docker compose version   # 确认不是 "docker-compose: not found"（沙箱环境踩过这个坑）
docker run --rm hello-world
```

### 3. 一键起服务（**这是从未被真实验证过的一步，重点关注**）

```bash
cd xingqudazi_im_project
docker compose -f deploy/docker-compose.yml up -d --build
```

**已知背景**：`server/Dockerfile` 和 `client/Dockerfile` 之前各修复过一处 `COPY` 路径 bug
（build context 是项目根目录而不是各自子目录，已修复并写入代码），但**修复后从未跑过真实
`docker build`**（沙箱无法跑嵌套容器），如果这一步报错，大概率还是路径/依赖类问题，请把报错
贴给我，我按同样方式定位修复。

### 4. 观察启动状态

```bash
docker compose -f deploy/docker-compose.yml logs -f
docker compose -f deploy/docker-compose.yml ps   # STATUS 列应显示 (healthy)
```

### 5. 端到端验证

```bash
./scripts/smoke_test.sh http://localhost:8080

# 数据库初始化确认（12张表 + 4条种子房间数据）
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U im_user -d xingqudazi_im -c "\dt"
docker compose -f deploy/docker-compose.yml exec postgres \
  psql -U im_user -d xingqudazi_im -c "SELECT name FROM rooms;"

# 前端访问
curl -I http://localhost:8081
```

按 `testcase/00-testcase-plan.md` 里的 T01-T37（P0-1健康检查 ~ Task4部分），逐条用 `curl`/WS 客户端
重新跑一遍作为"镜像层面"的最终确认（此前所有验证都是在原生二进制模式下做的，不是镜像模式，逻辑
上应该一致，但镜像模式的网络/DNS解析/健康检查依赖顺序是全新验证点）。

### 6. 验证通过后，把这份交接文档标记为"已在真实 Docker 环境验证通过"，并更新 `README.md` 的进度说明
（目前 README 只写到 Task1，需要同步补充 Task2-4 的完成情况，可参考本文档"二、当前完成情况总览"）。

---

## 四、开发环境注意事项

- **本地环境变量**：`server/.env.example`、`client/.env.example` 已提供，复制为 `.env` 按需修改。
  `JWT_SECRET` 默认值不安全（代码里有运行时 WARN 提示），本地开发无影响，正式使用前务必改掉。
- **端口占用**：后端 `8080`、前端（nginx容器）`8081`、PostgreSQL `5432`、Redis `6379`，与
  `docker-compose.yml` 一致。
- **数据库 schema**：`migrations/0001_init_schema.up.sql` 是唯一的迁移文件，一次性建齐了全部
  P0 表 + AI-native 远期预留字段（`pgvector` 相关列），docker-compose 里 postgres 服务会在首次启动
  时自动执行该脚本（挂载到 `/docker-entrypoint-initdb.d/`）。**后续新增表/字段请新增
  `0002_xxx.up/down.sql` 之类的增量迁移文件，不要直接改 `0001`**（除非还没有任何人跑过它）。
- **单元测试**：每个已完成 Task 都同步写了单测（`go test ./... -v` 应全绿），新增功能请延续这个习惯，
  优先把可测的业务逻辑抽成不依赖真实 DB/Redis 的纯函数或依赖接口（`XxxStore` 模式），方便无环境单测。
- **代码格式**：提交前记得跑 `gofmt -l .`（应无输出）+ `go vet ./...`。

---

## 五、如实说明的遗留/风险项

1. ~~**Docker 镜像层面从未真实验证**~~ **已于 2026-08-17 在具备完整 Docker 权限的新环境验证通过**，
   见下方"六、Docker 镜像模式验证结果（新环境补充）"。
2. ~~**跨进程多实例广播验证用的是临时脚本**~~ **已在新环境用两个独立容器（不同 PID、共享同一
   Redis/PostgreSQL）复现验证通过**，见下方"六"。
3. ~~**限流（Task6）尚未实现**~~ **已实现并真机验证通过**（连接级双层限流：突发窗口+
   分钟配额），见 `docs/08-task6-7-8-work-log.md`。
4. ~~**前端只有占位页**~~ **Task9 核心页面（登录注册/房间列表/聊天页）已实现并通过真实
   浏览器端到端验证**，见 `docs/11-task9-work-log.md`。
5. ~~**好友/私聊/关注事项/AI推荐/Web Push 前端页面未开发**~~ **已于 2026-08-18 补齐全部
   页面并完成功能闭环**（含发现并补齐 3 个后端接口缺口：好友请求列表事后查看、按用户名
   查找用户、批量查用户名修复群聊"用户ID前8位"问题；WebSocket 连接提升为应用级共享连接），
   已用 `playwright-cli` 驱动 3 个真实浏览器会话联动验证全部交互链路，见
   `docs/12-frontend-closure-work-log.md`。
6. ~~**README.md 进度描述滞后**~~ **已持续同步更新，当前反映 Task1-8、9(完整闭环)、14-20
   完成状态**（见 `README.md`）。
7. ~~**Task10-13 收尾未开始**~~ **已于 2026-08-18 完成**：Task10（索引补齐/连接池
   可配置/静态资源缓存/镜像体积复核）、Task11（`.golangci.yml` 统一 lint 配置全绿 +
   `scripts/integration_test.py` 45项黑盒接口回归）、Task12（`docs/13-architecture-
   and-adr.md` 架构图+ADR）、Task13（`scripts/demo_seed.py` + `docs/14-demo-guide.md`
   + `docs/15-ai-usage-notes.md`）。**Plan 文档 Task0-Task20 至此全部完成。**
8. ~~**用户验收反馈的能力缺口**~~ **已于 2026-08-18 完成**：修复"注册纯数字用户名无
   正确提示"缺陷（真实根因是密码长度提示不一致）；结合课题9个考察方向做差距盘点后，
   按优先级补齐 WS 断线自动重连（真机验证：容器停止/启动模拟断线，无需手动刷新自动
   恢复）、登录接口暴力破解防护（IP级限流，429）、前端自动化测试基础设施（vitest+
   testing-library，新增15项单测）。详见
   `docs/17-gap-closure-work-log.md`；集成测试脚本已扩充到46项断言。
9. ~~**群聊内无法快捷加好友**~~ **已于 2026-08-18 完成**：新增房间聊天页点击发言人
   用户名弹出"加为好友"气泡（`UserActionPopover`），不涉及后端改动，纯复用既有好友
   三件套接口；已用双浏览器会话（alice/bob）联动验证 C1-C8 全部通过（含点击外部关闭、
   收到新消息自动关闭、发起→接受完整闭环），详见 `docs/17-gap-closure-work-log.md`
   第六节、`testcase/00-testcase-plan.md`。
10. ~~**结合课题9个考察方向的诚实差距（安全性/可靠性/测试/性能/部署）**~~
    **已于 2026-08-19 完成**：密码复杂度校验、CORS 白名单可配置、**JWT 登出黑名单**
    （登出后旧 token 立即失效，HTTP+WS 两条鉴权路径均生效）、Web Push 发送失败重试
    （网络错误/5xx最多重试3次）、repository 层补充9项真实数据库单测（此前覆盖率0%）、
    新增 CI 配置 `.github/workflows/ci.yml`（如实标注未在真实CI环境跑过，因无远程
    git仓库）、首次真实压测（`scripts/load_test.py`，HTTP 100并发吞吐2486 req/s，
    WS端到端p50≈170ms）、数据库备份/恢复脚本+真机验证（含真实踩坑修复：`pg_dump`
    需 `--clean --if-exists` 才能幂等恢复）、好友/私聊未读从圆点升级为精确数字。
    集成测试脚本扩充到 **54 项**。详见 `docs/17-gap-closure-work-log.md` 第七节、
    `docs/18-backup-and-restore.md`。
11. ~~**测试覆盖率不均衡 + 部署方式仅停留在单机demo**~~ **已于 2026-08-19 完成（后续状态已更新）**：
    (1) 测试覆盖率专项补齐及后续机器人/训练数据管道测试：`internal/api` 0.3%→75.5%、
    `internal/middleware`0%→94.7%、`internal/ws`13.6%→61.2%、`internal/config`
    0%→97.6%、`internal/repository`0%→30.4%、`internal/service`67.4%→67.6%、
    新增`pkg/llm`82.4%。当前全项目加权总体为**52.8%**（`go tool cover -func`
    实测；早期58.0%因后续新增0%覆盖率命令层导致分母扩大而更新，非质量回退），
    详见 `docs/19-test-coverage-work-log.md`；
    (2) 更丰富的部署与运行时测试：新增 instance_id 可观测性字段 + 多实例部署
    overlay（`deploy/docker-compose.multi-instance.yml` + nginx负载均衡），用
    **两个真实独立进程**验证Redis Pub/Sub多实例扇出；并用真实`docker stop`/
    `start`验证实例故障自动降级（20/20请求成功）、Redis/Postgres故障后自动恢复。
    当前仍是**本地/同机多容器验证**，尚未部署任何公网云服务器；云端演示与独立
    压测计划见`docs/25-current-status-and-cloud-deployment-plan.md`。
12. ~~**"LLM驱动机器人代替用户社交"设计只有schema预留，从未真正验证**~~
    **已于 2026-08-19 完成**：设计之初讨论过的远期方向（★13/★14 已确认透明度
    披露/opt-in 决策），此前 `users.is_bot`/`bot_action_log` 等字段业务代码
    从未读写过。新增 `server/cmd/bot`（独立验证工具）+ `server/pkg/llm`
    （Qwen/DashScope 客户端）补一个最小闭环：机器人账号标记 `is_bot=true`
    → 调用 Qwen LLM 真实生成消息内容（非硬编码）→ 通过真实 WS 协议发到房间
    （`sender_type` 由服务端权威判定，不信任客户端）→ 写入 `bot_action_log`
    （此前从未被写入过的表）→ 向真实注册用户发起好友请求。数据库直查 +
    `playwright-cli` 真机验证前端正确展示"（机器人）"标识。如实标注局限：
    仍是一次性运行的确定性工具，非自主决策的常驻服务。详见
    `docs/23-llm-bot-minimal-validation-work-log.md`、
    `testcase/00-testcase-plan.md` B1-B6。
13. ~~**`interaction_events` 表从建表起从未被写入，行为数据训练管道缺失**~~
    **已于 2026-08-19 完成**：诚实检查发现这张 Plan 原始设计里"训练信号核心
    来源"的表一直是空的。新增 `ws.Hub`/`service.FriendService` 可选注入的
    `EventRecorder`（nil-safe，与 `PushNotifier` 同款模式），让
    `join_room`/`send_message`/`add_friend_request` 三类事件真实写入（只在
    行为真正成功发生时记录，失败/拦截/重复分支不产生噪音）；新增
    `cmd/export_training_data` 验证"数据库原始行到结构化训练格式"链路可行。
    真机验证：机器人触发事件 + 两个全新账号发起好友请求，导出 JSON 均正确。
    如实标注局限：历史数据永久缺失、`keywords`仍非结构化、无数据使用授权
    字段、无周期性画像聚合、账号删除仍是级联硬删除无归档流程。详见
    `docs/24-training-data-pipeline-work-log.md`、
    `testcase/00-testcase-plan.md` D1-D5。
14. **公网云端部署与独立压测（待完成，不应误报为已部署）**：当前本地Compose
    环境健康且同机多实例验证已通过，但尚无公网URL；此前Lighthouse `ap-guangzhou`
    未找到运行中实例。获得用户提供的可用实例地域后，按`docs/25-current-status-and-
    cloud-deployment-plan.md`部署（推荐4核8GB、80GB SSD、10Mbps公网），并以独立
    压测端执行梯度压测/采集服务端资源曲线，届时再更新本交接文档中的公网地址与
    云端性能数据。

---

## 六、Docker 镜像模式验证结果（新环境补充，2026-08-17）

在具备完整 Docker 权限的新环境执行"三、转移到新环境的操作步骤"，结果如下：

### 1. 一键起服务

`docker compose -f deploy/docker-compose.yml up -d --build` 一次性构建成功，
未出现交接文档担心的 `COPY` 路径问题。`postgres`/`redis`/`server`/`client` 四个容器均达到
`healthy`/`Up` 状态。

### 2. 部署后最小验证

- `./scripts/smoke_test.sh` 通过（`/healthz`、`/readyz` 均正常）。
- DB 表结构：12 张表齐全；种子房间数据：数码/追番/运动/美食 4 个房间均存在。
- 前端 `curl -I http://localhost:8081` 返回 200。

### 3. 镜像模式下逐条复测 Testcase（`testcase/00-testcase-plan.md`）

| 用例 | 结果 | 用例 | 结果 |
|---|---|---|---|
| T10-T15（注册/登录/访客） | ✅ 全部通过 | T33（send_message 广播+落库） | ✅ 通过 |
| T20（房间列表） | ✅ 通过（前序已验证） | T34（重复 msg_id 幂等去重） | ✅ 通过 |
| T21（历史消息-合法房间） | ✅ 通过 | T35（超长内容拒绝） | ✅ 通过 |
| T22（历史消息-非法房间404） | ✅ 通过 | T37（leave_room 广播人数-1） | ✅ 通过 |
| T30（WS建连-合法token） | ✅ 通过 | **T36（跨实例 Redis Pub/Sub 广播）** | ✅ **通过（见下）** |
| T31（WS建连-无token拒绝） | ✅ 通过 | | |
| T32（join_room + 人数广播 + 重复join幂等） | ✅ 通过 | | |

### 4. T36 跨实例广播——真实跨容器验证（非同进程模拟）

在同一个 `docker compose` 网络（`deploy_default`）内，用 `docker run` 额外起了一个**完全独立的
第二容器**（不同容器/PID，镜像与 compose 内 `server` 相同，仅端口不同：`8090`），与主实例
（`8080`）共享同一个 `postgres`/`redis`：

- 用户 A 连接实例1（`:8080`）加入房间，用户 B 连接实例2（`:8090`）加入同一房间；
- 双方 join 均触发跨实例的在线人数广播同步（两个实例上看到的 `online_count` 一致）；
- A 在实例1发消息 → 实例2 的订阅循环收到 Redis 广播 → 真实推送给 B，`msg_id`/`content` 完全一致；
- 验证完成后已清理临时容器（`docker rm -f im_server_instance2`），只保留 compose 管理的主实例。

**结论**：Redis Pub/Sub 多实例扇出架构在真实 Docker 多容器环境下验证生效，此前沙箱环境因无
`cap_sys_admin` 无法验证的最大风险项已清除。

至此，**交接文档"三、转移到新环境的操作步骤"全部完成，Task1-4 已在镜像模式下全量复核通过**，
可以放心在此基础上继续 Task14（好友关系链）等后续开发。
