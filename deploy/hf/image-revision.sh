#!/usr/bin/env bash
set -euo pipefail

# Print the org.opencontainers.image.revision label of an image reference.
# Used by deploy-hf.yml and by the image workflow right after pushing, so the
# exact read path a deploy depends on is exercised on every main build.
#
# Pushes made by build-push-action v6 are attested indexes; inspecting
# .Image.Config.Labels only works on a plain image, so fall back to walking
# the index manifests.

ref="${1:?usage: image-revision.sh <image[@digest|:tag]>}"
tool='docker'

if ! command -v docker >/dev/null 2>&1; then
  echo "image-revision.sh: docker (buildx) is required" >&2
  exit 1
fi

label_of() {
  # $1: json blob selected by the given --format
  printf '%s' "$1" | python3 -c '
import json, sys
try:
    v = json.load(sys.stdin)
except Exception:
    sys.exit(1)
labels = v.get("Image", {}).get("Config", {}).get("Labels") or v.get("Config", {}).get("Labels") or {}
rev = labels.get("org.opencontainers.image.revision", "")
print(rev)
'
}

image_json="$($tool buildx imagetools inspect "$ref" --format '{{ json . }}')"
revision="$(label_of "$image_json")"
if [[ -n "$revision" ]]; then
  echo "$revision"
  exit 0
fi

# Index without a direct label: inspect every manifest it lists.
mapfile -t children < <(printf '%s' "$image_json" | python3 -c '
import json, sys
v = json.load(sys.stdin)
out = []
for m in v.get("Manifest", {}).get("Manifests", []) or []:
    digest = m.get("Digest", "")
    if digest:
        out.append(digest)
    p = m.get("Platform") or {}
    if p.get("os") == "unknown" or "attestation" in (m.get("Annotations") or {}):
        continue
for digest in out:
    print(digest)
')
for digest in "${children[@]}"; do
  child_json="$($tool buildx imagetools inspect "${ref%%@*}@${digest}" --format '{{ json . }}' 2>/dev/null || true)"
  [[ -z "$child_json" ]] && continue
  revision="$(label_of "$child_json")"
  if [[ -n "$revision" ]]; then
    echo "$revision"
    exit 0
  fi
done

echo "image-revision.sh: no org.opencontainers.image.revision label on $ref" >&2
exit 1
