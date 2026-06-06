# Docker Guide

This guide covers building, configuring, and running the multi-agent system with Docker.

## Quick Start

```bash
# 1. Copy and edit the config
cp orchestrator.example.yml orchestrator.yml
# Edit orchestrator.yml: set repo_owner, repo_name, etc.

# 2. Export required secrets
export GITHUB_TOKEN=ghp_...
export SAM_API_KEY=...    # optional; only needed for the hunter service

# 3. Start the supervisor
docker compose up -d
docker compose logs -f supervisor
```

## Image Details

| Property | Value |
|----------|-------|
| Base image | `gcr.io/distroless/static-debian12:nonroot` |
| Go version | 1.25.1 |
| CGO | Disabled (fully static binaries) |
| Binaries | `/supervisor`, `/hunter` |
| Run as | `nonroot` (UID 65532) |

The image contains **no shell, no package manager, and no libc** — the attack surface is minimal.

## Configuration

Configuration is mounted read-only at `/app/orchestrator.yml`:

```yaml
# orchestrator.yml (copy from orchestrator.example.yml)
projects:
  - repo_owner: Mawar2
    repo_name: Kaimi
    conventions_path: ./CLAUDE.md

worker_tiers:
  gemini_flash: { max_workers: 5, model: gemini-flash-3.5 }
  gemini_pro:   { max_workers: 3, model: gemini-pro-3.5 }
  claude:       { max_workers: 2, model: claude-sonnet-4.5 }

poll_interval_seconds: 60
task_timeout_minutes: 120
max_retry_attempts: 3
task_queue_dir: ./tasks
```

## Volumes

| Volume | Mount | Purpose |
|--------|-------|---------|
| `tasks` | `/app/tasks` | JSON task queue — survives restarts |
| `workspaces` | `/app/projects` | Per-worker git clones — saves re-cloning |

To inspect the task queue from the host:

```bash
docker run --rm -v multi-agent-system_tasks:/data alpine ls /data
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `GITHUB_TOKEN` | Yes | GitHub PAT with `repo` + `read:org` scopes |
| `SAM_API_KEY` | Hunter only | SAM.gov API key for opportunity discovery |

## Running the Hunter

The hunter service is gated behind the `hunt` profile so it doesn't start by default:

```bash
# One-off search
docker compose --profile hunt run --rm hunter --keyword "cloud platform" --naics 541511

# Pipe results to jq
docker compose --profile hunt run --rm hunter --keyword "devops" | jq '.opportunitiesData[].title'
```

## Cleanup

```bash
# Stop the supervisor
docker compose down

# Remove volumes (task queue + workspaces)
docker compose down -v

# Remove the built image
docker rmi mawar2/multi-agent-system:latest
```
