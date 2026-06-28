# AGENTS.md

## Project

Go backend for music streaming: CDN reverse proxy + radio station logic. Uses GoFiber v2 + SQLite.
Module: `github.com/valiantlynx/raga-backend`. Runs on port 3000 (override with `RAGA_PROXY_PORT`).

## Build & Run

```bash
# Build (pure Go, no CGO needed thanks to modernc.org/sqlite)
CGO_ENABLED=0 go build -ldflags='-s -w' -o .build/raga-backend .

# Run directly
RAGA_MUSIC_DIR=./music RAGA_DB_PATH=./radio.db ./.build/raga-backend

# Docker (radio-enabled, multi-stage build from source)
docker build -f Dockerfile.radio -t raga-backend .
docker run -p 3000:3000 -v ./music:/music raga-backend

# Full radio stack (Icecast + Liquidsoap + backend)
docker compose -f docker-compose.radio.yml up -d --build

# Legacy Docker (requires pre-built binary in .build/)
docker-compose up -d
```

There are no Go tests, no linter config, and no formatter config.

## Environment Variables

| Variable | Default | Purpose |
|---|---|---|
| `RAGA_PROXY_PORT` | `3000` | Server listen port |
| `RAGA_MUSIC_DIR` | `/music` | Path to mounted music files (what Liquidsoap sees) |
| `RAGA_DB_PATH` | `./radio.db` | SQLite database file path |
| `RAGA_ICECAST_URL` | `http://icecast:8000` | Icecast server for status polling |

## Routes

### CDN Proxy (existing)
| Method | Path | Target |
|---|---|---|
| GET | `/media/+` | `https://c.saavncdn.com/{path}` (images) |
| GET | `/aac/:id/:path` | `https://aac.saavncdn.com/{id}/{path}` (audio) |
| GET | `/svg/+` | `https://www.jiosaavn.com/{path}` (SVGs) |

### Radio API (new)
| Method | Path | Purpose |
|---|---|---|
| GET | `/api/radio/next-track` | Plain text file path for Liquidsoap |
| GET | `/api/radio/now-playing` | SSE endpoint for live metadata |
| POST | `/api/radio/request` | `{"track_id": N}` — queue a song |
| GET | `/api/radio/tracks` | JSON list of all tracks |
| GET | `/api/radio/queue` | JSON list of pending requests |
| POST | `/api/radio/scan` | Trigger music directory rescan |

## Architecture

```
main.go              -- App entry, routes, startup logic
utils/
  proxyrequest.go    -- GoFiber proxy helper for CDN routes
db/
  db.go             -- SQLite schema, CRUD, music directory scanner
radio/
  radio.go          -- Radio state (NowPlaying, SSE broker), Icecast poller, handlers
```

### Database Schema (SQLite)
- `tracks`: id, title, artist, album, duration, file_path (UNIQUE), genre, added_at
- `requests`: id, track_id (FK), requested_at, played

On startup: initializes DB, scans `RAGA_MUSIC_DIR` if library is empty, starts Icecast poller.

### Music File Convention
Files are parsed from filename: `Artist - Title.ext` pattern. Supported: `.mp3`, `.flac`, `.ogg`, `.wav`.

## Directory Guide

| Path | What it is |
|---|---|
| `main.go`, `utils/`, `db/`, `radio/` | Application source |
| `Dockerfile.radio` | Multi-stage build (preferred for radio stack) |
| `docker-compose.radio.yml`, `radio.liq` | Self-hosted radio infra (Icecast + Liquidsoap) |
| `Dockerfile` | Legacy Alpine + pre-built binary |
| `terraform/` | AWS EC2 + Cloudflare DNS infra (region: `eu-north-1`) |
| `ansible/` | Production deployment: Docker, nginx, SSL, monitoring |
| `test/` | Legacy scripts (PocketBase/manga) — not tests for this backend |
| `.build/` | Build output directory for the compiled binary |

## CI/CD

GitHub Actions (`.github/workflows/raga-backend.yaml`): builds Docker image, pushes to Docker Hub + ghcr.io, then dispatches a deployment event to `valiantlynx/svelte-rich-text`.

## Gotchas

- The Go module name (`raga-backend`) differs from the git remote name (`vibraze-backend`). Import paths use `github.com/valiantlynx/raga-backend`.
- `modernc.org/sqlite` is pure Go (no CGO) — builds with `CGO_ENABLED=0` work fine.
- Liquidsoap expects file paths as seen inside its Docker container (`/music/...`), which must match what the Go backend stores in SQLite. Both containers mount the same host `./music` directory to `/music`.
- The Icecast poller logs at DEBUG level when Icecast is unreachable (expected during startup).
- Production infra uses Terraform S3 backend state in bucket `raga-backend-terraform` and deploys to `raga-backend.valiantlynx.com`.
