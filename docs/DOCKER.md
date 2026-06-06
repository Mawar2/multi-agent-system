# Docker Guide

This guide covers building and running the multi-agent system in Docker.

## Quick Start

```bash
# 1. Copy config and set your GitHub token
cp orchestrator.example.yml orchestrator.yml
# Edit orchestrator.yml with your project settings

# 2. Export required credentials
export GITHUB_TOKEN=ghp_...          # required
export SAM_API_KEY=your-key          # optional (hunter binary only)

# 3. Build and start
docker compose up -d

# 4. Tail logs
docker compose logs -f supervisor
```

## Image Details

The Dockerfile uses a two-stage build:

| Stage | Base image | Purpose |
|-------|-----------|---------|
| builder | `golang:1.25` | Compiles binaries with `CGO_ENABLED=0` |
| runtime | `gcr.io/distroless/static-debian12:nonroot` | Minimal attack surface, < 50 MB |

Both `supervisor` and `hunter` binaries are built as fully static Go binaries
(`-trimpath -ldflags="-s -w"`) so they run without libc in the distroless layer.

The nonroot variant runs as uid 65532 by default, so no root privileges are needed.

## Configuration

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | Yes | Personal access token with `repo` + `read:org` scopes |
| `SAM_API_KEY` | No | SAM.gov API key for the `hunter` binary |

The supervisor configuration is read from `/orchestrator.yml` inside the container,
which is bind-mounted from `./orchestrator.yml` on the host (read-only).

## Volumes

| Volume | Mount point | Contents |
|--------|------------|---------|
| `tasks` | `/app/tasks` | JSON task queue files |
| `workspaces` | `/app/projects` | Per-worker repository clones |

Both volumes persist across container restarts. Workspaces (~200 MB per worker × 10
workers = ~2 GB) accumulate over time; clean them periodically with the commands below.

## Running the Hunter Binary

The `hunter` binary is included in the image. Run it as a one-shot container:

```bash
docker run --rm \
  -e SAM_API_KEY="${SAM_API_KEY}" \
  multi-agent-system_supervisor \
  /hunter "artificial intelligence" 20
```

## Cleanup

```bash
# Stop and remove containers
docker compose down

# Remove containers + volumes (deletes task queue and workspace clones)
docker compose down -v

# Remove the built image
docker rmi $(docker images -q multi-agent-system_supervisor)
```
