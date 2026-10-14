#!/usr/bin/env bash
set -euo pipefail

# Print the org.opencontainers.image.revision label of an image reference.
# Used by deploy-hf.yml and by the image workflow right after pushing, so the
# exact read path a deploy depends on is exercised on every main build.
#
# `buildx imagetools inspect --format '{{ json .Image }}'` yields either
#   - the image object itself (architecture/os/config...; single-platform
#     result, buildx source util/imagetools/printers.go ImagePrinter), or
#   - a platform -> image mapping (the documented --format shape), where we
#     take linux/amd64.
# Key casing follows buildx source (lowercase name/manifest/image keys) and
# the OCI image spec (lowercase config, capital Labels). Parsing is testable
# offline via --parse (fixtures in deploy/hf/testdata/ mirror both shapes).

parse_stdin() {
  python3 -c '
import json, sys

def revision_of(image):
    labels = ((image.get("config") or {}).get("Labels")) or {}
    return labels.get("org.opencontainers.image.revision", "")

try:
    v = json.load(sys.stdin)
except Exception:
    sys.exit(3)
if not isinstance(v, dict):
    sys.exit(3)
if "config" in v:
    rev = revision_of(v)
else:
    # platform -> image mapping: prefer linux/amd64, else any single entry
    candidate = None
    for platform, image in v.items():
        if not isinstance(image, dict):
            continue
        if platform == "linux/amd64":
            candidate = image
            break
        if candidate is None:
            candidate = image
    rev = revision_of(candidate) if candidate is not None else ""
if not rev:
    sys.exit(1)
print(rev)
'
}

if [[ "${1:-}" = "--parse" ]]; then
  parse_stdin
  exit $?
fi

ref="${1:?usage: image-revision.sh <image[@digest|:tag]> | --parse < json}"

if ! command -v docker >/dev/null 2>&1; then
  echo "image-revision.sh: docker (buildx) is required" >&2
  exit 2
fi

docker buildx imagetools inspect "$ref" --format '{{ json .Image }}' | parse_stdin
