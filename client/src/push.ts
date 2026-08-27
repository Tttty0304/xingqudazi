/**
 * Web Push 前端最小实现（Task17，本轮新增；此前只有后端契约与文档，
 * 前端 Service Worker/订阅逻辑完全缺失，属于半成品，本次补齐）。
 * 后端接口：`GET /api/push/vapid-public-key`（无需鉴权）、
 * `POST /api/push/subscriptions`、`DELETE /api/push/subscriptions`。
 */
import { api } from './api/client'

/** VAPID 公钥是 URL-safe base64，需转成 `PushManager.subscribe` 要求的 Uint8Array。 */
function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const rawData = window.atob(base64)
  const outputArray = new Uint8Array(rawData.length)
  for (let i = 0; i < rawData.length; i++) {
    outputArray[i] = rawData.charCodeAt(i)
  }
  return outputArray
}

export function isPushSupported(): boolean {
  return typeof navigator !== 'undefined' && 'serviceWorker' in navigator && 'PushManager' in window
}

async function registerServiceWorker(): Promise<ServiceWorkerRegistration | null> {
  if (!isPushSupported()) return null
  return navigator.serviceWorker.register('/sw.js')
}

export async function getExistingSubscription(): Promise<PushSubscription | null> {
  if (!isPushSupported()) return null
  try {
    const reg = await navigator.serviceWorker.ready
    return reg.pushManager.getSubscription()
  } catch {
    return null
  }
}

/** 请求通知权限 + 创建浏览器推送订阅 + 上报后端，全部成功才返回。 */
export async function subscribeToPush(token: string | null): Promise<void> {
  if (!isPushSupported()) {
    throw new Error('当前浏览器不支持通知推送')
  }
  const reg = await registerServiceWorker()
  if (!reg) {
    throw new Error('Service Worker 注册失败')
  }
  const permission = await Notification.requestPermission()
  if (permission !== 'granted') {
    throw new Error('未授予通知权限')
  }

  const { public_key } = await api.get<{ public_key: string }>('/api/push/vapid-public-key')
  const sub = await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(public_key) as BufferSource,
  })
  const json = sub.toJSON() as { endpoint?: string; keys?: { p256dh?: string; auth?: string } }
  if (!json.endpoint || !json.keys?.p256dh || !json.keys?.auth) {
    throw new Error('订阅信息不完整')
  }
  await api.post(
    '/api/push/subscriptions',
    { endpoint: json.endpoint, keys: { p256dh: json.keys.p256dh, auth: json.keys.auth } },
    token,
  )
}

/** 取消浏览器订阅 + 通知后端清理。 */
export async function unsubscribeFromPush(token: string | null): Promise<void> {
  const sub = await getExistingSubscription()
  if (!sub) return
  const endpoint = sub.endpoint
  await sub.unsubscribe()
  await api.del('/api/push/subscriptions', { endpoint }, token)
}
