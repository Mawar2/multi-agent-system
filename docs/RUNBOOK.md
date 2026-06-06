# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06
**Audience:** Operators running or maintaining the supervisor in production

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

| Requirement | Minimum version | Notes |
|---|---|---|
| Go | 1.25.1 | `go version` to verify |
| GitHub CLI (`gh`) | Any recent | Authenticated as Mawar2 or your account |
| `golangci-lint` | Any recent | For running lint locally |
| `GITHUB_TOKEN` | — | Scopes: `repo`, `read:org` |
| Git | Any | Configured with `gh auth setup-git` |

> **Windows note:** `make` is not available. Use the raw `go` commands shown throughout this runbook.

### GitHub Token Setup

The supervisor reads `GITHUB_TOKEN` from the environment via `os.Getenv("GITHUB_TOKEN")`.

```powershell
# Verify the token is set (do NOT echo its value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set" }
```

If missing, restore from your PowerShell profile:

```powershell
. $PROFILE
```

Required token scopes: `repo`, `read:org`.

### GitHub CLI Authentication

Workers create PRs via `gh` and clone repos using git's credential helper configured by `gh`. Run this once on a new machine:

```powershell
gh auth login
gh auth setup-git
```

Verify:

```powershell
gh auth status
```

### Build

```powershell
# Build supervisor binary (output: bin/supervisor.exe)
go build -o bin/supervisor.exe ./cmd/supervisor

# Build backfill utility (output: bin/backfill.exe)
go build -o bin/backfill.exe ./cmd/backfill

# Verify all packages compile
go build ./...
```

### First-Run Verification

```powershell
# 1. Ensure tasks directory exists
New-Item -ItemType Directory -Force tasks

# 2. Copy example config (skip if orchestrator.yml already exists)
Copy-Item orchestrator.example.yml orchestrator.yml

# 3. Edit orchestrator.yml to point at your project(s)
notepad orchestrator.yml

# 4. Run supervisor (Ctrl+C to stop)
.\bin\supervisor.exe --config orchestrator.yml
```

You should see output like:

```
Loading configuration from orchestrator.yml...
Initializing task queue at ./tasks...
Initializing task router...
Initializing GitHub REST client...
Initializing supervisor...
Initializing worker pools...
Started 10 workers

Monitoring 1 project(s):
  - Mawar2/Kaimi

Supervisor running. Press Ctrl+C to stop.
```

---

## 2. Operation

### Normal State Log Indicators

| Log line | Meaning |
|---|---|
| `Supervisor: Polling project Mawar2/Kaimi` | Normal poll tick (every 60 s) |
| `Supervisor: Found N open issues` | GitHub returned N issues; supervisor will route each |
| `Supervisor: Routed issue #N - complexity: simple, tier: gemini-flash` | Issue classified, task enqueued |
| `[gemini-flash-1] Claimed task <uuid>` | Worker picked up a task |
| `[WorkspaceManager] Cloning Mawar2/Kaimi into workspace...` | First-time clone for that worker |
| `[WorkspaceManager] Successfully cloned Mawar2/Kaimi` | Clone done |
| `[QualityGates] ✅ Tests passed` | Test gate clear |
| `[QualityGates] ✅ Linter passed` | Lint gate clear |
| `[QualityGates] ✅ Formatter passed` | Format gate clear |
| `[QualityGates] ✅ All quality checks passed - safe to create PR` | PR creation proceeding |
| `[gemini-flash-1] Completed task <uuid> - PR #N created` | Task done; PR open for review |

### Issue → PR Lifecycle

```
GitHub Issue (open)
       │
       ▼
Supervisor polls (every 60 s)
       │
       ▼
RuleBasedRouter classifies complexity
  simple → gemini-flash tier
  medium → gemini-pro tier
  complex → claude tier
       │
       ▼
Task enqueued (status: pending) → tasks/<uuid>.json
       │
       ▼
Worker claims task (status: claimed)
       │
       ▼
Worker clones/updates per-worker workspace
  projects/<worker-id>/<owner>/<repo>/
       │
       ▼
Claude Code CLI implements solution
       │
       ▼
Quality gates (tests → linter → formatter → build?)
  FAIL → task marked failed; no PR created
  PASS → continue
       │
       ▼
PR created on GitHub (status: review)
       │
       ▼
AI review posts comment (prefix: "## 🤖 AI Code Review")
       │
       ▼
Supervisor detects comment (polls every 120 s)
       │
       ▼
Fix task created (pr_feedback, inherits branch/PR/tier)
       │
       ▼
Worker applies targeted fix, pushes, PR updated
  Max 3 iterations; 4th failure marks task failed
       │
       ▼
Human reviews / merges PR (status: complete)
```

### Worker Tiers

| Tier | Worker IDs | Default workers | Handles |
|---|---|---|---|
| `gemini-flash` | `gemini-flash-1` … `gemini-flash-5` | 5 | Simple (docs, typos, small fixes) |
| `gemini-pro` | `gemini-pro-1` … `gemini-pro-3` | 3 | Medium (features, refactors) |
| `claude` | `claude-1`, `claude-2` | 2 | Complex (architecture, migrations) |

> All tiers currently run the Claude Code backend. The `gemini-*` tier names are reserved for future Gemini/Antigravity integration.

### Task Queue — Useful Commands

```powershell
# List all tasks with status
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    [PSCustomObject]@{ ID = $t.id.Substring(0,8); Issue = $t.issue_number; Status = $t.status; Worker = $t.worker_id }
} | Format-Table -AutoSize

# Show a specific task in full
Get-Content tasks\<uuid>.json | ConvertFrom-Json | ConvertTo-Json -Depth 10

# Count tasks by status
Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_.FullName | ConvertFrom-Json).status
} | Group-Object | Select-Object Name, Count
```

### Day-to-Day Commands

```powershell
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Test (drop -race — CGO disabled on this machine)
go test -cover ./...

# Lint (4 pre-existing findings in github_rest_client.go — not regressions)
golangci-lint run ./...

# Vet
go vet ./...

# Clean task queue (archive before deleting)
Copy-Item tasks\ archive\tasks-$(Get-Date -Format yyyyMMdd)\ -Recurse
Remove-Item tasks\*.json

# Clean workspaces
Remove-Item -Recurse -Force projects\
```

---

## 3. Configuration Reference

### Full `orchestrator.yml` Schema

```yaml
# ── Projects to monitor ──────────────────────────────────────────────────────
projects:
  - name: kaimi                         # Internal name; used in logs
    repo_owner: Mawar2                  # GitHub org or user
    repo_name: Kaimi                    # Repository name
    conventions_path: ./CLAUDE.md       # Path to conventions file in the TARGET repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"  # Branch name template
    commit_pattern: "{ticket}_{description}"           # Commit message template
    labels: []                          # Optional: only process issues with these labels
                                        # e.g., ["orchestrator:pending", "ai-ready"]

# ── Worker tier settings ──────────────────────────────────────────────────────
worker_tiers:
  gemini_flash:
    max_workers: 5                      # Concurrent workers in this tier
    model: gemini-flash-3.5             # Model name (informational; backend currently uses Claude)

  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5

  claude:
    max_workers: 2
    model: claude-sonnet-4.5

# ── Supervisor settings ───────────────────────────────────────────────────────
poll_interval_seconds: 60              # GitHub polling interval (default: 60)
task_timeout_minutes: 120              # Max time a worker spends per task (default: 120)
max_retry_attempts: 3                  # Retries before marking task failed (default: 3)
task_queue_dir: ./tasks                # JSON queue directory (default: ./tasks)
```

### Routing Heuristics

The `RuleBasedRouter` in `internal/orchestrator/router.go` assigns complexity in this order:

| Priority | Signal | Complexity |
|---|---|---|
| 1 | Title/body contains `fix typo`, `update readme`, `docs:`, `[docs]`, `documentation`, `add comment`, `add godoc`, `add logging`, `format code`, `update version`, `add documentation` | Simple |
| 2 | Title/body matches `architecture`, `design`, `refactor.*system`, `implement.*agent`, `database`, `migration`, `schema change`, `security`, `authentication`, `authorization`, `breaking change`, `api redesign` | Complex |
| 3 | Body mentions files: ≤3 files → Simple; >10 files → Complex | Simple / Complex |
| 4 | Issue label contains `simple` or `easy` | Simple |
| 4 | Issue label contains `complex` or `hard` | Complex |
| 5 | No clear signal | Medium (default) |

Complexity maps to tier 1:1: Simple → `gemini-flash`, Medium → `gemini-pro`, Complex → `claude`.

### Branch and Commit Pattern Variables

| Variable | Value |
|---|---|
| `{ticket}` | Issue number (e.g., `47`) |
| `{summary}` | Slugified issue title |
| `{description}` | Short description for commit message |

Example: `branch_pattern: "feature/KAI-{ticket}-{summary}"` → `feature/KAI-47-add-comment-to-readme`

### Label Filtering

Set `labels` in a project config to process only issues with those labels:

```yaml
projects:
  - name: kaimi
    repo_owner: Mawar2
    repo_name: Kaimi
    labels: ["orchestrator:pending"]    # Only process issues tagged for orchestration
```

Leave `labels: []` (empty) to process all open issues.

---

## 4. Troubleshooting

### 401 — GitHub Authentication Failure

**Symptom:** `GitHub API returned status 401`

**Steps:**
1. Check token is set: `if ($env:GITHUB_TOKEN) { "set" } else { "NOT set" }`
2. If missing, restore: `. $PROFILE`
3. Verify token scopes: `gh auth status`
4. Confirm token hasn't expired in GitHub → Settings → Developer settings → Personal access tokens
5. Re-set if needed: `$env:GITHUB_TOKEN = "ghp_..."`

---

### 403 / Rate Limit Exceeded

**Symptom:** `GitHub API returned status 403` or `rate limit exceeded` in logs

**Steps:**
1. GitHub REST API: 5,000 requests/hour for authenticated requests
2. Check remaining quota:
   ```powershell
   $headers = @{ Authorization = "token $env:GITHUB_TOKEN" }
   Invoke-RestMethod "https://api.github.com/rate_limit" -Headers $headers | Select-Object -ExpandProperty rate
   ```
3. If limit hit, the supervisor will recover on the next poll cycle (60 s)
4. Reduce `poll_interval_seconds` if hitting limits repeatedly (increase the value)
5. Reduce `max_workers` to lower GitHub API call volume per cycle

---

### Stalled Tasks (stuck in `claimed` or `in_progress`)

**Symptom:** Tasks remain in `claimed`/`in_progress` status indefinitely; no worker logs for that task

**Steps:**
1. Find stalled tasks:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
       $t = Get-Content $_.FullName | ConvertFrom-Json
       if ($t.status -in @(1, 2)) {  # 1=claimed, 2=in_progress
           $age = (Get-Date) - [datetime]$t.claimed_at
           if ($age.TotalMinutes -gt 130) { $t | Select-Object id, status, worker_id, claimed_at }
       }
   } | Format-Table
   ```
2. If a task is stalled past `task_timeout_minutes` (default 120), the supervisor's Release mechanism returns it to `pending` automatically on the next startup
3. To manually release a stalled task, edit the JSON file: set `status` to `0` (pending) and clear `worker_id`
4. Check worker logs for the specific `worker_id` that claimed the task

---

### Quality Gate Failures

**Symptom:** Tasks fail with `quality gate failed - tests:` / `quality gate failed - linter:` / etc.

**Steps:**
1. Find failures:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
       $t = Get-Content $_.FullName | ConvertFrom-Json
       if ($t.status -eq 5 -and $t.error_msg -like "*quality gate*") {
           "$($t.issue_number): $($t.error_msg.Substring(0, [Math]::Min(120, $t.error_msg.Length)))"
       }
   }
   ```
2. Check the target repo's conventions file (CLAUDE.md or CONVENTIONS.md) for correct test/lint/format commands
3. Verify the commands work manually in the worker workspace:
   ```powershell
   cd projects\gemini-flash-1\Mawar2\Kaimi
   go test ./...
   golangci-lint run ./...
   ```
4. If the target project itself has failing tests (not caused by worker), fix the project first
5. Quality gate failures do NOT consume AI review budget — this is intentional

---

### Missing Fix Tasks (AI Review Comments Not Picked Up)

**Symptom:** AI posts a review comment on a PR but no `pr_feedback` task is created

**Steps:**
1. Confirm the comment starts with: `## 🤖 AI Code Review (Gemini 2.5 Pro)`
2. Check the supervisor is running (it polls every 120 s for review comments)
3. Verify the original task's `status` is `review` (value: `3`) in the JSON file
4. Check `review_comment_id` in existing tasks to ensure no deduplication collision
5. Check supervisor logs for `ReviewMonitor` entries around the time the comment was posted
6. Max iterations is 3 (`review_iteration` values 1, 2, 3); tasks at iteration 3 that still fail are marked `failed` with message `Max review iterations reached`

---

### Clone Failures

**Symptom:** `fatal: destination path already exists` or `fatal: repository not found`

**Steps:**
1. `destination path already exists` — This should not happen with per-worker isolation; if it does, a previous workspace is orphaned:
   ```powershell
   Remove-Item -Recurse -Force projects\<worker-id>\
   ```
2. `repository not found` — Verify `repo_owner`/`repo_name` in `orchestrator.yml` and that your token has access to the repo
3. `GIT_TERMINAL_PROMPT=0` — Workers set this flag; any auth gap fails fast. Run `gh auth setup-git` and retry

---

### Duplicate Tasks

**Symptom:** Multiple tasks created for the same issue number

**Steps:**
1. The supervisor checks for existing PRs for each issue before enqueuing; if a PR exists, the issue is skipped
2. Duplicate tasks may appear if:
   - The PR was deleted after the first task completed
   - The supervisor restarted mid-cycle
3. To clean up duplicates, find tasks for the same issue and mark extras as `failed` (status: `5`) manually in the JSON

---

### Supervisor Exits Immediately

**Symptom:** Supervisor prints startup lines then exits with a non-zero code

**Steps:**
1. Check for config errors:
   ```powershell
   .\bin\supervisor.exe --config orchestrator.yml
   ```
   The error message will identify the exact field
2. Common causes: missing `repo_owner`, missing `repo_name`, empty `projects` list, malformed YAML
3. Validate YAML syntax:
   ```powershell
   go run -v ./cmd/supervisor --config orchestrator.yml
   ```
4. Ensure `orchestrator.yml` is present (not just `orchestrator.example.yml`)

---

### CGO / Race Detector Build Failure

**Symptom:** `go test -race ./...` fails with `-race requires cgo`

**Cause:** CGO is disabled on this machine; the race detector requires a C compiler.

**Fix:** Drop `-race`:
```powershell
go test -cover ./...   # Works fine without -race
```

Do NOT use `make test` — the Makefile uses `-race`. Use the raw go command above.

---

### Pre-existing Lint Findings (Non-regression)

**Symptom:** `golangci-lint run ./...` reports 4 findings in `internal/ticket/github_rest_client.go`

These are pre-existing issues (3× unchecked `resp.Body.Close` errcheck, 1× staticcheck QF1003). They were not introduced by recent work. Fix opportunistically when touching that file; do not block deploys on them.

---

## 5. Monitoring

### Key Metrics — PowerShell Queries

```powershell
# Total tasks by status (0=pending 1=claimed 2=in_progress 3=review 4=complete 5=failed)
Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_.FullName | ConvertFrom-Json).status
} | Group-Object | Sort-Object Name | Select-Object Name, Count

# Throughput: tasks completed today
$today = (Get-Date).Date
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if ($t.status -eq 4 -and $t.completed_at -and [datetime]$t.completed_at -ge $today) { $t }
} | Measure-Object | Select-Object Count

# Failure rate (failed / total terminal)
$tasks = Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json }
$terminal = $tasks | Where-Object { $_.status -in @(4, 5) }
$failed = $terminal | Where-Object { $_.status -eq 5 }
"Failure rate: $([Math]::Round($failed.Count / [Math]::Max(1, $terminal.Count) * 100, 1))%"

# Quality gate failure rate
$qgFailed = $tasks | Where-Object { $_.status -eq 5 -and $_.error_msg -like "*quality gate*" }
"Quality gate failures: $($qgFailed.Count) / $($tasks.Count) total"

# Review iteration distribution (expect: most at 0, few at 1-3)
$tasks | Group-Object { $_.review_iteration } | Sort-Object Name | Select-Object Name, Count

# Tasks that hit max iterations
$tasks | Where-Object { $_.error_msg -like "*Max review iterations*" } | Measure-Object | Select-Object Count

# Backfilled task count
$tasks | Where-Object { $_.metadata.backfilled -eq "true" } | Measure-Object | Select-Object Count

# Active worker utilization (in-progress or claimed right now)
$tasks | Where-Object { $_.status -in @(1, 2) } | Select-Object worker_id, issue_number, status
```

### Log Pattern Table

| Pattern to grep | Significance | Normal frequency |
|---|---|---|
| `quality gate failed` | PR rejected before creation | Expect 20-40% of tasks |
| `Max review iterations` | Fix loop exhausted | Should be <5% of PRs |
| `Error claiming task` | Worker loop error (usually transient) | Occasional; watch for bursts |
| `GitHub API returned status 401` | Token missing or expired | Should be 0 |
| `GitHub API returned status 403` | Rate limit or permissions | Should be 0 |
| `fatal:` | Git error in workspace | Should be 0 |
| `Supervisor error` | Supervisor main loop crashed | Should be 0 |

### Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Failure rate | >40% | >60% |
| Quality gate failure rate | >50% | >70% |
| Max-iterations rate | >10% | >20% |
| Tasks stuck in `claimed`/`in_progress` >130 min | Any | — |
| `401` / `403` errors | Any | — |

---

## 6. Maintenance

### Task Queue Cleanup

Tasks accumulate in `tasks/` as JSON files. Archive periodically:

```powershell
# Archive tasks older than 30 days
$cutoff = (Get-Date).AddDays(-30)
$archive = "archive\tasks-$(Get-Date -Format yyyyMMdd)"
New-Item -ItemType Directory -Force $archive

Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    $date = if ($t.completed_at) { [datetime]$t.completed_at } elseif ($t.claimed_at) { [datetime]$t.claimed_at } else { $_.LastWriteTime }
    if ($date -lt $cutoff) {
        Move-Item $_.FullName $archive\
    }
}

Write-Host "Archived tasks to $archive"
```

To delete old terminal tasks outright (irreversible — archive first):

```powershell
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if ($t.status -in @(4, 5)) { Remove-Item $_.FullName }
}
```

### Workspace Cleanup

Worker workspaces are cloned repos under `projects/`. Each worker's workspace is approximately 200 MB. Total for 10 workers: ~2 GB.

```powershell
# Remove all workspaces (workers will re-clone on next task)
Remove-Item -Recurse -Force projects\

# Remove a single worker's workspace
Remove-Item -Recurse -Force projects\gemini-flash-3\
```

Safe to delete at any time the supervisor is not running. Running the supervisor with active workers during cleanup may cause clone errors.

### Binary Update Procedure

```powershell
# 1. Stop supervisor (Ctrl+C or kill process)

# 2. Pull latest code
git pull origin master

# 3. Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor
go build -o bin/backfill.exe ./cmd/backfill

# 4. Run tests
go test -cover ./...

# 5. Restart
.\bin\supervisor.exe --config orchestrator.yml
```

### Adding a New Project

1. Add an entry to `orchestrator.yml`:
   ```yaml
   projects:
     - name: my-project
       repo_owner: MyOrg
       repo_name: MyRepo
       conventions_path: ./CLAUDE.md
       branch_pattern: "feature/{ticket}-{summary}"
       commit_pattern: "{ticket}: {description}"
       labels: []
   ```

2. Ensure your `GITHUB_TOKEN` has `repo` access to `MyOrg/MyRepo`.

3. Restart the supervisor.

Workers will begin cloning `MyRepo` into `projects/<worker-id>/MyOrg/MyRepo/` on first task.

### Backfill Utility

Use `cmd/backfill` to import existing open PRs into the task queue as `review`-status tasks, so the AI review feedback loop picks them up:

```powershell
# Build (if not already built)
go build -o bin/backfill.exe ./cmd/backfill

# Run (requires GITHUB_TOKEN, reads ./tasks)
.\bin\backfill.exe
```

The backfill script:
- Fetches open PRs from `Mawar2/Kaimi`
- Skips draft PRs
- Creates a `StatusReview` task for each, with `metadata.backfilled = "true"`
- Infers complexity from PR additions+deletions (separate heuristic from the router)

Run with the supervisor stopped to avoid race conditions on task creation, then start the supervisor to process the backfilled tasks.

---

## 7. Security

### Token Handling

- Store `GITHUB_TOKEN` in your PowerShell profile (`$PROFILE`), not in `orchestrator.yml` or committed files
- `orchestrator.yml` is git-ignored — never commit it with credentials
- Required scopes: `repo`, `read:org` — do not grant broader permissions
- Rotate the token if it may have been exposed; revoke the old token immediately in GitHub Settings → Developer settings → Personal access tokens
- Do NOT echo the token value in logs or terminal output:
  ```powershell
  # Safe: only confirms presence
  if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set" }
  ```

### Worker Permission Scope

Workers run Claude Code CLI with `--dangerously-skip-permissions` so the headless agent can edit files and run git/gh/tests in its isolated workspace. This is intentional for automation but means the worker has full file and shell access within its workspace.

To restrict this for testing or dry-runs, set:

```powershell
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"   # Prompts before running shell commands
$env:CLAUDE_PERMISSION_MODE = "plan"           # Read-only planning, no writes
```

Restore to default (no restriction) by clearing:

```powershell
Remove-Item Env:\CLAUDE_PERMISSION_MODE -ErrorAction SilentlyContinue
```

### Log Hygiene

- Worker logs may contain issue body text and PR descriptions — treat as potentially sensitive if the monitored repositories contain non-public information
- Do not forward logs to external services without reviewing for secrets
- `supervisor_test.log` in the repo root is git-ignored; verify before committing log files

### Dependency Auditing

```powershell
# List direct dependencies
go list -m all

# Check for known vulnerabilities (requires govulncheck)
govulncheck ./...
```

Run `govulncheck` after updating `go.mod`. The module has two runtime dependencies: `gopkg.in/yaml.v3` and `github.com/google/uuid`.
