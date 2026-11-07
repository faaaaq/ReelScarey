# syntax=docker/dockerfile:1
FROM node:24-bookworm-slim AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run format:check && npm run check && npm test && npm run build

FROM golang:1.27.3-bookworm AS backend
WORKDIR /src
RUN sed -i 's/^Components: main$/Components: main contrib non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends p7zip-full unrar \
    && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/embed.go ./web/embed.go
COPY --from=frontend /src/web/dist ./web/dist
RUN go test ./... && go vet ./...
ARG VERSION=0.37.2-dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/reelscarey ./cmd/reelscarey

FROM debian:bookworm-slim AS youtube-tools
COPY scripts/yt-dlp-requirements.txt /tmp/yt-dlp-requirements.txt
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates python3 python3-venv \
    && python3 -m venv /opt/reelscarey-yt-dlp \
    && /opt/reelscarey-yt-dlp/bin/pip install --no-cache-dir --no-deps --require-hashes -r /tmp/yt-dlp-requirements.txt \
    && /opt/reelscarey-yt-dlp/bin/python -m pip uninstall -y pip setuptools

FROM debian:bookworm-slim AS runtime
RUN sed -i 's/^Components: main$/Components: main contrib non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources \
    && apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 gosu tzdata qbittorrent-nox wireguard-tools iproute2 iptables util-linux p7zip-full unrar \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 reelscarey \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin reelscarey \
    && mkdir -p /config /media /downloads /youtube-staging /backups /data \
    && chown 10001:10001 /config /media /downloads /youtube-staging /backups /data \
    && chmod 1777 /data
COPY --from=youtube-tools /opt/reelscarey-yt-dlp /opt/reelscarey-yt-dlp
COPY --from=backend /out/reelscarey /usr/local/lib/reelscarey
COPY scripts/container-entrypoint.sh /usr/local/bin/reelscarey
COPY scripts/container-init.py /usr/local/lib/reelscarey-init.py
RUN chmod 755 /usr/local/bin/reelscarey
ENV REELSCAREY_LISTEN_ADDR=0.0.0.0:8787 \
    REELSCAREY_CONFIG_DIR=/config \
    REELSCAREY_MEDIA_DIR=/media \
    REELSCAREY_DOWNLOADS_DIR=/downloads \
    REELSCAREY_LOG_LEVEL=info \
    PYTHONDONTWRITEBYTECODE=1 \
    REELSCAREY_YTDLP_PATH=/opt/reelscarey-yt-dlp/bin/yt-dlp
USER root
EXPOSE 8787
VOLUME ["/config", "/media", "/downloads", "/youtube-staging", "/backups"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/reelscarey", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/reelscarey"]
