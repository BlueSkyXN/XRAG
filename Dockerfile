# syntax=docker/dockerfile:1.7

FROM golang:1.23-bookworm AS build
RUN apt-get update \
    && apt-get install -y --no-install-recommends gcc libc6-dev libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -o /out/xrag-server ./cmd/xrag-server \
    && CGO_ENABLED=0 go build -trimpath -o /out/xrag ./cmd/xrag

FROM debian:bookworm-slim
LABEL org.opencontainers.image.title="XRAG" \
      org.opencontainers.image.source="https://github.com/BlueSkyXN/XRAG" \
      org.opencontainers.image.licenses="GPL-3.0-only"
RUN apt-get update \
    && apt-get install -y --no-install-recommends apache2-utils ca-certificates curl libsqlite3-0 nginx-light tini util-linux \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 1000 xrag \
    && useradd --uid 1000 --gid 1000 --home /data/xrag --shell /usr/sbin/nologin xrag \
    && mkdir -p /data/xrag /tmp/xrag-nginx \
    && chown -R xrag:xrag /data /tmp/xrag-nginx
COPY --from=build /out/xrag-server /out/xrag /usr/local/bin/
COPY examples /usr/local/share/xrag/examples
COPY nginx.conf /etc/nginx/nginx.conf
COPY entrypoint.sh /usr/local/bin/xrag-hfs-entrypoint
RUN chmod 0755 /usr/local/bin/xrag-server /usr/local/bin/xrag /usr/local/bin/xrag-hfs-entrypoint \
    && chmod 0644 /etc/nginx/nginx.conf
USER xrag
EXPOSE 7860
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=5 \
    CMD ["curl", "-fsS", "http://127.0.0.1:7860/healthz"]
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/xrag-hfs-entrypoint"]
