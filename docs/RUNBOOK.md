# Operator Runbook — Multi-Agent Orchestration System

**Version:** 1.0 — 2026-06-06
**Audience:** Operators and on-call engineers running the system in production.

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

| Requirement | Version / Detail | Verification |
|-------------|-----------------|--------------|
| Go | 1.25.1+ | `go version` |
| GitHub CLI (`gh`) | Any recent | `gh --version` |
| `golangci-lint` | Any recent | `golangci-lint --version` |
| Git | Any | `git --version` |
| `GITHUB_TOKEN` | `repo` + `read:org` scopes | `gh auth status` |
| Claude Code CLI | Latest | `claude --version` |
| Windows / PowerShell | Win 10+ / PS 5.1+ | `$PSVersionTable.PSVersion` |

> **Note:** `make` is NOT installed on this machine — use raw `go` commands (see §1.5).

### 1.2 GitHub Token Setup

The token is stored in the PowerShell profile and must be loaded into the
environment before starting the supervisor.

```powershell
# Load from profile (run once per shell session)
$env:GITHUB_TOKEN = (Get-Content $PROFILE | Select-String "ghp_").Matches.Value

# Verify it is present — do NOT echo the value
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — check `$PROFILE" }
```

Required scopes: `repo`, `read:org`.

> **Tip:** In an existing Claude Code session the token is already exported —
> use `$env:GITHUB_TOKEN` directly without re-loading the profile.

### 1.3 GitHub CLI Authentication

Workers clone/push repositories using `gh` as the git credential helper.
Run this once after installing `gh`:

```powershell
gh auth login          # follow prompts; select token method
gh auth setup-git      # wires git credential helper globally
gh auth status         # confirm: "Logged in to github.com as Mawar2"
```

### 1.4 Build

```powershell
# From repository root
go build -o bin/supervisor.exe ./cmd/supervisor   # main binary
go build ./...                                      # verify all packages compile
```

The binary is git-ignored. Rebuild whenever source changes.

### 1.5 First-Run Verification

```powershell
# 1. Confirm prerequisites
go version
gh auth status
if ($env:GITHUB_TOKEN) { "token ok" }

# 2. Copy and customise config
Copy-Item orchestrator.example.yml orchestrator.yml
# Edit orchestrator.yml (see §3)

# 3. Start supervisor
./bin/supervisor.exe --config orchestrator.yml
```

Expected console output on healthy start:

```
Loading configuration from orchestrator.yml...
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

[gemini-flash-1] Worker started (tier: gemini-flash)
...
[claude-2] Worker started (tier: claude)

Supervisor: Starting main loop
Supervisor: Polling project Mawar2/Kaimi
Supervisor: Found N open issues in Mawar2/Kaimi
```

---

## 2. Operation

### 2.1 Normal-State Log Indicators

| Log Pattern | Meaning |
|-------------|---------|
| `Supervisor: Polling project ...` | Healthy poll cycle; appears every 60 s |
| `Supervisor: Found N open issues` | Issues discovered; N may be 0 |
| `Worker started (tier: ...)` | Worker goroutine running |
| `Claimed task ... (issue #N)` | Worker picked up work |
| `QualityGates ✅ ... passed` | Gate passed; one line per gate |
| `Completed task — PR #N created` | PR opened successfully |
| `Worker ... sleeping, no tasks` | Idle worker; normal during quiet periods |

### 2.2 Issue → PR Lifecycle

```
GitHub Issue (open)
        │
        ▼  (supervisor poll, every 60 s)
  Complexity routing
        │
   ┌────┴──────┐
   │ simple    │ → TierGeminiFlash → gemini-flash-{1..5}
   │ medium    │ → TierGeminiPro   → gemini-pro-{1..3}
   │ complex   │ → TierClaude      → claude-{1..2}
   └───────────┘
        │
        ▼
  Task enqueued (status: pending)
        │
        ▼  (worker claims task)
  Clone / pull repo
  Checkout feature branch  feature/issue-{N}
        │
        ▼
  LLM executes implementation
        │
        ▼
  Quality gates
   ├── tests      (must pass)
   ├── linter     (must pass)
   ├── formatter  (must pass)
   └── build      (optional)
        │
   PASS ▼                    FAIL ──► task: failed
  Create / push branch
  Open PR  → status: review
        │
        ▼  (CI runs AI review every 120 s)
  AI review comment detected?
   └── yes → pr_feedback task created (iteration 1..3)
              Worker applies targeted fixes
              PR updated, CI reruns
              Repeat until passes or iteration 3 reached
```

### 2.3 Worker Tiers

| Tier | Workers | Handles | Backend |
|------|---------|---------|---------|
| `gemini-flash` | 5 (`gemini-flash-1..5`) | Simple issues | Claude Code CLI (Gemini bridge) |
| `gemini-pro` | 3 (`gemini-pro-1..3`) | Medium issues | Claude Code CLI (Gemini bridge) |
| `claude` | 2 (`claude-1..2`) | Complex issues | Claude Code CLI (`claude --print --dangerously-skip-permissions`) |

> **Note:** By default all tiers run the Claude backend. The Gemini bridge
> (`USE_GEMINI_WORKER=1`) is experimental and off by default — see CLAUDE.md §Workers.

### 2.4 Task Queue Queries (PowerShell)

```powershell
$tasks = ".\tasks"

# List all tasks with status
Get-ChildItem $tasks -Filter "*.json" |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
  Select-Object id, status, issue_number, worker_id |
  Format-Table

# Count by status
Get-ChildItem $tasks -Filter "*.json" |
  ForEach-Object { (Get-Content $_.FullName | ConvertFrom-Json).status } |
  Group-Object | Select-Object Name, Count

# View a specific task
Get-Content ".\tasks\<uuid>.json" | ConvertFrom-Json | Format-List

# Failed tasks with errors
Get-ChildItem $tasks -Filter "*.json" |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
  Where-Object { $_.status -eq "failed" } |
  Select-Object issue_number, error_msg |
  Format-Table -Wrap

# In-progress (possible stall check)
Get-ChildItem $tasks -Filter "*.json" |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
  Where-Object { $_.status -eq "in_progress" } |
  Select-Object id, worker_id, started_at |
  Format-Table
```

### 2.5 Day-to-Day Commands

```powershell
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Run tests (no -race; CGO not available on this machine)
go test -cover ./...

# Lint
golangci-lint run ./...

# Format
go fmt ./...

# Vet
go vet ./...

# Start supervisor
./bin/supervisor.exe --config orchestrator.yml

# Backfill open PRs into the feedback loop
go build -o bin/backfill.exe ./cmd/backfill
./bin/backfill.exe --config orchestrator.yml
```

---

## 3. Configuration Reference

### 3.1 Full Annotated Schema

```yaml
# orchestrator.yml — copy from orchestrator.example.yml and customise

projects:
  - name: kaimi                        # Short name used in logs
    repo_owner: Mawar2                 # GitHub org or user
    repo_name: Kaimi                   # Repository name (exact case)
    conventions_path: ./CLAUDE.md      # Path inside cloned repo to conventions file
    branch_pattern: "feature/KAI-{ticket}-{summary}"  # Branch naming template
    commit_pattern: "{ticket}_{description}"            # Commit message template
    labels: []                         # Optional: only process issues with these labels

  # Add additional projects following the same structure

worker_tiers:
  gemini_flash:
    max_workers: 5                     # Concurrent Gemini Flash workers (default: 5)
    model: gemini-flash-3.5            # Informational; routing is complexity-based

  gemini_pro:
    max_workers: 3                     # Concurrent Gemini Pro workers (default: 3)
    model: gemini-pro-3.5

  claude:
    max_workers: 2                     # Concurrent Claude workers (default: 2)
    model: claude-sonnet-4.5

poll_interval_seconds: 60              # GitHub poll frequency; minimum recommended: 30
task_timeout_minutes: 120              # Worker timeout before task is released back to queue
max_retry_attempts: 3                  # Retries before status → failed
task_queue_dir: ./tasks                # Directory for JSON task files (auto-created)
```

### 3.2 Branch / Commit Pattern Variables

| Variable | Value |
|----------|-------|
| `{ticket}` | Issue number (e.g., `47`) |
| `{summary}` | Slug derived from issue title |
| `{description}` | Short description for commit message |

### 3.3 Routing Heuristics

The `RuleBasedRouter` classifies each issue into **Simple / Medium / Complex** using
the following ordered rules (first match wins):

| Priority | Signal | Complexity |
|----------|--------|------------|
| 1 | Title/body contains: `add comment`, `add godoc`, `add documentation`, `fix typo`, `update readme`, `format code`, `add logging`, `update version`, `docs:`, `[docs]`, `documentation` | Simple |
| 2 | Title/body matches regex: `architecture`, `design`, `refactor.*system`, `implement.*agent`, `new feature.*complex`, `database`, `migration`, `schema change`, `security`, `authentication`, `authorization`, `breaking change`, `api redesign` | Complex |
| 3 | Body contains `files:` or `affected files` keyword and estimated file count ≤ 3 | Simple |
| 4 | Same file-count heuristic, count > 10 | Complex |
| 5 | Issue label contains `simple` or `easy` | Simple |
| 6 | Issue label contains `complex` or `hard` | Complex |
| 7 | No signal matched | Medium |

Complexity maps directly to tier: Simple → `gemini-flash`, Medium → `gemini-pro`,
Complex → `claude`.

### 3.4 Label Filtering

Set `labels` in a project to only pick up issues carrying all listed labels:

```yaml
projects:
  - name: kaimi
    repo_owner: Mawar2
    repo_name: Kaimi
    labels: ["orchestrator:pending"]   # Only process issues with this label
```

Leave `labels: []` (empty) to process all open issues.

---

## 4. Troubleshooting

### 4.1 GitHub 401 — Unauthorized

**Symptom:** `GitHub API returned status 401` in supervisor logs.

**Steps:**
1. Confirm token is set: `if ($env:GITHUB_TOKEN) { "ok" } else { "missing" }`
2. If missing, reload: `$env:GITHUB_TOKEN = (Get-Content $PROFILE | Select-String "ghp_").Matches.Value`
3. Verify scopes: `gh auth status` — must show `repo`, `read:org`
4. If token is expired, generate a new one and update `$PROFILE`

### 4.2 GitHub Rate Limit (403 / 429)

**Symptom:** `rate limit exceeded` in logs; supervisor slows or stops discovering issues.

**Steps:**
1. Check current limit: `gh api rate_limit`
2. Primary REST API limit: 5,000 requests/hour for authenticated requests
3. Increase `poll_interval_seconds` to reduce calls (e.g., 120 instead of 60)
4. Wait for limit reset (shown in `gh api rate_limit` output under `reset`)

### 4.3 Stalled Tasks

**Symptom:** Tasks stuck in `in_progress` for more than `task_timeout_minutes` (default: 120 min).

**Steps:**
```powershell
# Identify stalled tasks
Get-ChildItem .\tasks -Filter "*.json" |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
  Where-Object { $_.status -eq "in_progress" } |
  Select-Object id, worker_id, started_at

# Manually release: set status back to pending, clear worker_id
# (supervisor auto-releases stalled tasks via IsStalled() check on next poll)
```

The supervisor calls `IsStalled()` each poll cycle. Tasks stalled beyond
`task_timeout_minutes` are automatically released back to `pending` for retry.
If a task keeps stalling, check worker logs at the path stored in `logs_path`.

### 4.4 Quality Gate Failures

**Symptom:** Tasks fail with `error_msg` containing `quality gates`.

**Steps:**
```powershell
# Find quality gate failures
Get-ChildItem .\tasks -Filter "*.json" |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
  Where-Object { $_.error_msg -like "*quality gates*" } |
  Select-Object issue_number, error_msg |
  Format-Table -Wrap
```

Common causes:
- **Tests fail:** The LLM introduced a regression. Review the PR diff manually.
- **Linter fails:** Code style issues; check `golangci-lint` output in worker logs.
- **Formatter fails:** Run `go fmt ./...` inside the workspace; usually auto-fixed on retry.
- **Build fails:** Compilation error; the issue may be too complex for the assigned tier —
  re-label the GitHub issue with `complex` or `hard` so it routes to `claude`.

### 4.5 Missing Fix Tasks (Feedback Loop Not Triggering)

**Symptom:** PR has an AI review comment but no `pr_feedback` task is created.

**Steps:**
1. Confirm the comment prefix is exactly: `## 🤖 AI Code Review (Gemini 2.5 Pro)`
2. Check `ReviewCommentID` deduplication — if a task with the same comment ID exists,
   a duplicate is intentionally skipped.
3. Verify `max_retry_attempts` (default: 3) — tasks at iteration 3 will not generate
   a 4th fix task.
4. Check supervisor logs for `Skipping pr_feedback — max iterations reached`.

### 4.6 Clone Failures

**Symptom:** `fatal: destination path already exists` or `authentication failed`.

**Steps:**
- **Destination exists:** The worker workspace is leftover from a previous run.
  Run `Remove-Item -Recurse -Force .\projects\` and restart the supervisor.
- **Auth failure:** `gh auth setup-git` was not run. Execute it, then restart.
- **Network timeout:** Set `GIT_TERMINAL_PROMPT=0` is already done by workers;
  if intermittent, the supervisor will retry on the next poll.

### 4.7 Duplicate Tasks

**Symptom:** The same GitHub issue generates multiple tasks.

**Steps:**
The supervisor checks for existing PRs before enqueuing. If duplicates appear:
1. Search for tasks with the same `issue_number`: `Where-Object { $_.issue_number -eq N }`
2. Manually mark duplicates as `failed` by editing the JSON and setting `"status": 5`
3. Confirm the branch `feature/issue-{N}` already exists in the remote; the supervisor
   skips enqueueing when a matching open PR is found.

### 4.8 Supervisor Exits Immediately

**Symptom:** Binary starts and exits within seconds, no workers started.

**Steps:**
1. Verify config file path: `./bin/supervisor.exe --config orchestrator.yml`
2. Validate YAML syntax: `go run ./cmd/supervisor --config orchestrator.yml` — error
   messages include line numbers
3. Check required fields: at least one project with `name`, `repo_owner`, `repo_name`
4. Confirm `GITHUB_TOKEN` is set

### 4.9 CGO / Race Detector Build Failure

**Symptom:** `go test -race ./...` fails with `-race requires cgo`.

**Solution:** CGO is disabled on this machine (no C compiler). Drop `-race`:

```powershell
go test -cover ./...    # correct; -race is not available here
```

### 4.10 Pre-Existing Lint Findings

**Symptom:** `golangci-lint run ./...` reports 4 findings in
`internal/ticket/github_rest_client.go` (3× unchecked `resp.Body.Close`, 1×
`staticcheck QF1003`).

These are **pre-existing** and were not introduced by recent work. Fix
opportunistically when touching that file; do not block CI on them.

---

## 5. Monitoring

### 5.1 Key Metrics (PowerShell)

```powershell
$tasks = Get-ChildItem .\tasks -Filter "*.json" |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json }

# Throughput: tasks completed in last 24 hours
$cutoff = (Get-Date).AddHours(-24)
$tasks | Where-Object {
  $_.status -eq "complete" -and [datetime]$_.completed_at -gt $cutoff
} | Measure-Object | Select-Object -ExpandProperty Count

# Failure rate (all time)
$total   = ($tasks | Measure-Object).Count
$failed  = ($tasks | Where-Object { $_.status -eq "failed" } | Measure-Object).Count
if ($total -gt 0) { "{0:P1}" -f ($failed / $total) }

# Quality gate failure rate
$qgFailed = ($tasks | Where-Object { $_.error_msg -like "*quality gates*" } | Measure-Object).Count
if ($total -gt 0) { "{0:P1} QG failure rate" -f ($qgFailed / $total) }

# Feedback loop: fix task count
$tasks | Where-Object { $_.metadata.task_type -eq "pr_feedback" } | Measure-Object | Select-Object Count

# Review iteration distribution (0 = original, 1-3 = fixes)
$tasks | Group-Object review_iteration | Select-Object Name, Count | Sort-Object Name

# Max iterations hit (feedback loop stalled)
$tasks | Where-Object { $_.error_msg -like "*Max review iterations*" } | Measure-Object | Select-Object Count

# Backfilled task count
$tasks | Where-Object { $_.metadata.source -eq "backfill" } | Measure-Object | Select-Object Count
```

### 5.2 Log Patterns to Watch

| Pattern | Severity | Action |
|---------|----------|--------|
| `GitHub API returned status 401` | Critical | Refresh token immediately |
| `rate limit exceeded` | Warning | Increase `poll_interval_seconds` |
| `quality gates failed` | Info | Review PR diff; may need tier bump |
| `Max review iterations reached` | Warning | Manual review required for that PR |
| `fatal: authentication failed` | Critical | Run `gh auth setup-git` |
| `destination path already exists` | Warning | Clean `.\projects\` and restart |
| `Worker ... sleeping, no tasks` | Info | Normal during quiet periods |
| `Supervisor: Found 0 open issues` | Info | No work; normal if repo is clear |

### 5.3 Alerting Thresholds

| Metric | Warning | Critical |
|--------|---------|----------|
| Failure rate (all-time) | > 20% | > 40% |
| Quality gate failure rate | > 35% | > 55% |
| Tasks stuck in `in_progress` > 3 hours | Any | > 3 |
| Max-iterations hits | > 5% of tasks | > 10% |
| Supervisor last poll > 5 minutes ago | — | Alert |

---

## 6. Maintenance

### 6.1 Task Queue Archive and Cleanup

Completed and failed tasks accumulate in `.\tasks\`. Archive periodically:

```powershell
# Archive terminal tasks older than 7 days
$cutoff = (Get-Date).AddDays(-7)
New-Item -ItemType Directory -Force .\tasks\archive | Out-Null

Get-ChildItem .\tasks -Filter "*.json" |
  ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if (($t.status -in @("complete","failed")) -and
        [datetime]$t.completed_at -lt $cutoff) {
      Move-Item $_.FullName .\tasks\archive\
    }
  }

# Count remaining active tasks
(Get-ChildItem .\tasks -Filter "*.json" | Measure-Object).Count
```

### 6.2 Workspace Cleanup

Each worker maintains a clone under `.\projects\{workerID}\{owner}\{repo}\`.
These are safe to delete while the supervisor is stopped:

```powershell
# Stop supervisor (Ctrl+C), then:
Remove-Item -Recurse -Force .\projects\
```

Workers will re-clone on next startup. Each workspace is ~200 MB;
10 workers ≈ 2 GB total.

### 6.3 Binary Update Procedure

```powershell
# 1. Pull latest source
git pull origin master

# 2. Run tests
go test -cover ./...

# 3. Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor

# 4. Stop old supervisor (Ctrl+C or kill process)
# 5. Restart
./bin/supervisor.exe --config orchestrator.yml
```

### 6.4 Adding a New Project

1. Add a stanza to `orchestrator.yml` under `projects:`:

```yaml
projects:
  - name: new-project
    repo_owner: YourOrg
    repo_name: YourRepo
    conventions_path: ./CLAUDE.md
    branch_pattern: "feature/{ticket}-{summary}"
    commit_pattern: "{ticket} {description}"
    labels: []
```

2. Confirm `GITHUB_TOKEN` has `repo` access to the new repository.
3. Restart the supervisor — no code changes required.

### 6.5 Backfill Utility

Use `cmd/backfill` to inject existing open PRs into the feedback loop so the
supervisor monitors them for AI review comments:

```powershell
go build -o bin/backfill.exe ./cmd/backfill
./bin/backfill.exe --config orchestrator.yml
```

This fetches open PRs from configured projects, creates `StatusReview` tasks,
and the supervisor's feedback-loop poll picks them up within 120 seconds.

Run backfill once after deploying the feedback loop to existing repositories.

### 6.6 Dependency Audit

```powershell
# List direct dependencies
go list -m all

# Check for known vulnerabilities (requires govulncheck)
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...

# Update a dependency
go get github.com/some/module@latest
go mod tidy
go test -cover ./...    # confirm nothing broke
```

---

## 7. Security

### 7.1 Token Handling

- Store `GITHUB_TOKEN` in `$PROFILE` only; never commit it to source control.
- Do NOT echo or log the token value; use `if ($env:GITHUB_TOKEN) { "set" }` for checks.
- The token must have only the minimum required scopes: `repo`, `read:org`.
- Rotate the token if it is ever exposed in logs, terminals, or screenshots.
- `orchestrator.yml` may reference the token indirectly via environment; keep the file
  out of public repositories (it is already git-ignored by convention).

### 7.2 Worker Permission Scope

Workers run:
```
claude --print --dangerously-skip-permissions
```

This allows the headless Claude agent to edit files and run `git`/`gh`/tests inside
its isolated workspace clone **without interactive prompts**. Scope is limited to the
per-worker clone directory (`.\projects\{workerID}\{owner}\{repo}\`); the agent cannot
access files outside that directory.

To require interactive approval of worker actions (useful during debugging):

```powershell
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"   # prompts for file edits
# or
$env:CLAUDE_PERMISSION_MODE = "plan"          # dry-run only
```

Reset to default (no prompts) by unsetting the variable:
```powershell
Remove-Item Env:\CLAUDE_PERMISSION_MODE
```

### 7.3 Log Hygiene

- Supervisor logs are written to stdout only; redirect to a file if persistence is needed.
- Worker logs are stored at the path in `task.logs_path`; rotate periodically.
- Do not include full issue body content in alert messages — issue bodies may contain
  sensitive business context.
- Task JSON files in `.\tasks\` may contain issue descriptions; treat the directory
  as internal-only and do not expose it via web servers or shared drives.

### 7.4 Dependency Auditing

Run `govulncheck` before deploying a new binary to production (see §6.6).
The only runtime dependencies are `gopkg.in/yaml.v3` and `github.com/google/uuid`;
keep them up to date with `go get` + `go mod tidy`.
