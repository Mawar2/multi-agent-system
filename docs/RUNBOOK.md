# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06
**Audience:** Operators running the system in production

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

| Requirement | Version | Notes |
|-------------|---------|-------|
| Go | 1.25.1+ | `go version` to verify |
| GitHub CLI (`gh`) | Any recent | `gh --version` to verify |
| `golangci-lint` | Any recent | `golangci-lint --version` to verify |
| `GITHUB_TOKEN` | — | Needs `repo` + `read:org` scopes |
| PowerShell | 5.1+ | Windows: already present |

### 1.1 GitHub Token Setup

The token must have `repo` and `read:org` scopes. In this session it is already exported:

```powershell
# Verify token is present (do not echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — see below" }
```

If it is missing, restore it from the PowerShell profile:

```powershell
# Token is stored in $PROFILE — source it
. $PROFILE
# Then verify again
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "Token not in profile" }
```

### 1.2 GitHub CLI Authentication

Workers use `gh` to create pull requests. Confirm it is authenticated:

```powershell
gh auth status
# Expected: Logged in to github.com as Mawar2
```

Configure git to use the `gh` credential helper (run once per machine):

```powershell
gh auth setup-git
```

### 1.3 Build the Supervisor

```powershell
cd C:\Users\Owner\OneDrive\Documents\Builder\multi-agent-system

# Build to bin/ directory
go build -o bin/supervisor.exe ./cmd/supervisor

# Verify the binary exists
Test-Path bin/supervisor.exe   # Should print True
```

> **Note:** `make` is not installed on this Windows machine. Use raw `go` commands.
> The Makefile documents the canonical targets for reference but cannot be used directly.

### 1.4 First-Run Verification

```powershell
# 1. Confirm token
if ($env:GITHUB_TOKEN) { "OK" } else { "MISSING" }

# 2. Check orchestrator.yml exists
Test-Path orchestrator.yml

# 3. Dry-run build
go build ./...

# 4. Run tests (drop -race: CGO is disabled on this machine)
go test -cover ./...

# 5. Start supervisor
./bin/supervisor.exe --config orchestrator.yml
```

Expected first-line output: `Loading configuration from orchestrator.yml...`

---

## 2. Operation

### 2.1 Normal-State Log Indicators

When the system is running correctly you will see these log lines:

| Log pattern | Meaning |
|-------------|---------|
| `Supervisor: Polling project Mawar2/Kaimi` | Main loop tick (every 60 s by default) |
| `Supervisor: Found N open issues` | GitHub query succeeded |
| `Supervisor: Routed issue #N — complexity: X, tier: Y` | Issue classified and enqueued |
| `[worker-id] Claimed task <uuid>` | Worker picked up work |
| `[WorkspaceManager] Successfully cloned` | Repo cloned into isolated workspace |
| `[QualityGates] ✅ Tests passed` | Quality gate passed |
| `[Worker X] Completed task — PR #N created` | End-to-end success |
| `Supervisor: Checking PRs for AI review feedback` | Feedback-loop poll |

### 2.2 Issue → PR Lifecycle

```
GitHub Issue (open)
        │
        ▼
Supervisor polls (60 s interval)
        │
        ▼
Router classifies complexity → assigns tier
        │
        ▼
Task enqueued (./tasks/<uuid>.json, status=pending)
        │
        ▼
Worker claims task (status=claimed)
        │
        ▼
Worker clones repo into per-worker workspace
        │
        ▼
LLM executes solution (Claude Code CLI)
        │
        ▼
Quality gates run (tests / lint / format / build)
        │
   pass │  fail ──────────────────────────────────► task=failed
        ▼
PR created (status=review)
        │
        ▼
CI/CD runs AI code review (Gemini 2.5 Pro)
        │
   pass │  feedback ───────────────────────────────► fix task enqueued (max 3×)
        ▼
Human review → merge (status=complete)
```

### 2.3 Worker Tiers

| Tier | Workers | Default for | Model |
|------|---------|-------------|-------|
| `gemini-flash` | 5 (`gemini-flash-1` … `gemini-flash-5`) | Simple issues | `gemini-flash-3.5` |
| `gemini-pro` | 3 (`gemini-pro-1` … `gemini-pro-3`) | Medium issues | `gemini-pro-3.5` |
| `claude` | 2 (`claude-1`, `claude-2`) | Complex issues | `claude-sonnet-4.5` |

> All tiers currently use the Claude Code backend (`claude --print --dangerously-skip-permissions`).
> The `model` field in `orchestrator.yml` is informational until a Gemini direct-API backend is wired in.

### 2.4 Task Queue — Quick Commands

```powershell
# List all task files
Get-ChildItem tasks\*.json

# Pretty-print one task
Get-Content tasks\<uuid>.json | ConvertFrom-Json

# Count by status
Get-ChildItem tasks\*.json | ForEach-Object { (Get-Content $_.FullName | ConvertFrom-Json).status } | Group-Object

# Show all failed tasks
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if ($t.status -eq 5) { "$($t.issue_number): $($t.error_msg)" }
}
```

Status integer mapping (from `internal/taskqueue/task.go`):

| Value | String | Meaning |
|-------|--------|---------|
| 0 | `pending` | In queue, not yet claimed |
| 1 | `claimed` | Claimed by worker, not yet started |
| 2 | `in_progress` | Worker actively executing |
| 3 | `review` | PR created, awaiting review |
| 4 | `complete` | PR merged, done |
| 5 | `failed` | Terminal failure |

### 2.5 Day-to-Day Commands

```powershell
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Test (no -race; CGO disabled on this machine)
go test -cover ./...

# Lint
golangci-lint run ./...

# Format
gofmt -w .

# Vet
go vet ./...
```

---

## 3. Configuration Reference

### 3.1 Full Schema

`orchestrator.yml` (copy from `orchestrator.example.yml`):

```yaml
projects:
  - name: kaimi                           # Logical name (used in logs)
    repo_owner: Mawar2                    # GitHub org or user
    repo_name: Kaimi                      # Repository name
    conventions_path: ./CLAUDE.md         # Path to CLAUDE.md / CONVENTIONS.md in target repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"   # Branch name template
    commit_pattern: "{ticket}_{description}"             # Commit message template
    labels: []                            # Only process issues with these labels (empty = all)

worker_tiers:
  gemini_flash:
    max_workers: 5                        # Number of parallel workers at this tier
    model: gemini-flash-3.5              # Informational (model used when Gemini backend live)
  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5
  claude:
    max_workers: 2
    model: claude-sonnet-4.5

poll_interval_seconds: 60               # How often to query GitHub for new issues
task_timeout_minutes: 120               # Max time a worker spends per task before timeout
max_retry_attempts: 3                   # Retry count before marking task failed
task_queue_dir: ./tasks                 # Directory for JSON queue files
```

### 3.2 Routing Heuristics

The `RuleBasedRouter` (`internal/orchestrator/router.go`) maps each issue to a complexity level using these rules in order:

| Priority | Signal | Result |
|----------|--------|--------|
| 1 | Title/body contains `add comment`, `fix typo`, `update readme`, `docs:`, `[docs]`, `documentation`, `add logging`, `add godoc`, `format code`, `update version` | Simple |
| 2 | Title/body matches `architecture`, `design`, `refactor.*system`, `implement.*agent`, `database`, `migration`, `schema change`, `security`, `authentication`, `authorization`, `breaking change`, `api redesign` | Complex |
| 3 | Body mentions `files:` or `affected files` and estimated count ≤ 3 | Simple |
| 4 | Body mentions `files:` or `affected files` and estimated count > 10 | Complex |
| 5 | Issue label contains `simple` or `easy` | Simple |
| 6 | Issue label contains `complex` or `hard` | Complex |
| 7 | (none of the above) | Medium |

Complexity → Tier mapping (hard-coded, not configurable):

| Complexity | Tier |
|-----------|------|
| Simple | `gemini-flash` |
| Medium | `gemini-pro` |
| Complex | `claude` |

### 3.3 Branch and Commit Pattern Variables

| Variable | Value |
|----------|-------|
| `{ticket}` | Issue number (e.g. `47`) |
| `{summary}` | Kebab-cased issue title excerpt |
| `{description}` | Short description of the change |

Example with `branch_pattern: "feature/KAI-{ticket}-{summary}"` and issue #47 titled "Add login page":
→ `feature/KAI-47-add-login-page`

### 3.4 Label Filtering

Set `labels` in the project config to filter which issues the supervisor enqueues:

```yaml
labels: ["orchestrator:pending"]   # Only process issues with this label
```

Leave `labels: []` to process all open issues (default).

---

## 4. Troubleshooting

### 4.1 GitHub API Returns 401

**Symptom:** `GitHub API returned status 401` in logs.

**Steps:**
1. Confirm `$env:GITHUB_TOKEN` is set: `if ($env:GITHUB_TOKEN) { "OK" }`
2. If missing, re-source profile: `. $PROFILE`
3. Confirm token has `repo` and `read:org` scopes: `gh auth status`
4. If token expired, generate a new one at `github.com/settings/tokens` and update `$PROFILE`.

### 4.2 GitHub API Rate Limit (403 / 429)

**Symptom:** `API rate limit exceeded` or HTTP 429 in logs.

**Steps:**
1. Increase `poll_interval_seconds` in `orchestrator.yml` (e.g. 120 or 300).
2. Check current rate limit: `gh api rate_limit`.
3. Unauthenticated: 60 req/hour. Authenticated: 5000 req/hour. Ensure token is in every request.

### 4.3 Stalled Tasks

**Symptom:** Tasks stuck in `claimed` or `in_progress` for > 2 hours.

**Steps:**
1. Identify stalled tasks:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
       $t = Get-Content $_.FullName | ConvertFrom-Json
       if ($t.status -eq 1 -or $t.status -eq 2) {
           "$($t.id): status=$($t.status) worker=$($t.worker_id)"
       }
   }
   ```
2. Check if the worker process is still alive (look for `claude.exe` or `supervisor.exe` in Task Manager).
3. If worker is dead, reset the task manually:
   ```powershell
   $task = Get-Content tasks\<uuid>.json | ConvertFrom-Json
   $task.status = 0       # pending
   $task.worker_id = ""
   $task | ConvertTo-Json -Depth 10 | Set-Content tasks\<uuid>.json -Encoding utf8
   ```
4. The task will be reclaimed on the next supervisor poll.

### 4.4 Quality Gate Failures

**Symptom:** Tasks fail with `error_msg` containing `quality gates`.

**Steps:**
1. Identify failing tasks:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
       $t = Get-Content $_.FullName | ConvertFrom-Json
       if ($t.error_msg -like "*quality gates*") { "$($t.issue_number): $($t.error_msg)" }
   }
   ```
2. Check the task's `logs_path` for the full gate output.
3. Common causes:
   - **Test failures:** LLM introduced a regression. Re-queue the task; the worker will retry with a new approach.
   - **Lint errors:** Add lint commands to the project's `CLAUDE.md` so the LLM knows the rules before editing.
   - **Format failures:** The project formatter was not run. Ensure `conventions_path` file lists the format command.

### 4.5 Fix Tasks Not Created for AI Review Feedback

**Symptom:** AI posts review comments but no `pr_feedback` task appears in `./tasks/`.

**Steps:**
1. Confirm the AI review comment starts with `## 🤖 AI Code Review (Gemini 2.5 Pro)` — the prefix is checked exactly.
2. Confirm `ReviewCommentID` is not already tracked in an existing task (deduplication).
3. Check supervisor logs for `Supervisor: Checking PRs for AI review feedback`.
4. Confirm the parent task has `status=3` (review). If it was manually changed to `complete` or `failed`, the feedback loop will skip it.

### 4.6 Clone Failures (`destination path already exists`)

**Symptom:** `fatal: destination path '...' already exists and is not an empty directory`.

**Steps:**
1. This should be fixed by per-worker isolation (commit `3cd2649`). If it recurs, check whether two workers share the same `worker_id`.
2. Clean the stale workspace:
   ```powershell
   Remove-Item -Recurse -Force projects\<worker-id>\<owner>\<repo>
   ```
3. The next task claim will trigger a fresh clone.

### 4.7 Duplicate Tasks for the Same Issue

**Symptom:** Multiple tasks with the same `issue_number`.

**Steps:**
1. The supervisor checks for existing PRs before enqueuing. Verify the GitHub token has read access to pull requests (`repo` scope).
2. If a PR was closed manually without merging, the supervisor may re-enqueue the issue. Use `labels` filtering or close the issue to prevent re-processing.

### 4.8 Supervisor Exits on Startup

**Symptom:** `supervisor.exe` terminates immediately with a non-zero exit code.

**Steps:**
1. Run with visible output: `./bin/supervisor.exe --config orchestrator.yml 2>&1`.
2. Common causes:
   - `orchestrator.yml` not found — verify `Test-Path orchestrator.yml`.
   - YAML parse error — validate with an online YAML linter or `python -c "import yaml; yaml.safe_load(open('orchestrator.yml'))"`.
   - `GITHUB_TOKEN` not set — check token presence before starting.

### 4.9 `go test -race` Fails with "requires cgo"

**Symptom:** `go test -race ./...` exits with `FAILED: -race requires cgo`.

**Fix:** CGO is disabled on this Windows machine. Drop the `-race` flag:
```powershell
go test -cover ./...   # Always use this, not -race
```

### 4.10 Pre-Existing Lint Findings in `github_rest_client.go`

**Symptom:** `golangci-lint` reports 4 findings in `internal/ticket/github_rest_client.go`:
- 3× unchecked `resp.Body.Close` (`errcheck`)
- 1× `staticcheck QF1003`

These are pre-existing and were not introduced by recent work. They do not block operation. Fix opportunistically when touching that file.

---

## 5. Monitoring

### 5.1 Key Metrics

```powershell
# Total tasks enqueued
(Get-ChildItem tasks\*.json).Count

# Tasks by status
Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_.FullName | ConvertFrom-Json).status
} | Group-Object | Sort-Object Name

# Failure rate (status=5 / total)
$all = (Get-ChildItem tasks\*.json).Count
$failed = (Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if ($t.status -eq 5) { 1 }
} | Measure-Object -Sum).Sum
if ($all -gt 0) { "Failure rate: $([math]::Round($failed/$all*100,1))%" }

# PR creation rate (status >= 3 = review/complete/failed after PR)
$prs = (Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if ($t.pr_number -gt 0) { 1 }
} | Measure-Object -Sum).Sum
"PRs created: $prs"

# Quality gate failure count
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if ($t.error_msg -like "*quality gates*") { 1 }
} | Measure-Object -Sum

# Fix task (pr_feedback) count
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if ($t.metadata -and $t.metadata.task_type -eq "pr_feedback") { 1 }
} | Measure-Object -Sum

# Review iteration distribution (expect: ~70% at 0, ~20% at 1, ~8% at 2, ~2% at 3)
Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_.FullName | ConvertFrom-Json).review_iteration
} | Group-Object | Sort-Object Name

# Tasks that hit max iterations
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if ($t.error_msg -like "*Max review iterations*") { $t.id }
}

# Backfilled tasks (created by cmd/backfill)
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    if ($t.metadata -and $t.metadata.source -eq "backfill") { 1 }
} | Measure-Object -Sum
```

### 5.2 Log Patterns Table

| Pattern | Severity | Action |
|---------|----------|--------|
| `GitHub API returned status 401` | Critical | Check/rotate `GITHUB_TOKEN` |
| `API rate limit exceeded` | Warning | Increase `poll_interval_seconds` |
| `quality gates failed` | Info | Review task logs; retry or fix upstream conventions |
| `Max review iterations reached` | Warning | Manual review required for this PR |
| `fatal: destination path already exists` | Error | Clean workspace dir for affected worker |
| `context deadline exceeded` | Warning | Task took > `task_timeout_minutes`; will retry |
| `Worker started (tier: X)` | Info | Normal startup |
| `Supervisor: Starting main loop` | Info | Normal startup |

### 5.3 Alerting Thresholds

| Metric | Warning | Critical |
|--------|---------|---------|
| Failure rate | > 20% | > 40% |
| Max-iterations hit | > 5% of tasks | > 10% of tasks |
| Quality gate failure rate | > 40% | > 60% |
| Tasks stuck in `claimed`/`in_progress` | > 1 hour | > 2 hours |

---

## 6. Maintenance

### 6.1 Task Queue Cleanup

Tasks accumulate in `./tasks/`. Archive completed/failed tasks older than 7 days:

```powershell
$cutoff = (Get-Date).AddDays(-7)
$archiveDir = "tasks\archive"
New-Item -ItemType Directory -Force $archiveDir | Out-Null

Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_.FullName | ConvertFrom-Json
    # Archive terminal tasks (complete=4 or failed=5) older than cutoff
    if (($t.status -eq 4 -or $t.status -eq 5) -and $_.LastWriteTime -lt $cutoff) {
        Move-Item $_.FullName "$archiveDir\$($_.Name)"
        Write-Host "Archived $($_.Name)"
    }
}
```

To delete instead of archive (irreversible):

```powershell
Get-ChildItem tasks\archive\*.json | Remove-Item
```

### 6.2 Workspace Cleanup

Per-worker workspaces live in `./projects/`. Each is ~200 MB. Clean after a run:

```powershell
# Remove all workspaces (workers will re-clone on next task)
Remove-Item -Recurse -Force projects\
```

To clean only one worker's workspace:

```powershell
Remove-Item -Recurse -Force projects\gemini-flash-1\
```

### 6.3 Binary Update Procedure

```powershell
# 1. Pull latest code
git pull origin master

# 2. Stop running supervisor (Ctrl+C or kill the process)

# 3. Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor

# 4. Verify tests still pass
go test -cover ./...

# 5. Restart
./bin/supervisor.exe --config orchestrator.yml
```

### 6.4 Adding a New Project

1. Add an entry to `orchestrator.yml`:
   ```yaml
   - name: new-project
     repo_owner: Mawar2
     repo_name: NewRepo
     conventions_path: ./CLAUDE.md
     branch_pattern: "feature/{ticket}-{summary}"
     commit_pattern: "{ticket}: {description}"
     labels: []
   ```
2. Ensure the target repo has a `CLAUDE.md` (or `CONVENTIONS.md`) with test/lint/format commands.
3. Restart the supervisor.

No code changes are required unless you need a new worker tier or custom routing rules.

### 6.5 Backfill Utility

`cmd/backfill` fetches existing open PRs from Mawar2/Kaimi and enqueues them as `status=review` tasks so the feedback loop picks them up. Use it when the supervisor was down and PRs accumulated:

```powershell
# Build
go build -o bin/backfill.exe ./cmd/backfill

# Run (GITHUB_TOKEN must be set)
./bin/backfill.exe
```

The backfill utility infers complexity from PR diff size (additions + deletions), which is separate from the router's issue-based heuristics.

---

## 7. Security

### 7.1 Token Handling

- **Never log `GITHUB_TOKEN`** — supervisors and workers should only check presence (`if ($env:GITHUB_TOKEN)`), not echo the value.
- **Store in `$PROFILE`**, not in `orchestrator.yml` (which may be committed accidentally).
- **Minimum scopes:** `repo` and `read:org`. Do not grant `admin:org`, `delete_repo`, or write org permissions.
- **Rotate tokens** every 90 days or immediately on suspected compromise.

### 7.2 Worker Permission Scope

Workers run Claude Code with:
```
claude --print --dangerously-skip-permissions
```

This grants the worker unrestricted file-system and shell access inside its isolated workspace. The risk is bounded because:
- Each worker uses a **private per-worker directory** (`./projects/{workerID}/...`).
- Workers clone the **target repo** only; they cannot access unrelated paths on the host.
- Workers do **not** have network access beyond what `git`, `gh`, and the test suite require.

To restrict worker permissions to read/write-only (no shell execution), set:
```powershell
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"
```

Valid values: `acceptEdits` (file edits only), `plan` (dry-run, no writes), or unset (full autonomy, default).

### 7.3 Log Hygiene

- Do not redirect supervisor stdout to files that are committed to the repository.
- `supervisor_test.log` in the repo root is an example artifact — add it to `.gitignore` if not already present.
- Task JSON files (`./tasks/*.json`) may contain issue descriptions with sensitive content. Treat the `./tasks/` directory as internal and exclude it from any public exposure.

### 7.4 Dependency Auditing

```powershell
# Check for known vulnerabilities in Go module dependencies
go list -json -m all | go run golang.org/x/vuln/cmd/govulncheck@latest -json -

# Or simpler (requires govulncheck installed)
govulncheck ./...
```

Run this check after updating dependencies or at least monthly.
