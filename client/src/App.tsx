import { Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider } from './context/AuthContext'
import { SocketProvider } from './context/SocketContext'
import { ProtectedRoute } from './components/ProtectedRoute'
import { Layout } from './components/Layout'
import { AuthPage } from './pages/AuthPage'
import { RoomListPage } from './pages/RoomListPage'
import { RoomChatPage } from './pages/RoomChatPage'
import { FriendsPage } from './pages/FriendsPage'
import { ConversationsPage } from './pages/ConversationsPage'
import { DirectChatPage } from './pages/DirectChatPage'
import { WatchTopicsPage } from './pages/WatchTopicsPage'
import { RecommendationsPage } from './pages/RecommendationsPage'
import { ProfilePage } from './pages/ProfilePage'

/**
 * Task9 前端页面与交互：登录注册 / 房间列表 / 聊天页 / 好友 / 私聊 / 关注事项 / AI推荐，
 * 覆盖空态/加载态/错误态（★10 已确认：响应式网页方案）。
 * 本轮新增好友/私聊/关注事项/AI推荐页面（补齐 Task14/15/19/20 后端能力此前缺失的
 * 前端 UI），`SocketProvider` 提升为应用级别唯一 WS 连接（见 context/SocketContext.tsx）。
 */
function App() {
  return (
    <AuthProvider>
      <SocketProvider>
        <Routes>
          <Route path="/login" element={<AuthPage />} />
          <Route element={<ProtectedRoute />}>
            <Route element={<Layout />}>
              <Route path="/rooms" element={<RoomListPage />} />
              <Route path="/rooms/:roomId" element={<RoomChatPage />} />
              <Route path="/friends" element={<FriendsPage />} />
              <Route path="/messages" element={<ConversationsPage />} />
              <Route path="/messages/:peerId" element={<DirectChatPage />} />
              <Route path="/watch-topics" element={<WatchTopicsPage />} />
              <Route path="/recommendations" element={<RecommendationsPage />} />
              <Route path="/profile" element={<ProfilePage />} />
            </Route>
          </Route>
          <Route path="*" element={<Navigate to="/rooms" replace />} />
        </Routes>
      </SocketProvider>
    </AuthProvider>
  )
}

export default App
