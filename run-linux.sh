#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
DATA="${XRAG_DEMO_DATA:-.xrag}"
LISTEN="${XRAG_DEMO_LISTEN:-127.0.0.1:8765}"
./bin/xrag-server init --data "$DATA" --examples ./examples/docs
printf '\n打开 http://%s\nAPI Key：\n' "$LISTEN"
cat "$DATA/admin.key"
args=(serve --data "$DATA" --listen "$LISTEN")
if [[ -f "$DATA/RESTORE_INFO.json" ]]; then
  echo '恢复副本：本次不自动构建。请先确认来源路径，再显式提交构建。'
else
  args+=(--build demo)
fi
if [[ -n "${XRAG_DEMO_CONFIG:-}" ]]; then args+=(--config "$XRAG_DEMO_CONFIG"); fi
exec ./bin/xrag-server "${args[@]}"
