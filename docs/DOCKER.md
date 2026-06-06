# Docker Deployment

The supervisor ships as a multi-stage Docker image targeting **< 50 MB** on disk.

---

## Quick Start

```bash
# 1. Copy and edit the supervisor config
cp orchestrator.example.yml orchestrator.yml

# 2. (Optional) Copy and edit the hunter config to enable SAM.gov discovery
cp hunter.example.yml hunter.yml

# 3. Export required tokens
export GITHUB_TOKEN=ghp_...
export SAM_API_KEY=your-sam-gov-key   # only needed if running hunter

# 4. Start (supervisor only, or supervisor + hunter)
docker compose up -d supervisor          # supervisor only
docker compose up -d                     # supervisor + hunter

# 5. Tail logs
docker compose logs -f supervisor
docker compose logs -f hunter
```

---

## Image Details

| Stage  | Base image                           | Purpose              |
|--------|--------------------------------------|----------------------|
| Build  | `golang:1.25-alpine`                 | Compile static binary|
| Runtime| `gcr.io/distroless/static-debian12`  | Minimal, nonroot     |

The runtime stage contains only the static Go binary and the example config —
no shell, no package manager, no libc. Expected compressed size: **< 20 MB**.

---

## Configuration

### Supervisor

| Source                       | Description                                |
|------------------------------|--------------------------------------------|
| `orchestrator.yml` (volume)  | Mounted read-only at `/app/orchestrator.yml`|
| `GITHUB_TOKEN` (env)         | Required. repo + read:org scopes           |
| `CLAUDE_PERMISSION_MODE` (env)| Optional. Default: `dangerouslySkipPermissions` |

Copy `orchestrator.example.yml` to `orchestrator.yml` and edit the `projects`
block before starting.

### Hunter

| Source                 | Description                                                   |
|------------------------|---------------------------------------------------------------|
| `hunter.yml` (volume)  | Mounted read-only at `/app/hunter.yml`                        |
| `SAM_API_KEY` (env)    | Required. Register free at https://sam.gov/profile/details    |
| `GITHUB_TOKEN` (env)   | Required. repo scope — creates issues in the tracking repo    |

Copy `hunter.example.yml` to `hunter.yml` and edit `tracking_repo_owner`,
`tracking_repo_name`, `search.keywords`, and `scoring.must_keywords` before
starting. The hunter service is optional — the supervisor runs without it.

---

## Volumes

| Volume     | Container path | Purpose                         |
|------------|---------------|---------------------------------|
| `tasks`    | `/app/tasks`  | JSON task queue (persists restarts)|
| `projects` | `/app/projects`| Cloned target-repo workspaces  |

Both are Docker-managed named volumes. To use host-path mounts instead:

```yaml
volumes:
  - /data/tasks:/app/tasks
  - /data/projects:/app/projects
```

---

## Build Only

```bash
# Build image locally
docker build -t multi-agent-system:local .

# Inspect compressed image size
docker image ls multi-agent-system:local

# Run without compose
docker run --rm \
  -e GITHUB_TOKEN="$GITHUB_TOKEN" \
  -v "$PWD/orchestrator.yml:/app/orchestrator.yml:ro" \
  -v tasks:/app/tasks \
  -v projects:/app/projects \
  multi-agent-system:local
```

---

## Health Check

The container declares a `HEALTHCHECK` that verifies the binary remains executable.
Docker marks the container `healthy` after three consecutive successes, starting
10 seconds after launch.

```bash
# Check health status
docker inspect --format='{{.State.Health.Status}}' multi-agent-supervisor
```

---

## Stopping and Cleanup

```bash
# Stop gracefully (SIGTERM → 10s → SIGKILL)
docker compose stop

# Remove containers and anonymous volumes
docker compose down

# Remove named volumes (WARNING: deletes task queue and workspaces)
docker compose down -v
```
