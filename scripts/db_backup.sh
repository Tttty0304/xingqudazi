#!/usr/bin/env bash
# 兴趣搭子在线聊天室 —— 数据库备份脚本（能力补齐项：此前完全没有任何备份/恢复
# 策略说明，是部署与运行方式方向的真实缺口）。
#
# 用法：
#   ./scripts/db_backup.sh [输出目录，默认 ./backups]
#
# 依赖：需要能访问到运行中的 postgres 容器（通过 `docker compose exec`），
# 不要求本机安装 psql/pg_dump 客户端。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
COMPOSE_FILE="$PROJECT_ROOT/deploy/docker-compose.yml"
OUT_DIR="${1:-$PROJECT_ROOT/backups}"

mkdir -p "$OUT_DIR"
TIMESTAMP="$(date +%Y%m%d_%H%M%S)"
OUT_FILE="$OUT_DIR/xingqudazi_im_${TIMESTAMP}.sql.gz"

echo "==> 正在备份数据库到 ${OUT_FILE} ..."
# --clean --if-exists：让备份文件自带"先 DROP 再 CREATE"语句，恢复时天然
# 幂等（真实踩坑记录：最初没加这两个参数，恢复到已有数据/表结构的数据库时
# 产生大量 "relation already exists"/"duplicate key" 报错——这正是恢复脚本
# 最常见的真实场景：目标库已经跑过 migrations 初始化，不是恢复到一个空库）。
docker compose -f "$COMPOSE_FILE" exec -T postgres \
  pg_dump -U im_user -d xingqudazi_im --no-owner --no-privileges --clean --if-exists | gzip > "$OUT_FILE"

SIZE=$(du -h "$OUT_FILE" | cut -f1)
echo "==> 备份完成：${OUT_FILE}（${SIZE}）"
echo "==> 恢复方式见 scripts/db_restore.sh 或 docs/18-backup-and-restore.md"

# 简单的保留策略：只保留最近 14 份备份，避免磁盘无限增长（demo/评估项目场景的
# 最小合理策略；生产环境应按真实 RPO/RTO 要求接入专业备份系统，如云厂商托管
# 数据库自带的自动备份能力，而不是继续用这类本地脚本）。
KEEP=14
cd "$OUT_DIR"
ls -1t xingqudazi_im_*.sql.gz 2>/dev/null | tail -n +$((KEEP + 1)) | xargs -r rm -f --
echo "==> 已清理超出保留数量（${KEEP}）的旧备份"
