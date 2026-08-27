import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, errorMessage, newIdempotencyKey, resolveMediaUrl, translateCode } from '../api/client'
import { UserActionPopover, type FriendPopoverStatus } from '../components/UserActionPopover'
import { useAuth } from '../context/AuthContext'
import { useSocket } from '../context/SocketContext'
import { useUsernames } from '../hooks/useUsernames'
import type { Friend, PendingFriendRequest, RoomMessage, ServerEvent } from '../types'
import './RoomChatPage.css'

interface ChatMessage {
  msgId: string
  senderId: string
  senderType: string
  content: string
  contentType: string
  createdAt?: string
}

function toChatMessage(m: RoomMessage): ChatMessage {
  return {
    msgId: m.msg_id,
    senderId: m.sender_id,
    senderType: m.sender_type,
    content: m.content,
    contentType: m.content_type,
    createdAt: m.created_at,
  }
}

/** Task9：房间聊天页（T30-T35/T40/T50/T81/T90-T93 的前端消费方）。 */
export function RoomChatPage() {
  const { roomId } = useParams<{ roomId: string }>()
  const navigate = useNavigate()
  const { token, userId } = useAuth()
	const { send, sendReliable, status, subscribe } = useSocket()
  const { names, resolve } = useUsernames()

  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [historyLoading, setHistoryLoading] = useState(true)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const [onlineCount, setOnlineCount] = useState<number | null>(null)
  const [joined, setJoined] = useState(false)
  const [input, setInput] = useState('')
  const [sendError, setSendError] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)

  // 能力补齐（用户反馈驱动）：点击发言人用户名弹出"加为好友"气泡，见
  // `UserActionPopover`。不涉及新增后端接口，纯复用既有的好友三件套接口。
  const [popover, setPopover] = useState<{ userId: string; username: string; x: number; y: number } | null>(null)
  const [popoverStatus, setPopoverStatus] = useState<FriendPopoverStatus>('loading')
  const [popoverLoadError, setPopoverLoadError] = useState<string | null>(null)
  const [popoverActionError, setPopoverActionError] = useState<string | null>(null)
  const [popoverBusy, setPopoverBusy] = useState(false)
  // 防止"快速点击不同发言人"时，前一次未完成的好友状态查询以过期结果覆盖
  // 后一次已打开的气泡（经典的异步竞态问题，用自增 token 而非 AbortController
  // 是因为这里请求量极小，没必要引入额外复杂度）。
  const popoverRequestId = useRef(0)

  const seenMsgIds = useRef<Set<string>>(new Set())
  const listRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  // 本房间相关的 WS 事件订阅（应用级共享连接，见 SocketContext；不属于本房间的
  // 事件——如其它房间的广播/私聊消息/好友请求——由其它页面/Layout 各自订阅处理）。
  useEffect(() => {
    return subscribe((event: ServerEvent) => {
      switch (event.type) {
        case 'joined':
          if (event.room_id === roomId) setJoined(true)
          break
        case 'room_user_count_update':
          if (event.room_id === roomId && typeof event.online_count === 'number') {
            setOnlineCount(event.online_count)
          }
          break
        case 'message_received':
          if (event.room_id === roomId && event.msg_id && !seenMsgIds.current.has(event.msg_id)) {
            seenMsgIds.current.add(event.msg_id)
            resolve([event.sender_id ?? ''])
            setMessages((prev) => [
              ...prev,
              {
                msgId: event.msg_id!,
                senderId: event.sender_id ?? '',
                senderType: event.sender_type ?? 'human',
                content: event.content ?? '',
                contentType: event.content_type ?? 'text',
              },
            ])
            // 新消息到达时关闭已打开的"加好友"气泡，避免气泡随消息列表滚动
            // 后停留在错误位置、或引用的发言人状态已过期（Testcase C7）。
            setPopover(null)
          }
          break
        case 'error':
          setSendError(event.code ? translateCode(event.code) : '发送失败')
          break
      }
    })
  }, [subscribe, roomId, resolve])

  // 加载历史消息（T21）。
  useEffect(() => {
    if (!roomId) return
    let cancelled = false
    setHistoryLoading(true)
    setJoined(false)
    setMessages([])
    seenMsgIds.current = new Set()
    api
      .get<{ messages: RoomMessage[]; has_more: boolean }>(
        `/api/rooms/${roomId}/messages?page=1&size=50`,
      )
      .then((data) => {
        if (cancelled) return
        const chatMessages = data.messages.map(toChatMessage)
        chatMessages.forEach((m) => seenMsgIds.current.add(m.msgId))
        resolve(chatMessages.map((m) => m.senderId))
        setMessages(chatMessages)
      })
      .catch((err) => {
        if (!cancelled) setHistoryError(errorMessage(err))
      })
      .finally(() => {
        if (!cancelled) setHistoryLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [roomId, resolve])

  // WS 连接建立后加入房间；组件卸载/切换房间时离开。
  useEffect(() => {
    if (status !== 'open' || !roomId) return
    send({ type: 'join_room', room_id: roomId })
    return () => {
      send({ type: 'leave_room', room_id: roomId })
      setJoined(false)
    }
  }, [status, roomId, send])

  // 新消息到达时自动滚动到底部。
  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight })
  }, [messages])

  // 切换房间时关闭可能残留的气泡（Testcase C7）。
  useEffect(() => {
    setPopover(null)
  }, [roomId])

  const closePopover = () => setPopover(null)

  const handleOpenPopover = (e: React.MouseEvent, senderId: string, username: string) => {
    const requestId = ++popoverRequestId.current
    setPopover({ userId: senderId, username, x: e.clientX, y: e.clientY })
    setPopoverStatus('loading')
    setPopoverLoadError(null)
    setPopoverActionError(null)
    Promise.all([api.get<Friend[]>('/api/friends', token), api.get<PendingFriendRequest[]>('/api/friends/requests', token)])
      .then(([friends, pending]) => {
        if (popoverRequestId.current !== requestId) return // 已被更新的一次点击取代，丢弃过期结果
        if (friends.some((f) => f.user_id === senderId)) {
          setPopoverStatus('friend')
          return
        }
        const req = pending.find((p) => p.peer_id === senderId)
        if (req?.direction === 'outgoing') setPopoverStatus('outgoing_pending')
        else if (req?.direction === 'incoming') setPopoverStatus('incoming_pending')
        else setPopoverStatus('none')
      })
      .catch((err) => {
        if (popoverRequestId.current !== requestId) return
        setPopoverLoadError(errorMessage(err))
      })
  }

  const handleAddFriendFromPopover = () => {
    if (!popover) return
    setPopoverBusy(true)
    setPopoverActionError(null)
    api
      .post('/api/friends/requests', { target_user_id: popover.userId }, token)
      .then(() => setPopoverStatus('outgoing_pending'))
      .catch((err) => setPopoverActionError(errorMessage(err)))
      .finally(() => setPopoverBusy(false))
  }

  const handleSend = () => {
    const content = input.trim()
    if (!content || !roomId) return
    setSendError(null)
		sendReliable({ type: 'send_message', room_id: roomId, msg_id: newIdempotencyKey(), content })
    setInput('')
  }

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file || !roomId) return
    setSendError(null)
    setUploading(true)
    try {
      const formData = new FormData()
      formData.append('file', file)
      const result = await api.upload<{ media_id: string; url: string }>(
        '/api/media/upload',
        formData,
        token,
      )
		sendReliable({
        type: 'send_message',
        room_id: roomId,
        msg_id: newIdempotencyKey(),
        content: result.url,
        content_type: 'image',
      })
    } catch (err) {
      setSendError(errorMessage(err))
    } finally {
      setUploading(false)
    }
  }

  const displayName = (senderId: string) => names[senderId] ?? `用户${senderId.slice(0, 8)}`

  return (
    <div className="chat-shell">
      <div className="chat-topbar">
        <button className="link-button" onClick={() => navigate('/rooms')}>
          ← 返回房间列表
        </button>
        <span className="chat-status">
          {status === 'connecting' && '连接中…'}
          {status === 'reconnecting' && '连接已断开，正在自动重连…'}
          {status === 'open' && !joined && '加入房间中…'}
          {status === 'open' && joined && `已连接${onlineCount !== null ? ` · ${onlineCount} 人在线` : ''}`}
          {status === 'closed' && '连接已断开'}
        </span>
      </div>

      {historyLoading && <div className="chat-empty">正在加载历史消息…</div>}
      {historyError && <div className="chat-empty">⚠️ {historyError}</div>}

      {!historyLoading && !historyError && (
        <div className="chat-messages" ref={listRef}>
          {messages.length === 0 && <div className="chat-empty">还没有人发过消息，来打个招呼吧</div>}
          {messages.map((m) => {
            const isMine = m.senderId === userId
            return (
              <div key={m.msgId} className={isMine ? 'chat-bubble mine' : 'chat-bubble'}>
                <div className="chat-bubble-sender">
                  {isMine || m.senderType === 'bot' ? (
                    isMine ? '我' : displayName(m.senderId)
                  ) : (
                    <button
                      type="button"
                      className="chat-sender-name-btn"
                      onClick={(e) => handleOpenPopover(e, m.senderId, displayName(m.senderId))}
                      title="点击加为好友"
                    >
                      {displayName(m.senderId)}
                    </button>
                  )}
                  {m.senderType === 'bot' ? '（机器人）' : ''}
                </div>
                {m.contentType === 'image' ? (
                  <img
                    src={resolveMediaUrl(m.content)}
                    alt="图片消息"
                    className="chat-image"
                  />
                ) : (
                  <div className="chat-bubble-content">{m.content}</div>
                )}
              </div>
            )
          })}
        </div>
      )}

      {sendError && <div className="chat-send-error">⚠️ {sendError}</div>}

      <div className="chat-input-row">
        <input
          type="file"
          accept="image/jpeg,image/png,image/gif,image/webp"
          ref={fileInputRef}
          onChange={handleUpload}
          hidden
        />
        <button
          className="chat-upload-button"
          onClick={() => fileInputRef.current?.click()}
          disabled={uploading || status !== 'open'}
          title="发送图片"
          type="button"
        >
          📷
        </button>
        <input
          type="text"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter') handleSend()
          }}
          placeholder={status === 'open' ? '说点什么…' : status === 'reconnecting' ? '重连中，请稍候…' : '连接中，请稍候…'}
          disabled={status !== 'open'}
        />
        <button onClick={handleSend} disabled={status !== 'open' || !input.trim()}>
          发送
        </button>
      </div>

      {popover && (
        <UserActionPopover
          username={popover.username}
          x={popover.x}
          y={popover.y}
          status={popoverStatus}
          busy={popoverBusy}
          loadError={popoverLoadError}
          actionError={popoverActionError}
          onAddFriend={handleAddFriendFromPopover}
          onClose={closePopover}
        />
      )}
    </div>
  )
}
