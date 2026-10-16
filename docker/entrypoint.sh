#!/usr/bin/env bash
set -euo pipefail

# Generic XRAG container entrypoint. Platform-specific checks (uid, required
# mounts, fixed ports) belong to the wrapping layer, not here.

readonly data_dir="${XRAG_DATA_DIR:-/data/xrag}"
readonly listen="${XRAG_LISTEN:-0.0.0.0:8765}"
readonly server_bin="${XRAG_SERVER_BIN:-/usr/local/bin/xrag-server}"
readonly examples_dir="${XRAG_EXAMPLES_DIR:-/usr/local/share/xrag/examples/docs}"

fail() {
  printf '[xrag-entrypoint] ERROR: %s\n' "$*" >&2
  exit 1
}

# Side-effect-free health probe: Docker runs this every 30s, so it must not
# repeat validation, mkdir or the first-boot init.
if [[ "${1:-}" = "healthcheck" ]]; then
  port="${listen##*:}"
  host="${listen%%:*}"
  exec curl -fsS "http://${host}:${port}/healthz"
fi

test -x "${server_bin}" || fail "xrag-server not found at ${server_bin}"

admin_user="${ADMIN_USERNAME:-}"
admin_pass="${ADMIN_PASSWORD:-}"
if [[ -n "${admin_user}" && -n "${admin_pass}" ]]; then
  [[ "${admin_user}" =~ ^[A-Za-z0-9_.@-]{1,64}$ ]] || fail "ADMIN_USERNAME contains unsupported characters"
  [[ "${#admin_pass}" -ge 16 ]] || fail "ADMIN_PASSWORD must be at least 16 characters"
  [[ "${admin_pass}" != *$'\n'* && "${admin_pass}" != *$'\r'* ]] || fail "ADMIN_PASSWORD must be a single line"
  export XRAG_EDGE_LOGIN=1
elif [[ -n "${admin_user}" || -n "${admin_pass}" ]]; then
  fail "ADMIN_USERNAME and ADMIN_PASSWORD must be set together"
fi

mkdir -p "${data_dir}"
test -w "${data_dir}" || fail "XRAG data directory ${data_dir} is not writable"

build_kb="${XRAG_BUILD_ON_START:-}"
[[ -z "${build_kb}" || "${build_kb}" =~ ^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$ ]] \
  || fail "XRAG_BUILD_ON_START must be a KB ID (1..64 letters, digits, underscores or hyphens; first character alphanumeric)"

if [[ ! -f "${data_dir}/xrag.sqlite" ]]; then
  # init writes admin.key and prints its path; the key value stays out of logs.
  "${server_bin}" init --data "${data_dir}" --examples "${examples_dir}" >/dev/null
  build_kb="${build_kb:-demo}"
fi

args=(serve --data "${data_dir}" --listen "${listen}" --workers embedded)
if [[ -f "${data_dir}/RESTORE_INFO.json" ]]; then
  printf '[xrag-entrypoint] Restored copy: skipping automatic build.\n'
elif [[ -n "${build_kb}" ]]; then
  args+=(--build "${build_kb}")
fi
exec "${server_bin}" "${args[@]}"
