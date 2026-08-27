import { useCallback, useEffect, useState } from 'react'
import { api, errorMessage } from '../api/client'
import { useAuth } from '../context/AuthContext'
import type { Room, WatchTopic } from '../types'
import './WatchTopicsPage.css'

/**
 * 关注事项页（本轮新增，对应 Task19/P1 后端能力的前端闭环，此前
 * `POST/GET /api/watch-topics` 已实现但无任何前端入口，是 Task20 AI 推荐规则化
 * 匹配演示的直接输入源——不设置关注事项，AI 推荐页永远是空的，属于明显的
 * 功能断层，本次一并补齐，含 T123 新增的删除能力）。
 */
export function WatchTopicsPage() {
  const { token } = useAuth()

  const [topics, setTopics] = useState<WatchTopic[] | null>(null)
  const [rooms, setRooms] = useState<Room[]>([])
  const [loadError, setLoadError] = useState<string | null>(null)

  const [keywords, setKeywords] = useState('')
  const [roomId, setRoomId] = useState('')
  const [priority, setPriority] = useState(1)
  const [createBusy, setCreateBusy] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const [deleteError, setDeleteError] = useState<string | null>(null)

  const load = useCallback(() => {
    api
      .get<WatchTopic[]>('/api/watch-topics', token)
      .then(setTopics)
      .catch((err) => setLoadError(errorMessage(err)))
  }, [token])

  useEffect(() => {
    load()
    api.get<Room[]>('/api/rooms').then(setRooms).catch(() => setRooms([]))
  }, [load])

  const handleCreate = async () => {
    const trimmed = keywords.trim()
    if (!trimmed) return
    setCreateBusy(true)
    setCreateError(null)
    try {
      await api.post('/api/watch-topics', { room_id: roomId || undefined, keywords: trimmed, priority }, token)
      setKeywords('')
      setRoomId('')
      setPriority(1)
      load()
    } catch (err) {
      setCreateError(errorMessage(err))
    } finally {
      setCreateBusy(false)
    }
  }

  const handleDelete = async (id: string) => {
    setDeleteError(null)
    try {
      await api.del(`/api/watch-topics/${id}`, undefined, token)
      load()
    } catch (err) {
      setDeleteError(errorMessage(err))
    }
  }

  const roomName = (id?: string) => (id ? rooms.find((r) => r.id === id)?.name ?? id : '全局（不限房间）')

  return (
    <div className="watch-topics-page">
      <h2 className="watch-topics-title">关注事项</h2>
      <p className="watch-topics-hint">
        声明你最近关注的话题关键词（用逗号分隔），AI 推荐会基于这些关键词与其他用户的重合度、
        以及是否在同一兴趣房间，为你匹配可能聊得来的人（见"AI推荐"页）。
      </p>

      <section className="watch-topics-section">
        <h3>新增关注事项</h3>
        <div className="watch-topics-form">
          <input
            type="text"
            placeholder="关键词，如：摄影,徒步,猫"
            value={keywords}
            onChange={(e) => setKeywords(e.target.value)}
          />
          <select value={roomId} onChange={(e) => setRoomId(e.target.value)}>
            <option value="">全局（不限房间）</option>
            {rooms.map((r) => (
              <option key={r.id} value={r.id}>
                {r.name}
              </option>
            ))}
          </select>
          <input
            type="number"
            min={0}
            max={10}
            value={priority}
            onChange={(e) => setPriority(Number(e.target.value))}
            title="优先级（数字越大越优先）"
          />
          <button onClick={handleCreate} disabled={createBusy || !keywords.trim()}>
            {createBusy ? '提交中…' : '添加'}
          </button>
        </div>
        {createError && <div className="watch-topics-error">⚠️ {createError}</div>}
      </section>

      {loadError && <div className="watch-topics-error">⚠️ 加载失败：{loadError}</div>}
      {deleteError && <div className="watch-topics-error">⚠️ {deleteError}</div>}

      <section className="watch-topics-section">
        <h3>我的关注事项{topics ? `（${topics.length}）` : ''}</h3>
        {topics === null && <div className="watch-topics-empty">加载中…</div>}
        {topics !== null && topics.length === 0 && <div className="watch-topics-empty">还没有设置关注事项</div>}
        <ul className="watch-topics-list">
          {topics?.map((t) => (
            <li key={t.id} className="watch-topics-item">
              <div>
                <div className="watch-topics-keywords">{t.keywords}</div>
                <div className="watch-topics-meta">
                  {roomName(t.room_id)} · 优先级 {t.priority}
                  {t.expires_at ? ` · 过期于 ${new Date(t.expires_at).toLocaleString()}` : ''}
                </div>
              </div>
              <button className="watch-topics-delete" onClick={() => handleDelete(t.id)}>
                删除
              </button>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
