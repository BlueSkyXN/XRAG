#!/usr/bin/env bash
set -euo pipefail
root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${root}"
bash -n docker/entrypoint.sh
bash -n deploy/hf/entrypoint.sh
bash -n deploy/hf/render.sh
bash -n deploy/hf/image-revision.sh
bash -n scripts/prepare-bucket-prefix.sh

# Offline tests for scripts whose real inputs only exist in CI:
# buildx's --format JSON shapes per buildx source (see testdata comments).
rev="deploy/hf/image-revision.sh"
got="$(bash "$rev" --parse < deploy/hf/testdata/single-image.json)" || { echo "single-image fixture: parse failed"; exit 1; }
[[ "$got" =~ ^[0-9a-f]{40}$ ]] || { echo "single-image fixture: bad revision $got"; exit 1; }
got="$(bash "$rev" --parse < deploy/hf/testdata/platform-map.json)" || { echo "platform-map fixture: parse failed"; exit 1; }
[[ "$got" =~ ^[0-9a-f]{40}$ ]] || { echo "platform-map fixture: bad revision $got"; exit 1; }
if bash "$rev" --parse < deploy/hf/testdata/no-revision.json >/dev/null 2>&1; then
  echo "no-revision fixture: must exit non-zero"; exit 1
fi
ROOT="${root}" python3 - <<'PY'
import os, pathlib, re, tomllib
root = pathlib.Path(os.environ["ROOT"])
with (root / "hfs-dev.toml").open("rb") as handle:
    cfg = tomllib.load(handle)
if cfg.get("space_visibility") != "protected" or cfg.get("bucket_visibility") != "private":
    raise SystemExit("hfs-dev.toml must keep the Space protected and the bucket private")
hf_readme = (root / "deploy/hf/README.md").read_text(encoding="utf-8")
for line in ("sdk: docker", "app_port: 8765", "license: gpl-3.0"):
    if line not in hf_readme:
        raise SystemExit(f"deploy/hf/README.md is missing Space metadata: {line}")
dockerfile = (root / "Dockerfile").read_text(encoding="utf-8")
entrypoint = (root / "docker/entrypoint.sh").read_text(encoding="utf-8")
for snippet in ("USER xrag", "EXPOSE 8765", "ENTRYPOINT"):
    if snippet not in dockerfile and snippet not in entrypoint:
        raise SystemExit(f"runtime contract missing: {snippet}")
if "0.0.0.0:8765" not in entrypoint:
    raise SystemExit("generic entrypoint must bind 0.0.0.0:8765 by default")
for snippet in ("ADMIN_PASSWORD", "XRAG_EDGE_LOGIN"):
    if snippet not in entrypoint:
        raise SystemExit(f"docker/entrypoint.sh is missing: {snippet}")
if "nginx" in dockerfile or "htpasswd" in entrypoint:
    raise SystemExit("the image must not ship nginx; browser login lives in the app")
hf_entrypoint = (root / "deploy/hf/entrypoint.sh").read_text(encoding="utf-8")
for snippet in ("mountpoint -q /data", "XRAG_TRUST_FORWARDED_PROTO", "exec /usr/local/bin/xrag-entrypoint"):
    if snippet not in hf_entrypoint:
        raise SystemExit(f"deploy/hf/entrypoint.sh is missing: {snippet}")
if "must be 1000" not in hf_entrypoint:
    raise SystemExit("HF entrypoint must check the runtime UID")
hf_dockerfile = (root / "deploy/hf/Dockerfile").read_text(encoding="utf-8")
if "ARG XRAG_IMAGE=" not in hf_dockerfile or "${XRAG_IMAGE}" not in hf_dockerfile:
    raise SystemExit("deploy/hf/Dockerfile must build FROM the XRAG image via ARG")
if "deploy/hf/" in hf_dockerfile:
    raise SystemExit("deploy/hf/Dockerfile is built from a flat render; it must not reference deploy/hf/ paths")
if "COPY --chmod=0755 entrypoint.sh" not in hf_dockerfile:
    raise SystemExit("deploy/hf/Dockerfile must COPY the flattened entrypoint with --chmod (no RUN chmod under USER xrag)")
for script in ("deploy/hf/render.sh", "deploy/hf/image-revision.sh"):
    if not (root / script).stat().st_mode & 0o111:
        raise SystemExit(f"{script} must be executable")
web = root / "internal/server/web" / "dist"
index = web / "index.html"
if not index.is_file():
    raise SystemExit("internal/server/web/dist is missing; run make web")
if 'id="root"' not in index.read_text(encoding="utf-8"):
    raise SystemExit("dist/index.html must be the built workbench; run make web")
if not any('password-form' in p.read_text(encoding="utf-8") for p in (web / "assets").glob("*.js")):
    raise SystemExit("login page is missing the username and password form; run make web")

# 界面禁用句式；保留项在同一行或上一行注明 text-ok: 理由。
forbidden = re.compile(r"不代表|不等于|不承诺|不宣称|冒充|悄悄|不会被说成|尚未在真实|真实联调")
violations = []
for source in sorted((root / "webapp/src").rglob("*")):
    if source.suffix not in (".ts", ".tsx") or not source.is_file():
        continue
    lines = source.read_text(encoding="utf-8").splitlines()
    for index, line in enumerate(lines):
        if line.lstrip().startswith(("//", "*", "/*", "{/*")):
            continue
        if "text-ok" in line or (index > 0 and "text-ok" in lines[index - 1]):
            continue
        if forbidden.search(line):
            violations.append(f"{source.relative_to(root)}:{index + 1}: {line.strip()}")
if violations:
    print("\n".join(violations))
    raise SystemExit("界面文案包含禁用句式，请改写或用 text-ok 注明保留理由")
print("static contract: PASS")
PY
if [[ -d .git ]]; then git diff --check; fi
if command -v shellcheck >/dev/null 2>&1; then shellcheck docker/entrypoint.sh deploy/hf/entrypoint.sh deploy/hf/render.sh deploy/hf/image-revision.sh scripts/static-check.sh scripts/prepare-bucket-prefix.sh; fi
printf 'static checks: PASS\n'
