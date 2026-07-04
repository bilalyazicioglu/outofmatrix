# ---------------------------------------------------------------------------
# Build stage: static binary, no CGO
# ---------------------------------------------------------------------------
FROM golang:1.24-alpine AS build

WORKDIR /src

# Cache the module graph separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---------------------------------------------------------------------------
# Runtime stage: alpine + ffmpeg/ffprobe, non-root
# ---------------------------------------------------------------------------
FROM alpine:3.21

RUN apk add --no-cache ffmpeg ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S app -G app \
    && mkdir -p /data/media \
    && chown -R app:app /data/media

COPY --from=build /out/server /usr/local/bin/server
COPY --chown=app:app static /app/static

USER app
WORKDIR /app

ENV PORT=8080 \
    WEB_DIR=/app/static \
    STORAGE_PATH=/data/media \
    HWACCEL=auto \
    FFMPEG_PATH=ffmpeg \
    FFPROBE_PATH=ffprobe

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["server"]
