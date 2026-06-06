# Docker Guide

This document covers building, running, and configuring the multi-agent system with Docker.

## Quick Start

```bash
# 1. Copy example config
cp orchestrator.example.yml orchestrator.yml
# edit orchestrator.yml as needed

# 2. Set required environment variables
export GITHUB_TOKEN=ghp_...
export SAM_API_KEY=...   # optional, only needed for the hunter service

# 3. Build the image
docker build -t multi-agent-system:latest .

# 4. Start the supervisor
docker compose up -d supervisor
```

## Image Details

The multi-stage `Dockerfile` produces a minimal, non-root runtime image.

| Stage   | Base                                      | Purpose                        |
|---------|-------------------------------------------|--------------------------------|
| builder | `golang:1.25.1-alpine`                   | Compiles static Go binaries    |
| runtime | `gcr.io/distroless/static-debian12:nonroot` | Runs the compiled binaries  |

Both binaries are built with:
- `CGO_ENABLED=0` — fully static, no libc dependency
- `-ldflags="-s -w"` — strip debug symbols to reduce size
- `-trimpath` — remove local filesystem paths from the binary

The runtime image contains **no shell**, no package manager, and runs as a
non-root user (`nonroot`), which limits the blast radius of any vulnerability.

## Services

### `supervisor` (default)

Polls GitHub for open issues and routes them to AI workers.

```bash
docker compose up -d supervisor
docker compose logs -f supervisor
```

### `hunter` (opt-in via `hunt` profile)

Discovers federal contracting opportunities from SAM.gov and outputs them as JSON.
The service is gated behind the `hunt` profile so it does **not** start with
`docker compose up`.

```bash
# Run once
docker compose --profile hunt run --rm hunter --keyword "software development" --limit 10

# Run as a persistent service (e.g., scheduled externally)
docker compose --profile hunt up -d hunter
```

## Configuration

### Environment Variables

| Variable       | Service            | Required | Description                          |
|----------------|--------------------|----------|--------------------------------------|
| `GITHUB_TOKEN` | supervisor, hunter | Yes      | GitHub PAT with `repo`, `read:org`   |
| `SAM_API_KEY`  | hunter             | Yes      | SAM.gov API key                      |

Pass them at runtime or store them in a `.env` file (not committed to git):

```bash
# .env  (add to .gitignore)
GITHUB_TOKEN=ghp_...
SAM_API_KEY=...
```

### `orchestrator.yml`

Mount the config file into the supervisor container. The example config is at
`orchestrator.example.yml`. Copy it, edit it, and keep it **outside** the Docker
image (it is listed in `.dockerignore`).

## Volumes

| Volume       | Mount in container | Contents                                  |
|--------------|--------------------|-------------------------------------------|
| `tasks`      | `/app/tasks`       | JSON task queue files (`{uuid}.json`)     |
| `workspaces` | `/app/projects`    | Per-worker git clones of target repos     |

Both volumes are named Docker volumes so data persists across container restarts.

## Logging

Both services use the `json-file` log driver with rotation:

```bash
docker compose logs --tail 100 -f supervisor
```

Logs are capped at 10 MB per file, 3 files max.

## Cleanup

```bash
# Stop and remove containers
docker compose down

# Also remove named volumes (WARNING: destroys all task queue data)
docker compose down -v

# Remove the image
docker rmi multi-agent-system:latest
```
