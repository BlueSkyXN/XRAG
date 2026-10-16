#!/usr/bin/env bash
set -euo pipefail

# HF Spaces wrapper: enforce the platform contract, then hand off to the
# generic entrypoint that ships inside the image.

fail() {
  printf '[xrag-hf] ERROR: %s\n' "$*" >&2
  exit 1
}

test "$(id -u)" = "1000" || fail "runtime UID must be 1000 on Hugging Face Spaces"
if ! command -v mountpoint >/dev/null 2>&1 || ! mountpoint -q /data; then
  fail "/data must be a mounted persistent volume; pre-create the bucket prefix with scripts/prepare-bucket-prefix.sh --apply"
fi

port="${PORT:-8765}"
export XRAG_LISTEN="0.0.0.0:${port}"
export XRAG_TRUST_FORWARDED_PROTO=1
export XRAG_BUILD_ON_START="${XRAG_BUILD_ON_START:-demo}"

exec /usr/local/bin/xrag-entrypoint
