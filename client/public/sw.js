/**
 * Task17 Web Push 的 Service Worker（本轮新增前端实现，后端契约见
 * server/internal/service/push_service.go pushPayload：{"title":"...","body":"..."}）。
 * 仅处理两类事件：收到推送时展示系统通知；点击通知时聚焦/打开已有页面。
 */
self.addEventListener('push', (event) => {
  let data = { title: '新的通知', body: '' }
  try {
    if (event.data) {
      data = event.data.json()
    }
  } catch {
    // 解析失败时使用默认文案，不阻断通知展示。
  }

  event.waitUntil(
    self.registration.showNotification(data.title || '新的通知', {
      body: data.body || '',
      icon: '/favicon.svg',
      badge: '/favicon.svg',
    }),
  )
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  event.waitUntil(
    self.clients.matchAll({ type: 'window' }).then((clientList) => {
      for (const client of clientList) {
        if ('focus' in client) return client.focus()
      }
      if (self.clients.openWindow) return self.clients.openWindow('/')
      return undefined
    }),
  )
})
