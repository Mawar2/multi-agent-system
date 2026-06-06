# Docker guide

## Quick start

```bash
# 1. Copy and edit config
cp orchestrator.example.yml orchestrator.yml

# 2. Build image
docker build -t multi-agent-system:latest .

# 3. Start supervisor
GITHUB_TOKEN=ghp_... docker compose up -d supervisor

# 4. Tail logs
docker compose logs -f supervisor
```

## Image details

| Property | Value |
|---|---|
| Base image (runtime) | `gcr.io/distroless/static-debian12:nonroot` |
| Binaries | `/supervisor`, `/hunter` |
| Build flags | `CGO_ENABLED=0 -ldflags="-s -w" -trimpath` |
| Default entrypoint | `/supervisor` |

The multi-stage build compiles fully static Go binaries in a `golang:1.25` builder
stage and copies only the binaries into the distroless runtime image.  The resulting
image has no shell, no package manager, and no OS libraries — only the two binaries.

## Configuration

| Variable | Required | Description |
|---|---|---|
| `GITHUB_TOKEN` | Yes (supervisor) | GitHub PAT with `repo` + `read:org` scopes |
| `SAM_API_KEY` | Yes (hunter) | SAM.gov public API key |

Mount `orchestrator.yml` at `/orchestrator.yml` (the default path read by the
supervisor).  The compose file does this automatically:

```yaml
volumes:
  - ./orchestrator.yml:/orchestrator.yml:ro
```

## Volumes

| Volume | Mount point | Purpose |
|---|---|---|
| `tasks` | `/app/tasks` | JSON task queue (persisted between restarts) |
| `workspaces` | `/app/projects` | Per-worker repository clones |

## Services

### supervisor (default)

Starts automatically with `docker compose up -d`.

```bash
docker compose up -d supervisor
docker compose logs -f supervisor
```

### hunter (opt-in)

Gated behind the `hunt` profile so it does not start by default.

```bash
# Run a one-shot keyword search
SAM_API_KEY=... docker compose run --rm hunter \
  /hunter --keywords "software development" --naics 541511 --limit 25

# Or start via the hunt profile
SAM_API_KEY=... docker compose --profile hunt up hunter
```

## Cleanup

```bash
# Stop services
docker compose down

# Remove volumes (WARNING: deletes task queue and workspace clones)
docker compose down -v

# Remove image
docker rmi multi-agent-system:latest
```
