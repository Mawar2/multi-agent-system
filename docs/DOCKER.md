# Docker Guide

This document covers building, configuring, and running the multi-agent system with Docker.

## Quick Start

```bash
# Copy and edit configuration
cp orchestrator.example.yml orchestrator.yml

# Build and start the supervisor
GITHUB_TOKEN=ghp_... docker compose up -d supervisor

# View logs
docker compose logs -f supervisor
```

## Image Details

The `Dockerfile` uses a two-stage build:

| Stage | Base | Purpose |
|-------|------|---------|
| builder | `golang:1.25-alpine` | Compiles static binaries with `CGO_ENABLED=0` |
| runtime | `gcr.io/distroless/static-debian12:nonroot` | Minimal read-only runtime image |

Both `supervisor` and `hunter` binaries are built with `-ldflags="-s -w" -trimpath` for smaller, reproducible images.

## Services

### `supervisor` (default)

Polls GitHub for issues, routes tasks, and manages the worker pool.

```bash
docker compose up -d supervisor
```

### `hunter` (opt-in)

Discovers federal contracting opportunities on SAM.gov. Gated behind the `hunt` profile so it does not start by default.

```bash
docker compose --profile hunt up -d hunter
```

## Configuration

### Required environment variables

| Variable | Service | Description |
|----------|---------|-------------|
| `GITHUB_TOKEN` | supervisor, hunter | GitHub personal access token (scopes: `repo`, `read:org`) |
| `SAM_API_KEY` | hunter | SAM.gov public API key |

Pass them inline or via a `.env` file:

```bash
# .env (git-ignored)
GITHUB_TOKEN=ghp_...
SAM_API_KEY=...
```

### `orchestrator.yml`

Mount your configuration as a read-only file:

```yaml
# docker-compose.yml already mounts ./orchestrator.yml:/app/orchestrator.yml:ro
```

Copy the example and fill in your values:

```bash
cp orchestrator.example.yml orchestrator.yml
```

## Volumes

| Volume | Mount | Contents |
|--------|-------|---------|
| `tasks` | `/app/tasks` | JSON task queue — persisted across restarts |
| `workspaces` | `/app/projects` | Cloned repositories — can be removed to free disk space |

```bash
# Inspect task queue
docker compose run --rm supervisor sh -c 'ls /app/tasks/'

# Remove workspace volume (forces fresh clones)
docker volume rm multi-agent-system_workspaces
```

## Cleanup

```bash
# Stop all services
docker compose down

# Stop and remove volumes (WARNING: deletes task queue)
docker compose down -v

# Remove built image
docker rmi multi-agent-system-supervisor
```
