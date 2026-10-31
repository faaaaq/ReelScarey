# Disposable test runtime only. No download client is added to the app image.
FROM golang:1.27.3-bookworm AS go-tools
FROM debian:trixie-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    transmission-daemon ffmpeg ca-certificates gcc libc6-dev \
    && rm -rf /var/lib/apt/lists/*
COPY --from=go-tools /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH GOPATH=/go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ ./internal/
ENV REELSCAREY_LIVE_TRANSMISSION=1
CMD ["go", "test", "-race", "./internal/transfer", "-run", "Transmission", "-count=1", "-v", "-timeout=7m"]
