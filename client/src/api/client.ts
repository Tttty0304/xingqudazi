/**
 * 统一的 HTTP API 客户端：优先使用同源 HttpOnly 会话 Cookie，兼容非浏览器调用方
 * 传入的 Authorization 头，并统一错误处理
 * （后端错误响应统一形如 `{ "error": "some_code" }`，见各 api/*.go handler）。
 */

export const API_BASE = import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080'
// 仅表示“当前浏览器已由 HttpOnly Cookie 认证”，不是 JWT，绝不能写入请求头或持久化。
export const COOKIE_SESSION_TOKEN = '__cookie_session__'

/**
 * 计算后端服务的 origin（不含 `/api` 前缀）。当 `API_BASE` 是相对路径 `/api`
 * （Docker 构建默认值，同源部署避免 CORS）时，后端与前端共享同一个 origin
 * （nginx 反代到 server），直接用 `window.location.origin`；当 `API_BASE` 是
 * 绝对 URL（本地开发默认 `http://localhost:8080`）时，取其 origin。
 * `resolveWsUrl`（WS 端点）与图片消息 URL 拼接均复用本函数，因为后端
 * `/ws`、`/uploads` 都挂在服务根路径下，不带 `/api` 前缀（见 main.go）。
 */
export function serverOrigin(): string {
  return /^https?:\/\//.test(API_BASE) ? new URL(API_BASE).origin : window.location.origin
}

/**
 * 计算 WS 端点的完整 URL。后端 `/ws` 挂在服务根路径下，不带 `/api` 前缀
 * （见 server/cmd/server/main.go `router.GET("/ws", ...)`，与 `apiGroup` 平级），
 * 因此不能简单在 `API_BASE` 后面拼接 `/ws`——当 `API_BASE` 是相对路径 `/api`
 * 时，必须改用当前页面的 origin，而不是直接替换协议前缀（那样会得到错误的
 * `/api/ws`，与 nginx 只代理 `/ws` 不匹配）。
 */
export function resolveWsUrl(): string {
  return serverOrigin().replace(/^http/, 'ws') + '/ws'
}

/**
 * 把后端返回的媒体相对路径（如 `/uploads/xxx.png`，见 api/media.go）拼接成可
 * 直接在 <img> 中使用的绝对 URL；已是绝对 URL（http开头）时原样返回。
 */
export function resolveMediaUrl(path: string): string {
  if (/^https?:\/\//.test(path)) return path
  return serverOrigin() + path
}

export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string) {
    super(code)
    this.status = status
    this.code = code
  }
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  token?: string | null
  /** multipart 表单数据（图片上传），与 body 二选一。 */
  formData?: FormData
  /** 重试同一写命令时复用；未给定时为一次新命令生成 key。 */
  idempotencyKey?: string
}

export function newIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return `cmd-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = {}
  if (options.token && options.token !== COOKIE_SESSION_TOKEN) {
    headers['Authorization'] = `Bearer ${options.token}`
  }
  const method = options.method ?? 'GET'
  if (method !== 'GET') {
    headers['Idempotency-Key'] = options.idempotencyKey ?? newIdempotencyKey()
  }

  let body: BodyInit | undefined
  if (options.formData) {
    body = options.formData
    // 不手动设置 Content-Type，交给浏览器自动带上正确的 multipart boundary。
  } else if (options.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(options.body)
  }

  const res = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
    body,
    credentials: 'include',
  })

  const text = await res.text()
  const data = text ? JSON.parse(text) : {}

  if (!res.ok) {
    throw new ApiError(res.status, (data as { error?: string }).error ?? 'unknown_error')
  }
  return data as T
}

export const api = {
  get: <T>(path: string, token?: string | null) => request<T>(path, { method: 'GET', token }),
  post: <T>(path: string, body?: unknown, token?: string | null, idempotencyKey?: string) =>
    request<T>(path, { method: 'POST', body, token, idempotencyKey }),
  put: <T>(path: string, body?: unknown, token?: string | null, idempotencyKey?: string) =>
    request<T>(path, { method: 'PUT', body, token, idempotencyKey }),
  del: <T>(path: string, body?: unknown, token?: string | null, idempotencyKey?: string) =>
    request<T>(path, { method: 'DELETE', body, token, idempotencyKey }),
  upload: <T>(path: string, formData: FormData, token?: string | null, idempotencyKey?: string) =>
    request<T>(path, { method: 'POST', formData, token, idempotencyKey }),
}

/** 后端错误码 -> 中文提示文案，覆盖当前已实现接口（含 WS 事件）返回的全部 error code。 */
export const ERROR_MESSAGES: Record<string, string> = {
  username_taken: '用户名已被占用',
  invalid_password: '密码不符合要求（至少8位，需同时包含字母和数字）',
  invalid_username: '用户名格式不正确',
  invalid_credentials: '用户名或密码错误',
  guest_mode_disabled: '当前未开放访客模式',
  login_rate_limited: '登录尝试过于频繁，请稍后再试',
  invalid_token: '登录状态已失效，请重新登录',
  token_revoked: '登录状态已失效，请重新登录',
  missing_token: '请先登录',
  invalid_room_id: '房间ID格式不正确',

  invalid_room_name: '房间名称应为 2 到 64 个字符',
  invalid_room_topic: '房间简介不能超过 255 个字符',
  invalid_avatar_url: '头像必须使用本平台上传的图片',
  invalid_bio: '个人简介不能超过 280 个字符',
  room_not_found: '房间不存在',
  invalid_request_body: '请求参数不正确',
  invalid_request: '请求参数不正确',
  invalid_page: '页码格式不正确',
  invalid_size: '每页数量应在1到100之间',
  invalid_priority: '优先级应在1到5之间',
  invalid_reason: '举报理由不能为空且不能超过500个字符',
  invalid_idempotency_key: '请求重放标识格式不正确',
  idempotency_in_progress: '相同操作仍在处理中，请稍后重试',
  idempotency_unavailable: '当前无法安全重试该操作，请稍后再试',
  delivery_unconfirmed: '消息暂未确认送达，请检查网络后重试',
  unsupported_media_type: '不支持该图片格式',
  file_too_large: '图片文件过大',
  friend_required: '仅好友之间可以私聊',
  cannot_message_self: '不能给自己发私聊消息',
  cannot_friend_self: '不能添加自己为好友',
  user_not_found: '用户不存在',
  already_friends: '你们已经是好友了',
  friend_request_already_exists: '已经发过好友请求，请等待对方处理',
  friend_request_not_found: '好友请求不存在',
  already_resolved: '该请求已被处理过',
  not_friends: '你们还不是好友',
  invalid_action: '操作参数不正确',
  candidate_not_found: '推荐记录不存在',
  forbidden: '无权操作该记录',
  invalid_target_type: '举报目标类型不正确',
  report_target_not_found: '举报目标不存在',
  invalid_conversation_id: '会话ID格式不正确',
  conversation_not_found: '会话不存在',
  invalid_watch_topic: '关注事项参数不正确（关键词不能为空）',
  invalid_expires_at: '过期时间格式不正确',
  watch_topic_not_found: '关注事项不存在',
  invalid_push_subscription: '推送订阅信息不完整',
  // WS 事件返回的 error.code（见 server/internal/ws/message.go/hub.go）。
  rate_limited: '发送太频繁，请稍后再试',
  content_blocked: '消息内容包含敏感词，已被拦截',
  content_too_long: '消息内容过长',
  invalid_message: '消息内容不完整',
  unsupported_content_type: '不支持的消息类型',
  invalid_message_format: '消息格式不正确',
  unknown_event_type: '未知的事件类型',
  missing_room_id: '缺少房间ID',
  internal_error: '服务器内部错误，请稍后重试',
}

export function translateCode(code: string): string {
  return ERROR_MESSAGES[code] ?? code
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    return translateCode(err.code)
  }
  return err instanceof Error ? err.message : String(err)
}
