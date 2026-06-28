# AGENTS.md

## Project

Go reverse proxy for JioSaavn/Saavn CDN music streaming assets. Uses GoFiber v2.
Module: `github.com/valiantlynx/raga-backend`. Runs on port 3000 (override with `RAGA_PROXY_PORT`).

The entire app is two files: `main.go` (routes) and `utils/proxyrequest.go` (proxy helper).

## Build & Run

```bash
# Static binary (output goes to current dir, Dockerfile expects .build/raga-backend)
go build --ldflags '-s -w -linkmode external -extldflags "-static"' -o .build/raga-backend

# Run via Docker (requires binary pre-built in .build/)
docker-compose up -d
```

There are no Go tests, no linter config, and no formatter config.

## Directory Guide

| Path | What it is |
|---|---|
| `main.go`, `utils/` | Entire application source |
| `terraform/` | AWS EC2 + Cloudflare DNS infra (region: `eu-north-1`) |
| `ansible/` | Production deployment: Docker, nginx, SSL, monitoring |
| `dokploy-mcp/` | Cloned third-party MCP server — not project code, do not edit |
| `test/` | Legacy scripts for a different project (PocketBase/manga) — not tests for this backend |
| `.build/` | Build output directory for the compiled binary |
| `assets/` | Static PNG images |

## CI/CD

GitHub Actions (`.github/workflows/raga-backend.yaml`): builds Docker image, pushes to Docker Hub + ghcr.io, then dispatches a deployment event to `valiantlynx/svelte-rich-text`.

Extra workflows in `.github/extra_workflows/` are inactive (Azure deploy, Terraform apply/destroy).

## Gotchas

- The `Dockerfile` copies from `.build/raga-backend` — you must build the binary first before `docker-compose up`.
- The Go module name (`raga-backend`) differs from the git remote name (`vibraze-backend`). Import paths use `github.com/valiantlynx/raga-backend`.
- The `.env` file contains only `PUBLIC_DEPLOY_TARGET=node` which is unused by the Go app.
- Production infra uses Terraform S3 backend state in bucket `raga-backend-terraform` and deploys to `raga-backend.valiantlynx.com`.
