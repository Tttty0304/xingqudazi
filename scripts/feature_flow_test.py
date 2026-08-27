#!/usr/bin/env python3
"""功能链路测试 + 性能采样脚本。

用真实账号在目标服务上走通「注册 → 登录 → 加好友 → 关注事项 → AI 推荐」完整
业务链路，逐接口记录状态码、响应时间、结果，供撰写压测/功能验证报告使用。

用法：
    python3 scripts/feature_flow_test.py [BASE_URL]
    默认 BASE_URL=http://111.230.227.135
"""
import json
import sys
import time
import urllib.error
import urllib.request
import uuid

BASE = sys.argv[1].rstrip("/") if len(sys.argv) > 1 else "http://111.230.227.135"

results = []  # (名称, 状态码, 耗时ms, 结果摘要)


def req(method, path, body=None, token=None):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    r = urllib.request.Request(url, data=data, method=method, headers=headers)
    t0 = time.monotonic()
    try:
        with urllib.request.urlopen(r, timeout=15) as resp:
            text = resp.read().decode()
            ms = (time.monotonic() - t0) * 1000
            return resp.status, (json.loads(text) if text else {}), ms
    except urllib.error.HTTPError as e:
        text = e.read().decode()
        ms = (time.monotonic() - t0) * 1000
        return e.code, (json.loads(text) if text else {}), ms


def record(name, status, ms, detail=""):
    results.append((name, status, ms, detail))
    print(f"  [{status}] {name:<32} {ms:>7.1f}ms  {detail}")


def main():
    tag = uuid.uuid4().hex[:6]
    a = f"ft_a_{tag}"
    b = f"ft_b_{tag}"
    c = f"ft_c_{tag}"
    pw = "Feature123"

    print(f"==> 目标服务: {BASE}")
    print(f"==> 测试账号: {a} / {b} / {c}")

    # 1. 注册
    for u in (a, b, c):
        s, r, ms = req("POST", "/api/auth/register", {"username": u, "password": pw})
        record(f"注册 {u}", s, ms, r.get("username", r.get("error", "")))

    # 2. 登录
    tokens = {}
    ids = {}
    for u in (a, b, c):
        s, r, ms = req("POST", "/api/auth/login", {"username": u, "password": pw})
        tokens[u] = r.get("token")
        ids[u] = r.get("user_id")
        record(f"登录 {u}", s, ms, f"user_id={str(r.get('user_id',''))[:8]}")

    # 3. 好友链路：A -> B 发请求，B 接受；A -> C 发请求（留一个 pending）
    s, r, ms = req("POST", "/api/friends/requests", {"target_user_id": ids[b]}, token=tokens[a])
    record("A 发起好友请求 -> B", s, ms, r.get("request_id", r.get("error", "")))
    if s == 201:
        rid = r["request_id"]
        s2, r2, ms2 = req("PUT", f"/api/friends/requests/{rid}", {"action": "accept"}, token=tokens[b])
        record("B 接受好友请求", s2, ms2, r2.get("error", "ok"))

    s, r, ms = req("POST", "/api/friends/requests", {"target_user_id": ids[c]}, token=tokens[a])
    record("A 发起好友请求 -> C（pending）", s, ms, r.get("request_id", r.get("error", "")))

    # 好友列表
    s, r, ms = req("GET", "/api/friends", token=tokens[a])
    record("A 好友列表", s, ms, f"共 {len(r) if isinstance(r, list) else 0} 位好友")

    # 4. 关注事项
    for u, kw in ((a, "摄影,徒步,咖啡"), (b, "摄影,烘焙"), (c, "徒步,旅行")):
        s, r, ms = req("POST", "/api/watch-topics", {"keywords": kw}, token=tokens[u])
        record(f"关注事项 {u}", s, ms, r.get("id", r.get("error", "")))

    s, r, ms = req("GET", "/api/watch-topics", token=tokens[a])
    record("A 关注事项列表", s, ms, f"共 {len(r) if isinstance(r, list) else 0} 条")

    # 5. AI 推荐
    s, r, ms = req("POST", "/api/recommendations/generate", token=tokens[a])
    record("A 生成 AI 推荐", s, ms, r.get("generated", r.get("error", "")))

    s, r, ms = req("GET", "/api/recommendations", token=tokens[a])
    n = len(r) if isinstance(r, list) else 0
    record("A AI 推荐候选列表", s, ms, f"共 {n} 条候选")
    # 打印候选详情
    if isinstance(r, list):
        for cand in r[:5]:
            print(f"      候选: peer={cand.get('peer_username')} 分数={cand.get('match_score')} 理由={cand.get('match_reason','')[:40]}")

    # 6. 处理一条推荐（confirm）
    if isinstance(r, list) and r:
        cid = r[0].get("candidate_id")
        s, r2, ms = req("PUT", f"/api/recommendations/{cid}", {"action": "confirm"}, token=tokens[a])
        record("A 确认一条推荐", s, ms, r2.get("error", "ok"))

    # 7. 私聊（仅好友 A-B 可私聊，走 WS 之外先验证会话列表）
    s, r, ms = req("GET", "/api/conversations", token=tokens[a])
    record("A 会话列表", s, ms, f"共 {len(r) if isinstance(r, list) else 0} 个会话")

    print("\n" + "=" * 70)
    print("汇总统计：")
    total = len(results)
    ok = sum(1 for _, s, _, _ in results if 200 <= s < 300)
    err = sum(1 for _, s, _, _ in results if s >= 400)
    lat = [ms for _, _, ms, _ in results]
    print(f"  总操作数: {total}  成功(2xx): {ok}  失败(4xx/5xx): {err}")
    print(f"  接口响应时间 avg={sum(lat)/len(lat):.1f}ms  max={max(lat):.1f}ms  min={min(lat):.1f}ms")
    print("=" * 70)


if __name__ == "__main__":
    main()
