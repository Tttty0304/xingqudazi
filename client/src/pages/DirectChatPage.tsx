import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { api, errorMessage, newIdempotencyKey, resolveMediaUrl, translateCode } from '../api/client'
import { useAuth } from '../context/AuthContext'
import { useSocket } from '../context/SocketContext'
import { useUsernames } from '../hooks/useUsernames'
import type { ConversationSummary, DirectMessageItem, ServerEvent } from '../types'
import './RoomChatPage.css'

interface ChatMessage {
  msgId: string
  senderId: string
  content: string
  contentType: string
}

function toChatMessage(m: DirectMessageItem): ChatMessage {
  return { msgId: m.msg_id, senderId: m.sender_id, content: m.content, contentType: m.content_type }
}

/**
 * 私聊聊天页（本轮新增，对应 Task15 `send_direct_message`/`direct_message_received`
 * 的前端闭环）。路由按对方用户 ID（peerId）而非会话 ID 组织——不要求双方此前已经
 * 聊过天：若从未创建会话（`GET /api/conversations` 里查不到该 peer），历史消息为空，
 * 首次发送时后端会惰性创建会话，无需前端额外调用"创建会话"接口。
 */
export function DirectChatPage() {
  const { peerId } = useParams<{ peerId: string }>()
  const navigate = useNavigate()
  const { userId, token } = useAuth()
	const { sendReliable, status, subscribe } = useSocket()
  const { names, resolve } = useUsernames()

  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [historyLoading, setHistoryLoading] = useState(true)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const [input, setInput] = useState('')
  const [sendError, setSendError] = useState<string | null>(null)
  const [uploading, setUploading] = useState(false)

  const seenMsgIds = useRef<Set<string>>(new Set())
  const listRef = useRef<HTMLDivElement>(null)
  const fileInputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (peerId) resolve([peerId])
  }, [peerId, resolve])

  // 加载历史消息：先从会话列表里找到与该 peer 的会话，再拉取分页历史；
  // 若从未聊过（会话尚未创建），视为空历史，不是错误态。
  useEffect(() => {
    if (!peerId) return
    let cancelled = false
    setHistoryLoading(true)
    setMessages([])
    seenMsgIds.current = new Set()

    api
      .get<ConversationSummary[]>('/api/conversations', token)
      .then((conversations) => {
        const existing = conversations.find((c) => c.peer_id === peerId)
        if (!existing) return { messages: [], has_more: false } as { messages: DirectMessageItem[]; has_more: boolean }
        return api.get<{ messages: DirectMessageItem[]; has_more: boolean }>(
          `/api/conversations/${existing.conversation_id}/messages?page=1&size=50`,
          token,
        )
      })
      .then((data) => {
        if (cancelled) return
        const chatMessages = data.messages.map(toChatMessage)
        chatMessages.forEach((m) => seenMsgIds.current.add(m.msgId))
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
  }, [peerId, token])

  // 订阅私聊相关事件：只处理与当前 peer 之间往来的消息（双向）。
  useEffect(() => {
    if (!peerId) return
    return subscribe((event: ServerEvent) => {
      if (event.type === 'direct_message_received') {
        const isThisThread =
          (event.sender_id === peerId && event.target_user_id === userId) ||
          (event.sender_id === userId && event.target_user_id === peerId)
        if (isThisThread && event.msg_id && !seenMsgIds.current.has(event.msg_id)) {
          seenMsgIds.current.add(event.msg_id)
          setMessages((prev) => [
            ...prev,
            {
              msgId: event.msg_id!,
              senderId: event.sender_id ?? '',
              content: event.content ?? '',
              contentType: event.content_type ?? 'text',
            },
          ])
        }
      } else if (event.type === 'error') {
        setSendError(event.code ? translateCode(event.code) : '发送失败')
      }
    })
  }, [subscribe, peerId, userId])

  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight })
  }, [messages])

  const handleSend = () => {
    const content = input.trim()
    if (!content || !peerId) return
    setSendError(null)
		sendReliable({ type: 'send_direct_message', target_user_id: peerId, msg_id: newIdempotencyKey(), content })
    setInput('')
  }

  const handleUpload = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0]
    e.target.value = ''
    if (!file || !peerId) return
    setSendError(null)
    setUploading(true)
    try {
      const formData = new FormData()
      formData.append('file', file)
      const result = await api.upload<{ media_id: string; url: string }>('/api/media/upload', formData, token)
		sendReliable({
        type: 'send_direct_message',
        target_user_id: peerId,
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

  const peerName = peerId ? names[peerId] ?? `用户${peerId.slice(0, 8)}` : ''

  return (
    <div className="chat-shell">
      <div className="chat-topbar">
        <button className="link-button" onClick={() => navigate('/messages')}>
          ← 返回会话列表
        </button>
        <span className="chat-status">
          与 {peerName} 的私聊
          {status === 'reconnecting' && '（连接断开，正在自动重连…）'}
          {(status === 'connecting' || status === 'closed') && '（连接中…）'}
        </span>
      </div>

      {historyLoading && <div className="chat-empty">正在加载历史消息…</div>}
      {historyError && <div className="chat-empty">⚠️ {historyError}</div>}

      {!historyLoading && !historyError && (
        <div className="chat-messages" ref={listRef}>
          {messages.length === 0 && <div className="chat-empty">还没有聊天记录，发第一条消息吧</div>}
          {messages.map((m) => {
            const isMine = m.senderId === userId
            return (
              <div key={m.msgId} className={isMine ? 'chat-bubble mine' : 'chat-bubble'}>
                <div className="chat-bubble-sender">{isMine ? '我' : peerName}</div>
                {m.contentType === 'image' ? (
                  <img src={resolveMediaUrl(m.content)} alt="图片消息" className="chat-image" />
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
          placeholder={status === 'open' ? '说点什么…' : '连接中，请稍候…'}
          disabled={status !== 'open'}
        />
        <button onClick={handleSend} disabled={status !== 'open' || !input.trim()}>
          发送
        </button>
      </div>
    </div>
  )
}
