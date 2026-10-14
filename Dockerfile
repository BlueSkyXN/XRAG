# syntax=docker/dockerfile:1.7

FROM golang:1.23-bookworm AS build
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY . .
ARG GIT_SHA=dev
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-X github.com/BlueSkyXN/XRAG/core.Commit=${GIT_SHA}" -o /out/xrag-server ./cmd/xrag-server \
    && CGO_ENABLED=0 go build -trimpath -ldflags "-X github.com/BlueSkyXN/XRAG/core.Commit=${GIT_SHA}" -o /out/xrag ./cmd/xrag

FROM debian:bookworm-slim
ARG GIT_SHA=dev
LABEL org.opencontainers.image.title="XRAG" \
      org.opencontainers.image.source="https://github.com/BlueSkyXN/XRAG" \
      org.opencontainers.image.licenses="GPL-3.0-only" \
      org.opencontainers.image.revision="${GIT_SHA}"
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libsqlite3-0 tini util-linux \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 xrag \
    && useradd --uid 1000 --gid 1000 --home /data/xrag --shell /usr/sbin/nologin xrag \
    && mkdir -p /data/xrag \
    && chown -R xrag:xrag /data
COPY --from=build /out/xrag-server /out/xrag /usr/local/bin/
COPY examples /usr/local/share/xrag/examples
COPY docker/entrypoint.sh /usr/local/bin/xrag-entrypoint
RUN chmod 0755 /usr/local/bin/xrag-server /usr/local/bin/xrag /usr/local/bin/xrag-entrypoint
USER xrag
EXPOSE 8765
# The entrypoint serves the same curl against the actual XRAG_LISTEN port.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=5 \
    CMD ["/usr/local/bin/xrag-entrypoint", "healthcheck"]
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/xrag-entrypoint"]
