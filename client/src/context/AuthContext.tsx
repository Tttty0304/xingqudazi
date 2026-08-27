import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, COOKIE_SESSION_TOKEN } from '../api/client'
import type { AuthResult } from '../types'

interface AuthState {
  token: string | null
  userId: string | null
  username: string | null
  isGuest: boolean
}

interface AuthContextValue extends AuthState {
  register: (username: string, password: string) => Promise<void>
  login: (username: string, password: string) => Promise<void>
  guestLogin: () => Promise<void>
  logout: () => void
}

const STORAGE_KEY = 'xingqudazi_im_auth'

function loadInitialState(): AuthState {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return { token: null, userId: null, username: null, isGuest: false }
    const stored = JSON.parse(raw) as Omit<AuthState, 'token'>
    // 旧版本可能在此对象内存有 JWT；读取时故意忽略它，并在下一次 persist 时移除。
    return stored.userId ? { ...stored, token: COOKIE_SESSION_TOKEN } : { token: null, userId: null, username: null, isGuest: false }
  } catch {
    return { token: null, userId: null, username: null, isGuest: false }
  }
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>(loadInitialState)

  const persist = useCallback((next: AuthState) => {
    setState(next)
    const { token: _, ...safeState } = next
    localStorage.setItem(STORAGE_KEY, JSON.stringify(safeState))
  }, [])

  useEffect(() => {
    if (!state.token) return
    // localStorage 只保存展示信息；实际是否仍登录以服务端校验 HttpOnly Cookie 为准。
    api.get<AuthResult>('/api/auth/session')
      .then((session) => {
        setState((current) => current.token
          ? { ...current, userId: session.user_id, isGuest: Boolean(session.is_guest) }
          : current)
      })
      .catch(() => persist({ token: null, userId: null, username: null, isGuest: false }))
  // 仅在应用初始化和 Cookie 会话状态发生切换时验证，避免每次展示名称变化都请求。
  }, [state.token, persist])

  const register = useCallback(async (username: string, password: string) => {
    await api.post('/api/auth/register', { username, password })
  }, [])

  const login = useCallback(
    async (username: string, password: string) => {
      const result = await api.post<AuthResult>('/api/auth/login', { username, password })
      persist({ token: COOKIE_SESSION_TOKEN, userId: result.user_id, username, isGuest: false })
    },
    [persist],
  )

  const guestLogin = useCallback(async () => {
    const result = await api.post<AuthResult>('/api/auth/guest', {})
    persist({ token: COOKIE_SESSION_TOKEN, userId: result.user_id, username: '访客', isGuest: true })
  }, [persist])

  const logout = useCallback(() => {
    persist({ token: null, userId: null, username: null, isGuest: false })
    // Cookie 会随同源请求自动发送；登出后由服务端拉黑并清除 HttpOnly Cookie。
    if (state.token) {
      api.post('/api/auth/logout').catch(() => {})
    }
  }, [state.token, persist])

  const value = useMemo<AuthContextValue>(
    () => ({ ...state, register, login, guestLogin, logout }),
    [state, register, login, guestLogin, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}
