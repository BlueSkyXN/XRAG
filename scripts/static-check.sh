#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
bash -n entrypoint.sh
bash -n scripts/prepare-bucket-prefix.sh
ROOT="${root}" python3 - <<'PY'
import os, pathlib, tomllib
root = pathlib.Path(os.environ["ROOT"])
with (root / "hfs-dev.toml").open("rb") as handle:
    cfg = tomllib.load(handle)
if cfg.get("space_visibility") != "protected" or cfg.get("bucket_visibility") != "private":
    raise SystemExit("hfs-dev.toml must keep the Space protected and the bucket private")
readme = (root / "README.md").read_text(encoding="utf-8")
for line in ("sdk: docker", "app_port: 7860", "license: gpl-3.0"):
    if line not in readme:
        raise SystemExit(f"README.md is missing Space metadata: {line}")
dockerfile = (root / "Dockerfile").read_text(encoding="utf-8")
entrypoint = (root / "entrypoint.sh").read_text(encoding="utf-8")
nginx = (root / "nginx.conf").read_text(encoding="utf-8")
for snippet in ("USER xrag", "EXPOSE 7860", "ENTRYPOINT", "127.0.0.1:8080"):
    if snippet not in dockerfile and snippet not in entrypoint:
        raise SystemExit(f"runtime contract missing: {snippet}")
for snippet in ("mountpoint -q /data", "ADMIN_PASSWORD", "XRAG_EDGE_LOGIN"):
    if snippet not in entrypoint:
        raise SystemExit(f"entrypoint.sh is missing: {snippet}")
if "auth_basic" in nginx or "htpasswd" in entrypoint:
    raise SystemExit("browser login must be the application page, not a second basic-auth dialog")
if 'id="password-form"' not in (root / "internal/server/web/index.html").read_text(encoding="utf-8"):
    raise SystemExit("login page is missing the username and password form")
if "proxy_pass http://127.0.0.1:8080/healthz;" not in nginx or 'return 200 "ok' in nginx:
    raise SystemExit("public /healthz must proxy to the application, not a static nginx response")
print("static contract: PASS")
PY
if [[ -d .git ]]; then git diff --check; fi
if command -v shellcheck >/dev/null 2>&1; then shellcheck entrypoint.sh scripts/static-check.sh scripts/prepare-bucket-prefix.sh; fi
printf 'static checks: PASS\n'
