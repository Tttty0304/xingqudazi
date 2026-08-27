import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, errorMessage } from '../api/client'
import type { Room } from '../types'
import { useAuth } from '../context/AuthContext'
import './RoomListPage.css'

/** Task9：房间列表页（T20），覆盖加载态/空态/错误态。 */
export function RoomListPage() {
  const [rooms, setRooms] = useState<Room[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [topic, setTopic] = useState('')
  const [creating, setCreating] = useState(false)
  const navigate = useNavigate()
  const { token } = useAuth()

  useEffect(() => {
    let cancelled = false
    api
      .get<Room[]>('/api/rooms')
      .then((data) => {
        if (!cancelled) setRooms(data)
      })
      .catch((err) => {
        if (!cancelled) setError(errorMessage(err))
      })
    return () => {
      cancelled = true
    }
  }, [])

  const createRoom = async (event: FormEvent) => {
    event.preventDefault()
    setCreating(true)
    setError(null)
    try {
      const room = await api.post<Room>('/api/rooms', { name, topic }, token)
      setRooms((current) => current ? [...current, room] : [room])
      setName('')
      setTopic('')
      navigate(`/rooms/${room.id}`)
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setCreating(false)
    }
  }

  if (error) {
    return (
      <div className="room-list-empty">
        <p>⚠️ 加载房间列表失败：{error}</p>
      </div>
    )
  }

  if (rooms === null) {
    return <div className="room-list-empty">正在加载房间列表…</div>
  }

  if (rooms.length === 0) {
    return <div className="room-list-empty">暂无可用房间</div>
  }

  return (
    <div>
      <h2 className="room-list-title">兴趣聊天室</h2>
      <form className="room-create-form" onSubmit={createRoom}>
        <label>
          新建兴趣房间
          <input value={name} onChange={(event) => setName(event.target.value)} placeholder="例如：桌游同好会" minLength={2} maxLength={64} required />
        </label>
        <input value={topic} onChange={(event) => setTopic(event.target.value)} placeholder="一句话描述（可选）" maxLength={255} />
        <button type="submit" disabled={creating}>{creating ? '创建中…' : '创建并进入'}</button>
      </form>
      <div className="room-list">
        {rooms.map((room) => (
          <button
            key={room.id}
            className="room-card"
            onClick={() => navigate(`/rooms/${room.id}`)}
          >
            <div className="room-card-name">{room.name}</div>
            <div className="room-card-topic">{room.topic}</div>
            <div className="room-card-count">{room.online_count} 人在线</div>
          </button>
        ))}
      </div>
    </div>
  )
}
