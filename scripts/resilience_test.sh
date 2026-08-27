#!/usr/bin/env bash
# 兴趣搭子在线聊天室 —— 运行时可靠性验证脚本（能力补齐项：更丰富的部署/运行时
# 测试）。用真实的 docker stop/start 制造故障，而不是纸面推演"理论上应该没问题"。
#
# 覆盖两个场景：
#   1. 实例故障自动降级：多实例拓扑下 kill 掉一个后端实例，验证负载均衡器
#      能自动把流量路由到剩余健康实例（服务整体不中断），随后恢复该实例。
#   2. 依赖故障自动恢复：短暂停止 Redis/Postgres，验证 /readyz 正确从
#      "ready" 转为 "not_ready"（且明确指出哪个依赖故障），恢复依赖后
#      /readyz 能在无需重启应用进程的情况下自动变回 "ready"。
#
# 前提：多实例拓扑已启动
#   docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.multi-instance.yml up -d --build
#
# 用法：
#   ./scripts/resilience_test.sh [LB_BASE_URL] [PROJECT_ROOT]
set -uo pipefail

LB_URL="${1:-http://localhost:8082}"
PROJECT_ROOT="${2:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
COMPOSE_FILES="-f ${PROJECT_ROOT}/deploy/docker-compose.yml -f ${PROJECT_ROOT}/deploy/docker-compose.multi-instance.yml"

PASS=0
FAIL=0

check() {
  local desc="$1"
  local ok="$2"
  if [ "$ok" = "0" ]; then
    echo "[PASS] $desc"
    PASS=$((PASS + 1))
  else
    echo "[FAIL] $desc"
    FAIL=$((FAIL + 1))
  fi
}

echo "======================================================================"
echo "== 场景1：实例故障自动降级（kill server2，验证负载均衡器自动路由到 server1）=="
echo "======================================================================"

echo "--> 停止 server2 容器（模拟该实例崩溃/被驱逐）..."
docker compose $COMPOSE_FILES stop server2 > /dev/null 2>&1

echo "--> 停止后连续请求负载均衡器 20 次，验证服务整体不中断..."
SUCCESS_COUNT=0
for i in $(seq 1 20); do
  if curl -sf "${LB_URL}/healthz" > /dev/null 2>&1; then
    SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
  fi
  sleep 0.3
done
echo "    20 次请求中成功 ${SUCCESS_COUNT} 次"
# 不要求 100% 成功（nginx 检测到后端不可用前的极短窗口内可能有个别请求打到
# 已停止的 server2 上而失败一次，这是负载均衡器"检测到故障 -> 标记临时下线"
# 这个过程本身固有的短暂代价，属于预期内的正常现象，不代表设计有问题）；
# 但绝大多数应该成功，验证的是"故障后服务整体仍可用"而非"零丢包"。
if [ "$SUCCESS_COUNT" -ge 15 ]; then
  check "server2 故障期间，负载均衡器自动路由到 server1，服务整体保持可用（${SUCCESS_COUNT}/20 成功）" 0
else
  check "server2 故障期间，负载均衡器自动路由到 server1，服务整体保持可用（${SUCCESS_COUNT}/20 成功，低于预期）" 1
fi

echo "--> 恢复 server2 容器..."
docker compose $COMPOSE_FILES start server2 > /dev/null 2>&1
sleep 8

RECOVERED=1
for i in $(seq 1 15); do
  BODY=$(curl -sf "${LB_URL}/healthz" 2>/dev/null || echo "")
  if echo "$BODY" | grep -q '"instance_id":"server2"'; then
    RECOVERED=0
    break
  fi
  sleep 1
done
check "server2 恢复后重新加入负载均衡池（15秒内观测到该实例重新被路由到）" $RECOVERED

echo ""
echo "======================================================================"
echo "== 场景2：依赖故障自动恢复（Redis 短暂中断后 /readyz 自动恢复）=="
echo "======================================================================"

echo "--> 停止 redis 容器..."
docker compose $COMPOSE_FILES stop redis > /dev/null 2>&1
sleep 2

READYZ_BODY=$(curl -s "${LB_URL}/readyz" 2>/dev/null || echo "")
echo "    停止 redis 后 /readyz 响应: ${READYZ_BODY}"
if echo "$READYZ_BODY" | grep -q '"status":"not_ready"' && echo "$READYZ_BODY" | grep -q 'redis'; then
  check "Redis 故障后 /readyz 正确报告 not_ready 并明确指出 redis 故障" 0
else
  check "Redis 故障后 /readyz 正确报告 not_ready 并明确指出 redis 故障" 1
fi

echo "--> 恢复 redis 容器（不重启应用进程，验证自动重新连接）..."
docker compose $COMPOSE_FILES start redis > /dev/null 2>&1

RECOVERED=1
for i in $(seq 1 20); do
  BODY=$(curl -sf "${LB_URL}/readyz" 2>/dev/null || echo "")
  if echo "$BODY" | grep -q '"status":"ready"'; then
    RECOVERED=0
    break
  fi
  sleep 1
done
check "Redis 恢复后 /readyz 在 20 秒内自动变回 ready（无需重启应用进程）" $RECOVERED

echo ""
echo "======================================================================"
echo "== 场景3：依赖故障自动恢复（Postgres 短暂中断后 /readyz 自动恢复）=="
echo "======================================================================"

echo "--> 停止 postgres 容器..."
docker compose $COMPOSE_FILES stop postgres > /dev/null 2>&1
sleep 2

READYZ_BODY=$(curl -s "${LB_URL}/readyz" 2>/dev/null || echo "")
echo "    停止 postgres 后 /readyz 响应: ${READYZ_BODY}"
if echo "$READYZ_BODY" | grep -q '"status":"not_ready"' && echo "$READYZ_BODY" | grep -q '"db":"error'; then
  check "Postgres 故障后 /readyz 正确报告 not_ready 并明确指出 db 故障" 0
else
  check "Postgres 故障后 /readyz 正确报告 not_ready 并明确指出 db 故障" 1
fi

echo "--> 恢复 postgres 容器..."
docker compose $COMPOSE_FILES start postgres > /dev/null 2>&1

RECOVERED=1
for i in $(seq 1 30); do
  BODY=$(curl -sf "${LB_URL}/readyz" 2>/dev/null || echo "")
  if echo "$BODY" | grep -q '"status":"ready"'; then
    RECOVERED=0
    break
  fi
  sleep 1
done
check "Postgres 恢复后 /readyz 在 30 秒内自动变回 ready（无需重启应用进程）" $RECOVERED

echo ""
echo "======================================================================"
echo "结果：${PASS} 项通过，${FAIL} 项失败"
if [ "$FAIL" -gt 0 ]; then
  exit 1
fi
