# syntax=docker/dockerfile:1
FROM node:24-bookworm-slim AS frontend
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run format:check && npm run check && npm test && npm run build

FROM golang:1.27.4-bookworm AS backend
WORKDIR /src
RUN sed -i 's/^Components: main$/Components: main contrib non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources \
    && apt-get update \
    && apt-get install -y --no-install-recommends p7zip-full unrar \
    && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/embed.go ./web/embed.go
COPY --from=frontend /src/web/dist ./web/dist
# The default keeps the historic behaviour for local and manual builds. The
# release workflow passes RUN_GO_TESTS=0 because the application checks already
# ran the Go suite natively and the release candidate is smoke-tested; running
# it again under QEMU emulation is the single most expensive build step.
ARG RUN_GO_TESTS=1
RUN --mount=type=cache,target=/root/.cache/go-build \
    if [ "$RUN_GO_TESTS" = "1" ]; then go test ./... && go vet ./...; fi
ARG VERSION=0.54.43
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/reelscarey ./cmd/reelscarey

FROM debian:bookworm-slim AS youtube-tools
COPY scripts/yt-dlp-requirements.txt /tmp/yt-dlp-requirements.txt
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates python3 python3-venv \
    && python3 -m venv /opt/reelscarey-yt-dlp \
    && /opt/reelscarey-yt-dlp/bin/pip install --no-cache-dir --no-deps --require-hashes -r /tmp/yt-dlp-requirements.txt \
    && /opt/reelscarey-yt-dlp/bin/python -m pip uninstall -y pip setuptools

# The TIDAL provider bundles tidaler (AGPL-3.0, https://github.com/maya-doshi/tidaler)
# and its own headless adapter script. Dependencies are installed from the
# hash-verified lock in scripts/tidaler-requirements.txt (regenerate after any
# version bump); scripts/tidal-find-links vendors the two wheels PyPI only
# ships as sdists (pyaes, ratelimit) so the lock verifies end to end.
FROM python:3.12-slim-bookworm AS tidal-tools
COPY scripts/tidal-provider.py /tmp/tidal-provider.py
COPY scripts/tidaler-requirements.txt /tmp/tidaler-requirements.txt
COPY scripts/tidal-find-links/ /tmp/tidal-find-links/
RUN python3 -m venv /opt/reelscarey-tidal \
    && /opt/reelscarey-tidal/bin/pip install --no-cache-dir --no-deps --require-hashes --find-links /tmp/tidal-find-links -r /tmp/tidaler-requirements.txt \
    && /opt/reelscarey-tidal/bin/python -m pip uninstall -y pip setuptools \
    && mv /tmp/tidal-provider.py /opt/reelscarey-tidal/bin/tidal-provider.py \
    && chmod 755 /opt/reelscarey-tidal/bin/tidal-provider.py

FROM debian:bookworm-slim AS runtime
# sabnzbdplus is in Debian's contrib component; par2 is in main. The sources
# line above enables both, and unrar comes from non-free for SABnzbd.
RUN sed -i 's/^Components: main$/Components: main contrib non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources \
    && apt-get update && apt-get install -y --no-install-recommends ca-certificates ffmpeg libchromaprint-tools mkvtoolnix python3 gosu tzdata qbittorrent-nox sabnzbdplus par2 wireguard-tools iproute2 iptables util-linux p7zip-full unrar \
    && fpcalc -version >/dev/null \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 reelscarey \
    && useradd --uid 10001 --gid 10001 --no-create-home --shell /usr/sbin/nologin reelscarey \
    && mkdir -p /config /media /downloads /youtube-staging /backups /data \
    && chown 10001:10001 /config /media /downloads /youtube-staging /backups /data \
    && chmod 1777 /data
COPY --from=youtube-tools /opt/reelscarey-yt-dlp /opt/reelscarey-yt-dlp
# The TIDAL venv requires Python >=3.12 (tidaler pin), but the runtime's apt
# python3 is 3.11. Copy the interpreter, stdlib and libpython from the
# tidal-tools base image into /usr/local so the venv's absolute symlinks
# resolve; bookworm glibc matches, and apt python3 in /usr/bin is untouched.
COPY --from=tidal-tools /usr/local/bin/python3.12 /usr/local/bin/python3.12
# The venv's symlinks target /usr/local/bin/python3 and /usr/local/bin/python,
# so both must exist beside the real interpreter or every provider call fails.
RUN ln -sf python3.12 /usr/local/bin/python3 && ln -sf python3.12 /usr/local/bin/python
COPY --from=tidal-tools /usr/local/lib/libpython3.12.so.1.0 /usr/local/lib/
COPY --from=tidal-tools /usr/local/lib/python3.12 /usr/local/lib/python3.12
RUN ldconfig
COPY --from=tidal-tools /opt/reelscarey-tidal /opt/reelscarey-tidal
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
    REELSCAREY_YTDLP_PATH=/opt/reelscarey-yt-dlp/bin/yt-dlp \
    REELSCAREY_TIDAL_PROVIDER=/opt/reelscarey-tidal/bin/tidal-provider.py \
    REELSCAREY_TIDAL_PYTHON=/opt/reelscarey-tidal/bin/python3
USER root
EXPOSE 8787
VOLUME ["/config", "/media", "/downloads", "/youtube-staging", "/backups"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 CMD ["/usr/local/bin/reelscarey", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/reelscarey"]
