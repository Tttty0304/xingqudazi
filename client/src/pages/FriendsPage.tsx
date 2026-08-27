import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { api, errorMessage } from '../api/client'
import { useAuth } from '../context/AuthContext'
import { useSocket } from '../context/SocketContext'
import type { Friend, PendingFriendRequest, UserLookupResult } from '../types'
import './FriendsPage.css'

/**
 * 好友页（本轮新增，对应 Task14 后端能力的前端闭环）：
 * 按用户名添加好友 -> 查看/处理收到的请求 -> 好友列表（含实时在线态）-> 一键私聊/删除。
 * 补齐此前"好友请求只能靠 WS 实时通知得知、离线错过就再也看不到"的功能缺口
 * （见 `GET /api/friends/requests`，T120）。
 */
export function FriendsPage() {
  const navigate = useNavigate()
  const { token } = useAuth()
  const { subscribe } = useSocket()

  const [friends, setFriends] = useState<Friend[] | null>(null)
  const [pending, setPending] = useState<PendingFriendRequest[] | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)

  const [usernameInput, setUsernameInput] = useState('')
  const [addBusy, setAddBusy] = useState(false)
  const [addError, setAddError] = useState<string | null>(null)
  const [addSuccess, setAddSuccess] = useState<string | null>(null)

  const [actionError, setActionError] = useState<string | null>(null)

  const load = useCallback(() => {
    setLoadError(null)
    Promise.all([
      api.get<Friend[]>('/api/friends', token),
      api.get<PendingFriendRequest[]>('/api/friends/requests', token),
    ])
      .then(([f, p]) => {
        setFriends(f)
        setPending(p)
      })
      .catch((err) => setLoadError(errorMessage(err)))
  }, [token])

  useEffect(() => {
    load()
  }, [load])

  // 对方实时发来好友请求时自动刷新列表（无需手动刷新页面）。
  useEffect(() => {
    return subscribe((event) => {
      if (event.type === 'friend_request_received') {
        load()
      }
    })
  }, [subscribe, load])

  const handleAddFriend = async () => {
    const username = usernameInput.trim()
    if (!username) return
    setAddBusy(true)
    setAddError(null)
    setAddSuccess(null)
    try {
      const user = await api.get<UserLookupResult>(
        `/api/users/lookup?username=${encodeURIComponent(username)}`,
        token,
      )
      await api.post('/api/friends/requests', { target_user_id: user.id }, token)
      setAddSuccess(`已向 ${username} 发送好友请求`)
      setUsernameInput('')
      load()
    } catch (err) {
      setAddError(errorMessage(err))
    } finally {
      setAddBusy(false)
    }
  }

  const handleRespond = async (requestId: string, action: 'accept' | 'reject') => {
    setActionError(null)
    try {
      await api.put(`/api/friends/requests/${requestId}`, { action }, token)
      load()
    } catch (err) {
      setActionError(errorMessage(err))
    }
  }

  const handleRemoveFriend = async (peerId: string) => {
    setActionError(null)
    try {
      await api.del(`/api/friends/${peerId}`, undefined, token)
      load()
    } catch (err) {
      setActionError(errorMessage(err))
    }
  }

  const incoming = pending?.filter((p) => p.direction === 'incoming') ?? []
  const outgoing = pending?.filter((p) => p.direction === 'outgoing') ?? []

  return (
    <div className="friends-page">
      <h2 className="friends-title">好友</h2>

      <section className="friends-section">
        <h3>添加好友</h3>
        <div className="friends-add-row">
          <input
            type="text"
            placeholder="输入对方用户名"
            value={usernameInput}
            onChange={(e) => setUsernameInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleAddFriend()
            }}
          />
          <button onClick={handleAddFriend} disabled={addBusy || !usernameInput.trim()}>
            {addBusy ? '发送中…' : '发起请求'}
          </button>
        </div>
        {addError && <div className="friends-error">⚠️ {addError}</div>}
        {addSuccess && <div className="friends-success">{addSuccess}</div>}
      </section>

      {loadError && <div className="friends-error">⚠️ 加载失败：{loadError}</div>}
      {actionError && <div className="friends-error">⚠️ {actionError}</div>}

      <section className="friends-section">
        <h3>收到的好友请求{incoming.length > 0 ? `（${incoming.length}）` : ''}</h3>
        {incoming.length === 0 && <div className="friends-empty">暂无待处理的请求</div>}
        <ul className="friends-list">
          {incoming.map((r) => (
            <li key={r.request_id} className="friends-list-item">
              <span>{r.peer_username}</span>
              <div className="friends-item-actions">
                <button onClick={() => handleRespond(r.request_id, 'accept')}>接受</button>
                <button className="friends-danger" onClick={() => handleRespond(r.request_id, 'reject')}>
                  拒绝
                </button>
              </div>
            </li>
          ))}
        </ul>
      </section>

      {outgoing.length > 0 && (
        <section className="friends-section">
          <h3>我发出的请求</h3>
          <ul className="friends-list">
            {outgoing.map((r) => (
              <li key={r.request_id} className="friends-list-item">
                <span>{r.peer_username}</span>
                <span className="friends-waiting">等待对方处理</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      <section className="friends-section">
        <h3>我的好友{friends ? `（${friends.length}）` : ''}</h3>
        {friends === null && <div className="friends-empty">加载中…</div>}
        {friends !== null && friends.length === 0 && <div className="friends-empty">还没有好友，去上方添加一个吧</div>}
        <ul className="friends-list">
          {friends?.map((f) => (
            <li key={f.user_id} className="friends-list-item">
              <span>
                <span className={f.online ? 'friends-online-dot online' : 'friends-online-dot'} />
                {f.username}
              </span>
              <div className="friends-item-actions">
                <button onClick={() => navigate(`/messages/${f.user_id}`)}>发消息</button>
                <button className="friends-danger" onClick={() => handleRemoveFriend(f.user_id)}>
                  删除
                </button>
              </div>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
