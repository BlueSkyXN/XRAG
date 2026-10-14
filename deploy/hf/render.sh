#!/usr/bin/env bash
set -euo pipefail

# Render the Space files: flatten deploy/hf into <out> and pin the base image
# reference. The output directory is what gets uploaded, so the Dockerfile
# inside it must not reference deploy/hf/ paths that only exist in the repo.

usage() {
  echo "usage: render.sh <image-reference> <output-dir>" >&2
  exit 2
}

image="${1:?}"
out="${2:?}"
[[ $# -le 2 ]] || usage
here="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

[[ "$image" =~ ^(ghcr\.io/)?[A-Za-z0-9._/-]+(@sha256:[0-9a-f]{64}|:[A-Za-z0-9._-]+)$ ]] || {
  echo "render.sh: not an image reference: $image" >&2
  exit 1
}

mkdir -p "$out"
cp "$here/README.md" "$here/Dockerfile" "$here/entrypoint.sh" "$out/"
sed -i.bak "s|^ARG XRAG_IMAGE=.*|ARG XRAG_IMAGE=${image}|" "$out/Dockerfile"
rm -f "$out/Dockerfile.bak"
grep -q "^ARG XRAG_IMAGE=${image}$" "$out/Dockerfile" || {
  echo "render.sh: failed to pin ARG XRAG_IMAGE to ${image}" >&2
  exit 1
}
if grep -q 'deploy/hf/' "$out/Dockerfile" "$out/entrypoint.sh" "$out/README.md"; then
  echo "render.sh: rendered files must not reference deploy/hf/ paths" >&2
  exit 1
fi
echo "rendered Space files into ${out} (XRAG_IMAGE=${image})"
