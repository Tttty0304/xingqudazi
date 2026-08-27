#!/usr/bin/env python3
"""兴趣搭子在线聊天室 —— 简易压测脚本（能力补齐项：性能与成本方向此前从未做过

真实压测，"1000并发下延迟多少"完全是未知数。这不是一个专业压测工具（没有做到
wrk/k6/Locust 那种精细的负载模型/分布式发压能力），只是用最小成本换取"至少有一份
真实并发数据，而不是完全空白"——用于本项目 demo/评估场景的自我认知，不是生产级
SLA 验证工具。

覆盖两条路径：
  1. HTTP：并发访问 `GET /api/rooms`（无需鉴权，代表最常见的只读请求）。
  2. WebSocket：并发建立连接 + 加入同一房间 + 发消息，测量端到端往返延迟
     （发送时间 -> 收到服务端广播回自己的 `message_received` 事件的时间）。

依赖：
  - HTTP 部分用标准库 `urllib` + `threading`，零额外依赖。
  - WS 部分用 `websockets` 库（`pip install websockets`），未安装时该部分会
    给出清晰提示并跳过，不影响 HTTP 部分正常运行。

用法：
    python3 scripts/load_test.py [BASE_URL] [--http-levels 20,50,100] [--ws-clients N] [--ws-messages M]

    默认 BASE_URL=http://localhost:8080，--ws-clients 默认 50，--ws-messages 默认 5
    （每个 WS 客户端发送的消息数）。
"""
import argparse
import json
import statistics
import sys
import time
import urllib.error
import urllib.request
import uuid
from concurrent.futures import ThreadPoolExecutor, as_completed


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("base_url", nargs="?", default="http://localhost:8080")
    parser.add_argument("--http-concurrency", type=int, default=50, help="兼容单档 HTTP 并发请求数")
    parser.add_argument("--http-levels", default="", help="HTTP 梯度并发档位，逗号分隔，如 20,50,100")
    parser.add_argument("--http-requests-per-client", type=int, default=5, help="每个 HTTP 并发档位的请求倍率")
    parser.add_argument("--ws-clients", type=int, default=50, help="WS 并发连接数")
    parser.add_argument("--ws-messages", type=int, default=5, help="每个 WS 客户端发送的消息数")
    return parser.parse_args()


def percentile(data, p):
    if not data:
        return float("nan")
    data = sorted(data)
    k = (len(data) - 1) * (p / 100)
    f, c = int(k), min(int(k) + 1, len(data) - 1)
    if f == c:
        return data[f]
    return data[f] + (data[c] - data[f]) * (k - f)


def print_stats(name, latencies_ms, errors, total):
    print(f"\n== {name} ==")
    print(f"  总请求数: {total}  成功: {total - errors}  失败: {errors}")
    if latencies_ms:
        print(f"  延迟(ms)  avg={statistics.mean(latencies_ms):.1f}  "
              f"p50={percentile(latencies_ms, 50):.1f}  "
              f"p95={percentile(latencies_ms, 95):.1f}  "
              f"p99={percentile(latencies_ms, 99):.1f}  "
              f"max={max(latencies_ms):.1f}")
    else:
        print("  （没有成功的请求，无延迟数据）")


# ---------- 1. HTTP 并发压测：GET /api/rooms ----------

def http_get_rooms(base_url):
    start = time.monotonic()
    try:
        with urllib.request.urlopen(base_url + "/api/rooms", timeout=10) as resp:
            resp.read()
            ok = resp.status == 200
    except Exception:
        ok = False
    elapsed_ms = (time.monotonic() - start) * 1000
    return ok, elapsed_ms


def run_http_load_test(base_url, concurrency, total_requests):
    latencies = []
    errors = 0
    overall_start = time.monotonic()
    with ThreadPoolExecutor(max_workers=concurrency) as pool:
        futures = [pool.submit(http_get_rooms, base_url) for _ in range(total_requests)]
        for f in as_completed(futures):
            ok, elapsed_ms = f.result()
            if ok:
                latencies.append(elapsed_ms)
            else:
                errors += 1
    overall_elapsed = time.monotonic() - overall_start
    print_stats(f"HTTP GET /api/rooms（并发={concurrency}，总请求={total_requests}）", latencies, errors, total_requests)
    if overall_elapsed > 0:
        print(f"  吞吐: {total_requests / overall_elapsed:.1f} req/s（总耗时 {overall_elapsed:.2f}s）")


# ---------- 2. WS 并发压测：并发连接 + 加入房间 + 发消息，测端到端往返延迟 ----------

async def ws_client_run(base_url, ws_url, room_id, num_messages, results, errors_counter, lock):
    import aiohttp  # 仅在实际调用本函数时才需要，避免未装依赖时 import 报错影响 HTTP 部分
    import websockets

    try:
        async with aiohttp.ClientSession() as session:
            async with session.post(base_url + "/api/auth/guest", json={}) as resp:
                data = await resp.json()
                token = data.get("token")
            cookie_header = "; ".join(f"{key}={value.value}" for key, value in session.cookie_jar.filter_cookies(base_url).items())
    except Exception as e:
        async with lock:
            errors_counter[0] += 1
        print(f"  [WS] guest login failed: {e}", file=sys.stderr)
        return

    try:
        # 生产浏览器模式不会在 JSON 中返回 JWT，改走 HttpOnly Cookie。脚本同时
        # 兼容本地开发的 Bearer/query-token 响应，便于同一压测命令验证两种部署。
        connect_url = f"{ws_url}?token={token}" if token else ws_url
        headers = {"Cookie": cookie_header} if cookie_header else None
        async with websockets.connect(connect_url, additional_headers=headers, open_timeout=10) as ws:
            await ws.send(json.dumps({"type": "join_room", "room_id": room_id}))
            # 等待 joined 确认（否则紧接着发消息可能因为尚未真正加入房间而被丢弃）。
            joined = False
            deadline = time.monotonic() + 5
            while not joined and time.monotonic() < deadline:
                raw = await asyncio.wait_for(ws.recv(), timeout=5)
                evt = json.loads(raw)
                if evt.get("type") == "joined":
                    joined = True

            for _ in range(num_messages):
                msg_id = str(uuid.uuid4())
                send_ts = time.monotonic()
                await ws.send(json.dumps({
                    "type": "send_message", "room_id": room_id, "msg_id": msg_id,
                    "content": f"load-test-{msg_id}",
                }))
                # 等待服务端把这条消息广播回来（自己也是房间成员之一）。
                deadline = time.monotonic() + 5
                got_echo = False
                while time.monotonic() < deadline:
                    raw = await asyncio.wait_for(ws.recv(), timeout=5)
                    evt = json.loads(raw)
                    if evt.get("type") == "message_received" and evt.get("msg_id") == msg_id:
                        rtt_ms = (time.monotonic() - send_ts) * 1000
                        async with lock:
                            results.append(rtt_ms)
                        got_echo = True
                        break
                if not got_echo:
                    async with lock:
                        errors_counter[0] += 1
            await ws.send(json.dumps({"type": "leave_room", "room_id": room_id}))
    except Exception as e:
        async with lock:
            errors_counter[0] += 1
        print(f"  [WS] client error: {e}", file=sys.stderr)


async def run_ws_load_test(base_url, num_clients, num_messages):
    try:
        import aiohttp  # noqa: F401
        import websockets  # noqa: F401
    except ImportError:
        print("\n== WS 并发压测 ==")
        print("  跳过：未安装 `websockets`/`aiohttp`（`pip3 install websockets aiohttp`），"
              "HTTP 部分结果不受影响。")
        return

    # 拿真实房间 ID（不依赖硬编码，避免种子数据变化导致脚本失效）。
    with urllib.request.urlopen(base_url + "/api/rooms", timeout=10) as resp:
        rooms = json.loads(resp.read())
    if not rooms:
        print("\n== WS 并发压测 ==\n  跳过：`/api/rooms` 返回空列表，没有可用房间。")
        return
    room_id = rooms[0]["id"]

    ws_url = base_url.replace("http://", "ws://").replace("https://", "wss://") + "/ws"

    results = []
    errors_counter = [0]
    lock = asyncio.Lock()

    overall_start = time.monotonic()
    await asyncio.gather(*[
        ws_client_run(base_url, ws_url, room_id, num_messages, results, errors_counter, lock)
        for _ in range(num_clients)
    ])
    overall_elapsed = time.monotonic() - overall_start

    total_messages = num_clients * num_messages
    print_stats(
        f"WS 端到端消息往返延迟（并发连接={num_clients}，每连接发送={num_messages}条，房间={rooms[0]['name']}）",
        results, errors_counter[0], total_messages,
    )
    print(f"  {num_clients} 个并发连接建立 + 全部消息收发完成总耗时: {overall_elapsed:.2f}s")


def main():
    args = parse_args()
    base_url = args.base_url.rstrip("/")

    print(f"目标服务: {base_url}")
    print("=" * 60)

    levels = [args.http_concurrency]
    if args.http_levels:
        try:
            levels = [int(part.strip()) for part in args.http_levels.split(",") if int(part.strip()) > 0]
        except ValueError:
            print("--http-levels 必须是正整数的逗号分隔列表", file=sys.stderr)
            return 2
    for level in levels:
        run_http_load_test(base_url, level, level * args.http_requests_per_client)

    global asyncio
    import asyncio
    asyncio.run(run_ws_load_test(base_url, args.ws_clients, args.ws_messages))

    print("\n" + "=" * 60)
    print("压测结束。已支持梯度 HTTP 加压；请同时记录 docker stats、运行时长和机器规格，")
    print("再把结果写入 docs/27-production-readiness-work-log.md，避免把一次瞬时结果当作生产 SLA。")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
