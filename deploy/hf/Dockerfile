# syntax=docker/dockerfile:1.7
# This file is always built from a flat render (see render.sh): the three Space
# files sit next to each other in the build context, exactly like in the Space
# repo. Never build it straight from the repository root.
ARG XRAG_IMAGE=ghcr.io/blueskyxn/xrag:main
FROM ${XRAG_IMAGE}
COPY --chmod=0755 entrypoint.sh /usr/local/bin/xrag-hf-entrypoint
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/xrag-hf-entrypoint"]
