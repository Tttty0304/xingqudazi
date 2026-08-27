-- 0001_init_schema.up.sql
-- 兴趣搭子在线聊天室 —— 初始 schema
-- 覆盖 Plan Part3「接口/数据/事件映射」与「AI-native 二期扩展设计」确认的 P0 表结构。
-- 一次性建齐字段（含 AI-native 远期预留列），避免后续 Task 反复二次迁移。

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS vector; -- pgvector，★4 已确认

-- ========== 用户体系（Task2） ==========
CREATE TABLE IF NOT EXISTS users (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    username            VARCHAR(64) UNIQUE NOT NULL,
    password_hash       VARCHAR(255),           -- 访客用户为空
    is_guest            BOOLEAN NOT NULL DEFAULT false,
    -- AI-native 参与者模型预留（Plan Part3「AI-native 二期扩展设计」，Task19）
    is_bot              BOOLEAN NOT NULL DEFAULT false,
    proxy_for_user_id   UUID REFERENCES users(id) ON DELETE SET NULL,
    -- AI-native 兴趣画像向量预留（★4，pgvector；当前不写入真实数据）
    interest_embedding  vector(768),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_users_is_bot ON users(is_bot);

-- ========== 房间（Task3） ==========
CREATE TABLE IF NOT EXISTS rooms (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name        VARCHAR(64) NOT NULL,
    topic       VARCHAR(255),
    is_preset   BOOLEAN NOT NULL DEFAULT true,   -- 预置兴趣房间（问题#6：暂不开放用户建房）
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ========== 群聊消息（Task5） ==========
CREATE TABLE IF NOT EXISTS messages (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    msg_id       VARCHAR(64) NOT NULL,           -- 客户端幂等去重键
    room_id      UUID NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    sender_id    UUID NOT NULL REFERENCES users(id),
    -- AI-native 交互留痕/透明度预留（★13，Task19）
    sender_type  VARCHAR(16) NOT NULL DEFAULT 'human' CHECK (sender_type IN ('human', 'bot')),
    content      TEXT NOT NULL,
    content_type VARCHAR(16) NOT NULL DEFAULT 'text' CHECK (content_type IN ('text', 'image', 'voice', 'file')),
    -- AI-native 消息语义向量预留（★4，pgvector；当前不写入真实数据）
    embedding    vector(768),
    is_blocked   BOOLEAN NOT NULL DEFAULT false, -- 命中内容安全拦截（Task18）
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (room_id, msg_id)
);
CREATE INDEX IF NOT EXISTS idx_messages_room_created ON messages(room_id, created_at DESC);

-- ========== 好友关系链（Task14） ==========
CREATE TABLE IF NOT EXISTS friendships (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    requester_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    target_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    status        VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'accepted', 'rejected')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (requester_id, target_id)
);
CREATE INDEX IF NOT EXISTS idx_friendships_target ON friendships(target_id, status);

-- ========== 私聊（Task15） ==========
CREATE TABLE IF NOT EXISTS conversations (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_a_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_a_id, user_b_id)
);

CREATE TABLE IF NOT EXISTS direct_messages (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    msg_id           VARCHAR(64) NOT NULL,
    conversation_id  UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_id        UUID NOT NULL REFERENCES users(id),
    sender_type      VARCHAR(16) NOT NULL DEFAULT 'human' CHECK (sender_type IN ('human', 'bot')),
    content          TEXT NOT NULL,
    content_type     VARCHAR(16) NOT NULL DEFAULT 'text' CHECK (content_type IN ('text', 'image', 'voice', 'file')),
    is_blocked       BOOLEAN NOT NULL DEFAULT false,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (conversation_id, msg_id)
);
CREATE INDEX IF NOT EXISTS idx_direct_messages_conv_created ON direct_messages(conversation_id, created_at DESC);

-- ========== 多媒体消息元信息（Task16，P0图片/P1语音文件） ==========
CREATE TABLE IF NOT EXISTS media_assets (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    owner_id      UUID NOT NULL REFERENCES users(id),
    media_type    VARCHAR(16) NOT NULL CHECK (media_type IN ('image', 'voice', 'file')),
    url           VARCHAR(512) NOT NULL,
    mime_type     VARCHAR(128),
    size_bytes    BIGINT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ========== 内容安全（Task18） ==========
CREATE TABLE IF NOT EXISTS reports (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    reporter_id     UUID NOT NULL REFERENCES users(id),
    target_type     VARCHAR(16) NOT NULL CHECK (target_type IN ('message', 'direct_message', 'user')),
    target_id       UUID NOT NULL,
    reason          VARCHAR(255) NOT NULL,
    status          VARCHAR(16) NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'reviewed', 'dismissed')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ========== AI-native 数据生产与参与者模型预留（Task19，P0 schema） ==========

-- 结构化行为事件日志：训练信号的核心来源，历史数据无法回溯补采，必须从上线第一天开始记录。
CREATE TABLE IF NOT EXISTS interaction_events (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room_id         UUID REFERENCES rooms(id) ON DELETE SET NULL,
    target_user_id  UUID REFERENCES users(id) ON DELETE SET NULL,
    event_type      VARCHAR(32) NOT NULL, -- join_room/send_message/view_profile/long_dwell/add_friend 等
    payload         JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_interaction_events_user_created ON interaction_events(user_id, created_at DESC);

-- 关注事项（Task19，P1）：用户主动声明"最近关注XX话题"，是 Task20 简单匹配演示的直接输入源。
CREATE TABLE IF NOT EXISTS user_watch_topics (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    room_id     UUID REFERENCES rooms(id) ON DELETE SET NULL, -- 可空：全局关注 or 限定某房间
    keywords    VARCHAR(255) NOT NULL,
    priority    INT NOT NULL DEFAULT 0,
    expires_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_user_watch_topics_user ON user_watch_topics(user_id);

-- AI 推荐候选（Task20，P0建表+P1规则化生成逻辑）：承载"待人工确认列表"的推荐理由展示。
CREATE TABLE IF NOT EXISTS match_candidates (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_a_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    shared_topic  VARCHAR(255),
    room_id       UUID REFERENCES rooms(id) ON DELETE SET NULL,
    match_reason  TEXT,           -- 结构化推荐理由文案
    match_score   REAL,
    status        VARCHAR(16) NOT NULL DEFAULT 'pending_review'
                   CHECK (status IN ('pending_review', 'confirmed', 'dismissed')),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_a_id, user_b_id, room_id)
);
CREATE INDEX IF NOT EXISTS idx_match_candidates_user_a ON match_candidates(user_a_id, status);

-- 机器人决策留痕（Task19，P0字段/P2真实写入逻辑）：本次不产生机器人消息，故暂不写入，仅预留表结构。
CREATE TABLE IF NOT EXISTS bot_action_log (
    id                     UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    bot_user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    trigger_watch_topic_id UUID REFERENCES user_watch_topics(id) ON DELETE SET NULL,
    room_id                UUID REFERENCES rooms(id) ON DELETE SET NULL,
    decision_reason        TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ========== 种子数据：预置兴趣房间（问题#6） ==========
INSERT INTO rooms (name, topic, is_preset) VALUES
    ('数码', '数码产品与科技话题', true),
    ('追番', '动漫番剧讨论', true),
    ('运动', '运动健身话题', true),
    ('美食', '美食探店与烹饪', true)
ON CONFLICT DO NOTHING;
