-- 0003_perf_indexes.up.sql
-- Task10（性能与成本落地）：补齐此前遗漏的索引。
--
-- 背景：friendships/conversations/match_candidates 三张表的查询模式都是
-- `WHERE (col_a = $1 OR col_b = $1)`（双向关系表的常见写法），但 0001 建表时
-- 只给其中一侧建了索引（如 match_candidates 只有 idx_match_candidates_user_a，
-- 没有对应的 user_b 索引），导致按 user_b 一侧命中的查询无法走索引，随数据量
-- 增长会退化为全表扫描。当前数据量下 EXPLAIN 尚未表现出明显代价，但索引缺口是
-- 真实存在的设计遗漏，补齐成本极低（纯新增索引，不改变任何查询结果），随建随用。

CREATE INDEX IF NOT EXISTS idx_friendships_requester ON friendships(requester_id, status);
CREATE INDEX IF NOT EXISTS idx_conversations_user_b ON conversations(user_b_id);
CREATE INDEX IF NOT EXISTS idx_match_candidates_user_b ON match_candidates(user_b_id, status);
