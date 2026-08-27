#!/usr/bin/env python3
"""兴趣搭子在线聊天室 —— REST 接口集成测试（Task11：测试与代码质量收尾新增）。

背景：此前每个 Task 都在 Go 层面写了单元测试（service 层用内存假 Store，覆盖率见
`go test ./... -cover`），但缺一份可重复运行的"黑盒集成测试"——真实开发过程中的
接口级验证（对应 testcase/00-testcase-plan.md 里的 T10-T139）都是临时写在 `/tmp/`
下的一次性脚本，验证完就丢弃，没有沉淀为仓库内可复用的资产。本脚本把其中不依赖
WebSocket 的 REST 接口部分固化下来，覆盖注册登录、房间、好友、私聊会话、关注事项、
AI推荐、Web Push 订阅管理的主路径 + 关键边界。WebSocket 相关用例（群聊/私聊实时
收发、限流、跨实例广播等）仍按 testcase 文档里记录的方式人工/脚本按需验证，不在
本脚本覆盖范围（Python 标准库没有内置 WS 客户端，引入额外依赖对"零依赖即可跑"的
诉求不划算）。

用法：
    python3 scripts/integration_test.py [BASE_URL]
    默认 BASE_URL=http://localhost:8080，假设服务已通过 docker compose 启动。

退出码：全部断言通过为 0；任意一条失败立即退出并打印失败详情，退出码为 1。
"""
import json
import sys
import urllib.error
import urllib.request
import uuid

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"

_failures = 0
_passes = 0


def req(method, path, body=None, token=None, idempotency_key=None):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    if idempotency_key:
        headers["Idempotency-Key"] = idempotency_key
    r = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(r, timeout=10) as resp:
            text = resp.read().decode()
            return resp.status, (json.loads(text) if text else {})
    except urllib.error.HTTPError as e:
        text = e.read().decode()
        return e.code, (json.loads(text) if text else {})


def check(cond, msg):
    global _failures, _passes
    if cond:
        _passes += 1
        print(f"[PASS] {msg}")
    else:
        _failures += 1
        print(f"[FAIL] {msg}")


def fresh_user(prefix):
    """注册并登录一个全新用户，返回 (token, user_id, username)。用随机后缀避免多次
    运行之间的用户名冲突（`users.username` 唯一约束）。"""
    name = f"{prefix}_{uuid.uuid4().hex[:8]}"
    s, r = req("POST", "/api/auth/register", {"username": name, "password": "password123"})
    check(s == 201, f"register {prefix} -> {s}")
    s, r = req("POST", "/api/auth/login", {"username": name, "password": "password123"})
    check(s == 200 and "token" in r, f"login {prefix} -> {s}")
    return r.get("token"), r.get("user_id"), name


def run():
    # ---------- 0. 健康检查 ----------
    s, r = req("GET", "/healthz")
    check(s == 200, f"T00 healthz -> {s}")
    s, r = req("GET", "/readyz")
    check(s == 200 and r.get("status") == "ready", f"T00 readyz -> {s} {r}")

    # ---------- 1. 鉴权（T10-T15） ----------

    # ---------- 0.5 命令格式/重放保护（V1-V3，2026-08-26） ----------
    replay_name = f"replay_{uuid.uuid4().hex[:8]}"
    replay_key = f"register-{uuid.uuid4().hex}"
    replay_body = {"username": replay_name, "password": "password123"}
    s, first_replay = req("POST", "/api/auth/register", replay_body, idempotency_key=replay_key)
    s2, second_replay = req("POST", "/api/auth/register", replay_body, idempotency_key=replay_key)
    check(s == 201 and s2 == 201 and first_replay == second_replay,
          f"V3 相同 Idempotency-Key 重放注册应返回首次响应且不重复创建 -> {s}/{s2}")
    s, r = req("POST", "/api/auth/register", {"username": f"strict_{uuid.uuid4().hex[:8]}", "password": "password123", "typo": True})
    check(s == 400 and r.get("error") == "invalid_request_body", f"V1 未知 JSON 字段应拒绝 -> {s} {r}")

    s, r = req("POST", "/api/auth/register", {"username": "ab", "password": "password123"})
    check(s == 400, f"T10 register 用户名过短 -> {s}")
    s, r = req("POST", "/api/auth/login", {"username": "no_such_user_xyz", "password": "wrong"})
    check(s == 401, f"T12 login 用户不存在 -> {s}")
    s, r = req("POST", "/api/auth/guest", {})
    check(s == 200 and r.get("is_guest") is True, f"T14 guest 模式 -> {s} {r}")

    # ---------- 1.1 密码复杂度校验（能力补齐项：纯数字/纯字母密码应被拒绝） ----------
    digits_only_name = f"pwtest_{uuid.uuid4().hex[:8]}"
    s, r = req("POST", "/api/auth/register", {"username": digits_only_name, "password": "12345678"})
    check(s == 400 and r.get("error") == "invalid_password", f"能力补齐：纯数字密码应被拒绝 -> {s} {r}")
    letters_only_name = f"pwtest_{uuid.uuid4().hex[:8]}"
    s, r = req("POST", "/api/auth/register", {"username": letters_only_name, "password": "abcdefgh"})
    check(s == 400 and r.get("error") == "invalid_password", f"能力补齐：纯字母密码应被拒绝 -> {s} {r}")
    valid_complex_name = f"pwtest_{uuid.uuid4().hex[:8]}"
    s, r = req("POST", "/api/auth/register", {"username": valid_complex_name, "password": "abcd1234"})
    check(s == 201, f"能力补齐：字母+数字混合密码应被接受 -> {s} {r}")

    alice_token, alice_id, alice_name = fresh_user("alice")
    bob_token, bob_id, bob_name = fresh_user("bob")

    # ---------- 2. 房间（T20-T22） ----------
    s, r = req("GET", "/api/rooms")
    check(s == 200 and isinstance(r, list) and len(r) >= 4, f"T20 房间列表 -> {s} 共{len(r) if isinstance(r, list) else 'N/A'}个")
    room_id = r[0]["id"] if isinstance(r, list) and r else None
    if room_id:
        s, r2 = req("GET", f"/api/rooms/{room_id}/messages?page=1&size=10")
        check(s == 200 and "messages" in r2, f"T21 房间历史消息 -> {s}")
        s, r2 = req("GET", f"/api/rooms/{room_id}/messages?page=oops")
        check(s == 400 and r2.get("error") == "invalid_page", f"V2 非法分页应拒绝 -> {s} {r2}")
    s, r = req("GET", "/api/rooms/00000000-0000-0000-0000-000000000000/messages")
    check(s == 404, f"T22 非法房间ID历史消息 -> {s}")

    # ---------- 3. 用户查找/批量查询（T121/T122） ----------
    s, r = req("GET", f"/api/users/lookup?username={bob_name}", token=alice_token)
    check(s == 200 and r.get("id") == bob_id, f"T121 按用户名查找 -> {s}")
    s, r = req("GET", "/api/users/lookup?username=no_such_user_xyz", token=alice_token)
    check(s == 404 and r.get("error") == "user_not_found", f"T121 查找不存在的用户 -> {s}")
    s, r = req("GET", f"/api/users?ids={alice_id},{bob_id},not-a-uuid", token=alice_token)
    names = {u["id"]: u["username"] for u in r} if s == 200 else {}
    check(s == 200 and names.get(alice_id) == alice_name and names.get(bob_id) == bob_name,
          f"T122 批量查用户名（含非法ID不报错） -> {s}")

    # ---------- 4. 好友关系链（T60-T63, T120） ----------
    s, r = req("GET", "/api/friends/requests", token=alice_token)
    check(s == 200 and r == [], f"T120 初始待处理请求为空 -> {s}")
    s, r = req("POST", "/api/friends/requests", {"target_user_id": bob_id}, token=alice_token)
    check(s == 201, f"T60 发起好友请求 -> {s}")
    request_id = r.get("request_id")
    s, r = req("POST", "/api/friends/requests", {"target_user_id": bob_id}, token=alice_token)
    check(s == 409, f"T60 重复发起好友请求幂等拒绝 -> {s}")
    s, r = req("GET", "/api/friends/requests", token=bob_token)
    check(s == 200 and len(r) == 1 and r[0]["direction"] == "incoming", f"T120 bob 收到的待处理请求 -> {s}")
    s, r = req("PUT", f"/api/friends/requests/{request_id}", {"action": "accept"}, token=bob_token)
    check(s == 200 and r.get("status") == "accepted", f"T61 接受好友请求 -> {s}")
    s, r = req("PUT", f"/api/friends/requests/{request_id}", {"action": "accept"}, token=bob_token)
    check(s == 409, f"T62 重复处理已结束请求 -> {s}")
    s, r = req("GET", "/api/friends", token=alice_token)
    check(s == 200 and any(f["user_id"] == bob_id for f in r), f"T63 好友列表包含 bob -> {s}")

    # ---------- 5. 关注事项（T95, T123） ----------
    s, r = req("POST", "/api/watch-topics", {"keywords": "摄影,徒步", "priority": 3}, token=alice_token)
    check(s == 201, f"T95 创建关注事项 -> {s}")
    topic_id = r.get("topic_id")
    s, r = req("POST", "/api/watch-topics", {"keywords": ""}, token=alice_token)
    check(s == 400, f"T95 关键词为空拒绝 -> {s}")
    s, r = req("GET", "/api/watch-topics", token=alice_token)
    check(s == 200 and len(r) == 1, f"T95 关注事项列表 -> {s}")
    s, r = req("DELETE", f"/api/watch-topics/{topic_id}", token=alice_token)
    check(s == 204, f"T123 删除关注事项 -> {s}")
    s, r = req("DELETE", f"/api/watch-topics/{topic_id}", token=alice_token)
    check(s == 404, f"T123 重复删除返回404 -> {s}")
    s, r = req("DELETE", f"/api/watch-topics/{topic_id}", token=bob_token)
    check(s == 404, f"T123 删除非本人所有的关注事项返回404（越权） -> {s}")

    # ---------- 6. AI 推荐（T110-T112） ----------
    carol_token, carol_id, carol_name = fresh_user("carol")
    req("POST", "/api/watch-topics", {"keywords": "摄影,爬山"}, token=alice_token)
    req("POST", "/api/watch-topics", {"keywords": "摄影,烘焙"}, token=carol_token)
    s, r = req("POST", "/api/recommendations/generate", token=alice_token)
    check(s == 200 and "created" in r, f"T110 生成推荐 -> {s}")
    s, r = req("GET", "/api/recommendations", token=alice_token)
    check(s == 200, f"T111 推荐列表 -> {s}")
    carol_candidate = next((c for c in r if c.get("peer_id") == carol_id), None)
    check(carol_candidate is not None, "T111 推荐列表包含 carol")
    if carol_candidate:
        s, r = req("PUT", f"/api/recommendations/{carol_candidate['candidate_id']}", {"action": "confirm"}, token=alice_token)
        check(s == 200, f"T112 确认推荐候选 -> {s}")
        s, r = req("PUT", f"/api/recommendations/{carol_candidate['candidate_id']}", {"action": "confirm"}, token=alice_token)
        check(s == 409, f"T112 重复处理已结束候选 -> {s}")
    s, r = req("PUT", "/api/recommendations/00000000-0000-0000-0000-000000000000", {"action": "confirm"}, token=alice_token)
    check(s == 404, f"T112 候选不存在 -> {s}")

    # ---------- 7. 私聊会话（T70-T71） ----------
    s, r = req("GET", "/api/conversations", token=alice_token)
    check(s == 200 and r == [], f"T71 alice/bob 尚未发过私聊消息，会话列表为空 -> {s}")
    s, r = req("GET", "/api/conversations/00000000-0000-0000-0000-000000000000/messages", token=alice_token)
    check(s == 404, f"T70 会话不存在 -> {s}")

    # ---------- 8. Web Push 订阅管理（T100-T101） ----------
    s, r = req("GET", "/api/push/vapid-public-key")
    check(s == 200 and "public_key" in r, f"T99 获取 VAPID 公钥（无需鉴权） -> {s}")
    sub_body = {"endpoint": f"https://example.com/push/{uuid.uuid4().hex}", "keys": {"p256dh": "fake-p256dh-value", "auth": "fake-auth-value"}}
    s, r = req("POST", "/api/push/subscriptions", sub_body, token=alice_token)
    check(s == 201, f"T100 创建推送订阅 -> {s}")
    s, r = req("DELETE", "/api/push/subscriptions", {"endpoint": sub_body["endpoint"]}, token=alice_token)
    check(s == 204, f"T101 删除推送订阅 -> {s}")

    # ---------- 9. 举报（T82，目标不存在场景） ----------
    s, r = req("POST", "/api/reports", {"target_type": "user", "target_id": "00000000-0000-0000-0000-000000000000", "reason": "spam"}, token=alice_token)
    check(s == 404, f"T82 举报不存在的用户 -> {s}")
    s, r = req("POST", "/api/reports", {"target_type": "user", "target_id": bob_id, "reason": "spam"}, token=alice_token)
    check(s == 201, f"T82 举报存在的用户 -> {s}")

    # ---------- 10. 未鉴权访问拦截（T8x 安全回归） ----------
    s, r = req("GET", "/api/friends")
    check(s == 401, f"未鉴权访问 /api/friends -> {s}")
    s, r = req("GET", "/api/watch-topics")
    check(s == 401, f"未鉴权访问 /api/watch-topics -> {s}")

    # ---------- 10.5 登出后 token 立即失效（能力补齐项） ----------
    logout_token, logout_user_id, logout_name = fresh_user("logouttest")
    s, r = req("GET", "/api/friends", token=logout_token)
    check(s == 200, f"能力补齐：登出前 token 应可正常访问 -> {s}")
    s, r = req("POST", "/api/auth/logout", {}, token=logout_token)
    check(s == 200, f"能力补齐：登出接口本身 -> {s}")
    s, r = req("GET", "/api/friends", token=logout_token)
    check(s == 401 and r.get("error") == "token_revoked", f"能力补齐：登出后旧 token 应立即失效 -> {s} {r}")

    # ---------- 11. 登录接口暴力破解防护（能力补齐项，必须放在本脚本最后一步） ----------
    # 限流按客户端 IP 做 60 秒固定窗口计数（默认 LOGIN_RATE_LIMIT_PER_MINUTE=10），
    # 本脚本前面已经调用过若干次 /api/auth/login（T12 + 3 个 fresh_user），因此这里
    # 不假设"恰好第 11 次触发"，而是连续打 20 次，只要求"这个窗口内出现过 429"，
    # 对已消耗的额度更鲁棒。注意：这一步会真实消耗/占满该 IP 当前 60 秒窗口的登录额度，
    # 必须是脚本最后一步，否则后面的正常登录断言会被误伤为 429；若在 60 秒内重复手动
    # 运行本脚本，本段可能导致前面的正常登录断言提前失败，可用
    # `docker exec <redis容器> redis-cli DEL login_rate:<IP>` 清空计数器后重试。
    saw_429 = False
    for _ in range(20):
        s, _ = req("POST", "/api/auth/login", {"username": "no_such_user_bruteforce", "password": "wrongpass"})
        if s == 429:
            saw_429 = True
            break
    check(saw_429, "能力补齐：登录接口触发暴力破解限流（429 login_rate_limited）")


if __name__ == "__main__":
    run()
    print(f"\n==> {_passes} passed, {_failures} failed (against {BASE})")
    sys.exit(1 if _failures else 0)
