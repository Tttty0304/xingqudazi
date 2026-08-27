import { useEffect, useState } from 'react'
import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { api } from '../api/client'
import { useAuth } from '../context/AuthContext'
import { useSocket } from '../context/SocketContext'
import type { PendingFriendRequest } from '../types'
import { NotificationToggle } from './NotificationToggle'
import './Layout.css'

/**
 * 登录后的通用页面外壳：顶部导航 + 当前用户信息 + 退出登录。
 * 好友/私聊未读提醒展示精确数字（能力补齐项：此前只有一个圆点，看不出到底
 * 有多少条未处理）：
 *   - 好友请求数：进入页面时先用 `GET /api/friends/requests` 拉取真实的
 *     "收到的待处理请求"基准数量（不是凭空捏造的假数字），此后每收到一次
 *     `friend_request_received` WS 事件计数 +1，保持与后端状态一致。
 *   - 私聊未读数：后端当前没有持久化的"已读游标"（`direct_messages` 表未记录
 *     阅读状态），因此这里如实只统计"本次会话期间收到的新消息数"（不含历史
 *     累积的未读，避免打着"精确"的旗号展示一个实际拿不到真实数据支撑的数字）；
 *     每收到一次他人发来的 `direct_message_received` 事件计数 +1（排除自己
 *     发出消息时收到的"送达确认"回执，那不是新消息）。
 * 两个计数在访问对应页面时清零，与此前圆点提醒"进入页面即视为已读"的语义一致。
 */
export function Layout() {
  const { username, isGuest, logout, token, userId } = useAuth()
  const { subscribe } = useSocket()
  const navigate = useNavigate()
  const location = useLocation()

  const [friendNoticeCount, setFriendNoticeCount] = useState(0)
  const [messageNoticeCount, setMessageNoticeCount] = useState(0)

  // 拉取好友请求的真实基准数量（避免刷新页面后计数归零、看起来"没有未读"，
  // 实际上后端还有几条从未处理过的请求）。
  useEffect(() => {
    if (!token) return
    api
      .get<PendingFriendRequest[]>('/api/friends/requests', token)
      .then((list) => {
        const incoming = list.filter((r) => r.direction === 'incoming').length
        setFriendNoticeCount(incoming)
      })
      .catch(() => {
        // 拉取失败不影响页面渲染，静默忽略——退化为"至少能通过 WS 事件累计"，
        // 与此前布尔值实现在网络异常时的降级程度一致。
      })
  }, [token])

  useEffect(() => {
    return subscribe((event) => {
      if (event.type === 'friend_request_received') {
        setFriendNoticeCount((n) => n + 1)
      } else if (event.type === 'direct_message_received' && event.sender_id && event.sender_id !== userId) {
        setMessageNoticeCount((n) => n + 1)
      }
    })
  }, [subscribe, userId])

  // 访问对应页面时清除提醒。
  useEffect(() => {
    if (location.pathname.startsWith('/friends')) setFriendNoticeCount(0)
    if (location.pathname.startsWith('/messages')) setMessageNoticeCount(0)
  }, [location.pathname])

  const handleLogout = () => {
    logout()
    navigate('/login', { replace: true })
  }

  return (
    <div className="layout-shell">
      <header className="layout-header">
        <Link to="/rooms" className="layout-brand">
          兴趣搭子
        </Link>
        <nav className="layout-nav">
          <Link to="/rooms">房间</Link>
          <Link to="/messages" className={messageNoticeCount > 0 ? 'nav-has-notice' : ''}>
            私聊{messageNoticeCount > 0 ? ` (${messageNoticeCount})` : ''}
          </Link>
          <Link to="/friends" className={friendNoticeCount > 0 ? 'nav-has-notice' : ''}>
            好友{friendNoticeCount > 0 ? ` (${friendNoticeCount})` : ''}
          </Link>
          <Link to="/watch-topics">关注事项</Link>
          <Link to="/recommendations">AI推荐</Link>
          <Link to="/profile">资料</Link>
        </nav>
        <div className="layout-user">
          <NotificationToggle />
          <span>
            {username}
            {isGuest ? '（访客）' : ''}
          </span>
          <button onClick={handleLogout} className="link-button">
            退出
          </button>
        </div>
      </header>
      <main className="layout-main">
        <Outlet />
      </main>
    </div>
  )
}
