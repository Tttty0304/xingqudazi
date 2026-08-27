# 七项质量缺口收敛记录（2026-08-27）

## 已落地并验收

1. **生产安全与部署**：新增 `.env.example`、`.env.production.example`、生产 Compose 与 Caddy 入口。生产模式拒绝短 JWT 密钥、不安全 Cookie 与 CORS 通配符；数据库、Redis、后端和 Grafana/Prometheus/Alertmanager 管理端均不直接暴露公网。
2. **浏览器会话安全**：登录/访客登录设置 `HttpOnly + SameSite=Lax` Cookie；生产不再在 JSON 返回 JWT。HTTP、WebSocket 和退出登录同时兼容 Cookie 与非浏览器 Bearer 调用。`/api/auth/session` 防止刷新页面时只信任 localStorage。
3. **可靠实时消息**：前端对两类发消息事件保留同一 `msg_id`，4 秒间隔最多重试 4 次；收到服务器回执后清除。网络断开时保留队列并在重连后重发，服务端唯一约束保证不重复落库/广播。
4. **可观测性**：`/metrics` 改为 Prometheus text exposition，旧 JSON 保留在 `/metrics.json`；提供 Prometheus、Grafana、Alertmanager 叠加编排、服务概览面板和两项规则（服务不可抓取、WS 错误突增）。未配置外部通知凭据时，告警在 Alertmanager UI 可见；云端应再接入团队的企业微信/钉钉/邮件 Webhook。
5. **性能验证能力**：压测脚本支持 HTTP 梯度档位，WS 压测兼容 Cookie 会话；本机最小梯度基线 `10→20` 并发均为 100% 成功。该基线不等同容量结论，云端应按 `50→100→200`、独立发压机、容器资源曲线和 10–30 分钟稳态测试复验。
6. **产品/API 交付**：已实现用户创建房间（前端入口、服务端格式校验、幂等写入）、个人资料与本地上传头像、私聊数据库持久未读游标和会话列表 `unread_count`；新增 `docs/openapi.yaml` 作为 HTTP/WS 交付契约。
7. **CI/配置一致性**：CI 已使用 Go 1.25 与 Node 22，与项目依赖保持一致；生产编排经 `docker compose config --quiet` 校验。

## 当前仍需用户决定的唯一业务前置

“忘记密码”需要可验证的账户恢复通道。当前账户模型只有用户名，没有邮箱/手机号，也没有可授权使用的邮件、短信或第三方身份服务；在缺少该能力时生成“找回链接/验证码”会把凭据泄露给请求方，不能作为安全实现上线。

待确认的最小决策：新增邮箱字段并使用 SMTP/Resend 等邮件服务，或接入手机短信/第三方登录。确认服务与凭据后，再实现重置令牌、通用响应防枚举、限流、单次使用和到期机制。

## 本轮可复现验证

- `go test -count=1 ./...`：通过（Cookie、CORS、会话、指标、消息可靠性等）。
- 前端 `npm run build && npm test -- --run`：通过，3 个测试文件 / 17 项测试。
- Docker Compose 生产+监控编排：语法和变量引用校验通过。
- 本地服务：`/healthz` 200、`/metrics` 200 且为 Prometheus text 格式。
- 端到端：注册、Cookie 登录、会话校验、创建房间及相同 `Idempotency-Key` 重放均通过；重放响应一致。

## 云端部署前清单

1. 提供运行中的云主机、域名和 DNS A/AAAA 记录（指向云主机）；开放 80/443/SSH。
2. 从 `.env.production.example` 生成仅保存在云主机上的 `.env.production`，填入强随机密钥和正式域名。
3. 如启用监控，设置 Grafana 管理员强密码，并选择告警通知 Webhook。
4. Caddy 自动申请 TLS；域名解析未生效前不要启动正式 HTTPS 编排。
