#!/usr/bin/env python3
"""兴趣搭子在线聊天室 —— 多实例横向扩展真实验证脚本（能力补齐项）。

背景：项目架构文档一直声称"Redis Pub/Sub 多实例扇出为强制设计"（非可选），
但此前的全部测试/演示场景永远只跑了一个 server 容器——广播消息本质上是
"进程自己发布给自己订阅"，Redis 是否真的承担了跨进程转发的职责从未被
证实过。本脚本配合 `deploy/docker-compose.multi-instance.yml` 起两个独立的
后端进程（server + server2）+ 一个 nginx 负载均衡器，验证：

  1. 负载均衡器是否真的把流量分流到了两个不同的物理实例（而不是名义上配置了
     两个但实际全部落到同一个）；
  2. 两个用户各自连接到**不同**物理实例的 WS 连接，其中一方发消息，另一方
     能否通过 Redis Pub/Sub 真实收到广播——这是本次验证的核心，直接证明
     "多实例扇出"不是一句空话；
  3. 私聊消息跨实例投递同理验证一次。

用法：
    # 1. 启动多实例拓扑（叠加在默认 docker-compose.yml 之上）：
    docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.multi-instance.yml up -d --build

    # 2. 运行本脚本（默认指向负载均衡器的 8082 端口，不是默认的 8080）：
    python3 scripts/multi_instance_test.py http://localhost:8082

依赖：`websockets`、`aiohttp`（与 scripts/load_test.py 一致）。
"""
import asyncio
import json
import sys
import urllib.request
import uuid


def check(condition, message):
    status = "PASS" if condition else "FAIL"
    print(f"[{status}] {message}")
    if not condition:
        global failures
        failures += 1


failures = 0


async def guest_login(session, base_url):
    async with session.post(base_url + "/api/auth/guest", json={}) as resp:
        data = await resp.json()
        return data["token"], data["user_id"]


async def ws_connect_and_get_instance_id(ws_url, token):
    """建立 WS 连接并读取 connected 事件里的 instance_id（能力补齐项新增字段），
    返回 (websocket连接对象, instance_id)。连接保持打开，调用方负责后续关闭。
    """
    import websockets

    ws = await websockets.connect(f"{ws_url}?token={token}", open_timeout=10)
    raw = await asyncio.wait_for(ws.recv(), timeout=5)
    evt = json.loads(raw)
    assert evt.get("type") == "connected", f"expected connected event, got {evt}"
    return ws, evt.get("instance_id", "")


async def recv_until(ws, event_type, timeout=8):
    deadline = asyncio.get_event_loop().time() + timeout
    while True:
        remaining = deadline - asyncio.get_event_loop().time()
        if remaining <= 0:
            raise TimeoutError(f"timed out waiting for event type={event_type}")
        raw = await asyncio.wait_for(ws.recv(), timeout=remaining)
        evt = json.loads(raw)
        if evt.get("type") == event_type:
            return evt


async def check_lb_distributes_across_instances(base_url, samples=10):
    """通过负载均衡器连续请求 /healthz，收集观测到的 instance_id 集合，
    验证真的分流到了多个不同实例（而不是名义上配了两个但实际全部落到同一个）。
    """
    seen = set()
    for _ in range(samples):
        with urllib.request.urlopen(base_url + "/healthz", timeout=5) as resp:
            data = json.loads(resp.read())
            seen.add(data.get("instance_id", ""))
    check(
        len(seen) >= 2,
        f"负载均衡器把 {samples} 次 /healthz 请求分流到了 {len(seen)} 个不同实例：{seen}",
    )
    return seen


async def check_cross_instance_room_broadcast(base_url, ws_url):
    """核心验证：两个用户分别连接到（大概率）不同物理实例的 WS 连接，
    加入同一房间，一方发消息，验证另一方能否通过 Redis Pub/Sub 真实收到。
    """
    import aiohttp

    async with aiohttp.ClientSession() as session:
        token_a, _ = await guest_login(session, base_url)
        token_b, _ = await guest_login(session, base_url)

    # 取一个真实房间 ID。
    with urllib.request.urlopen(base_url + "/api/rooms", timeout=5) as resp:
        rooms = json.loads(resp.read())
    room_id = rooms[0]["id"]

    ws_a, instance_a = await ws_connect_and_get_instance_id(ws_url, token_a)
    ws_b, instance_b = await ws_connect_and_get_instance_id(ws_url, token_b)

    print(f"    客户端A落在实例: {instance_a!r}  客户端B落在实例: {instance_b!r}")
    check(
        instance_a != "" and instance_b != "" and instance_a != instance_b,
        f"两个 WS 连接真实落在了两个不同的物理实例（A={instance_a!r}, B={instance_b!r}）",
    )

    try:
        await ws_a.send(json.dumps({"type": "join_room", "room_id": room_id}))
        await recv_until(ws_a, "joined")
        await ws_b.send(json.dumps({"type": "join_room", "room_id": room_id}))
        await recv_until(ws_b, "joined")

        msg_id = str(uuid.uuid4())
        test_content = f"cross-instance-broadcast-test-{msg_id}"
        await ws_a.send(json.dumps({
            "type": "send_message", "room_id": room_id, "msg_id": msg_id, "content": test_content,
        }))

        # A 自己也会收到广播（房间成员之一），先排空。
        got_a = await recv_until(ws_a, "message_received")
        check(got_a.get("content") == test_content, "发送方A自己收到了广播（房间成员之一）")

        got_b = await recv_until(ws_b, "message_received")
        check(
            got_b.get("content") == test_content and got_b.get("msg_id") == msg_id,
            f"跨实例广播成功：落在实例 {instance_b!r} 的客户端B收到了落在实例 {instance_a!r} 的客户端A发送的消息",
        )
    finally:
        await ws_a.close()
        await ws_b.close()


async def check_cross_instance_direct_message(base_url, ws_url):
    """验证私聊消息跨实例投递：两个用户先互加好友（走 HTTP，可能落在任一实例，
    因为好友关系存于共享 Postgres，不受实例区分），再各自建立落在不同物理实例
    的 WS 连接，验证私聊消息广播同样能跨实例送达。
    """
    import aiohttp

    async with aiohttp.ClientSession() as session:
        token_a, user_a = await guest_login(session, base_url)
        token_b, user_b = await guest_login(session, base_url)

        headers_a = {"Authorization": f"Bearer {token_a}"}
        headers_b = {"Authorization": f"Bearer {token_b}"}

        async with session.post(base_url + "/api/friends/requests",
                                 json={"target_user_id": user_b}, headers=headers_a) as resp:
            data = await resp.json()
            request_id = data["request_id"]

        async with session.put(base_url + f"/api/friends/requests/{request_id}",
                                json={"action": "accept"}, headers=headers_b) as resp:
            await resp.json()

    ws_a, instance_a = await ws_connect_and_get_instance_id(ws_url, token_a)
    ws_b, instance_b = await ws_connect_and_get_instance_id(ws_url, token_b)
    print(f"    私聊测试：客户端A落在实例 {instance_a!r}  客户端B落在实例 {instance_b!r}")

    try:
        msg_id = str(uuid.uuid4())
        content = f"cross-instance-dm-test-{msg_id}"
        await ws_a.send(json.dumps({
            "type": "send_direct_message", "target_user_id": user_b, "msg_id": msg_id, "content": content,
        }))

        got_b = await recv_until(ws_b, "direct_message_received")
        check(
            got_b.get("content") == content,
            f"跨实例私聊投递成功：实例 {instance_b!r} 的客户端B收到了实例 {instance_a!r} 的客户端A发的私聊消息",
        )
    finally:
        await ws_a.close()
        await ws_b.close()


async def main():
    base_url = (sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8082").rstrip("/")
    ws_url = base_url.replace("http://", "ws://").replace("https://", "wss://") + "/ws"

    print(f"目标负载均衡器: {base_url}")
    print("=" * 70)

    try:
        import aiohttp  # noqa: F401
        import websockets  # noqa: F401
    except ImportError:
        print("需要 `pip3 install websockets aiohttp` 才能运行本脚本。")
        sys.exit(1)

    print("\n== 1. 负载均衡分流验证 ==")
    await check_lb_distributes_across_instances(base_url)

    print("\n== 2. 跨实例群聊广播验证（核心）==")
    await check_cross_instance_room_broadcast(base_url, ws_url)

    print("\n== 3. 跨实例私聊投递验证 ==")
    await check_cross_instance_direct_message(base_url, ws_url)

    print("\n" + "=" * 70)
    if failures:
        print(f"完成，但有 {failures} 项断言失败。")
        sys.exit(1)
    print("全部通过：多实例横向扩展 + Redis Pub/Sub 跨实例扇出设计，"
          "已用两个真实独立进程验证，不再只是理论声明。")


if __name__ == "__main__":
    asyncio.run(main())
