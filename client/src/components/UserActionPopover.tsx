import { useEffect, useRef } from 'react'
import './UserActionPopover.css'

/**
 * 房间聊天页"点击发言人用户名加好友"气泡（能力补齐项，用户验收反馈驱动）。
 * 不涉及后端改动，纯复用既有的 `GET /api/friends`、`GET /api/friends/requests`、
 * `POST /api/friends/requests` 三个接口——后端在 Task14/本轮更早的能力补齐中
 * 已经具备了完整的好友关系判定与发起请求能力，这里只是把"发起好友请求"这条
 * 转化路径从"必须离开聊天室、去好友页手动输入用户名查找"缩短为"就地点击"。
 */

export type FriendPopoverStatus = 'loading' | 'friend' | 'outgoing_pending' | 'incoming_pending' | 'none'

interface UserActionPopoverProps {
  username: string
  /** 触发点击事件时的视口坐标（clientX/clientY），用于定位气泡。 */
  x: number
  y: number
  status: FriendPopoverStatus
  busy: boolean
  loadError?: string | null
  actionError?: string | null
  onAddFriend: () => void
  onClose: () => void
}

const POPOVER_WIDTH = 220
const POPOVER_ESTIMATED_HEIGHT = 120

export function UserActionPopover({
  username,
  x,
  y,
  status,
  busy,
  loadError,
  actionError,
  onAddFriend,
  onClose,
}: UserActionPopoverProps) {
  const ref = useRef<HTMLDivElement>(null)

  // 点击气泡外部区域自动关闭（Testcase C6）。用 mousedown 而非 click 监听，
  // 避免和"触发打开气泡"的那次点击在冒泡阶段产生时序竞争（同一次点击既要
  // 打开新气泡又被外部监听器判定为"点击外部"而立刻关闭）。
  useEffect(() => {
    const handleOutside = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onClose()
      }
    }
    document.addEventListener('mousedown', handleOutside)
    return () => document.removeEventListener('mousedown', handleOutside)
  }, [onClose])

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [onClose])

  // 简单的视口边界钳制，避免气泡在靠近屏幕边缘点击时溢出屏幕外不可见。
  const left = Math.min(Math.max(8, x), window.innerWidth - POPOVER_WIDTH - 8)
  const top = Math.min(y + 8, window.innerHeight - POPOVER_ESTIMATED_HEIGHT - 8)

  return (
    <div className="user-action-popover" ref={ref} style={{ left, top }}>
      <div className="user-action-popover-name">{username}</div>
      {loadError ? (
        <div className="user-action-popover-error">⚠️ {loadError}</div>
      ) : (
        <>
          {status === 'loading' && <div className="user-action-popover-hint">加载中…</div>}
          {status === 'friend' && <div className="user-action-popover-hint">已经是好友</div>}
          {status === 'outgoing_pending' && (
            <div className="user-action-popover-hint">已发出好友请求，等待对方处理</div>
          )}
          {status === 'incoming_pending' && (
            <div className="user-action-popover-hint">对方向你发起了好友请求，去「好友」页处理</div>
          )}
          {status === 'none' && (
            <button className="user-action-popover-btn" onClick={onAddFriend} disabled={busy}>
              {busy ? '发送中…' : '加为好友'}
            </button>
          )}
          {actionError && <div className="user-action-popover-error">⚠️ {actionError}</div>}
        </>
      )}
    </div>
  )
}
