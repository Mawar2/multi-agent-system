# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06

This runbook covers the full operational lifecycle of the multi-agent orchestration system: from first-time setup through day-to-day operation, troubleshooting, monitoring, and maintenance.

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

| Requirement | Minimum | Notes |
|-------------|---------|-------|
| Go | 1.25.1 | `go version` to check |
| GitHub CLI | any | `gh version`; must be `gh auth login`'d |
| golangci-lint | any | `golangci-lint --version`; optional, for local linting |
| `GITHUB_TOKEN` env var | — | Scopes: `repo`, `read:org` |
| Disk space | ~2 GB | 10 worker workspaces × ~200 MB each |
| OS | Windows / Linux / macOS | PowerShell or Bash |

### 1.2 GitHub Token Setup

The supervisor reads `GITHUB_TOKEN` from the environment via `os.Getenv("GITHUB_TOKEN")`.

**Windows (PowerShell):**
```powershell
# Verify it is already set in this session
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — set it below" }

# If not set, load from PowerShell profile (token is persisted there)
. $PROFILE
# Or set directly:
$env:GITHUB_TOKEN = "ghp_YOUR_TOKEN_HERE"
```

**Linux / macOS (Bash):**
```bash
export GITHUB_TOKEN="ghp_YOUR_TOKEN_HERE"
# Or add to ~/.bashrc / ~/.zshrc for persistence
```

Required token scopes:
- `repo` — read issues, create branches, open PRs
- `read:org` — read org membership for private repos

### 1.3 GitHub CLI Authentication

Workers create PRs via `gh pr create`. Ensure `gh` is authenticated and that git credentials are configured:

```bash
gh auth login           # follow prompts; choose HTTPS and paste token
gh auth setup-git       # configures git credential helper for HTTPS clones
gh auth status          # verify: shows logged-in account
```

### 1.4 Build the Supervisor

```bash
# From the repository root
go build -o bin/supervisor.exe ./cmd/supervisor   # Windows
go build -o bin/supervisor     ./cmd/supervisor   # Linux/macOS

# Verify the binary exists
ls bin/
```

> **Note:** `make` is not required. The `Makefile` targets map to raw `go` commands; use those directly if `make` is unavailable.

### 1.5 First-Run Verification

```bash
# 1. Copy example config (if orchestrator.yml doesn't exist)
cp orchestrator.example.yml orchestrator.yml

# 2. Edit orchestrator.yml — set repo_owner/repo_name at minimum
# (See Section 3 for full schema)

# 3. Start the supervisor
./bin/supervisor.exe --config orchestrator.yml    # Windows
./bin/supervisor     --config orchestrator.yml    # Linux/macOS
```

Expected startup output:
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
```

If you see this output, the system is running. Watch for the first poll cycle (within `poll_interval_seconds` seconds) to confirm GitHub connectivity.

---

## 2. Operation

### 2.1 Normal State Log Indicators

| Log Pattern | Meaning | Action |
|-------------|---------|--------|
| `Supervisor: Polling project ...` | Poll cycle started | Normal |
| `Supervisor: Found N open issues` | Issues discovered | Normal |
| `Supervisor: Routed issue #N — complexity: X, tier: Y` | Issue classified | Normal |
| `Supervisor: Enqueued task <uuid> for issue #N` | Task created | Normal |
| `[workerID] Claimed task <uuid>` | Worker picked up work | Normal |
| `[QualityGates] ✅ Tests passed` | Quality gate passed | Normal |
| `[Worker X] Completed task — PR #N created` | PR opened | Normal |
| `[Worker X] Quality gates FAILED` | Bad code prevented | Normal (cost-saving) |
| `Supervisor: Issue #N already has open PR #M` | Dedup check | Normal |
| `Supervisor: Monitoring PR #N for feedback` | Feedback loop active | Normal |

### 2.2 Issue → PR Lifecycle

```
GitHub Issue (open)
       │
       ▼
Supervisor polls (every poll_interval_seconds)
       │
       ▼
Router classifies complexity → assigns tier
       │
       ▼
Task enqueued as JSON in ./tasks/{uuid}.json  (status: pending)
       │
       ▼
Worker claims task                             (status: claimed)
       │
       ▼
Worker prepares workspace                      (status: in_progress)
  └── Clone repo to ./projects/{workerID}/{owner}/{repo}/
  └── Checkout new branch feature/issue-{N}
       │
       ▼
LLM executes solution (Claude Code CLI)
       │
       ▼
Quality gates run
  ├── PASS → commit + push + gh pr create      (status: review)
  └── FAIL → task marked failed                (status: failed)
       │
       ▼  [if pass]
AI review runs on PR (CI/CD)
       │
  ┌────┴────────────────────────────────┐
  │ review passes                       │ review has feedback
  ▼                                     ▼
Human review                   Supervisor creates pr_feedback task
(status: complete when merged)         │
                                       ▼
                              Worker applies targeted fixes
                                       │
                                       ▼
                              Updated PR, CI re-runs (up to 3 iterations)
```

### 2.3 Worker Tiers

| Tier | Workers | Model | Default Use |
|------|---------|-------|-------------|
| `gemini-flash` | 5 (gemini-flash-1 … 5) | gemini-flash-3.5 | Simple issues (docs, typos, small config) |
| `gemini-pro` | 3 (gemini-pro-1 … 3) | gemini-pro-3.5 | Medium issues (features, refactors) |
| `claude` | 2 (claude-1, claude-2) | claude-sonnet-4.5 | Complex issues (architecture, security, migrations) |

> **Implementation note:** As of 2026-06-06, all tiers run the `ClaudeCodeWorker` backed by the `claude` CLI. The Gemini worker (`GeminiWorker`) is gated behind `USE_GEMINI_WORKER=1` and is not production-ready. Setting this flag is NOT recommended.

### 2.4 Task Queue — Day-to-Day Commands

All tasks are JSON files in `./tasks/`. Use PowerShell's `Get-Content` + `ConvertFrom-Json`, or `jq` if installed.

**PowerShell:**
```powershell
# List all tasks (summary)
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    [PSCustomObject]@{ ID=$t.id.Substring(0,8); Issue=$t.issue_number; Status=$t.status; Worker=$t.worker_id }
} | Format-Table

# Count by status
Get-ChildItem tasks\*.json | ForEach-Object { (Get-Content $_ | ConvertFrom-Json).status } | Group-Object | Sort-Object Count -Descending

# View a specific task
Get-Content tasks\<uuid>.json | ConvertFrom-Json | Format-List

# Failed tasks with error messages
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -eq 5) { "$($t.issue_number): $($t.error_msg)" }
}
```

**jq (Bash/WSL):**
```bash
# All tasks summarised
jq -r '[.id[0:8], (.issue_number|tostring), (.status|tostring), .worker_id] | join("\t")' tasks/*.json

# Failed tasks
jq -r 'select(.status == 5) | "\(.issue_number): \(.error_msg)"' tasks/*.json

# Quality gate failures
jq -r 'select(.error_msg | contains("quality gates")) | "\(.issue_number): \(.error_msg)"' tasks/*.json

# PR feedback tasks only
jq -r 'select(.metadata.task_type == "pr_feedback") | "\(.id[0:8]) iter=\(.review_iteration)"' tasks/*.json
```

Status integer values: `0`=pending, `1`=claimed, `2`=in_progress, `3`=review, `4`=complete, `5`=failed.

### 2.5 Build, Test, Lint Commands

```bash
# Build
go build ./...                              # build all packages (fast check)
go build -o bin/supervisor.exe ./cmd/supervisor  # build supervisor binary

# Test (omit -race; CGO is off on this machine)
go test -cover ./...                        # all packages with coverage
go test ./internal/orchestrator             # single package
go test -run TestRoute ./internal/orchestrator -v  # single test by name

# Vet
go vet ./...

# Lint (4 pre-existing findings in github_rest_client.go — not introduced by new work)
golangci-lint run ./...
```

---

## 3. Configuration Reference

### 3.1 Full `orchestrator.yml` Schema

```yaml
# orchestrator.yml — copy from orchestrator.example.yml

projects:
  - name: kaimi                        # logical name (used in logs)
    repo_owner: Mawar2                 # GitHub org or user
    repo_name: Kaimi                   # GitHub repository name
    conventions_path: ./CLAUDE.md      # path to conventions file in target repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"
    commit_pattern: "{ticket}_{description}"
    labels: []                         # optional: only process issues with these labels
                                       # e.g. ["orchestrator:pending", "good first issue"]

  # Add more projects:
  # - name: other-project
  #   repo_owner: YourOrg
  #   repo_name: YourRepo
  #   conventions_path: ./PROJECT_RULES.md
  #   branch_pattern: "{ticket}-{summary}"
  #   commit_pattern: "[{ticket}] {description}"

worker_tiers:
  gemini_flash:
    max_workers: 5                     # max concurrent gemini-flash workers
    model: gemini-flash-3.5            # informational; workers use claude CLI today

  gemini_pro:
    max_workers: 3                     # max concurrent gemini-pro workers
    model: gemini-pro-3.5

  claude:
    max_workers: 2                     # max concurrent claude workers
    model: claude-sonnet-4.5

poll_interval_seconds: 60              # how often to poll GitHub for new issues
task_timeout_minutes: 120              # max time a worker spends on a task (then stalled)
max_retry_attempts: 3                  # retries before marking a task as failed
task_queue_dir: ./tasks                # directory for task JSON files
```

### 3.2 Branch and Commit Pattern Variables

| Variable | Value |
|----------|-------|
| `{ticket}` | GitHub issue number (e.g., `47`) |
| `{summary}` | Slugified issue title (e.g., `add-readme-comment`) |
| `{description}` | Short description derived from issue title |

### 3.3 Label Filtering

When `labels` is non-empty, the supervisor only processes issues that carry **at least one** of the listed labels. Issues without a matching label are skipped silently.

```yaml
labels:
  - "orchestrator:pending"   # custom label you apply to issues you want automated
```

To process all open issues, leave `labels: []` (or omit the key entirely).

### 3.4 Routing Heuristics

The `RuleBasedRouter` classifies issues deterministically — no API calls.

**Simple** (→ gemini-flash): title or body contains any of:
- `add comment`, `add godoc`, `add documentation`
- `fix typo`, `update readme`, `format code`
- `add logging`, `update version`
- `docs:`, `[docs]`, `documentation`

**Complex** (→ claude): title or body matches any of (regex):
- `architecture`, `design`, `refactor.*system`
- `implement.*agent`, `new feature.*complex`
- `database`, `migration`, `schema change`
- `security`, `authentication`, `authorization`
- `breaking change`, `api redesign`

**File-count heuristic** (when body mentions "files:" or "affected files"):
- ≤3 files → simple
- >10 files → complex

**Label heuristic**:
- Labels containing `simple` or `easy` → simple
- Labels containing `complex` or `hard` → complex

**Default:** medium (→ gemini-pro) when no signal matches.

---

## 4. Troubleshooting

### 4.1 GitHub API Returns 401

**Symptom:** `GitHub API returned status 401` in supervisor logs.

**Cause:** `GITHUB_TOKEN` is not set or has expired.

**Fix:**
```powershell
# Check if set
if ($env:GITHUB_TOKEN) { "SET" } else { "MISSING" }

# Reload from profile
. $PROFILE

# Verify required scopes
gh auth status
```

If the token has expired, generate a new one at GitHub → Settings → Developer settings → Personal access tokens with scopes `repo` and `read:org`, then update `$PROFILE` and re-export.

### 4.2 GitHub API Rate Limit Hit

**Symptom:** `GitHub API returned status 403` or `rate limit exceeded` in logs.

**Cause:** Authenticated limit is 5,000 requests/hour. Heavy polling or many projects can exceed this.

**Fix:**
```powershell
# Check current limit
$headers = @{ Authorization = "Bearer $env:GITHUB_TOKEN" }
(Invoke-RestMethod -Uri "https://api.github.com/rate_limit" -Headers $headers).rate
```

Mitigations:
- Increase `poll_interval_seconds` (e.g., 120 or 300)
- Reduce number of monitored projects
- Use label filtering to reduce issues scanned per poll

### 4.3 Tasks Stuck in `claimed` or `in_progress`

**Symptom:** Task JSON shows `status: 1` (claimed) or `status: 2` (in_progress) for longer than `task_timeout_minutes`.

**Cause:** Worker process crashed, network error, or hung subprocess.

**Fix:**
```powershell
# Identify stalled tasks (claimed_at > 2 hours ago)
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -in 1,2) {
        $age = (Get-Date) - [datetime]$t.claimed_at
        if ($age.TotalMinutes -gt 120) { "$($t.id): stuck for $([int]$age.TotalMinutes) min" }
    }
}
```

To reset a stalled task back to pending, edit the JSON directly:
```powershell
$path = "tasks\<uuid>.json"
$t = Get-Content $path | ConvertFrom-Json
$t.status = 0          # 0 = pending
$t.worker_id = ""
$t | ConvertTo-Json -Depth 10 | Set-Content $path -Encoding utf8
```

The supervisor will re-enqueue it on the next poll.

### 4.4 Quality Gate Failures

**Symptom:** `[Worker X] Quality gates FAILED` / task status = `failed` with error containing "quality gates".

**Cause:** The LLM-generated code failed tests, linting, or formatting in the target repo.

**Diagnosis:**
```powershell
# Show all quality gate failures
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.error_msg -like "*quality gates*") { "$($t.issue_number): $($t.error_msg)" }
}
```

**Fix options:**
1. Investigate the issue description — add more context or acceptance criteria so the LLM has better guidance.
2. Check `./projects/{workerID}/{owner}/{repo}/` for the failed workspace and inspect the code manually.
3. If the target repo's tests are inherently broken, fix the target repo first.
4. Mark the task failed and re-open the GitHub issue with additional context:
   ```bash
   gh issue comment <N> --repo Mawar2/Kaimi --body "Quality gates failed. Needs clearer acceptance criteria."
   ```

### 4.5 AI Review Feedback Tasks Not Being Created

**Symptom:** PRs are open with AI review comments but no `pr_feedback` tasks appear in `./tasks/`.

**Cause:** Supervisor's feedback detection looks for comments prefixed with `## 🤖 AI Code Review (Gemini 2.5 Pro)`. If your CI posts reviews with a different prefix, detection will miss them.

**Diagnosis:**
```powershell
# Count pr_feedback tasks
(Get-ChildItem tasks\*.json | Where-Object {
    (Get-Content $_ | ConvertFrom-Json).metadata.task_type -eq "pr_feedback"
}).Count
```

**Fix:** Check `internal/orchestrator/supervisor.go` for the review comment prefix string and ensure it matches your CI output. Update the prefix constant if needed.

### 4.6 Clone Failures (`destination path already exists`)

**Symptom:** `fatal: destination path already exists and is not an empty directory`.

**Cause:** A previous run left a partial clone in the worker workspace.

**Fix:**
```powershell
# Remove all worker workspaces and let workers re-clone
Remove-Item -Recurse -Force projects\
```

Workers will re-clone on next task claim. This is safe — workspaces are ephemeral.

### 4.7 Duplicate Tasks for Same Issue

**Symptom:** Multiple task JSON files with the same `issue_number`.

**Cause:** The supervisor's dedup check (`searchPullRequests`) did not find an existing open PR. This can happen if the PR was just closed or if the search window is too narrow.

**Diagnosis:**
```powershell
# Find duplicates
Get-ChildItem tasks\*.json | ForEach-Object { (Get-Content $_ | ConvertFrom-Json).issue_number } |
    Group-Object | Where-Object { $_.Count -gt 1 }
```

**Fix:** Manually mark duplicate tasks as failed:
```powershell
$path = "tasks\<uuid>.json"
$t = Get-Content $path | ConvertFrom-Json
$t.status = 5                          # 5 = failed
$t.error_msg = "duplicate — removed manually"
$t | ConvertTo-Json -Depth 10 | Set-Content $path -Encoding utf8
```

### 4.8 Supervisor Exits Immediately

**Symptom:** Supervisor binary starts, prints a few lines, then exits with a non-zero code.

**Cause:** Config validation error (missing `repo_owner`, `repo_name`, etc.) or YAML parse error.

**Fix:**
```bash
./bin/supervisor.exe --config orchestrator.yml 2>&1
# Read the error message; common causes:
# - "no projects configured" → add at least one entry under `projects:`
# - YAML parse error         → check indentation (use spaces, not tabs)
# - "failed to read config"  → wrong --config path
```

### 4.9 CGO / Race Detector Build Failures

**Symptom:** `go test -race ./...` fails with `-race requires cgo`.

**Cause:** CGO is disabled (`CGO_ENABLED=0`) and no C compiler is installed, which is typical on this Windows machine.

**Fix:** Drop `-race`:
```bash
go test -cover ./...    # works without CGO
```

Do not add `-race` to CI scripts on this machine unless a C compiler is installed.

### 4.10 Pre-Existing Lint Findings

**Symptom:** `golangci-lint run ./...` reports findings even on a clean checkout.

**Cause:** Four pre-existing findings in `internal/ticket/github_rest_client.go`:
- 3× `errcheck` — unchecked `resp.Body.Close()`
- 1× `staticcheck QF1003`

These are pre-existing and **not introduced by recent work**. Fix opportunistically when editing that file; do not fail CI over them.

---

## 5. Monitoring

### 5.1 Key Metrics — PowerShell Queries

```powershell
# Total task count
(Get-ChildItem tasks\*.json).Count

# Throughput: tasks completed today
$today = (Get-Date).Date
(Get-ChildItem tasks\*.json | Where-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    $t.status -eq 4 -and [datetime]$t.completed_at -ge $today
}).Count

# Failure rate (%)
$all = (Get-ChildItem tasks\*.json).Count
$failed = (Get-ChildItem tasks\*.json | Where-Object {
    (Get-Content $_ | ConvertFrom-Json).status -eq 5
}).Count
if ($all -gt 0) { [math]::Round(($failed / $all) * 100, 1) }

# Quality gate failure rate
$qgFailed = (Get-ChildItem tasks\*.json | Where-Object {
    (Get-Content $_ | ConvertFrom-Json).error_msg -like "*quality gates*"
}).Count
"Quality gate failures: $qgFailed / $all"

# Review iteration distribution (expect ~70% at 0, ~20% at 1, ~8% at 2, ~2% at 3)
Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_ | ConvertFrom-Json).review_iteration
} | Group-Object | Sort-Object Name | Format-Table Name, Count

# Tasks that hit max review iterations (should be <5%)
(Get-ChildItem tasks\*.json | Where-Object {
    (Get-Content $_ | ConvertFrom-Json).error_msg -like "*Max review iterations*"
}).Count

# Backfilled tasks (created via backfill utility)
(Get-ChildItem tasks\*.json | Where-Object {
    (Get-Content $_ | ConvertFrom-Json).metadata.source -eq "backfill"
}).Count
```

### 5.2 Log Patterns Table

| Pattern | Severity | Meaning |
|---------|----------|---------|
| `GitHub API returned status 401` | ERROR | Token invalid/expired |
| `GitHub API returned status 403` | WARN | Rate limit or permission issue |
| `GitHub API returned status 404` | WARN | Repo or resource not found |
| `quality gates FAILED` | INFO | Cost-saving filter worked |
| `Max review iterations reached` | WARN | PR stuck after 3 AI review cycles |
| `Worker X failed to claim task` | DEBUG | Normal contention, another worker got it |
| `Workspace clone failed` | ERROR | Git/network issue |
| `Task timeout exceeded` | WARN | Worker took > task_timeout_minutes |

### 5.3 Alerting Thresholds

| Metric | Warning | Critical |
|--------|---------|----------|
| Failure rate | > 20% | > 40% |
| Quality gate failure rate | > 40% | > 60% |
| Max-iteration tasks | > 5% of PRs | > 15% |
| Stalled tasks (> 2 hrs) | any | > 3 simultaneous |
| Poll errors (401/403) | any | sustained > 10 min |

---

## 6. Maintenance

### 6.1 Task Queue Cleanup

Tasks accumulate in `./tasks/`. Periodically archive or delete old terminal-state tasks.

**Archive completed tasks older than 30 days (PowerShell):**
```powershell
$cutoff = (Get-Date).AddDays(-30)
$archiveDir = "tasks\archive"
New-Item -ItemType Directory -Force $archiveDir | Out-Null

Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    $isTerminal = $t.status -in 4, 5  # complete or failed
    if ($isTerminal -and $t.completed_at -and [datetime]$t.completed_at -lt $cutoff) {
        Move-Item $_.FullName (Join-Path $archiveDir $_.Name)
    }
}
```

**Delete failed tasks older than 7 days:**
```powershell
$cutoff = (Get-Date).AddDays(-7)
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -eq 5 -and $t.completed_at -and [datetime]$t.completed_at -lt $cutoff) {
        Remove-Item $_.FullName
    }
}
```

### 6.2 Workspace Cleanup

Worker workspaces are created under `./projects/`. They are NOT automatically cleaned up after task completion. Clean periodically to reclaim disk space:

```powershell
# Remove all workspaces (workers will re-clone on next task)
Remove-Item -Recurse -Force projects\

# Or remove only a specific worker's workspace
Remove-Item -Recurse -Force "projects\gemini-flash-1\"
```

> Safe to do while the supervisor is stopped. If done while running, workers will re-clone automatically when they next claim a task.

### 6.3 Binary Update Procedure

```bash
# 1. Pull latest code
git pull origin master

# 2. Run tests to confirm nothing broken
go test -cover ./...

# 3. Build new binary
go build -o bin/supervisor.exe ./cmd/supervisor   # Windows
go build -o bin/supervisor     ./cmd/supervisor   # Linux/macOS

# 4. Stop running supervisor (Ctrl+C or kill process)

# 5. Restart with new binary
./bin/supervisor.exe --config orchestrator.yml
```

The binary is git-ignored (`bin/`). The new binary replaces the old one in-place.

### 6.4 Adding a New Project

1. Add entry to `orchestrator.yml` under `projects:`:
   ```yaml
   - name: new-project
     repo_owner: YourOrg
     repo_name: YourRepo
     conventions_path: ./CLAUDE.md
     branch_pattern: "feature/{ticket}-{summary}"
     commit_pattern: "{ticket} {description}"
     labels: []
   ```

2. Ensure the `GITHUB_TOKEN` has `repo` access to the new repo.

3. Optionally add a `CLAUDE.md` / `CONVENTIONS.md` to the target repo — the conventions parser will use it to determine test/lint/build commands for quality gates.

4. Restart the supervisor. It will start polling the new repo on the next cycle.

### 6.5 Backfill Utility

The `backfill` binary enqueues existing open PRs from a target repo as `StatusReview` tasks so the supervisor's AI-review feedback loop picks them up. Useful when:
- You added the feedback loop to an existing repo with open PRs
- You want to retroactively run AI review on already-open PRs

```bash
# Build
go build -o bin/backfill.exe ./cmd/backfill   # Windows
go build -o bin/backfill     ./cmd/backfill   # Linux/macOS

# Run (reads GITHUB_TOKEN from environment)
./bin/backfill.exe

# Verify tasks were created
Get-ChildItem tasks\*.json | Measure-Object
```

---

## 7. Security

### 7.1 Token Handling Rules

- **Never log the token.** Do not `echo $env:GITHUB_TOKEN` or add it to any log statement. Verify presence only: `if ($env:GITHUB_TOKEN) { "SET" }`.
- **Minimum required scopes.** Use `repo` + `read:org` only. Do not use a token with `admin:org`, `delete_repo`, or other elevated scopes.
- **Rotate tokens** if they are accidentally printed or committed. Check `git log -p` before pushing if you suspect accidental exposure.
- **Do not commit tokens.** `orchestrator.yml` is git-ignored for this reason. Never add the token to any checked-in file.

### 7.2 Worker Permission Scope

The Claude-tier workers run:
```bash
claude --print --dangerously-skip-permissions
```

`--dangerously-skip-permissions` allows the headless agent to edit files, run git commands, and execute test/lint/build tools inside the **isolated worker workspace** (`./projects/{workerID}/...`). It does NOT grant write access outside that workspace.

**Reduce scope when needed:**
```powershell
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"   # prompts before destructive actions
# or
$env:CLAUDE_PERMISSION_MODE = "plan"          # dry-run only, no file writes
```

Unset the variable (or set it back to `""`) to restore the default headless mode.

### 7.3 Log Hygiene

- Worker logs capture LLM output which may echo back issue titles or code snippets. Do not ship raw logs to external aggregators without scrubbing.
- `supervisor_test.log` (created during testing) may contain token-adjacent output. Exclude it from log collectors.
- `tasks/*.json` contains issue titles, PR numbers, and error messages but no secrets. Safe to retain locally.

### 7.4 Dependency Auditing

```bash
# Check for known vulnerabilities in Go modules
go list -m all | grep -v "^github.com/Mawar2"   # show external deps

# govulncheck (install separately if needed)
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

Current external dependencies (as of 2026-06-06):
- `gopkg.in/yaml.v3` — YAML parsing for config
- `github.com/google/uuid` — UUID generation for task IDs

Run `go get -u ./...` + `go mod tidy` periodically to pull security patches, then re-run tests before deploying.
