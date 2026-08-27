-- 0001_init_schema.down.sql
-- 回滚 0001_init_schema.up.sql 创建的全部对象（逆序删除，遵守外键依赖）。

DROP TABLE IF EXISTS bot_action_log;
DROP TABLE IF EXISTS match_candidates;
DROP TABLE IF EXISTS user_watch_topics;
DROP TABLE IF EXISTS interaction_events;
DROP TABLE IF EXISTS reports;
DROP TABLE IF EXISTS media_assets;
DROP TABLE IF EXISTS direct_messages;
DROP TABLE IF EXISTS conversations;
DROP TABLE IF EXISTS friendships;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS users;
