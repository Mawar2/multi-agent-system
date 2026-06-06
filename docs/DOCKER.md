# Docker Guide

This document explains how to build, configure, and run the multi-agent system
using Docker and Docker Compose.

---

## Quick start

```bash
# Copy and edit the config
cp orchestrator.example.yml orchestrator.yml

# Export required secrets
export GITHUB_TOKEN=ghp_...

# Build and start the supervisor
docker compose up -d

# Tail logs
docker compose logs -f supervisor
```

---

## Image details

The `Dockerfile` uses a **multi-stage build** with two named final stages:

| Stage        | Binary      | Base image                                   |
|--------------|-------------|----------------------------------------------|
| `supervisor` | `/supervisor` | `gcr.io/distroless/static-debian12:nonroot` |
| `hunter`     | `/hunter`     | `gcr.io/distroless/static-debian12:nonroot` |

Both binaries are compiled as **fully static Go binaries** (`CGO_ENABLED=0`)
with debug symbols stripped (`-ldflags="-s -w"`) and build paths trimmed
(`-trimpath`). The distroless base contains no shell, package manager, or
libc — only the binary and its CA certificates.

---

## Configuration

### Environment variables

| Variable        | Required for    | Description                          |
|-----------------|-----------------|--------------------------------------|
| `GITHUB_TOKEN`  | supervisor      | GitHub personal access token (`repo`, `read:org`) |
| `SAM_API_KEY`   | hunter          | SAM.gov API key for opportunity discovery |

### Config file

`orchestrator.yml` is bind-mounted read-only into the supervisor container.
It is excluded from the Docker build context via `.dockerignore`.
Copy `orchestrator.example.yml` and edit before starting:

```bash
cp orchestrator.example.yml orchestrator.yml
# edit orchestrator.yml
```

---

## Volumes

| Volume       | Mount in container | Purpose                              |
|--------------|--------------------|--------------------------------------|
| `tasks`      | `/tasks`           | JSON task queue (persisted)          |
| `workspaces` | `/workspaces`      | Per-worker git clones (persisted)    |

Data in these volumes survives container restarts and upgrades.

---

## Services

### supervisor (default)

Started by default with `docker compose up`.

```bash
docker compose up -d supervisor
docker compose logs -f supervisor
```

### hunter (opt-in profile)

The hunter service is gated behind the `hunt` Docker Compose profile so it
does not start by accident (it consumes SAM.gov API quota):

```bash
# Start hunter alongside supervisor
docker compose --profile hunt up -d

# Or start only the hunter for a one-shot run
docker compose --profile hunt run --rm hunter --keywords "software" --naics 541511
```

---

## Building images manually

```bash
# Build supervisor image
docker build --target supervisor -t mas-supervisor:latest .

# Build hunter image
docker build --target hunter -t mas-hunter:latest .
```

---

## Cleanup

```bash
# Stop all services
docker compose down

# Stop and remove volumes (WARNING: deletes all task state)
docker compose down -v

# Remove built images
docker rmi mas-supervisor:latest mas-hunter:latest
```
