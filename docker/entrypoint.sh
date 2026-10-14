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

if [[ ! -f "${data_dir}/xrag.sqlite" ]]; then
  # init writes admin.key and prints its path; the key value stays out of logs.
  "${server_bin}" init --data "${data_dir}" --examples "${examples_dir}" >/dev/null
fi

if [[ "${1:-}" = "healthcheck" ]]; then
  port="${listen##*:}"
  host="${listen%%:*}"
  exec curl -fsS "http://${host}:${port}/healthz"
fi

exec "${server_bin}" serve --data "${data_dir}" --listen "${listen}" --workers embedded
