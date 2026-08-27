# 部署及运行方式：更丰富的部署与运行时测试（工作日志，2026-08-19）

> 背景：结合课题9个考察方向的诚实评估中指出，此前"部署及运行方式"这一项只能算
> "能跑起来的部署"——单机 Docker Compose、单实例 server/postgres/redis，没有任何
> 手段验证架构文档里反复声明的"Redis Pub/Sub 多实例扇出为强制设计"是否真的成立，
> 也没有做过任何真实的故障注入测试。用户明确要求"在单机 demo 基础上更进一步，
> 进行更丰富的部署和运行时测试"，本轮据此实施并记录。

## 一、问题的本质：一直只是"理论上应该没问题"

`docs/13-architecture-and-adr.md` ★3 明确写了"Redis 作为多实例横向扩展的强制基座"，
但翻查此前所有的测试脚本、演示流程、真机验证记录，会发现一个共同的事实：**所有
场景永远只启动了一个 `server` 容器**。这意味着"跨实例广播"这条链路本质上一直是
"进程自己发布消息给自己订阅"——Redis 到底有没有真的承担起跨进程转发的职责，
在此之前完全没有被证实过，只是一句"设计上应该支持"的声明。

## 二、新增能力：instance_id 可观测性

在动手搭建多实例拓扑之前，先解决一个前置问题：**如果起了两个进程，怎么从外部
区分某个响应/某条 WS 连接到底落在哪一个物理实例上？** 此前完全没有这个手段。

- `server/cmd/server/main.go` 新增 `instanceID()`：优先读取 `INSTANCE_ID` 环境
  变量，否则回落到 `os.Hostname()`（Docker 默认把容器 ID 短哈希设为 hostname，
  同一 compose 服务的不同副本天然具有不同的值，无需额外配置即可工作）。
- `api.HealthHandler.InstanceID`：`/healthz`、`/readyz` 响应体新增 `instance_id`
  字段。
- `ws.Hub.instanceID` / `ws.ServerMessage.InstanceID`：WS 连接建立后的 `connected`
  事件新增 `instance_id` 字段，客户端/测试脚本据此知道自己这条连接落在哪个进程。

这个字段本身也是"日志、监控及可观测性"方向的一项真实增强（此前的结构化日志里
无法区分是哪个实例产生的，多实例部署下这是基本需求），是一举两得的改动。

## 三、多实例部署拓扑

新增两个文件，采用 Docker Compose **overlay** 方式叠加在默认 `docker-compose.yml`
之上，不修改、不影响默认单实例部署：

- `deploy/docker-compose.multi-instance.yml`：新增 `server2`（第二个独立的后端
  进程，`INSTANCE_ID=server2`，连接同一个共享 Postgres/Redis）+ `lb`（nginx，
  监听 8082 端口，作为 `server`/`server2` 的负载均衡器）。
- `deploy/nginx.lb.conf`：nginx upstream 配置，默认 round-robin 策略把 HTTP/WS
  连接轮流分配给两个后端实例；WS 路径显式设置 `Upgrade`/`Connection` 头与
  `proxy_read_timeout 90s`（心跳周期54s，留足余量避免代理层误断连接）。

用法：

```bash
# 启动多实例拓扑（叠加在默认部署之上）：
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.multi-instance.yml up -d --build

# 验证完毕后可以只停掉 overlay 引入的额外容器，恢复默认单实例状态：
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.multi-instance.yml stop server2 lb
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.multi-instance.yml rm -f server2 lb
```

## 四、验证脚本与真实结果

### 1. `scripts/multi_instance_test.py`（对应 testcase M1-M3）

- **负载均衡真实分流**：连续10次请求 `lb:8082/healthz`，收集响应中的
  `instance_id`，实测确认观测到了两个不同的值（不是名义上配了两个实际全落在
  同一个）。
- **跨实例群聊广播（核心验证）**：两个 WS 客户端分别连接负载均衡器，实测确认
  两条连接分别落在 `server`/`server2` 两个不同的物理进程上；A 发消息后，验证
  落在**另一个**实例的 B 能通过 Redis Pub/Sub 真实收到广播——这一步是本次验证
  真正要证明的事情：多实例扇出不再是一句空话。
- **跨实例私聊投递**：同一拓扑下验证私聊消息跨实例投递同样生效。

全部通过，输出结论：

```
全部通过：多实例横向扩展 + Redis Pub/Sub 跨实例扇出设计，
已用两个真实独立进程验证，不再只是理论声明。
```

### 2. `scripts/resilience_test.sh`（对应 testcase M4-M6）

用真实的 `docker stop`/`docker start` 制造故障，而非模拟或纸面推演：

- **场景1（实例故障自动降级）**：`docker stop server2` 后连续20次请求负载均衡器，
  实测 20/20 成功（服务整体未中断，负载均衡器自动把流量路由到剩余的健康实例
  `server`）；`docker start server2` 后15秒内重新观测到该实例被路由到，验证
  故障恢复后能自动重新加入负载均衡池，无需人工介入。
- **场景2（Redis 故障自动恢复）**：`docker stop redis` 后 `/readyz` 立即正确
  报告 `not_ready` 并明确指出是 `redis` 故障（而非笼统的"服务异常"）；
  `docker start redis` 后20秒内 `/readyz` 自动变回 `ready`，全程无需重启应用
  进程（验证的是应用层的连接池/客户端具备自动重连能力，而不是"重启一下就好了"
  这种更弱的保证）。
- **场景3（Postgres 故障自动恢复）**：同上，验证 `/readyz` 明确指出 `db` 故障，
  恢复后30秒内自动变回 `ready`。

实测结果：**6 项全部通过**。

## 五、已知边界（如实标注）

- 这仍然是**同一台机器上的多个 Docker 容器**，不是真正的多机/多可用区部署——
  没有验证网络分区、跨机房延迟等场景。
- 没有引入 Kubernetes/Nomad 等编排系统，因此也没有验证自动扩缩容、健康检查
  驱逐（liveness/readiness probe 触发的自动重建）、滚动升级等编排层能力——
  本轮验证的是"多进程 + Redis Pub/Sub 广播 + nginx 负载均衡"这一条应用架构
  核心链路是否真的通，而非生产级编排系统的完整能力矩阵。
- `resilience_test.sh` 场景1中不要求 100% 请求成功（允许 nginx 检测到后端不可用
  前的极短窗口内个别请求失败一次），这是负载均衡器"检测故障→标记临时下线"这个
  过程本身固有的短暂代价，属于预期内的正常现象。
- 多实例拓扑与验证脚本是**按需叠加的 overlay**，默认交付形态仍是单实例
  `docker-compose.yml`——多实例只是"这套架构具备横向扩展能力，且已被验证"，
  不代表默认部署方式发生了变化。

## 六、验证汇总

- `go build ./...`/`go vet ./...`/`golangci-lint run ./...`：全绿（instance_id
  相关改动无新增问题）。
- `go test ./...`：全部通过，含新增的 `TestHub_Register_SendsConnectedEventAndMarksOnline`
  对 `instance_id` 字段的断言。
- 多实例拓扑真机验证：`scripts/multi_instance_test.py` 3项全部通过，
  `scripts/resilience_test.sh` 6项全部通过。
- 验证完毕后已清理 `server2`/`lb` 容器，恢复默认单实例部署状态；在默认
  8080 端口上重新运行 `scripts/integration_test.py`（54项）与
  `scripts/smoke_test.sh`，确认本轮改动对默认单实例部署无回归。
