/**
 * Task9 前端类型定义：与后端 API 响应结构保持一致（见 server/internal/api/*.go）。
 * 只声明前端实际会用到的字段，避免与后端 DTO 产生不必要的强耦合。
 */

export interface Room {
  id: string
  name: string
  topic: string
  online_count: number
}

export interface RoomMessage {
  id: string
  msg_id: string
  sender_id: string
  sender_type: 'human' | 'bot'
  content: string
  content_type: 'text' | 'image' | 'voice' | 'file'
  created_at: string
}

export interface AuthResult {
	token?: string // 仅兼容非浏览器 API 客户端；浏览器使用 HttpOnly Cookie。
  user_id: string
  is_guest?: boolean
}

/** 好友（GET /api/friends，见 server/internal/api/friend.go friendResponse）。 */
export interface Friend {
  user_id: string
  username: string
  online: boolean
}

/** 待处理好友请求（GET /api/friends/requests，本轮新增，对应 T120）。 */
export interface PendingFriendRequest {
  request_id: string
  peer_id: string
  peer_username: string
  direction: 'incoming' | 'outgoing'
  created_at: string
}

/** 用户基础信息（GET /api/users/lookup、GET /api/users，本轮新增，对应 T121/T122）。 */
export interface UserLookupResult {
  id: string
  username: string
}

/** 私聊会话摘要（GET /api/conversations）。 */
export interface ConversationSummary {
  conversation_id: string
  peer_id: string
  last_message: string
  last_message_at: string
  unread_count: number
}

/** 私聊历史消息（GET /api/conversations/:id/messages）。 */
export interface DirectMessageItem {
  id: string
  msg_id: string
  sender_id: string
  sender_type: 'human' | 'bot'
  content: string
  content_type: 'text' | 'image' | 'voice' | 'file'
  created_at: string
}

/** 关注事项（GET/POST /api/watch-topics）。 */
export interface WatchTopic {
  id: string
  room_id?: string
  keywords: string
  priority: number
  expires_at?: string
}

/** AI 推荐候选（GET /api/recommendations）。 */
export interface RecommendationCandidate {
  candidate_id: string
  peer_id: string
  peer_username: string
  shared_topic?: string
  room_id?: string
  match_reason: string
  match_score: number
}

/** WS 客户端 -> 服务端 事件（见 server/internal/ws/message.go ClientMessage）。 */
export type ClientEvent =
  | { type: 'join_room'; room_id: string }
  | { type: 'leave_room'; room_id: string }
  | {
      type: 'send_message'
      room_id: string
      msg_id: string
      content: string
      content_type?: 'text' | 'image'
    }
  | {
      type: 'send_direct_message'
      target_user_id: string
      msg_id: string
      content: string
      content_type?: 'text' | 'image'
    }

/** WS 服务端 -> 客户端 事件（见 server/internal/ws/message.go ServerMessage）。 */
export interface ServerEvent {
  type: string
  user_id?: string
  room_id?: string
  msg_id?: string
  content?: string
  content_type?: string
  sender_id?: string
  sender_type?: string
  online_count?: number
  code?: string
  request_id?: string
  from_user_id?: string
  conversation_id?: string
  target_user_id?: string
}
