# Docker Deployment Guide

This document covers building, running, and configuring the multi-agent-system with Docker.

## Quick Start

```bash
# 1. Copy and edit configuration
cp orchestrator.example.yml orchestrator.yml
# Edit orchestrator.yml with your project settings

# 2. Set required environment variables
export GITHUB_TOKEN=ghp_your_token_here

# 3. Build and start the supervisor
docker compose up -d

# 4. View logs
docker compose logs -f supervisor
```

## Image Details

The `Dockerfile` uses a two-stage build:

| Stage | Base Image | Purpose |
|-------|-----------|---------|
| builder | `golang:1.25-bookworm` | Compiles static binaries |
| runtime | `gcr.io/distroless/static-debian12:nonroot` | Minimal attack surface |

Both `supervisor` and `hunter` binaries are built with:
- `CGO_ENABLED=0` — fully static, no libc dependency
- `-ldflags="-s -w"` — strip debug info and DWARF tables
- `-trimpath` — remove local build paths from binary

The final image contains only the two binaries, CA certificates, and timezone data.
It runs as the `nonroot` user (uid 65532) by default.

## Services

### supervisor (default)

The main orchestration daemon. Polls GitHub for issues, routes tasks to workers,
and monitors PRs for AI review feedback.

```bash
docker compose up -d supervisor
```

Requires:
- `GITHUB_TOKEN` — GitHub personal access token with `repo` and `read:org` scopes
- `orchestrator.yml` — mounted read-only at `/orchestrator.yml`

### hunter (hunt profile)

SAM.gov opportunity discovery. Searches the SAM.gov Opportunities API for relevant
IT/software contract opportunities and prints matches to stdout.

The hunter service is gated behind the `hunt` profile so it does not start by default.

```bash
# Run hunter once
SAM_API_KEY=your_key docker compose --profile hunt run --rm hunter

# Or with the env var already exported
docker compose --profile hunt run --rm hunter
```

Requires:
- `SAM_API_KEY` — SAM.gov API key (register at https://sam.gov/profile/details)

## Configuration

### Environment Variables

| Variable | Service | Required | Description |
|----------|---------|----------|-------------|
| `GITHUB_TOKEN` | supervisor | Yes | GitHub PAT with `repo` + `read:org` scopes |
| `SAM_API_KEY` | hunter | Yes | SAM.gov API key |

Set these in a `.env` file (not committed to git) or export them in your shell:

```bash
# .env (gitignored)
GITHUB_TOKEN=ghp_xxxxxxxxxxxx
SAM_API_KEY=your_sam_key
```

### Volumes

| Volume | Mount Point | Description |
|--------|------------|-------------|
| `tasks` | `/tasks` | JSON task queue — persists between restarts |
| `workspaces` | `/workspaces` | Per-worker repo clones — can be cleared safely |

The `orchestrator.yml` file is bind-mounted read-only; edit it on the host and
restart the container to pick up changes.

## Build Commands

```bash
# Build the image
docker build -t mawar2/multi-agent-system:latest .

# Build without cache (after dependency changes)
docker build --no-cache -t mawar2/multi-agent-system:latest .

# Verify binary sizes
docker run --rm --entrypoint /bin/sh mawar2/multi-agent-system:latest ls -lh /supervisor /hunter
# Note: distroless has no shell; use a debug variant for inspection:
docker run --rm gcr.io/distroless/static-debian12:debug ls -lh /supervisor /hunter
```

## Logs

Both services use the `json-file` log driver:

```bash
# Stream supervisor logs
docker compose logs -f supervisor

# Last 100 lines
docker compose logs --tail=100 supervisor
```

Log files are stored at `/var/lib/docker/containers/<id>/<id>-json.log` on the host.
`supervisor` rotates at 50 MB × 5 files; `hunter` at 10 MB × 3 files.

## Cleanup

```bash
# Stop services
docker compose down

# Stop and remove volumes (clears task queue and workspace clones)
docker compose down -v

# Remove the built image
docker rmi mawar2/multi-agent-system:latest
```
