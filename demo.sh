#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
if ! command -v go >/dev/null; then
  echo '需要 Go 1.23 或更新版本。安装 Go 后重新运行；脚本不会自动修改你的系统。' >&2
  exit 1
fi
if ! command -v cc >/dev/null; then
  echo 'Server 需要 C 编译器及 SQLite 开发头文件。macOS: xcode-select --install；Linux: 安装 build-essential libsqlite3-dev。' >&2
  exit 1
fi
make build
DATA="${XRAG_DEMO_DATA:-.xrag}"
LISTEN="${XRAG_DEMO_LISTEN:-127.0.0.1:8765}"
./bin/xrag-server init --data "$DATA" --examples ./examples/docs
printf '\n打开 http://%s\n输入以下本地 API Key：\n' "$LISTEN"
cat "$DATA/admin.key"
printf '\n停止服务：Ctrl+C。资料保留在 %s。\n\n' "$DATA"
args=(serve --data "$DATA" --listen "$LISTEN")
if [[ -f "$DATA/RESTORE_INFO.json" ]]; then
  echo '恢复副本：本次不自动构建。请先确认来源路径，再显式提交构建。'
else
  args+=(--build demo)
fi
if [[ -n "${XRAG_DEMO_CONFIG:-}" ]]; then args+=(--config "$XRAG_DEMO_CONFIG"); fi
exec ./bin/xrag-server "${args[@]}"
