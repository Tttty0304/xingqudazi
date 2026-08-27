import { useEffect, useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { errorMessage } from '../api/client'
import { getExistingSubscription, isPushSupported, subscribeToPush, unsubscribeFromPush } from '../push'

/**
 * Web Push 订阅开关（Task17，本轮新增前端 UI；后端能力此前已完整实现但无入口，
 * 属于半成品）。放在 Layout 头部，全局可见，不局限于某一页面。
 */
export function NotificationToggle() {
  const { token } = useAuth()
  const supported = isPushSupported()
  const [subscribed, setSubscribed] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!supported) return
    getExistingSubscription().then((sub) => setSubscribed(!!sub))
  }, [supported])

  if (!supported) return null

  const toggle = async () => {
    setLoading(true)
    setError(null)
    try {
      if (subscribed) {
        await unsubscribeFromPush(token)
        setSubscribed(false)
      } else {
        await subscribeToPush(token)
        setSubscribed(true)
      }
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setLoading(false)
    }
  }

  return (
    <button
      type="button"
      className="link-button notification-toggle"
      onClick={toggle}
      disabled={loading}
      title={error ?? (subscribed ? '点击关闭离线消息通知' : '点击开启离线消息通知')}
    >
      {subscribed ? '🔔' : '🔕'}
    </button>
  )
}
