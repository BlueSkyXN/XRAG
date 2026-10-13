#!/usr/bin/env bash
set -euo pipefail

readonly data_root="/data/xrag"
readonly nginx_home="/tmp/xrag-nginx"
readonly nginx_auth_file="${nginx_home}/htpasswd"

fail() {
  printf '[xrag-hfs] ERROR: %s\n' "$*" >&2
  exit 1
}

test "$(id -u)" = "1000" || fail "runtime UID must be 1000 on Hugging Face Spaces"
test "${PORT:-7860}" = "7860" || fail "PORT must remain 7860"
if ! command -v mountpoint >/dev/null 2>&1 || ! mountpoint -q /data; then
  fail "/data must be a mounted persistent volume; pre-create the bucket prefix with scripts/prepare-bucket-prefix.sh --apply"
fi

: "${ADMIN_USERNAME:?ADMIN_USERNAME is required}"
: "${ADMIN_PASSWORD:?ADMIN_PASSWORD is required}"
[[ "${ADMIN_USERNAME}" =~ ^[A-Za-z0-9_.@-]{1,64}$ ]] || fail "ADMIN_USERNAME contains unsupported characters"
[[ "${#ADMIN_PASSWORD}" -ge 16 ]] || fail "ADMIN_PASSWORD must be at least 16 characters"
[[ "${ADMIN_PASSWORD}" != *$'\n'* && "${ADMIN_PASSWORD}" != *$'\r'* ]] || fail "ADMIN_PASSWORD must be a single line"

umask 077
mkdir -p "${data_root}/corpus" "${nginx_home}/client_body" "${nginx_home}/proxy" "${nginx_home}/fastcgi" "${nginx_home}/uwsgi" "${nginx_home}/scgi"
test -w "${data_root}" || fail "XRAG data directory is not writable; run scripts/prepare-bucket-prefix.sh --apply"

printf '%s\n' "${ADMIN_PASSWORD}" | htpasswd -imc "${nginx_auth_file}" "${ADMIN_USERNAME}" >/dev/null
chmod 0600 "${nginx_auth_file}"

/usr/local/bin/xrag-server serve --data "${data_root}" --listen 127.0.0.1:8080 --workers embedded &
app_pid=$!
ready=0
for _ in $(seq 1 50); do
  if curl -fsS http://127.0.0.1:8080/healthz >/dev/null; then
    ready=1
    break
  fi
  if ! kill -0 "${app_pid}" >/dev/null 2>&1; then
    fail "xrag-server exited before it became ready"
  fi
  sleep 0.1
done
test "${ready}" = "1" || fail "xrag-server did not become ready"
cleanup() {
  kill "${app_pid}" >/dev/null 2>&1 || true
  nginx -s quit >/dev/null 2>&1 || true
}
nginx -g 'daemon off;' &
nginx_pid=$!
trap cleanup TERM INT
set +e
wait -n "${app_pid}" "${nginx_pid}"
status=$?
set -e
cleanup
wait || true
exit "${status}"
