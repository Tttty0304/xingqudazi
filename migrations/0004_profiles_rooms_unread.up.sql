-- 产品闭环补齐：用户资料、用户创建房间与已读游标。
ALTER TABLE users
  ADD COLUMN IF NOT EXISTS avatar_url VARCHAR(512),
  ADD COLUMN IF NOT EXISTS bio VARCHAR(280) NOT NULL DEFAULT '';

ALTER TABLE rooms
  ADD COLUMN IF NOT EXISTS creator_id UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_rooms_creator_created ON rooms(creator_id, created_at DESC);

-- 群聊和私聊的已读状态分别按用户+会话持久化。last_read_message_id 可为空，
-- 以 read_at 作为兼容旧消息/顺序判断的兜底字段。
CREATE TABLE IF NOT EXISTS room_read_cursors (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  room_id UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
  last_read_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
  read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, room_id)
);

CREATE TABLE IF NOT EXISTS conversation_read_cursors (
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  last_read_message_id UUID REFERENCES direct_messages(id) ON DELETE SET NULL,
  read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (user_id, conversation_id)
);
