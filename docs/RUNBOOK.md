# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06
**Maintainer:** Mawar2/multi-agent-system

This runbook covers the full operational lifecycle of the multi-agent orchestration system: startup, normal operation, troubleshooting, monitoring, and maintenance.

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

### 1.1 Prerequisites

| Requirement | Version / Notes | Verify |
|---|---|---|
| Go | 1.25.1+ | `go version` |
| GitHub CLI (`gh`) | Any recent | `gh --version` |
| golangci-lint | Any recent | `golangci-lint --version` |
| Git | 2.x+ | `git --version` |
| GITHUB_TOKEN | `repo` + `read:org` scopes | See §1.2 |

> **Windows note:** `make` is NOT installed on the target machine. Use raw `go` commands (documented throughout this runbook).

### 1.2 GitHub Token Setup

The supervisor and backfill utility authenticate via `GITHUB_TOKEN`.

**Check if the token is already set (current session):**
```powershell
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — see below" }
```

**Set from PowerShell profile (if not already set):**
```powershell
# Token is stored in $PROFILE — source it
. $PROFILE
# Verify without echoing the value
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set" }
```

**Required token scopes:**
- `repo` — read issues, create PRs, push branches
- `read:org` — read organisation membership for private repos

### 1.3 GitHub CLI Authentication

The `gh` CLI must be authenticated so workers can clone private repos and create PRs:

```powershell
gh auth status          # check current auth state
gh auth setup-git       # configure git credential helper (run once per machine)
```

If `gh auth status` shows the correct account (`Mawar2`), no further action is needed.

### 1.4 Build

```powershell
# From the repo root
go build -o bin/supervisor.exe ./cmd/supervisor   # main binary
go build ./...                                     # verify all packages compile
```

The `bin/` directory is git-ignored. Rebuild after any source change.

### 1.5 Configuration File

Copy the example config and customise:

```powershell
Copy-Item orchestrator.example.yml orchestrator.yml
# Edit orchestrator.yml — see §3 for full schema
```

`orchestrator.yml` is git-ignored. Never commit tokens or secrets into it.

### 1.6 First-Run Verification

```powershell
# 1. Confirm token is set
if ($env:GITHUB_TOKEN) { "OK" }

# 2. Confirm gh is authenticated
gh auth status

# 3. Confirm binary built
Test-Path bin/supervisor.exe

# 4. Start the supervisor
.\bin\supervisor.exe --config orchestrator.yml
```

Expected startup output (abridged):
```
Loading configuration from orchestrator.yml...
Initializing task queue at ./tasks...
Started 10 workers
Supervisor running. Press Ctrl+C to stop.
[gemini-flash-1] Worker started (tier: gemini-flash)
...
Supervisor: Starting main loop
```

---

## 2. Operation

### 2.1 Starting and Stopping

```powershell
# Start (foreground — logs to stdout)
.\bin\supervisor.exe --config orchestrator.yml

# Stop gracefully
Ctrl+C    # sends SIGINT; supervisor drains in-progress tasks
```

There is no background daemon mode; run inside a terminal multiplexer or Windows service wrapper if persistent operation is needed.

### 2.2 Normal-State Log Indicators

| Log line | Meaning |
|---|---|
| `Supervisor: Polling project Mawar2/Kaimi` | Poll cycle started |
| `Supervisor: Found N open issues` | GitHub returned N issues |
| `Supervisor: Routed issue #N - complexity: X, tier: Y` | Issue classified and queued |
| `[workerID] Claimed task <uuid>` | Worker picked up a task |
| `[WorkspaceManager] Successfully cloned Mawar2/Kaimi` | Repo cloned into worker workspace |
| `[QualityGates] ✅ Tests passed` | Test gate passed |
| `[QualityGates] ✅ Linter passed` | Lint gate passed |
| `[QualityGates] ✅ Formatter passed` | Format gate passed |
| `[Worker X] Completed task - PR #N created` | PR opened successfully |
| `Supervisor: Monitoring PRs for AI review feedback` | Feedback loop poll started |
| `Supervisor: Created fix task for PR #N` | AI review detected; fix task enqueued |

**No output during a poll interval** is normal if GitHub has no new issues and no tasks are queued.

### 2.3 Issue → PR Lifecycle

```
GitHub Issue (open)
      │
      ▼
Supervisor polls every 60 s
      │
      ▼  classify complexity
Task enqueued (status: pending)
      │
      ▼  worker claims
Task in-progress (status: in_progress)
      │  clone workspace, run LLM, quality gates
      ▼
PR created on GitHub (status: review)
      │
      ├─── AI review passes ──────────► Human reviews PR ──► Merged (status: complete)
      │
      └─── AI review has feedback ────► Fix task enqueued (pr_feedback, iter N)
                                              │
                                              ▼
                                        Worker applies targeted fixes
                                              │
                                              ▼
                                        PR updated, CI reruns
                                              │
                                         (repeat up to 3 iterations)
```

### 2.4 Worker Tiers

| Tier | Worker IDs | Max Workers | Handles | Backend |
|---|---|---|---|---|
| `gemini-flash` | `gemini-flash-1..5` | 5 | Simple tasks | Claude Code CLI (default) |
| `gemini-pro` | `gemini-pro-1..3` | 3 | Medium tasks | Claude Code CLI (default) |
| `claude` | `claude-1..2` | 2 | Complex tasks | Claude Code CLI |

> The `gemini-flash` and `gemini-pro` tiers currently run `ClaudeCodeWorker` on the Claude backend. A native Gemini backend (`USE_GEMINI_WORKER=1`) exists but is not production-ready; see `internal/worker/gemini.go`.

### 2.5 Task Queue — PowerShell Commands

The queue lives in `./tasks/` as individual JSON files, one per task.

```powershell
# List all task files
Get-ChildItem tasks\*.json | Select-Object Name, LastWriteTime

# Pretty-print a single task
Get-Content tasks\<uuid>.json | ConvertFrom-Json | ConvertTo-Json -Depth 10

# Count tasks by status (0=pending,1=claimed,2=in_progress,3=review,4=complete,5=failed)
Get-ChildItem tasks\*.json | ForEach-Object { (Get-Content $_ | ConvertFrom-Json).status } |
  Group-Object | Sort-Object Name

# Show all failed tasks with error messages
Get-ChildItem tasks\*.json | ForEach-Object {
  $t = Get-Content $_ | ConvertFrom-Json
  if ($t.status -eq 5) { "$($t.issue_number): $($t.error_msg)" }
}

# Show all tasks currently in-progress
Get-ChildItem tasks\*.json | ForEach-Object {
  $t = Get-Content $_ | ConvertFrom-Json
  if ($t.status -eq 2) { "$($t.id): issue #$($t.issue_number) worker=$($t.worker_id)" }
}
```

### 2.6 Day-to-Day Build, Test, and Lint

```powershell
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Test (no -race; CGO is disabled on this machine)
go test -cover ./...

# Single package
go test ./internal/orchestrator

# Named test, verbose
go test -run TestRoute ./internal/orchestrator -v

# Vet
go vet ./...

# Lint (4 pre-existing findings in github_rest_client.go are known; do not fix in unrelated PRs)
golangci-lint run ./...
```

---

## 3. Configuration Reference

### 3.1 Full `orchestrator.yml` Schema

```yaml
# ── Projects ─────────────────────────────────────────────────────────────────
projects:
  - name: kaimi                        # Human-readable identifier (required)
    repo_owner: Mawar2                 # GitHub owner (required)
    repo_name: Kaimi                   # GitHub repo name (required)
    conventions_path: ./CLAUDE.md      # Path to conventions file in the target repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"
    commit_pattern: "{ticket}_{description}"
    labels: []                         # Optional: only process issues with these labels

# ── Worker Tiers ─────────────────────────────────────────────────────────────
worker_tiers:
  gemini_flash:
    max_workers: 5                     # Concurrent workers for this tier
    model: gemini-flash-3.5            # Informational; actual backend wired in main.go
  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5
  claude:
    max_workers: 2
    model: claude-sonnet-4.5

# ── Supervisor Settings ───────────────────────────────────────────────────────
poll_interval_seconds: 60              # How often to poll GitHub for new issues (default: 60)
task_timeout_minutes: 120              # Max minutes a worker may spend on one task (default: 120)
max_retry_attempts: 3                  # Retries before a task is marked failed (default: 3)
task_queue_dir: ./tasks                # Directory for JSON task files (default: ./tasks)
```

**Config validation rules** (enforced by `internal/orchestrator/config.go`):
- At least one project must be defined.
- Each project must have `name`, `repo_owner`, and `repo_name`.
- Defaults are applied automatically; omitting optional keys is safe.

### 3.2 Routing Heuristics

The `RuleBasedRouter` (`internal/orchestrator/router.go`) classifies each issue without LLM calls:

| Signal | Simple | Complex | Default |
|---|---|---|---|
| Title keywords | "fix typo", "add comment", "update readme", "add logging", "format code", "add godoc", "add documentation", "update version" | "architecture", "design", "refactor.*system", "implement.*agent", "new feature.*complex", "database", "migration", "schema change", "security", "authentication", "authorization", "breaking change", "api redesign" | — |
| Title prefix | "docs:", "[docs]" | — | — |
| Estimated file count | ≤ 3 | > 10 | 4–10 |
| Issue labels | contains "simple" or "easy" | contains "complex" or "hard" | — |
| Fallback | — | — | Medium |

**Complexity → Tier mapping:**

| Complexity | Tier |
|---|---|
| `simple` | `gemini-flash` |
| `medium` | `gemini-pro` |
| `complex` | `claude` |

### 3.3 Branch and Commit Pattern Variables

| Variable | Value |
|---|---|
| `{ticket}` | Issue number (e.g., `47`) |
| `{summary}` | Slugified issue title |
| `{description}` | Short description for commit message |

### 3.4 Label Filtering

Setting `labels: ["bug", "enhancement"]` in a project config limits issue discovery to issues that carry at least one of those labels. An empty list (default) processes all open issues.

---

## 4. Troubleshooting

### 4.1 GitHub API Returns 401 Unauthorized

**Symptoms:** Log line `GitHub API returned status 401`

**Steps:**
1. Confirm the token is set: `if ($env:GITHUB_TOKEN) { "set" } else { "NOT set" }`
2. If not set, source the profile: `. $PROFILE`
3. Verify token has `repo` and `read:org` scopes via `gh auth status`
4. If the token has expired, generate a new one at GitHub → Settings → Developer settings → Personal access tokens

### 4.2 GitHub API Returns 403 / Rate-Limited

**Symptoms:** Log line `GitHub API returned status 403` with `rate limit` in the body

**Steps:**
1. The REST client uses the token automatically — a 403 here usually means the token's hourly request budget is exhausted.
2. Check remaining quota: `gh api rate_limit`
3. Reduce `poll_interval_seconds` in `orchestrator.yml` (increase the value to poll less often).
4. If multiple supervisors share a token, stagger their poll intervals.

### 4.3 Tasks Stuck in `in_progress` (Stalled Workers)

**Symptoms:** Tasks remain at `status: 2` (in_progress) beyond `task_timeout_minutes`

**Steps:**
1. Identify stalled tasks:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
     $t = Get-Content $_ | ConvertFrom-Json
     if ($t.status -eq 2) { "$($t.id): started=$($t.started_at) worker=$($t.worker_id)" }
   }
   ```
2. Check whether the supervisor process is still running.
3. If the supervisor was killed mid-task, tasks remain claimed. Manually reset them:
   ```powershell
   # Open the task JSON and set status to 0 (pending), clear worker_id and claimed_at
   $t = Get-Content tasks\<uuid>.json | ConvertFrom-Json
   $t.status = 0; $t.worker_id = ""; $t.claimed_at = $null
   $t | ConvertTo-Json -Depth 10 | Set-Content tasks\<uuid>.json
   ```
4. Restart the supervisor; workers will re-claim the reset tasks.

### 4.4 Quality Gate Failures

**Symptoms:** Log line `[Worker X] Quality gates FAILED`, task ends with `status: 5`

**Diagnosis:**
```powershell
Get-ChildItem tasks\*.json | ForEach-Object {
  $t = Get-Content $_ | ConvertFrom-Json
  if ($t.error_msg -like "*quality gate*") { "$($t.issue_number): $($t.error_msg)" }
}
```

**Common causes and fixes:**

| Gate | Symptom | Fix |
|---|---|---|
| Tests | `go test` failures | The LLM introduced a regression. Increase `max_retry_attempts` or reduce `task_timeout_minutes`. |
| Linter | `golangci-lint` findings | The LLM wrote non-compliant code. Verify `conventions_path` points to the correct CLAUDE.md. |
| Formatter | `git status --porcelain` is non-empty after `gofmt` | The LLM did not run `gofmt`. Confirm format command in conventions. |
| Build | `go build` fails | Compilation error in generated code. Usually indicates a complex issue routed to the wrong tier. |

### 4.5 AI Review Feedback: Fix Tasks Not Created

**Symptoms:** PRs receive AI review comments but no `pr_feedback` tasks appear in `tasks/`

**Steps:**
1. Confirm the AI review comment starts with exactly: `## 🤖 AI Code Review (Gemini 2.5 Pro)`
2. Check that the supervisor's `MonitorPRsForFeedback` goroutine is running (it polls every 120 s).
3. Verify `ReviewCommentID` is not already recorded in the parent task (deduplication):
   ```powershell
   Get-Content tasks\<parent-uuid>.json | ConvertFrom-Json | Select-Object review_comment_id
   ```
4. If the parent task's `review_iteration` is already 3, the max iteration limit has been reached; no further fix tasks will be created.

### 4.6 Clone Failures

**Symptoms:** `fatal: destination path already exists` or `GIT_TERMINAL_PROMPT=0` authentication errors

**Destination already exists:**
```powershell
# Clean the affected worker's workspace
Remove-Item -Recurse -Force projects\gemini-flash-1\Mawar2\Kaimi
```
The per-worker workspace lock (in `internal/worker/workspace.go`) prevents concurrent clones for the same worker, but a previous interrupted run can leave a partial clone. Deleting it allows a fresh clone.

**Authentication errors (prompt disabled):**
- Workers set `GIT_TERMINAL_PROMPT=0` to fail fast instead of hanging.
- Run `gh auth setup-git` to configure the credential helper.
- Verify `gh auth status` shows the `Mawar2` account.

### 4.7 Duplicate Tasks for the Same Issue

**Symptoms:** Multiple task files reference the same `issue_number`

**Root cause:** The supervisor checks for an existing open PR before enqueueing. If the PR was not yet indexed by GitHub's search API when the check ran, a duplicate can appear.

**Fix:**
1. Identify the duplicate tasks:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json } |
     Group-Object issue_number | Where-Object { $_.Count -gt 1 } |
     ForEach-Object { $_.Group | Select-Object id, issue_number, status, pr_number }
   ```
2. Keep the task with the highest `review_iteration` (or the one that created a PR).
3. Mark the others as `status: 5` (failed) with `error_msg: "duplicate — resolved manually"`.

### 4.8 Supervisor Exits Immediately

**Symptoms:** The binary prints the startup banner and exits with no error, or exits with a config parse error.

**Config parse error:**
```
Error loading configuration: yaml: line N: ...
```
Open `orchestrator.yml` and fix the YAML syntax on the indicated line.

**No projects defined:**
```
Error: no projects defined in configuration
```
Ensure at least one entry exists under `projects:` with `name`, `repo_owner`, and `repo_name`.

**Missing binary:**
If the binary is missing, rebuild: `go build -o bin/supervisor.exe ./cmd/supervisor`

### 4.9 Build Fails: `-race requires cgo`

**Symptoms:**
```
# runtime/race
runtime/race: -race requires cgo
```

This is a known limitation: CGO is disabled on this machine (no C compiler). Drop the `-race` flag:
```powershell
# Instead of: go test -race ./...
go test -cover ./...
```
The `Makefile`'s `test` target uses `-race`; do not use `make test` locally.

### 4.10 Pre-Existing Lint Findings in `github_rest_client.go`

`golangci-lint` reports four known findings in `internal/ticket/github_rest_client.go`:
- 3× `errcheck`: unchecked `resp.Body.Close()` error
- 1× `staticcheck QF1003`: simplifiable string comparison

These were present before current work. Do not fix them in unrelated PRs; track them separately and fix in a dedicated cleanup commit.

---

## 5. Monitoring

### 5.1 Key Metrics — PowerShell Queries

**Task throughput (completed today):**
```powershell
$today = (Get-Date).Date.ToString("yyyy-MM-dd")
(Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json } |
  Where-Object { $_.status -eq 4 -and $_.completed_at -like "$today*" }).Count
```

**Overall failure rate:**
```powershell
$tasks = Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json }
$total = $tasks.Count
$failed = ($tasks | Where-Object { $_.status -eq 5 }).Count
if ($total -gt 0) { "Failure rate: $([math]::Round($failed/$total*100,1))%" }
```

**Quality gate failure rate:**
```powershell
$tasks = Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json }
$qgFailed = ($tasks | Where-Object { $_.error_msg -like "*quality gate*" }).Count
"Quality gate failures: $qgFailed / $($tasks.Count)"
```

**Review iteration distribution (feedback loop health):**
```powershell
Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json } |
  Group-Object review_iteration | Sort-Object Name |
  ForEach-Object { "iter=$($_.Name): $($_.Count)" }
# Expected: ~70% at 0, ~20% at 1, ~8% at 2, ~2% at 3
```

**Tasks that hit max iterations (should be < 5%):**
```powershell
(Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json } |
  Where-Object { $_.error_msg -like "*Max review iterations*" }).Count
```

**Backfilled tasks (from `cmd/backfill`):**
```powershell
(Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json } |
  Where-Object { $_.metadata.task_type -eq "pr_feedback" }).Count
```

**Pending task backlog:**
```powershell
(Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json } |
  Where-Object { $_.status -eq 0 }).Count
```

### 5.2 Log Pattern Reference

| Pattern | Severity | Meaning |
|---|---|---|
| `GitHub API returned status 401` | Critical | Token missing or expired |
| `GitHub API returned status 403` | Warning | Rate-limited |
| `Quality gates FAILED` | Warning | PR blocked; task failed |
| `Max review iterations reached` | Info | Feedback loop limit hit |
| `Created fix task for PR #N` | Info | Feedback loop triggered |
| `fatal: destination path already exists` | Warning | Stale workspace; clean `projects/` |
| `GIT_TERMINAL_PROMPT=0` | Error | Git auth failure in worker |
| `context deadline exceeded` | Warning | Task timed out |

### 5.3 Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Failure rate | > 20% | > 40% |
| Quality gate failure rate | > 35% | > 50% |
| Max-iterations rate | > 5% | > 10% |
| Pending backlog | > 50 tasks | > 200 tasks |
| API 401 errors | Any | — |

---

## 6. Maintenance

### 6.1 Task Queue Cleanup

Tasks accumulate in `./tasks/` indefinitely. Clean up periodically to keep disk usage manageable.

**Archive completed tasks older than 30 days:**
```powershell
$cutoff = (Get-Date).AddDays(-30)
$archiveDir = "tasks\archive"
if (-not (Test-Path $archiveDir)) { New-Item -ItemType Directory $archiveDir }

Get-ChildItem tasks\*.json | ForEach-Object {
  $t = Get-Content $_ | ConvertFrom-Json
  if ($t.status -eq 4 -and [DateTime]$t.completed_at -lt $cutoff) {
    Move-Item $_.FullName "$archiveDir\$($_.Name)"
  }
}
```

**Delete failed tasks older than 7 days (after reviewing):**
```powershell
$cutoff = (Get-Date).AddDays(-7)
Get-ChildItem tasks\*.json | ForEach-Object {
  $t = Get-Content $_ | ConvertFrom-Json
  if ($t.status -eq 5 -and [DateTime]$t.completed_at -lt $cutoff) {
    Remove-Item $_.FullName
  }
}
```

### 6.2 Workspace Cleanup

Worker workspaces under `./projects/` consume roughly 200 MB per worker repo (~2 GB for all 10 workers). Clean them after confirming no tasks are in-progress:

```powershell
# Confirm no tasks are in-progress before cleaning
$inProgress = (Get-ChildItem tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json } |
  Where-Object { $_.status -eq 2 }).Count
if ($inProgress -eq 0) {
  Remove-Item -Recurse -Force projects\
  "Workspaces cleaned."
} else {
  "WARNING: $inProgress tasks in-progress. Stop supervisor first."
}
```

Workers will re-clone repositories on next execution.

### 6.3 Updating the Binary

```powershell
# Stop the supervisor (Ctrl+C or kill the process)

# Pull latest changes
git pull origin master

# Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor

# Verify tests pass
go test -cover ./...

# Restart
.\bin\supervisor.exe --config orchestrator.yml
```

### 6.4 Adding a New Project

1. Add a new entry under `projects:` in `orchestrator.yml`:
   ```yaml
   - name: new-project
     repo_owner: Mawar2
     repo_name: NewRepo
     conventions_path: ./CLAUDE.md
     branch_pattern: "feature/NR-{ticket}-{summary}"
     commit_pattern: "{ticket}_{description}"
     labels: []
   ```
2. Ensure `GITHUB_TOKEN` has access to the new repository.
3. Restart the supervisor. It will begin polling `Mawar2/NewRepo` on the next cycle.

To add a new worker tier:
1. Add the tier constant to `internal/taskqueue/task.go`.
2. Add routing logic to `internal/orchestrator/router.go`.
3. Wire up workers in `cmd/supervisor/main.go`.
4. Add the tier entry to `orchestrator.yml`.

### 6.5 Backfill Utility

The `cmd/backfill` utility enqueues open PRs from a repository as `status: review` tasks so the supervisor's feedback loop processes them:

```powershell
# Build
go build -o bin/backfill.exe ./cmd/backfill

# Run (uses GITHUB_TOKEN from environment)
.\bin\backfill.exe
```

Use this when the supervisor was offline while PRs received AI review comments. The backfill script re-queues those PRs so the feedback loop can pick them up.

### 6.6 Dependency Audit

```powershell
# Check for known vulnerabilities
go list -m all
go mod tidy          # remove unused deps
go mod verify        # verify checksums match go.sum

# Current direct dependencies (go.mod):
#   gopkg.in/yaml.v3      — config parsing
#   github.com/google/uuid — task ID generation
```

---

## 7. Security

### 7.1 Token Handling

- **Never log `GITHUB_TOKEN`.** The token is passed via environment variable only; it is not written to config files, task JSON, or log output.
- Store the token in the user's shell profile (`$PROFILE`) or a secrets manager. Do not hard-code it.
- Rotate the token if it is accidentally exposed in logs, commits, or screenshots.
- Use a fine-grained personal access token scoped to exactly the repos the supervisor needs.

### 7.2 Worker Permission Scope

Workers execute `claude --print --dangerously-skip-permissions` to allow the headless agent to edit files and run shell commands in its isolated workspace. This flag bypasses Claude Code's interactive permission prompts.

**Scope limitation:** The flag applies only within the per-worker workspace clone under `./projects/{workerID}/`. It does not grant the worker access to the host filesystem outside that directory.

**Override for dry-runs or restricted environments:**
```powershell
$env:CLAUDE_PERMISSION_MODE = "plan"        # show proposed actions, don't execute
$env:CLAUDE_PERMISSION_MODE = "acceptEdits" # allow edits, prompt for shell commands
```

Remove the override to restore headless operation.

### 7.3 Log Hygiene

- Worker logs may contain issue titles and PR descriptions — treat them as potentially sensitive if the repositories are private.
- Do not ship raw log files to external log aggregators without scrubbing PII or confidential issue content.
- `tasks/*.json` files contain issue titles and AI-generated code. Restrict file system access accordingly.

### 7.4 Dependency Auditing

Run `go mod verify` and review `go.sum` after any dependency update. The project has two direct dependencies (`yaml.v3`, `uuid`); new dependencies added by contributors should be reviewed for licence compatibility and supply-chain risk.

---

*End of runbook. For architecture details see `docs/SYSTEM_DESIGN.md`. For the AI review feedback loop see `AI_REVIEW_FEEDBACK_LOOP.md`.*
