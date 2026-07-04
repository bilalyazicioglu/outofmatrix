# outofmatrix

A self-hosted personal media cloud — Google Photos + Spotify functionality in a
single Go binary. Upload photos, music and video; the server extracts metadata
with ffprobe, generates thumbnails and BlurHash placeholders, and segments
videos into HLS for adaptive streaming.

## Architecture

Clean / hexagonal architecture:

```
cmd/server/            composition root (wiring, lifecycle, graceful shutdown)
internal/domain/       entities + repository ports (no external deps)
internal/usecase/      business logic (auth, media pipeline, collections)
internal/repository/   PostgreSQL adapters (pgx/v5, plain SQL)
internal/delivery/http chi router, handlers, middleware
internal/worker/       bounded worker pool (channel-based job queue)
pkg/ffmpeg/            standalone ffmpeg/ffprobe wrapper
migrations/            SQL schema
web/                   single-file demo client (HTML5 + hls.js)
```

The media pipeline: chunked resumable upload → chunks written at their exact
offset into a sparse staging file (constant RAM, any file size) → row saved as
`pending` → job queued into the bounded worker pool → worker probes with
ffprobe, generates a thumbnail + BlurHash, and for video an adaptive
multi-bitrate HLS set (1080p + 720p, `h264_videotoolbox` on macOS with
automatic `libx264` fallback) → status `ready`. Every step streams progress
events over WebSocket (`/api/v1/ws`), parsed live from FFmpeg's
`-progress pipe:1` output. Unfinished jobs are recovered at boot, and the
binary migrates its own database schema on startup.

## Run with Docker (recommended)

```sh
JWT_SECRET=$(openssl rand -hex 32) docker compose up --build -d
```

Open http://localhost:8080 — register an account, upload media, play it.
The schema in `migrations/` is applied automatically on the first boot of a
fresh database volume.

## Run locally

Requires Go 1.22+, PostgreSQL and ffmpeg/ffprobe on PATH.

```sh
make db-up                 # start Postgres in Docker (schema auto-applied)
cp .env.example .env       # adjust if needed; export the variables
make run
```

## API

| Method | Path                                        | Description                              |
|--------|---------------------------------------------|------------------------------------------|
| POST   | `/api/v1/auth/register`                     | `{username, password}`                   |
| POST   | `/api/v1/auth/login`                        | → `{token, user}` (JWT, HS256)           |
| GET    | `/api/v1/ws`                                | WebSocket: live processing events        |
| POST   | `/api/v1/uploads`                           | open resumable session `{filename,size}` |
| GET    | `/api/v1/uploads/{id}`                      | received chunk indexes (resume)          |
| PUT    | `/api/v1/uploads/{id}/chunks/{index}`       | raw chunk bytes at exact file offset     |
| POST   | `/api/v1/uploads/{id}/complete`             | assemble → MediaItem → queue processing  |
| DELETE | `/api/v1/uploads/{id}`                      | abort session                            |
| POST   | `/api/v1/media/upload`                      | legacy single-request multipart upload   |
| GET    | `/api/v1/media?type=&limit=&offset=`        | paginated library, newest first          |
| GET    | `/api/v1/media/{id}`                        | one item incl. extracted metadata        |
| DELETE | `/api/v1/media/{id}`                        | row + original + derivatives             |
| GET    | `/api/v1/media/stream/{id}/master.m3u8`     | adaptive HLS master playlist             |
| GET    | `/api/v1/media/stream/{id}/index_1080p.m3u8`| per-rendition media playlist             |
| GET    | `/api/v1/media/stream/{id}/{segment}.ts`    | HLS segment                              |
| GET    | `/api/v1/media/raw/{id}`                    | original, HTTP Range (audio/photos)      |
| GET    | `/api/v1/media/thumb/{id}`                  | JPEG thumbnail                           |
| POST   | `/api/v1/collections`                       | `{name, type: playlist\|album}`          |
| GET    | `/api/v1/collections` / `/{id}`             | list / detail with ordered items         |
| POST   | `/api/v1/collections/{id}/items`            | `{media_id, position}`                   |
| DELETE | `/api/v1/collections/{id}/items/{mediaID}`  | remove item                              |

All `/media` and `/collections` routes require `Authorization: Bearer <token>`.
Media elements (`<video>`, `<img>`, HLS segment fetches) cannot send headers,
so those endpoints also accept `?token=<jwt>`; the playlist handler re-appends
the token to every segment URI it serves.

## Configuration

Everything is environment-driven; see `.env.example`. The important ones:

- `MAX_WORKERS` — concurrent FFmpeg jobs (default: half the CPU cores). Keep
  low on small home servers.
- `HWACCEL` — `auto` (VideoToolbox on macOS, libx264 elsewhere),
  `videotoolbox`, or `none`. A failing hardware encoder always falls back to
  libx264 automatically.
- `MAX_UPLOAD_BYTES` — hard upload cap (default 10 GiB).
- `UPLOAD_TTL` — abandoned chunked-upload sessions are reclaimed after this
  (default 48h).
- `PROCESS_TIMEOUT` — per-job ceiling so a corrupt file can't wedge a worker.
- `JWT_SECRET` — set it; otherwise a random one is minted per boot and logins
  don't survive restarts.
