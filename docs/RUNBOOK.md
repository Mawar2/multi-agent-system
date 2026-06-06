# Operator Runbook — Multi-Agent Orchestration System

**Audience:** Engineers operating or on-call for the supervisor process  
**Last updated:** 2026-06-06

---

## Table of Contents

1. [Getting Started](#1-getting-started)
2. [Operation](#2-operation)
3. [Configuration Reference](#3-configuration-reference)
4. [Troubleshooting](#4-troubleshooting)
5. [Monitoring](#5-monitoring)
6. [Maintenance](#6-maintenance)
7. [Security](#7-security)

---

## 1. Getting Started

### Prerequisites

| Requirement | Version / Notes |
|---|---|
| Go | 1.25.1+ (`go version`) |
| GitHub CLI (`gh`) | Any recent release; must be authenticated |
| `golangci-lint` | Any recent release (lint gate) |
| `GITHUB_TOKEN` env var | Scopes: `repo`, `read:org` |
| Git | Configured with `gh auth setup-git` |
| Windows | PowerShell 5.1+; WSL/Bash also works for POSIX commands |

### Set up `GITHUB_TOKEN`

The token is read from the environment variable `GITHUB_TOKEN` by both `cmd/supervisor` and `cmd/backfill` via `os.Getenv("GITHUB_TOKEN")`.

```powershell
# Verify the token is present (do not echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — check `$PROFILE`" }
```

If missing, the token is stored in `$PROFILE`. Source it with:

```powershell
# Re-source your PowerShell profile
. $PROFILE
```

Required scopes: **`repo`** (full repository access) and **`read:org`** (org membership).

### Authenticate the GitHub CLI

Workers create PRs via `gh`. Authenticate once:

```bash
gh auth login
gh auth setup-git   # configures git credential helper so clone/push work headlessly
```

Verify authentication:

```bash
gh auth status
```

### Build the supervisor

> **Note:** `make` is not installed on this Windows machine — use raw `go` commands.

```bash
# From repo root
go build -o bin/supervisor.exe ./cmd/supervisor

# Verify build
./bin/supervisor.exe --help
```

### First-run verification

```bash
# Start supervisor with the example config (dry-run friendly)
./bin/supervisor.exe --config orchestrator.example.yml
```

Expected console output on healthy startup:

```
Loading configuration from orchestrator.example.yml...
Initializing task queue at ./tasks...
Initializing task router...
Initializing GitHub REST client...
Initializing GitHub ticket client...
Initializing supervisor...
Initializing worker pools...
Started 10 workers

Monitoring 1 project(s):
  - Mawar2/Kaimi

Supervisor running. Press Ctrl+C to stop.
```

If you see `Error loading config` or `no projects configured`, check your YAML for required fields (`name`, `repo_owner`, `repo_name`).

---

## 2. Operation

### Normal-state log indicators

| Log line | Meaning |
|---|---|
| `Supervisor: Polling project Mawar2/Kaimi` | Heartbeat — one per `poll_interval_seconds` |
| `Supervisor: Found N open issues` | Issue discovery succeeded |
| `Supervisor: Enqueued task <uuid> for issue #N` | Task added to queue |
| `[gemini-flash-1] Claimed task <uuid>` | Worker picked up a task |
| `[WorkspaceManager] Successfully cloned Mawar2/Kaimi` | Repo cloned to per-worker workspace |
| `[QualityGates] ✅ Tests passed` | All quality checks green |
| `[Worker X] Completed task — PR #N created` | PR live on GitHub |
| `Supervisor: Found PR #N for issue #M` | Skipping already-open issue |

### Issue → PR lifecycle

```
GitHub Issue (open)
       │
       ▼  (supervisor polls every 60s)
  Task created → StatusPending
       │
       ▼  (worker claims)
  StatusClaimed → StatusInProgress
       │
       ▼  (LLM implements, quality gates run)
  Quality gates pass → PR created → StatusReview
       │
       ▼  (AI review posts comment)
  Supervisor detects feedback → creates pr_feedback task
       │
       ▼  (worker applies fixes, pushes)
  PR updated → StatusReview (iteration 1–3)
       │
       ▼  (human approves / merges)
  StatusComplete
```

### Worker tier overview

| Tier | Workers | IDs | Handles |
|---|---|---|---|
| Gemini Flash | 5 | `gemini-flash-1` … `gemini-flash-5` | Simple (docs, typos, config) |
| Gemini Pro | 3 | `gemini-pro-1` … `gemini-pro-3` | Medium (features, refactors) |
| Claude | 2 | `claude-1`, `claude-2` | Complex (architecture, security, migrations) |

### Inspect the task queue (PowerShell)

```powershell
# List all tasks and their status
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    [PSCustomObject]@{ ID=$t.id[0..7] -join ''; Status=$t.status; Issue=$t.issue_number; Worker=$t.worker_id }
} | Format-Table -AutoSize

# Count by status (0=pending,1=claimed,2=in_progress,3=review,4=complete,5=failed)
Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_ | ConvertFrom-Json).status
} | Group-Object | Sort-Object Name
```

### Day-to-day commands

```bash
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Test (drop -race; CGO is off on this machine)
go test -cover ./...

# Lint
golangci-lint run ./...

# Format
gofmt -w .

# Run supervisor
./bin/supervisor.exe --config orchestrator.yml
```

---

## 3. Configuration Reference

### Full `orchestrator.yml` schema

```yaml
# ── Projects ────────────────────────────────────────────────────────────────
projects:
  - name: kaimi                        # Internal project identifier (required)
    repo_owner: Mawar2                 # GitHub organization or user (required)
    repo_name: Kaimi                   # GitHub repository name (required)
    conventions_path: ./CLAUDE.md      # Path to target repo's conventions file
    branch_pattern: "feature/KAI-{ticket}-{summary}"
    commit_pattern: "{ticket}_{description}"
    labels: []                         # Filter issues by label (empty = all issues)

# ── Worker Tiers ─────────────────────────────────────────────────────────────
worker_tiers:
  gemini_flash:
    max_workers: 5                     # Concurrent workers (default: 5)
    model: gemini-flash-3.5            # Informational only (all tiers use Claude backend today)

  gemini_pro:
    max_workers: 3                     # Default: 3
    model: gemini-pro-3.5

  claude:
    max_workers: 2                     # Default: 2
    model: claude-sonnet-4.5

# ── Supervisor Settings ──────────────────────────────────────────────────────
poll_interval_seconds: 60              # How often to poll GitHub for new issues (default: 60)
task_timeout_minutes: 120             # Max time a worker spends per task (default: 120)
max_retry_attempts: 3                  # Max attempts before StatusFailed (default: 3)
task_queue_dir: ./tasks               # Directory for JSON task files (default: ./tasks)
```

### Routing heuristics (verified against `internal/orchestrator/router.go`)

The `RuleBasedRouter` classifies each issue as Simple, Medium, or Complex:

| Signal | Result |
|---|---|
| Title/body contains: `add comment`, `add godoc`, `fix typo`, `update readme`, `format code`, `add logging`, `update version`, `docs:`, `[docs]`, `documentation` | **Simple** → Gemini Flash |
| Title/body matches: `architecture`, `design`, `refactor.*system`, `implement.*agent`, `database`, `migration`, `schema change`, `security`, `authentication`, `authorization`, `breaking change`, `api redesign` | **Complex** → Claude |
| Body mentions `files:` or `affected files` with ≤3 file bullet points | **Simple** |
| Body mentions `files:` or `affected files` with >10 file bullet points | **Complex** |
| Label contains `simple` or `easy` | **Simple** |
| Label contains `complex` or `hard` | **Complex** |
| No signals match | **Medium** → Gemini Pro (default) |

### Branch and commit pattern variables

| Variable | Substituted with |
|---|---|
| `{ticket}` | Issue number |
| `{summary}` | Slugified issue title |
| `{description}` | Short description of change |

### Label filtering

Set `labels` in a project config to restrict which issues the supervisor picks up:

```yaml
projects:
  - name: kaimi
    repo_owner: Mawar2
    repo_name: Kaimi
    labels:
      - "orchestrator:pending"   # Only issues with this label
```

Leave `labels: []` (or omit the field) to process all open issues.

---

## 4. Troubleshooting

### 4.1 GitHub 401 Unauthorized

**Symptom:** `GitHub API returned status 401`

**Steps:**
1. Verify token is set: `if ($env:GITHUB_TOKEN) { "ok" } else { "missing" }`
2. Re-source profile if missing: `. $PROFILE`
3. Check token scopes: `gh auth status` (needs `repo` and `read:org`)
4. If the token is expired, generate a new one at GitHub → Settings → Developer settings → Personal access tokens

### 4.2 GitHub rate limit (403 / `rate limit exceeded`)

**Symptom:** `GitHub API returned status 403` with body containing `rate limit`

**Steps:**
1. Check remaining quota: `gh api rate_limit`
2. The REST client uses authenticated requests (5,000/hour limit) — if exhausted, wait for reset (`reset` timestamp in the response)
3. Reduce `poll_interval_seconds` in `orchestrator.yml` to poll less often

### 4.3 Stalled tasks (stuck in `claimed` or `in_progress`)

**Symptom:** Tasks remain in status 1 (claimed) or 2 (in_progress) beyond `task_timeout_minutes`

**Steps:**
1. Identify stalled tasks:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
       $t = Get-Content $_ | ConvertFrom-Json
       if ($t.status -in @(1,2)) { $t | Select-Object id, status, worker_id, started_at }
   }
   ```
2. Check if the worker process is still alive (the supervisor goroutine logs `[workerID] Worker stopping` on shutdown)
3. To manually recover a stalled task, edit the JSON file and set `"status": 0` (pending) and clear `"worker_id": ""`
4. The supervisor will automatically release tasks whose `started_at` exceeds `task_timeout_minutes` via `IsStalled()`

### 4.4 Quality gate failures

**Symptom:** Tasks fail with `error_msg` containing `quality gates`

**Steps:**
1. Identify failures:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
       $t = Get-Content $_ | ConvertFrom-Json
       if ($t.error_msg -like "*quality gates*") { $t | Select-Object issue_number, error_msg }
   }
   ```
2. The quality gate runner (`internal/worker/quality_gates.go`) runs the commands defined in the target repo's conventions file
3. Check that `conventions_path` in `orchestrator.yml` points to a valid file in the target repo
4. If a target repo's test/lint commands are missing or broken, the gate will fail — fix the target repo's conventions file or temporarily disable the gate

### 4.5 AI review feedback tasks not created

**Symptom:** PRs receive AI review comments but no `pr_feedback` tasks appear in `tasks/`

**Steps:**
1. Confirm AI review comments start with the expected prefix: `## 🤖 AI Code Review (Gemini 2.5 Pro)`
2. Check that the task for the original PR has `status: 3` (review) — the supervisor only monitors `StatusReview` tasks
3. Verify `GITHUB_TOKEN` has `repo` scope (needed to read PR comments)
4. Check `review_iteration` on existing tasks — if already at 3, no further fix tasks are created (max iterations reached)

### 4.6 Clone failures (`destination path already exists`)

**Symptom:** `fatal: destination path already exists and is not an empty directory`

**Steps:**
1. This should not occur with per-worker isolation — each worker has its own path under `./projects/{workerID}/`
2. If it does occur, a previous run left a dirty workspace. Clean it:
   ```bash
   rm -rf projects/
   ```
3. The `WorkspaceManager` holds a per-worker-repo mutex; if the process crashes mid-clone, the lock is released on restart

### 4.7 Duplicate tasks for the same issue

**Symptom:** Multiple tasks with the same `issue_number`

**Steps:**
1. The supervisor checks for existing PRs before creating a task — if a PR already exists for an issue, it skips it
2. If duplicates appear, a PR may have been closed/deleted after the task was enqueued
3. Manual cleanup: identify duplicates and set extras to `"status": 5` (failed) with an explanatory `error_msg`

### 4.8 Supervisor exits immediately

**Symptom:** Process starts then exits with `Supervisor error: ...`

**Common causes:**
- `orchestrator.yml` not found — pass `--config path/to/orchestrator.yml`
- YAML parse error — validate with `go run ./cmd/supervisor --config orchestrator.yml` and read the error
- No projects configured — `projects:` must have at least one entry

### 4.9 Build fails with `CGO_ENABLED=0` / `-race requires cgo`

**Symptom:** `go test -race ./...` fails with `requires cgo`

**Fix:** Drop the `-race` flag. CGO is disabled on this machine (no C compiler):

```bash
go test -cover ./...   # works
go test -race ./...    # fails — do not use
```

### 4.10 Pre-existing lint findings in `github_rest_client.go`

**Symptom:** `golangci-lint` reports findings in `internal/ticket/github_rest_client.go`

**Context:** There are 4 pre-existing findings (3× unchecked `resp.Body.Close` `errcheck`, 1× `staticcheck QF1003`) that predate recent work. They are tracked but not introduced by new changes. Fix opportunistically when touching that file.

---

## 5. Monitoring

### Key metrics (PowerShell queries)

```powershell
# Task throughput — how many tasks completed today
$today = (Get-Date).Date
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -eq 4 -and [datetime]$t.completed_at -ge $today) { $t }
} | Measure-Object | Select-Object -ExpandProperty Count

# Failure rate
$all = (Get-ChildItem tasks\*.json).Count
$failed = (Get-ChildItem tasks\*.json | Where-Object {
    (Get-Content $_ | ConvertFrom-Json).status -eq 5
}).Count
"Failure rate: $([math]::Round($failed / [math]::Max($all,1) * 100, 1))%"

# Review iteration distribution (expect: most tasks at 0)
Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_ | ConvertFrom-Json).review_iteration
} | Group-Object | Sort-Object Name | Format-Table Name, Count

# Tasks that hit the max-iterations limit
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.error_msg -like "*Max review iterations*") { $t | Select-Object id, issue_number, error_msg }
}

# Count backfilled tasks
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.metadata.backfilled -eq "true") { $t }
} | Measure-Object | Select-Object -ExpandProperty Count
```

### Log patterns table

| Pattern to watch | Meaning | Action if frequent |
|---|---|---|
| `Quality gate failed` | LLM produced code that doesn't compile/test | Check target repo's test commands; review LLM prompt quality |
| `Error claiming task` | Worker loop error (non-fatal) | Check logs for root cause; usually transient |
| `Max review iterations` | PR couldn't pass AI review in 3 rounds | Manual review required |
| `GitHub API returned status 401` | Token expired or missing | Rotate token |
| `GitHub API returned status 403` | Rate limit or permission error | Check quota; verify token scopes |
| `fatal: destination path already exists` | Workspace collision (should not occur) | Clean `projects/` directory |

### Alerting thresholds

| Metric | Warning | Critical |
|---|---|---|
| Failure rate | >20% | >40% |
| Max-iterations hit | >5% of tasks | >10% of tasks |
| Tasks stuck in `in_progress` | >30 min past timeout | >60 min past timeout |
| Queue depth (pending tasks) | >50 | >200 |

---

## 6. Maintenance

### Task queue cleanup

Tasks accumulate in `./tasks/`. Clean up periodically.

**Archive old completed tasks (PowerShell):**
```powershell
$cutoff = (Get-Date).AddDays(-30)
$archiveDir = "tasks\archive"
New-Item -ItemType Directory -Force $archiveDir | Out-Null

Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    # Archive tasks completed more than 30 days ago
    if ($t.status -in @(4,5) -and $t.completed_at -and [datetime]$t.completed_at -lt $cutoff) {
        Move-Item $_.FullName "$archiveDir\$($_.Name)"
    }
}
```

**Delete all terminal tasks (destructive — do not use in production without backup):**
```powershell
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -in @(4,5)) { Remove-Item $_.FullName }
}
```

### Workspace cleanup

Worker workspaces live under `./projects/`. Remove them when:
- Disk space is low
- A worker's workspace is in an unknown state after a crash

```bash
# Remove all workspaces (workers will re-clone on next task)
rm -rf projects/

# Remove one worker's workspace only
rm -rf projects/gemini-flash-1/
```

### Updating the supervisor binary

```bash
# Pull latest source
git pull origin master

# Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor

# Restart (stop the running process first with Ctrl+C)
./bin/supervisor.exe --config orchestrator.yml
```

### Adding a new project

1. Add an entry to `projects:` in `orchestrator.yml`:
   ```yaml
   - name: new-project
     repo_owner: YourOrg
     repo_name: YourRepo
     conventions_path: ./CLAUDE.md
     branch_pattern: "feature/{ticket}-{summary}"
     commit_pattern: "{ticket}_{description}"
     labels: []
   ```
2. Ensure `GITHUB_TOKEN` has access to the new repo
3. Restart the supervisor

### Backfill existing open PRs into the feedback loop

Use `cmd/backfill` to enqueue existing open PRs as `StatusReview` tasks so the AI review feedback loop picks them up:

```bash
go build -o bin/backfill.exe ./cmd/backfill
./bin/backfill.exe
```

The backfill tool:
- Fetches all open (non-draft) PRs from `Mawar2/Kaimi`
- Creates `StatusReview` tasks with `metadata.backfilled = "true"`
- Infers complexity from PR size (lines changed) and routes to appropriate tier
- Deduplicates via `ReviewCommentID` when the supervisor later processes feedback

### Dependency audit

```bash
# List direct dependencies
go list -m all

# Check for known vulnerabilities
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

---

## 7. Security

### Token handling

- `GITHUB_TOKEN` must never appear in logs, task JSON files, or source code
- The token is read only via `os.Getenv("GITHUB_TOKEN")` — do not pass it as a command-line flag
- Store the token in your PowerShell `$PROFILE` or a secrets manager; do not commit it to the repository
- Rotate the token immediately if it appears in any log file or is accidentally exposed

### Worker permission scope

Workers run Claude Code with `--dangerously-skip-permissions` so the headless agent can edit files and invoke `git`/`gh` without prompting:

```bash
claude --print --dangerously-skip-permissions
```

This flag grants the worker full file-system and shell access **within its isolated workspace** (`./projects/{workerID}/`). Workers should never be given access to paths outside their workspace.

To restrict permissions during debugging or dry-runs, set:

```bash
# Plan mode — LLM plans but does not execute
export CLAUDE_PERMISSION_MODE=plan

# Accept edits only — no shell execution
export CLAUDE_PERMISSION_MODE=acceptEdits
```

### Log hygiene

- Supervisor logs are printed to stdout and are not currently persisted to disk
- Do not redirect supervisor output to a shared or world-readable file without redacting token values first
- Task JSON files in `./tasks/` may contain `review_feedback` with PR content — treat them as internal data

### Dependency auditing

Run `govulncheck` before upgrading dependencies or after a security advisory:

```bash
govulncheck ./...
```

Current direct dependencies (`go.mod`):
- `gopkg.in/yaml.v3` — YAML parsing
- `github.com/google/uuid` — Task ID generation

Both are low-risk; audit if upgrading major versions.
