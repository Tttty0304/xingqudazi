# Task9 工作记录：前端页面与交互（登录注册/房间列表/聊天页）

> 日期：2026-08-18
> 范围：按 Plan Task9 定义（登录注册/空态/加载态/错误态/房间列表/聊天页），本轮先交付
> 核心可体验闭环；好友/私聊/关注事项/AI推荐等后端已完成但暂无对应 UI 的功能，留待后续
> 迭代补充（已在下方"遗留"说明，非遗漏）。

## 技术选型

- 新增 `react-router-dom@7`（React 19 兼容），用于登录页/受保护路由/房间列表/聊天页的
  路由管理。
- 沿用既有 Vite + React + TypeScript 骨架，不引入额外 UI 框架（保持依赖精简）。
- 状态管理：`AuthContext`（token/userId/username 存 localStorage，跨刷新保持登录态），
  不引入 Redux/Zustand（当前页面复杂度不需要）。
- WS 连接：`useWebSocket` 通用 hook，管理连接生命周期，事件处理逻辑由调用方（聊天页）
  通过回调完成，为后续私聊页复用同一套连接管理逻辑预留了扩展点。

## 代码改动清单

- `src/types.ts`：与后端 API/WS 协议对齐的前端类型定义。
- `src/api/client.ts`：统一 fetch 封装（自动带 Authorization、统一错误码转中文提示）、
  `serverOrigin()`/`resolveWsUrl()`/`resolveMediaUrl()`（后端 `/ws`、`/uploads` 挂在
  服务根路径而非 `/api` 前缀下，需要单独解析，见下方"真机验证中发现的 bug"）。
- `src/context/AuthContext.tsx`：登录/注册/访客登录/退出登录。
- `src/hooks/useWebSocket.ts`：通用 WS 连接 hook。
- `src/components/ProtectedRoute.tsx`：未登录访问受保护路由重定向到 `/login`。
- `src/components/Layout.tsx`+`.css`：登录后页面外壳（导航+用户信息+退出）。
- `src/pages/AuthPage.tsx`+`.css`：登录/注册合一 + 访客快速进入，覆盖加载态（提交中禁用
  按钮）/错误态（中文提示）/成功态提示。
- `src/pages/RoomListPage.tsx`+`.css`：房间列表，覆盖加载态/空态/错误态。
- `src/pages/RoomChatPage.tsx`+`.css`：聊天页——历史消息加载（T21）、WS 加入/离开房间
  （T30/T32/T37）、实时收发文本消息（T33）、图片消息发送与展示（Task16/T90-T93）、
  在线人数实时更新、连接状态提示。
- `App.tsx`/`main.tsx`：接入 `BrowserRouter`+路由表。
- `index.css`：修正 Vite 模板遗留的 `#root { width:1126px; text-align:center }`
  （与聊天页/表单布局冲突，已按需要调整为弹性布局+左对齐）。
- 删除 `App.css`（Task1 占位页样式，已被路由化页面替代，无引用方）。

## 真机验证中发现并修复的 3 个真实 bug（非假设性问题）

浏览器端到端验证（见下方"验证结果"）过程中，命中了 3 个只有真实浏览器环境才能暴露的
问题，均已修复并二次验证通过：

1. **`VITE_API_BASE_URL` 双重 `/api` 前缀 bug**：`deploy/docker-compose.yml` 与
   `client/Dockerfile` 原先都把 `VITE_API_BASE_URL` 构建参数默认设为 `/api`，而前端
   代码里所有请求路径本身已经带 `/api/xxx` 前缀，导致生产构建下的真实请求变成
   `/api/api/auth/register`，触发 404。浏览器控制台报错精确定位到这个问题
   （`Failed to load resource: 404 @ http://localhost:8081/api/api/auth/register`）。
   修复：两处默认值改为空字符串（同源相对路径，前端路径自带的 `/api` 前缀已经足够）。
2. **WS 端点 URL 拼接错误**：`API_BASE` 为相对路径 `/api` 时，简单地把协议前缀替换成
   `ws` 会得到 `/api/ws`，但后端 `/ws` 挂在服务根路径、不带 `/api` 前缀，nginx 也只代理
   了 `/ws`（不是 `/api/ws`）。修复：新增 `serverOrigin()`，按 `API_BASE` 是绝对/相对
   URL 分别取正确的 origin，`resolveWsUrl()`/`resolveMediaUrl()` 复用该函数。
3. **`/uploads` 静态资源未被 nginx 代理**：`deploy/nginx.conf` 原先只有 `/`、`/api/`、
   `/ws` 三个 location，图片消息的 URL（`/uploads/xxx.png`）在浏览器里（走 nginx :8081）
   会 404——即使后端本身用 `gin.Static` 正确提供了这个路径（在直连 :8080 时能访问）。
   修复：新增 `location /uploads/` 反代到后端。

这三个问题在此前所有轮次的"真机验证"里都没有被发现，因为此前的验证脚本全部是直接用
`curl`/Python 脚本连 `:8080`（后端直连），完全绕开了 nginx/前端构建产物这一层——这也
印证了"浏览器端到端验证"和"接口直连验证"是两种互补但不能互相替代的验证手段。

## 验证结果（真实 Docker 容器环境 + 真实浏览器，非模拟）

使用 `playwright-cli` 驱动真实浏览器（非 headless 断言脚本），完整走通以下闭环：

| 场景 | 结果 |
|---|---|
| 编译（`tsc -b`）+ 生产构建（`vite build`）+ lint（`oxlint`） | ✅ 0 错误（1条 fast-refresh 最佳实践警告，非错误） |
| 未登录访问 `/rooms` -> 重定向到 `/login` | ✅ |
| 注册新账号 -> 提示"注册成功，请登录"并自动切回登录 Tab | ✅ |
| 用刚注册的账号登录 -> 跳转 `/rooms`，Header 显示正确用户名 | ✅ |
| 错误密码登录 -> 显示"用户名或密码错误" | ✅ |
| 访客快速登录 -> 跳转 `/rooms` | ✅ |
| 房间列表：4 个种子房间正确渲染（名称/主题/在线人数） | ✅ |
| 进入房间 -> WS 连接建立 -> 收到 `joined` -> 在线人数变为 1 | ✅ |
| 历史消息正确回填（含此前各轮真机验证遗留的文本/图片/XSS测试消息，均正确展示，XSS payload 显示为转义后的字面文本，未被执行） | ✅ |
| 发送文本消息 -> 实时出现在消息列表，标记为"我" | ✅ |
| 上传图片 -> 后端返回 URL -> 作为图片消息实时展示 | ✅ |
| 返回房间列表（触发 `leave_room`） -> 该房间在线人数恢复为 0 | ✅ |
| 退出登录 -> 跳转 `/login`，本地登录态清除 | ✅ |
| 全量后端回归（Task4/6/8/14/16/17/18/19/20 既有验证脚本） | ✅ 全部通过，无回归 |

## 遗留（如实说明，非遗漏）

- **好友/私聊/关注事项/AI推荐/Web Push 均暂无前端页面**：这些后端能力（Task14/15/17/
  19/20）已完整实现并验证，但 Plan 中 Task9 的原始范围只明确列出"房间列表/聊天页/登录
  注册/空态/加载态/错误态"，本轮严格按此范围交付；上述功能的 UI 属于后续迭代（不阻塞
  当前"打开浏览器能看到真实产品"这一诉求，核心体验闭环已经打通）。
- **聊天气泡对方发送者展示为 `用户 xxxxxxxx`（用户ID前8位）**，非真实用户名——后端
  `message_received`/历史消息接口目前只返回 `sender_id`，没有返回用户名，且没有"批量按
  ID查用户名"的接口；如需展示真实用户名，需要后端补一个轻量的用户信息查询接口（不在
  本轮 Task9 范围内新增后端接口，如实记录为已知简化点）。
- **深色/浅色主题自适应**（`prefers-color-scheme`）沿用 Task1 骨架已有的 CSS 变量机制，
  但聊天气泡背景色为硬编码深色，浅色模式下对比度不是最优（可读但非精细打磨），下轮如
  需要可优化。
- 图片消息在服务端容器重建后会因本地磁盘存储丢失（Task16 已记录的已知简化点），前端
  展示时会显示图裂图标——这是预期行为，不是本轮前端 bug。

## 后续建议

按当前节奏，下一步可以：（a）继续补齐好友/私聊页面 UI（把 Task14/15 的后端能力接上
真实界面）；或（b）先做收尾（Task10-13）+ 部署文档定稿。建议以用户下一步反馈为准。
