#!/usr/bin/env bash
# 兴趣搭子在线聊天室 —— 数据库恢复脚本（配套 scripts/db_backup.sh）。
#
# 用法：
#   ./scripts/db_restore.sh <备份文件路径.sql.gz>
#
# ⚠️ 危险操作：会先清空当前数据库全部数据再导入备份内容，仅应在明确需要
# 恢复/迁移数据时执行，执行前会二次确认。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/deploy/docker-compose.yml"

BACKUP_FILE="${1:-}"
if [ -z "$BACKUP_FILE" ] || [ ! -f "$BACKUP_FILE" ]; then
  echo "用法: $0 <备份文件路径.sql.gz>" >&2
  echo "可用备份：" >&2
  ls -1t "$PROJECT_ROOT/backups"/*.sql.gz 2>/dev/null || echo "  (无)" >&2
  exit 1
fi

echo "⚠️  即将用备份 [$BACKUP_FILE] 完全覆盖当前数据库 xingqudazi_im 的全部数据。"
read -r -p "确认继续？输入 yes 继续: " CONFIRM
if [ "$CONFIRM" != "yes" ]; then
  echo "已取消。"
  exit 0
fi

echo "==> 正在恢复数据库..."
gunzip -c "$BACKUP_FILE" | docker compose -f "$COMPOSE_FILE" exec -T postgres \
  psql -U im_user -d xingqudazi_im

echo "==> 恢复完成。建议随后跑一遍 scripts/smoke_test.sh 确认服务正常。"
