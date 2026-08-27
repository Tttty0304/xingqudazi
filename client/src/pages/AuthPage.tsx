import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'
import { errorMessage } from '../api/client'
import './AuthPage.css'

type Mode = 'login' | 'register'

/**
 * Task9：登录/注册合一页面，含访客快速进入入口（对应 T10-T15 后端能力）。
 * 覆盖加载态/错误态：提交中禁用按钮，失败时展示可读中文错误提示。
 */
export function AuthPage() {
  const [mode, setMode] = useState<Mode>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const { login, register, guestLogin } = useAuth()
  const navigate = useNavigate()

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    setError(null)
    setNotice(null)
    setLoading(true)
    try {
      if (mode === 'register') {
        await register(username, password)
        setNotice('注册成功，请登录')
        setMode('login')
      } else {
        await login(username, password)
        navigate('/rooms', { replace: true })
      }
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setLoading(false)
    }
  }

  const handleGuest = async () => {
    setError(null)
    setLoading(true)
    try {
      await guestLogin()
      navigate('/rooms', { replace: true })
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="auth-shell">
      <div className="auth-card">
        <h1 className="auth-title">兴趣搭子</h1>
        <p className="auth-subtitle">基于兴趣主题的实时聊天室</p>

        <div className="auth-tabs">
          <button
            className={mode === 'login' ? 'auth-tab active' : 'auth-tab'}
            onClick={() => setMode('login')}
            type="button"
          >
            登录
          </button>
          <button
            className={mode === 'register' ? 'auth-tab active' : 'auth-tab'}
            onClick={() => setMode('register')}
            type="button"
          >
            注册
          </button>
        </div>

        <form onSubmit={handleSubmit} className="auth-form">
          <input
            type="text"
            placeholder="用户名"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
            autoComplete="username"
          />
          {mode === 'register' && (
            <p className="auth-hint">3-32 位，支持字母、数字、下划线（纯数字也可以）</p>
          )}
          <input
            type="password"
            placeholder="密码（至少8位，需同时包含字母和数字）"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            minLength={mode === 'register' ? 8 : undefined}
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
          />
          {error && <p className="auth-error">{error}</p>}
          {notice && <p className="auth-notice">{notice}</p>}
          <button type="submit" disabled={loading} className="auth-submit">
            {loading ? '处理中…' : mode === 'login' ? '登录' : '注册'}
          </button>
        </form>

        <div className="auth-divider">或</div>
        <button onClick={handleGuest} disabled={loading} className="auth-guest">
          以访客身份快速体验
        </button>
      </div>
    </div>
  )
}
