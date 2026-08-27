import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, errorMessage } from '../api/client'
import { useAuth } from '../context/AuthContext'
import { useSocket } from '../context/SocketContext'
import { useUsernames } from '../hooks/useUsernames'
import type { ConversationSummary } from '../types'
import './ConversationsPage.css'

/**
 * 私聊会话列表页（本轮新增，对应 Task15 `GET /api/conversations` 的前端闭环，
 * 此前该接口已实现但前端完全没有消费方，属于半成品）。
 */
export function ConversationsPage() {
  const navigate = useNavigate()
  const { token } = useAuth()
  const { subscribe } = useSocket()
  const { names, resolve } = useUsernames()

  const [conversations, setConversations] = useState<ConversationSummary[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .get<ConversationSummary[]>('/api/conversations', token)
      .then((data) => {
        setConversations(data)
        resolve(data.map((c) => c.peer_id))
      })
      .catch((err) => setError(errorMessage(err)))
  }, [resolve, token])

  useEffect(() => {
    load()
  }, [load])

  // 新私聊消息到达时刷新列表（更新最近一条消息摘要/排序）。
  useEffect(() => {
    return subscribe((event) => {
      if (event.type === 'direct_message_received') load()
    })
  }, [subscribe, load])

  if (error) {
    return (
      <div className="conversations-empty">
        <p>⚠️ 加载会话列表失败：{error}</p>
      </div>
    )
  }

  if (conversations === null) {
    return <div className="conversations-empty">正在加载会话列表…</div>
  }

  if (conversations.length === 0) {
    return (
      <div className="conversations-empty">
        暂无私聊会话，去<Link to="/friends">好友页</Link>找个朋友聊聊吧
      </div>
    )
  }

  return (
    <div>
      <h2 className="conversations-title">私聊</h2>
      <div className="conversations-list">
        {conversations.map((c) => (
          <button
            key={c.conversation_id}
            className="conversation-card"
            onClick={() => navigate(`/messages/${c.peer_id}`)}
          >
            <div className="conversation-card-name">{names[c.peer_id] ?? `用户${c.peer_id.slice(0, 8)}`}</div>
            {c.unread_count > 0 && <div className="conversation-card-unread">{c.unread_count > 99 ? '99+' : c.unread_count} 条未读</div>}
            <div className="conversation-card-preview">{c.last_message || '（暂无消息）'}</div>
          </button>
        ))}
      </div>
    </div>
  )
}
