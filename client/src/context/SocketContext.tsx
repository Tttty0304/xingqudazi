import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { resolveWsUrl } from '../api/client'
import { useAuth } from './AuthContext'
import type { ClientEvent, ServerEvent } from '../types'

export type ConnectionStatus = 'connecting' | 'open' | 'reconnecting' | 'closed'
type Listener = (event: ServerEvent) => void
type ReliableEvent = Extract<ClientEvent, { type: 'send_message' | 'send_direct_message' }>

interface PendingMessage {
  event: ReliableEvent
  attempts: number
}

interface SocketContextValue {
  send: (event: ClientEvent) => void
	/** 发送需确认的聊天消息；断线或未收到服务端回显时会复用同一 msg_id 安全重试。 */
	sendReliable: (event: ReliableEvent) => void
  status: ConnectionStatus
  /** 注册一个事件监听器，返回取消订阅函数（组件卸载/依赖变化时务必调用）。 */
  subscribe: (listener: Listener) => () => void
}

const SocketContext = createContext<SocketContextValue | null>(null)

// Task21（能力补齐：WS 自动重连）：指数退避参数。此前网络抖动/服务端短暂重启导致
// 连接断开后，`status` 变为 `closed` 就再也不会自己恢复，用户必须手动刷新页面才能
// 继续收发消息——这是真实影响使用体验的缺口，而非"已知的简化点"。
const RECONNECT_BASE_DELAY_MS = 1000
const RECONNECT_MAX_DELAY_MS = 10000
const DELIVERY_RETRY_DELAY_MS = 4000
const DELIVERY_MAX_ATTEMPTS = 4
const PENDING_MESSAGES_STORAGE_KEY = 'xingqudazi_im_pending_messages'

function loadPendingMessages(): Map<string, PendingMessage> {
	try {
		const raw = sessionStorage.getItem(PENDING_MESSAGES_STORAGE_KEY)
		if (!raw) return new Map()
		const entries = JSON.parse(raw) as Array<[string, PendingMessage]>
		return new Map(entries.filter(([id, pending]) => Boolean(id) && pending?.event?.msg_id))
	} catch {
		return new Map()
	}
}

/**
 * 应用级别唯一的 WebSocket 连接（替代此前"每个用到 WS 的页面各自建立一条连接"的设计）。
 * 好友请求通知、私聊消息等事件可能在用户停留在任意页面（房间列表/好友页/关注事项页等）
 * 时到达，若连接仅在聊天页内建立，会导致"只有正好开着聊天页才能收到通知"的半成品体验，
 * 因此把连接上提到 `SocketProvider`（挂载在 `AuthProvider` 之下、路由树之上），任意页面
 * 通过 `useSocket().subscribe(...)` 注册自己关心的事件，多个订阅方互不影响。
 *
 * 断线自动重连：非主动关闭（网络抖动、服务端重启、代理超时等）触发 `onclose` 后，用
 * 指数退避（1s → 2s → 4s → 8s → 封顶 10s）自动重建连接，重连成功后已注册的 `subscribe`
 * 监听器无需重新注册（`listenersRef` 与具体连接对象解耦）；各页面自身对 `status` 的
 * `useEffect` 依赖（如加入房间）会随 `status` 变为 `open` 自动重新执行，天然完成"重连后
 * 自动重新加入房间"，不需要额外的显式重放逻辑。
 */
export function SocketProvider({ children }: { children: ReactNode }) {
  const { token } = useAuth()
  const wsRef = useRef<WebSocket | null>(null)
  const listenersRef = useRef<Set<Listener>>(new Set())
	const pendingRef = useRef<Map<string, PendingMessage>>(loadPendingMessages())
	const retryTimersRef = useRef<Map<string, ReturnType<typeof setTimeout>>>(new Map())
  const [status, setStatus] = useState<ConnectionStatus>('connecting')

	const persistPending = useCallback(() => {
		try {
			sessionStorage.setItem(PENDING_MESSAGES_STORAGE_KEY, JSON.stringify([...pendingRef.current.entries()]))
		} catch {
			// sessionStorage 不可用时仍保持当前页内存队列，不影响本次连接内重试。
		}
	}, [])

	const publishLocalDeliveryFailure = useCallback((msgID: string) => {
		listenersRef.current.forEach((listener) => listener({ type: 'error', code: 'delivery_unconfirmed', msg_id: msgID }))
	}, [])

	const scheduleRetry = useCallback((msgID: string) => {
		const oldTimer = retryTimersRef.current.get(msgID)
		if (oldTimer) clearTimeout(oldTimer)
		retryTimersRef.current.set(
			msgID,
			setTimeout(() => {
				const pending = pendingRef.current.get(msgID)
				if (!pending) return
				if (pending.attempts >= DELIVERY_MAX_ATTEMPTS) {
					pendingRef.current.delete(msgID)
					retryTimersRef.current.delete(msgID)
					persistPending()
					publishLocalDeliveryFailure(msgID)
					return
				}
				const ws = wsRef.current
				if (ws?.readyState === WebSocket.OPEN) {
					pending.attempts += 1
					ws.send(JSON.stringify(pending.event))
					persistPending()
				}
				scheduleRetry(msgID)
			}, DELIVERY_RETRY_DELAY_MS),
		)
	}, [persistPending, publishLocalDeliveryFailure])

	const acknowledge = useCallback((msgID?: string) => {
		if (!msgID || !pendingRef.current.delete(msgID)) return
		const timer = retryTimersRef.current.get(msgID)
		if (timer) clearTimeout(timer)
		retryTimersRef.current.delete(msgID)
		persistPending()
	}, [persistPending])

	const sendReliable = useCallback((event: ReliableEvent) => {
		pendingRef.current.set(event.msg_id, { event, attempts: 0 })
		persistPending()
		const ws = wsRef.current
		if (ws?.readyState === WebSocket.OPEN) {
			const pending = pendingRef.current.get(event.msg_id)!
			pending.attempts += 1
			ws.send(JSON.stringify(event))
			persistPending()
		}
		scheduleRetry(event.msg_id)
	}, [persistPending, scheduleRetry])

  useEffect(() => {
    if (!token) {
      setStatus('closed')
      return
    }

    let intentionalClose = false
    let reconnectAttempt = 0
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null

    const connect = () => {
      // 同源 WebSocket 握手会自动携带 HttpOnly 会话 Cookie；不再把 JWT 放入 URL。
      const ws = new WebSocket(resolveWsUrl())
      wsRef.current = ws
      setStatus(reconnectAttempt > 0 ? 'reconnecting' : 'connecting')

      ws.onopen = () => {
        reconnectAttempt = 0
        setStatus('open')
		// 连接恢复后重放尚未收到服务端回显的消息。后端按 msg_id 去重，因此不会重复落库。
		pendingRef.current.forEach((pending, msgID) => {
			if (pending.attempts < DELIVERY_MAX_ATTEMPTS) {
				pending.attempts += 1
				ws.send(JSON.stringify(pending.event))
				persistPending()
				scheduleRetry(msgID)
			}
		})
      }

      ws.onclose = () => {
        if (intentionalClose) {
          setStatus('closed')
          return
        }
        setStatus('reconnecting')
        const delay = Math.min(RECONNECT_BASE_DELAY_MS * 2 ** reconnectAttempt, RECONNECT_MAX_DELAY_MS)
        reconnectAttempt += 1
        reconnectTimer = setTimeout(connect, delay)
      }

      // onerror 之后浏览器规范上一定会紧接着触发 onclose，这里不重复处理，
      // 避免重连定时器被排两次。
      ws.onerror = () => {}

      ws.onmessage = (evt) => {
        try {
          const data = JSON.parse(evt.data) as ServerEvent
			if (data.type === 'message_received' || data.type === 'direct_message_received') acknowledge(data.msg_id)
          listenersRef.current.forEach((listener) => listener(data))
        } catch {
          // 忽略无法解析的帧，不阻断整条连接。
        }
      }
    }

    connect()

    return () => {
      intentionalClose = true
      if (reconnectTimer) clearTimeout(reconnectTimer)
      wsRef.current?.close()
      wsRef.current = null
    }
  }, [token])

  const value = useMemo<SocketContextValue>(
    () => ({
      send: (event: ClientEvent) => {
        const ws = wsRef.current
        if (ws && ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify(event))
        }
      },
		sendReliable,
      status,
      subscribe: (listener: Listener) => {
        listenersRef.current.add(listener)
        return () => {
          listenersRef.current.delete(listener)
        }
      },
    }),
		[status, sendReliable],
  )

  return <SocketContext.Provider value={value}>{children}</SocketContext.Provider>
}

export function useSocket(): SocketContextValue {
  const ctx = useContext(SocketContext)
  if (!ctx) throw new Error('useSocket must be used within SocketProvider')
  return ctx
}
