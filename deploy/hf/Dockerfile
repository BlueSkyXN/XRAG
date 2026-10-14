# syntax=docker/dockerfile:1.7
# The Space builds this file as-is. The deploy workflow rewrites the default
# of XRAG_IMAGE to the pinned digest before uploading; CI smokes override it
# with --build-arg XRAG_IMAGE=xrag:ci.
ARG XRAG_IMAGE=ghcr.io/blueskyxn/xrag:main
FROM ${XRAG_IMAGE}
COPY deploy/hf/entrypoint.sh /usr/local/bin/xrag-hf-entrypoint
RUN chmod 0755 /usr/local/bin/xrag-hf-entrypoint
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/xrag-hf-entrypoint"]
