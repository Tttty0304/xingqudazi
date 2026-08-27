import { Navigate, Outlet } from 'react-router-dom'
import { useAuth } from '../context/AuthContext'

/** 未登录用户访问受保护路由时重定向到登录页（Task9：空态/未鉴权态处理）。 */
export function ProtectedRoute() {
  const { token } = useAuth()
  if (!token) {
    return <Navigate to="/login" replace />
  }
  return <Outlet />
}
