# syntax=docker/dockerfile:1
FROM node:24-bookworm-slim AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run format:check && npm run check && npm test && npm run build

FROM golang:1.27.3-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/embed.go ./web/embed.go
COPY --from=frontend /src/web/dist ./web/dist
RUN go test ./... && go vet ./...
ARG VERSION=0.25.0-dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/reelscarey ./cmd/reelscarey

FROM debian:bookworm-slim AS youtube-tools
COPY scripts/yt-dlp-requirements.txt /tmp/yt-dlp-requirements.txt
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates python3 python3-venv \
    && python3 -m venv /opt/reelscarey-yt-dlp \
    && /opt/reelscarey-yt-dlp/bin/pip install --no-cache-dir --no-deps --require-hashes -r /tmp/yt-dlp-requirements.txt \
    && /opt/reelscarey-yt-dlp/bin/python -m pip uninstall -y pip setuptools

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg python3 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 reelscarey \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin reelscarey \
    && mkdir -p /config /media /downloads /youtube-staging /backups \
    && chown 10001:10001 /config /media /downloads /youtube-staging /backups
COPY --from=youtube-tools /opt/reelscarey-yt-dlp /opt/reelscarey-yt-dlp
COPY --from=backend /out/reelscarey /usr/local/bin/reelscarey
ENV REELSCAREY_LISTEN_ADDR=0.0.0.0:8787 \
    REELSCAREY_CONFIG_DIR=/config \
    REELSCAREY_MEDIA_DIR=/media \
    REELSCAREY_DOWNLOADS_DIR=/downloads \
    REELSCAREY_BACKUPS_DIR=/backups \
    REELSCAREY_LOG_LEVEL=info \
    PYTHONDONTWRITEBYTECODE=1 \
    REELSCAREY_YTDLP_PATH=/opt/reelscarey-yt-dlp/bin/yt-dlp \
    REELSCAREY_YOUTUBE_STAGING_DIR=/youtube-staging/work
USER 10001:10001
EXPOSE 8787
VOLUME ["/config", "/media", "/downloads", "/youtube-staging", "/backups"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/reelscarey", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/reelscarey"]
