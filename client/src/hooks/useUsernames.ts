import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import { useAuth } from '../context/AuthContext'
import type { UserLookupResult } from '../types'

/**
 * 批量解析用户 ID -> 用户名，供聊天页/会话列表展示真实用户名
 * （替代此前"用户ID前8位"的占位展示，见 `GET /api/users?ids=`，本轮新增）。
 * 内部维护一个跨调用的缓存 Map，同一 ID 只请求一次；调用方通过 `resolve(ids)`
 * 追加需要解析的 ID 集合，通过 `names` 读取当前已知的 id -> username 映射。
 */
export function useUsernames() {
  const { token } = useAuth()
  const [names, setNames] = useState<Record<string, string>>({})
  const knownIds = useRef<Set<string>>(new Set())
  const pending = useRef<Set<string>>(new Set())

  const resolve = useCallback(
    (ids: string[]) => {
      const missing = ids.filter((id) => id && !knownIds.current.has(id) && !pending.current.has(id))
      if (missing.length === 0) return
      missing.forEach((id) => pending.current.add(id))

      api
        .get<UserLookupResult[]>(`/api/users?ids=${missing.map(encodeURIComponent).join(',')}`, token)
        .then((users) => {
          const next: Record<string, string> = {}
          users.forEach((u) => {
            next[u.id] = u.username
            knownIds.current.add(u.id)
          })
          missing.forEach((id) => {
            knownIds.current.add(id) // 即使后端未返回（如用户已注销）也标记为"已尝试"，避免重复请求
            pending.current.delete(id)
          })
          if (Object.keys(next).length > 0) {
            setNames((prev) => ({ ...prev, ...next }))
          }
        })
        .catch(() => {
          missing.forEach((id) => pending.current.delete(id))
        })
    },
    [token],
  )

  useEffect(() => {
    // token 变化（如重新登录）时缓存清空，避免跨账号残留。
    knownIds.current = new Set()
    pending.current = new Set()
    setNames({})
  }, [token])

  return { names, resolve }
}
