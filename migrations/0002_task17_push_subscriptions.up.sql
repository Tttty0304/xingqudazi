-- 0002_task17_push_subscriptions.up.sql
-- Task17（Web Push 离线通知）：浏览器 PushSubscription 信息持久化。
-- 未在 0001 中预先建表（0001 是 Plan Part3 数据映射确认时的表清单，Web Push 订阅表是
-- Task17 实现时才明确需要的具体存储结构），故单独建一个迁移文件，不回填修改 0001。

CREATE TABLE IF NOT EXISTS push_subscriptions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint    TEXT NOT NULL,
    p256dh      VARCHAR(255) NOT NULL, -- 浏览器订阅公钥（base64url，未加 padding）
    auth        VARCHAR(255) NOT NULL, -- 浏览器订阅认证密钥（base64url，未加 padding）
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, endpoint)
);
CREATE INDEX IF NOT EXISTS idx_push_subscriptions_user ON push_subscriptions(user_id);
