#!/usr/bin/env bash
# 部署后最小验证脚本（对应 Testcase Part4）
# 用法：./scripts/smoke_test.sh [BASE_URL]
# 默认假设服务已通过 docker compose 启动，BASE_URL 默认 http://localhost:8080

set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"
MAX_RETRY=30
INTERVAL=2

echo "==> [1/3] 轮询 ${BASE_URL}/healthz 直到就绪..."
for i in $(seq 1 "$MAX_RETRY"); do
  if curl -sf "${BASE_URL}/healthz" > /dev/null; then
    echo "    healthz OK (第 ${i} 次尝试)"
    break
  fi
  if [ "$i" -eq "$MAX_RETRY" ]; then
    echo "!!  healthz 在 $((MAX_RETRY * INTERVAL)) 秒内未就绪，退出"
    exit 1
  fi
  sleep "$INTERVAL"
done

echo "==> [2/3] 检查 ${BASE_URL}/readyz（依赖健康）..."
READYZ_BODY=$(curl -sf "${BASE_URL}/readyz")
echo "    ${READYZ_BODY}"
if ! echo "$READYZ_BODY" | grep -q '"status":"ready"'; then
  echo "!!  readyz 未返回 ready 状态，退出"
  exit 1
fi

echo "==> [3/3] 基础检查通过。后续 Task2 起接入注册/登录/房间/WS 用例后，"
echo "    本脚本将扩展为完整 P0 闭环验证（对应 Testcase T10-T33）。"
