#!/usr/bin/env python3
"""演示数据预置脚本（Task13：演示准备）。

现场演示时从空数据库开始会浪费时间在"注册两个账号、互相加好友、填关注事项"这些
操作上，本脚本用 REST 接口预先把这些状态搭好，演示者可以直接登录后展示"已经是
好友"“AI推荐已经有候选”这些效果，把演示时间留给真正需要临场展示的部分（群聊/
私聊实时收发、图片消息、Web Push 通知）。

用法：
    python3 scripts/demo_seed.py [BASE_URL]
    默认 BASE_URL=http://localhost:8080

创建结果：
    - 两个demo账号：demo_alice / demo_bob（密码均为 Demo123456）
    - 二者已互为好友
    - 二者各设置一条关注事项（关键词均含"摄影"），确保 AI 推荐页有真实候选
    - 打印可直接使用的登录信息，供演示时复制粘贴

重复运行是安全的：账号名固定，若已存在会直接登录复用，不会报错中断；好友关系/
关注事项的创建也是幂等的（重复创建时接口返回 409/已存在也视为成功）。
"""
import json
import sys
import urllib.error
import urllib.request

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8080"
DEMO_PASSWORD = "Demo123456"


def req(method, path, body=None, token=None):
    url = BASE + path
    data = json.dumps(body).encode() if body is not None else None
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = f"Bearer {token}"
    r = urllib.request.Request(url, data=data, method=method, headers=headers)
    try:
        with urllib.request.urlopen(r, timeout=10) as resp:
            text = resp.read().decode()
            return resp.status, (json.loads(text) if text else {})
    except urllib.error.HTTPError as e:
        text = e.read().decode()
        return e.code, (json.loads(text) if text else {})


def ensure_user(username):
    """账号不存在则注册，然后登录，返回 (token, user_id)。"""
    req("POST", "/api/auth/register", {"username": username, "password": DEMO_PASSWORD})
    s, r = req("POST", "/api/auth/login", {"username": username, "password": DEMO_PASSWORD})
    if s != 200:
        print(f"!! 登录 {username} 失败: {s} {r}", file=sys.stderr)
        sys.exit(1)
    return r["token"], r["user_id"]


def main():
    print(f"==> 目标服务: {BASE}")

    alice_token, alice_id = ensure_user("demo_alice")
    bob_token, bob_id = ensure_user("demo_bob")
    print("==> [1/3] demo_alice / demo_bob 账号就绪")

    # 建立好友关系（若已是好友，SendRequest 返回 409 already_friends，忽略即可）。
    s, r = req("POST", "/api/friends/requests", {"target_user_id": bob_id}, token=alice_token)
    if s == 201:
        req("PUT", f"/api/friends/requests/{r['request_id']}", {"action": "accept"}, token=bob_token)
        print("==> [2/3] 已建立 demo_alice <-> demo_bob 好友关系")
    elif s == 409:
        print("==> [2/3] demo_alice <-> demo_bob 好友关系已存在，跳过")
    else:
        print(f"!! 建立好友关系时出现意外状态: {s} {r}", file=sys.stderr)

    # 关注事项（用于 AI 推荐页有真实候选可展示）。
    req("POST", "/api/watch-topics", {"keywords": "摄影,徒步,咖啡"}, token=alice_token)
    req("POST", "/api/watch-topics", {"keywords": "摄影,烘焙"}, token=bob_token)
    req("POST", "/api/recommendations/generate", token=alice_token)
    print("==> [3/3] 已为两个账号设置关注事项并生成一轮 AI 推荐候选")

    print("\n演示账号信息（密码均为同一个）：")
    print(f"  用户名: demo_alice   密码: {DEMO_PASSWORD}   user_id: {alice_id}")
    print(f"  用户名: demo_bob     密码: {DEMO_PASSWORD}   user_id: {bob_id}")
    print("\n可打开两个浏览器窗口分别登录这两个账号，按 docs/14-demo-guide.md 的步骤演示。")


if __name__ == "__main__":
    main()
