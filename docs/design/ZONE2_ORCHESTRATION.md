# Zone 2 Orchestration Architecture

**Status:** Design proposal
**Date:** 2026-06-06
**Branch context:** `feature/issue-6-containerization`
**Author:** Architecture pass (Claude)

---

## 0. Terminology note

"Zone 2" is **not** a pre-existing concept in this repo — the phrase only appeared
as a placeholder prompt string in `internal/llm/claude_code_test.go:143`. This
document *defines* it. If you meant something else by "Zone 2," stop here and
correct me; everything below assumes the definition in §2.

---

## 1. Why this design exists (the problem)

The containerization work on this branch packs the **entire system into one
image**:

```dockerfile
# Dockerfile (current)
FROM gcr.io/distroless/static-debian12:nonroot   # no shell, no git, no node
ENTRYPOINT ["/app/supervisor"]                    # spawns 10 worker goroutines
```

The supervisor (`cmd/supervisor/main.go`) starts 10 workers as in-process
goroutines. Each worker, via `ClaudeCodeWorker`, shells out to:

- `git` / `gh` (clone, branch, push, open PR — `internal/worker/workspace.go`)
- `claude --print --dangerously-skip-permissions` (`internal/llm/claude_code.go`)
- the target repo's `go test` / linter / formatter (`internal/worker/quality_gates.go`)
- (optionally) the Antigravity/Gemini bridge

**None of those binaries exist in a distroless static image.** The current
container can run the *supervisor loop* (poll GitHub, route, enqueue) but **every
worker will fail the moment it tries to execute a task.** The image is correct for
the control plane and impossible for the execution plane.

Beyond packaging, mixing the two planes in one container is a security and
operability problem:

| Concern | Control plane needs | Execution plane needs |
|---|---|---|
| Attack surface | Minimal (distroless, nonroot, read-only FS) | Large (full toolchain, runs **untrusted AI-generated code**) |
| Privileges | None beyond GitHub API egress | git/gh/network/compile/test execution |
| Failure blast radius | Must stay up (it's the brain) | Expected to crash, OOM, hang, be killed |
| Resource profile | Tiny, steady | Bursty, CPU/RAM/disk heavy, parallel |
| Scaling axis | One instance | N workers, scale horizontally |

These are opposite requirements. **Zone 2 is the answer: split the worker
execution plane out of the supervisor.**

---

## 2. Zone model

```
┌──────────────────────────────────────────────────────────────────────┐
│  ZONE 0 — External (untrusted, outside our trust boundary)             │
│  GitHub API · target git repos · Anthropic/Vertex LLM endpoints        │
└──────────────────────────────────────────────────────────────────────┘
                ▲ GitHub API (token)              ▲ git/gh/PR · LLM calls
                │                                  │
┌───────────────┴──────────────┐   ┌──────────────┴───────────────────────┐
│  ZONE 1 — Control Plane       │   │  ZONE 2 — Execution Plane             │
│  (trusted, no code exec)      │   │  (semi-trusted, runs untrusted code)  │
│                               │   │                                       │
│  • Supervisor loop            │   │  • Worker runtime (per tier)          │
│  • Router (complexity→tier)   │──▶│  • Workspace clone/checkout           │
│  • Task queue authority       │   │  • LLM CLI invocation                 │
│  • PR-feedback monitor        │   │  • Quality gates (test/lint/fmt)      │
│  • Health / reassignment      │◀──│  • git push + gh pr create            │
│                               │   │                                       │
│  Image: distroless static     │   │  Image: fat (git, gh, node, claude,   │
│  (current Dockerfile ✓)       │   │  go toolchain, gemini bridge)         │
└───────────────────────────────┘   └───────────────────────────────────────┘
        │                                          │
        └──────────────┬───────────────────────────┘
                       ▼
        ┌───────────────────────────────┐
        │  Shared task queue (boundary)  │
        │  Phase A: shared volume (JSON) │
        │  Phase B: queue service / DB   │
        └───────────────────────────────┘
```

- **Zone 1 (Control Plane).** Exactly what the current distroless image *should*
  be. Owns the task queue's authoritative state, polls GitHub, routes, monitors
  PR feedback, and reassigns stalled tasks. **Never executes target code.** Holds
  the `GITHUB_TOKEN` for *discovery* only.
- **Zone 2 (Execution Plane).** Where the actual coding happens. Pulls claimed
  tasks, clones the target repo into an isolated workspace, runs the LLM and
  quality gates, and pushes the PR. Treated as **semi-trusted**: it runs
  AI-generated code and arbitrary target-repo test suites, so it is the
  containment boundary.

---

## 3. Zone 2 internal design

### 3.1 Worker runtime image

Zone 2 needs a **fat image** — the opposite of distroless:

```dockerfile
# Dockerfile.worker (new)
FROM golang:1.25-bookworm AS builder
# ... build the worker binary (cmd/worker) ...

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
      git ca-certificates curl \
 && curl -fsSL https://cli.github.com/... | install gh \
 && install node + @anthropic-ai/claude-code \
 && rm -rf /var/lib/apt/lists/*
# Go toolchain available for target-repo quality gates (or per-language layer)
USER worker            # non-root; UID matches volume ownership
ENTRYPOINT ["/app/worker"]
```

Key point: the **target repo's language toolchain** lives here (Go today; future
repos may need Node, Python, etc.). This is the dominant source of image weight
and the reason it can't share the supervisor image. Per-language worker variants
(or a tier→image mapping) keep each image lean.

### 3.2 Splitting the worker out of the supervisor

Today `cmd/supervisor/main.go` constructs workers and runs them as goroutines.
Zone 2 requires a **new `cmd/worker/main.go` entry point** that:

1. Reads its tier from config/env (`WORKER_TIER`, `WORKER_ID`).
2. Constructs one `ClaudeCodeWorker` (or `GeminiWorker`) — the existing types are
   reused **unchanged**; only the wiring moves.
3. Runs the claim→execute→release loop against the shared queue.

The `Worker`, `LLMBackend`, `WorkspaceManager`, and `QualityGate` interfaces
already exist and are already seam-tested (see the fakes in
`internal/worker/claudecode_test.go`). **No worker business logic changes** — this
is a packaging/deployment refactor, not a rewrite.

### 3.3 Worker execution model — two options

**Option A — Long-lived worker containers (recommended for Phase A).**
One container per worker (or per tier, scaled with `--scale`), each running the
claim loop continuously. Maps cleanly onto today's goroutine model and
docker-compose. Workspaces persist between tasks (warm git caches).

```yaml
worker-claude:
  image: mawar2/mas-worker:latest
  deploy: { replicas: 2 }
  # reuses /app/projects/<workerID> isolation that already exists
```

**Option B — Ephemeral per-task containers.**
Zone 1 (or a thin dispatcher) launches a fresh container per claimed task; it
runs exactly one task and exits. Maximum isolation (clean FS every task, trivial
resource capping, no cross-task leakage) at the cost of cold-start + clone every
time. This is the natural Kubernetes-Job / cloud-run-task model and the right
**Phase B** target.

> Recommendation: ship **Option A** now (smallest delta from the current
> goroutine design, works in docker-compose), design the queue boundary (§4) so
> **Option B** is a later swap without touching worker logic.

### 3.4 Workspace isolation

The existing per-worker workspace scheme
(`./projects/{workerID}/{owner}/{repo}/`, per-repo mutex in `workspace.go`)
already prevents *intra-process* conflicts. In Zone 2 each worker container gets
its **own volume**, so the per-repo mutex now only guards a single container's
workspaces — isolation gets *stronger*, not weaker. The CLAUDE.md "~2 GB for 10
workspaces" budget becomes per-container disk quota.

---

## 4. The Zone 1 ↔ Zone 2 boundary (task queue)

This is the load-bearing decision. The queue stops being an in-process detail and
becomes an **inter-zone contract**.

### Phase A — Shared volume (minimal change)

Keep `internal/taskqueue/json.go`. Mount the same `tasks/` volume into Zone 1 and
all Zone 2 containers. Atomic claim already exists via `Dequeue(ctx, tier,
workerID)`.

- ✅ Zero code change to the queue.
- ⚠️ **Risk:** JSON-file atomicity across containers depends on the shared
  filesystem honoring atomic rename/locking. Fine on a single Docker host with a
  local volume; **not safe across hosts** (NFS rename semantics, no cross-host
  lock). Acceptable for single-host compose; a known ceiling.

### Phase B — Queue service

Promote the queue to a real backend behind the existing `TaskQueue` interface
(`internal/taskqueue/queue.go`) — Redis, Postgres `SELECT … FOR UPDATE SKIP
LOCKED`, or the Firestore option already named in `docs/PLAN.md` Phase 3. Zone 2
workers talk to it over the network; Zone 1 retains authority (routing,
reassignment, feedback-loop writes).

- ✅ Multi-host, real atomic claims, observable.
- ✅ Drop-in: implement `TaskQueue`, swap construction in both `main.go`s.

> The interface already abstracts this — **Phase B is an implementation, not a
> redesign.** Designing the split now (instead of after compose ships) is the
> whole point of doing this architecture before the container topology calcifies.

---

## 5. Security boundaries (Zone 2 is the containment layer)

Zone 2 runs **untrusted AI-generated code and arbitrary target-repo test
suites**. Treat it as hostile-by-default:

| Control | Mechanism |
|---|---|
| **Non-root** | `USER worker`; UID-matched volume ownership |
| **Read-only root FS** | `read_only: true` + `tmpfs` for `/tmp`; only the workspace volume is writable |
| **Resource caps** | per-container `cpus`, `mem_limit`, `pids_limit`, disk quota — a runaway `go test` can't starve the host or Zone 1 |
| **Dropped capabilities** | `cap_drop: [ALL]`, `no-new-privileges`, seccomp default |
| **Network egress allow-list** | Zone 2 only needs GitHub (git/gh/API) + the LLM endpoint. Block everything else so exfil/lateral movement from injected code is contained. Zone 1 needs *only* the GitHub API. |
| **Secret scoping** | Zone 2 gets a **push-scoped** token (or short-lived gh credential); Zone 1's discovery token need not grant the worker's push rights. Pass secrets via env/secret mount, never baked into the image. |
| **Timeouts** | `task_timeout_minutes` (already in config) enforced as a hard container kill in Option B, not just a Go context deadline. |

The current `CLAUDE_PERMISSION_MODE: dangerouslySkipPermissions` default is
acceptable **only because** Zone 2 is sandboxed — that flag is precisely why the
worker must not share a container or a network namespace with the control plane.

---

## 6. Mapping to the repo (implementation plan)

| Step | Change | Files |
|---|---|---|
| 1 | Add worker entry point (lift wiring out of supervisor) | **new** `cmd/worker/main.go` |
| 2 | Make supervisor *not* spawn worker goroutines when running split; keep monolith mode behind a flag for local dev | `cmd/supervisor/main.go` |
| 3 | Fat worker image with git/gh/node/claude/toolchain | **new** `Dockerfile.worker` |
| 4 | Keep distroless image for supervisor (already correct) | `Dockerfile` (rename → `Dockerfile.supervisor` for clarity) |
| 5 | Compose: 1 supervisor + N worker services per tier, shared `tasks` volume, per-worker `projects` volumes, resource caps, egress rules | `docker-compose.yml` |
| 6 | Document the two-image topology and security model | `docs/DOCKER.md` (extend) |
| 7 | (Phase B) Implement networked `TaskQueue` backend | **new** `internal/taskqueue/<backend>.go` |

Worker business logic (`internal/worker/*`, `internal/llm/*`,
`internal/conventions/*`) is **untouched** — the interfaces already isolate it.
This keeps the change reviewable and the existing hermetic tests valid.

### Backward-compat / dev ergonomics

Keep a **monolith mode** (supervisor spawns in-process workers, as today) for
`go run ./cmd/supervisor` local development — gated by a config flag or the
presence of `cmd/worker`. Split mode is for containerized/production. This avoids
forcing every dev to run docker-compose.

---

## 7. Open questions (need your call)

1. **Is "Zone 2 = worker execution plane" actually what you meant?** If "zones"
   are a *deployment/network topology* concept from your own roadmap (e.g. DMZ
   tiers, GCP zones, geographic regions), this design is aimed at the wrong
   target — tell me and I'll redo it.
2. **Execution model:** long-lived worker containers (Option A) now, or jump
   straight to ephemeral per-task containers (Option B)?
3. **Orchestrator runtime:** docker-compose single-host (Phase A ceiling), or are
   we targeting Kubernetes / Cloud Run (which makes the queue-service decision
   urgent now)?
4. **Queue boundary:** stay on shared-volume JSON for Phase A, or invest in the
   networked queue immediately?
5. **Per-language toolchains:** one fat worker image, or tier/language-specific
   worker images?

---

## 8. Summary

The current single-container design cannot run workers (distroless lacks the
worker toolchain) and shouldn't (control plane and untrusted execution have
opposite security/scaling needs). **Zone 2 splits the worker execution plane into
its own fat, sandboxed, horizontally-scalable image, behind the task-queue
interface that already exists.** Phase A is a near-mechanical refactor on
docker-compose; the queue interface keeps the door open to a networked Phase B
without rewriting worker logic.
