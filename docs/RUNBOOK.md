# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06  
**Repository:** `Mawar2/multi-agent-system`  
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

| Requirement | Version / Notes | How to verify |
|-------------|-----------------|---------------|
| Go | 1.25.1+ | `go version` |
| GitHub CLI (`gh`) | Any recent version | `gh --version` |
| `golangci-lint` | Any recent version | `golangci-lint --version` |
| `GITHUB_TOKEN` env var | Scopes: `repo`, `read:org` | `if ($env:GITHUB_TOKEN) { "set" }` |
| Git credential helper | Configured via `gh auth setup-git` | `gh auth status` |

### Token Setup

The GitHub token is persisted in `$PROFILE` (PowerShell profile). Load it into the current shell with:

```powershell
# Read token from PowerShell profile
$env:GITHUB_TOKEN = (cat $PROFILE | Select-String "ghp_").Matches.Value

# Verify it is present (never echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — check `$PROFILE`" }
```

Required token scopes:

- `repo` — full control of repositories (clone, push, create PRs)
- `read:org` — read org membership (for private repos under an org)

### Git Authentication for Workers

Workers clone and push repos using the `gh` credential helper. Run this once per machine:

```powershell
gh auth setup-git
```

Verify authentication:

```powershell
gh auth status
```

Workers set `GIT_TERMINAL_PROMPT=0` internally — any auth gap fails fast instead of hanging indefinitely.

### Build

```powershell
# Build the supervisor binary
go build -o bin/supervisor.exe ./cmd/supervisor

# Build all packages (sanity check)
go build ./...
```

The binary is git-ignored. Rebuild after any code change.

### First-Run Verification

1. Copy the example config:

   ```powershell
   Copy-Item orchestrator.example.yml orchestrator.yml
   ```

2. Edit `orchestrator.yml` to point at your target repository (see [Configuration Reference](#3-configuration-reference)).

3. Start the supervisor:

   ```powershell
   .\bin\supervisor.exe --config orchestrator.yml
   ```

4. Confirm you see startup output like:

   ```
   Loading configuration from orchestrator.yml...
   Initializing task queue at ./tasks...
   Started 10 workers
   Monitoring 1 project(s):
     - Mawar2/Kaimi
   Supervisor running. Press Ctrl+C to stop.
   ```

5. Within 60 seconds you should see the first poll:

   ```
   Supervisor: Polling project Mawar2/Kaimi
   Supervisor: Found N open issues in Mawar2/Kaimi
   ```

---

## 2. Operation

### Normal-State Log Indicators

| Log line | Meaning |
|----------|---------|
| `Supervisor: Polling project …` | Normal poll cycle; fires every `poll_interval_seconds` |
| `Supervisor: Found N open issues` | GitHub query returned N results; 0 is fine when the backlog is empty |
| `Supervisor: Routed issue #N — complexity: X, tier: Y` | Issue classified and enqueued |
| `[worker-id] Claimed task … (issue #N)` | Worker picked up a task |
| `[WorkspaceManager] Successfully cloned …` | Fresh repo clone; first task for this worker |
| `[WorkspaceManager] Pulled latest changes` | Worker reused existing workspace |
| `[QualityGates] ✅ All quality checks passed` | Code passed tests/lint/format; PR will be created |
| `[worker-id] Completed task … — PR #N created` | End-to-end success |
| `[worker-id] Task … failed: …` | Worker encountered an error; task released back to queue |

### Issue → PR Lifecycle

```
GitHub Issue (open)
        │
        ▼
Supervisor polls (every 60 s)
        │
        ▼
Router classifies complexity
 simple → gemini-flash tier
 medium → gemini-pro tier
 complex → claude tier
        │
        ▼
Task enqueued (status: pending)
        │
        ▼
Worker claims task (status: claimed → in_progress)
        │
        ├─► Clone/pull workspace
        ├─► Create feature branch
        ├─► Execute LLM (Claude Code CLI)
        ├─► Run quality gates
        │      ├─ Tests pass?   ✅ continue / ❌ task failed
        │      ├─ Linter clean? ✅ continue / ❌ task failed
        │      └─ Formatter ok? ✅ continue / ❌ task failed
        ├─► Create PR (status: review)
        │
        ▼
CI runs AI code review (Gemini 2.5 Pro)
        │
        ├─ Review passes → human merges → task complete
        └─ Review has feedback →
               Supervisor detects comment (120 s poll)
               Creates pr_feedback task (iteration 1–3)
               Worker applies targeted fixes
               PR updated → CI re-runs
               (max 3 iterations, then task failed)
```

### Worker Tiers

| Tier | Worker IDs | Tasks | Model |
|------|-----------|-------|-------|
| `gemini-flash` | `gemini-flash-1` … `gemini-flash-5` | Simple (docs, typos, small fixes) | `gemini-flash-3.5` |
| `gemini-pro` | `gemini-pro-1` … `gemini-pro-3` | Medium (features, refactors) | `gemini-pro-3.5` |
| `claude` | `claude-1`, `claude-2` | Complex (architecture, security, migrations) | `claude-sonnet-4.5` |

**Note:** All tiers currently use the Claude Code CLI backend. The model names above are informational targets; actual model selection is configured per-tier in `orchestrator.yml`.

### Task Queue — Day-to-Day Commands

```powershell
# List all task files
Get-ChildItem tasks\*.json | Select-Object Name

# Inspect a specific task
Get-Content tasks\<uuid>.json | ConvertFrom-Json

# Count by status (requires jq)
Get-Content tasks\*.json | jq -s 'group_by(.status) | map({status: .[0].status, count: length})'

# Show pending tasks
Get-Content tasks\*.json | jq -r 'select(.status == 0) | "\(.id) issue #\(.issue_number) \(.title)"'

# Show failed tasks with reason
Get-Content tasks\*.json | jq -r 'select(.status == 5) | "\(.issue_number): \(.error_msg)"'

# Show tasks in review
Get-Content tasks\*.json | jq -r 'select(.status == 3) | "\(.issue_number) PR #\(.pr_number)"'
```

Task status integer mapping (from `task.go`):

| Value | Name | Description |
|-------|------|-------------|
| 0 | `pending` | In queue, available to claim |
| 1 | `claimed` | Claimed by worker, not yet started |
| 2 | `in_progress` | Worker actively executing |
| 3 | `review` | PR created, awaiting review |
| 4 | `complete` | PR merged, done |
| 5 | `failed` | Exhausted retries |

### Build / Test / Lint Commands

```powershell
# Build
go build -o bin/supervisor.exe ./cmd/supervisor
go build ./...

# Test (drop -race: CGO is off on this machine)
go test -cover ./...
go test ./internal/orchestrator -v
go test -run TestRoute ./internal/orchestrator -v

# Vet
go vet ./...

# Lint (4 pre-existing findings in github_rest_client.go — not regressions)
golangci-lint run ./...
```

---

## 3. Configuration Reference

### Full `orchestrator.yml` Schema

```yaml
# ─────────────────────────────────────────────────────────────
# Projects — list one entry per GitHub repository to monitor
# ─────────────────────────────────────────────────────────────
projects:
  - name: kaimi                          # Internal project name (used in logs)
    repo_owner: Mawar2                   # GitHub owner/org
    repo_name: Kaimi                     # GitHub repository name
    conventions_path: ./CLAUDE.md        # Path to conventions file read by workers
                                         # (relative to the cloned workspace root)
    branch_pattern: "feature/KAI-{ticket}-{summary}"
    commit_pattern: "{ticket}_{description}"
    labels: []                           # Optional: only process issues with these labels
                                         # e.g., ["orchestrator:pending", "ai-ready"]
                                         # Empty list = all open issues

  # Additional project example:
  # - name: other-project
  #   repo_owner: YourOrg
  #   repo_name: YourRepo
  #   conventions_path: ./CONVENTIONS.md
  #   branch_pattern: "{ticket}-{summary}"
  #   commit_pattern: "[{ticket}] {description}"
  #   labels: ["needs-fix"]

# ─────────────────────────────────────────────────────────────
# Worker tiers — control concurrency and model selection
# ─────────────────────────────────────────────────────────────
worker_tiers:
  gemini_flash:
    max_workers: 5           # Concurrent workers for simple tasks
    model: gemini-flash-3.5  # Informational; actual model set by LLM backend

  gemini_pro:
    max_workers: 3           # Concurrent workers for medium tasks
    model: gemini-pro-3.5

  claude:
    max_workers: 2           # Concurrent workers for complex tasks
    model: claude-sonnet-4.5

# ─────────────────────────────────────────────────────────────
# Supervisor settings
# ─────────────────────────────────────────────────────────────
poll_interval_seconds: 60    # How often to poll GitHub for new issues (default: 60)
task_timeout_minutes: 120    # Max minutes a worker may spend on a task (default: 120)
max_retry_attempts: 3        # Times to retry a task before marking it failed (default: 3)
task_queue_dir: ./tasks      # Directory for JSON queue files (default: ./tasks)
```

### Routing Heuristics

The rule-based router (`internal/orchestrator/router.go`) classifies issues in order:

| Priority | Signal | Result |
|----------|--------|--------|
| 1 | Title/body contains: `add comment`, `add godoc`, `add documentation`, `fix typo`, `update readme`, `format code`, `add logging`, `update version`, `docs:`, `[docs]`, `documentation` | `simple` → `gemini-flash` |
| 2 | Title/body matches regex: `architecture`, `design`, `refactor.*system`, `implement.*agent`, `new feature.*complex`, `database`, `migration`, `schema change`, `security`, `authentication`, `authorization`, `breaking change`, `api redesign` | `complex` → `claude` |
| 3 | Body contains `files:` or `affected files` + file count ≤ 3 | `simple` |
| 3 | Body contains `files:` or `affected files` + file count > 10 | `complex` |
| 4 | Label contains `simple` or `easy` | `simple` |
| 4 | Label contains `complex` or `hard` | `complex` |
| 5 | No clear signal | `medium` → `gemini-pro` |

### Branch and Commit Pattern Variables

| Variable | Value |
|----------|-------|
| `{ticket}` | GitHub issue number (e.g., `47`) |
| `{summary}` | Slugified issue title (e.g., `fix-login-bug`) |
| `{description}` | Short description of the change |

### Label Filtering

When `labels` is non-empty in a project config, the supervisor only enqueues issues that carry **all** of the listed labels. Use this to gate which issues the system processes:

```yaml
labels: ["orchestrator:pending"]  # Only process issues tagged by a human
```

Remove the filter (or use `labels: []`) to process all open issues.

---

## 4. Troubleshooting

### 1. GitHub 401 Unauthorized

**Symptom:** `GitHub API returned status 401` in logs.

**Steps:**
1. Verify token is set: `if ($env:GITHUB_TOKEN) { "set" } else { "NOT set" }`
2. If not set, reload from profile: `$env:GITHUB_TOKEN = (cat $PROFILE | Select-String "ghp_").Matches.Value`
3. Test token manually: `gh api user --jq '.login'` — should print your GitHub username.
4. Verify token scopes: `gh auth status` — look for `repo` and `read:org`.
5. If token is expired, generate a new one at GitHub → Settings → Developer settings → Personal access tokens.

### 2. GitHub Rate Limit (429 / `API rate limit exceeded`)

**Symptom:** `GitHub API returned status 429` or `API rate limit exceeded`.

**Steps:**
1. Check remaining rate limit: `gh api rate_limit --jq '.rate'`
2. Increase `poll_interval_seconds` in `orchestrator.yml` (e.g., `120` or `300`).
3. Reduce `worker_tiers.*.max_workers` temporarily.
4. Rate limits reset every hour. You can check `reset` field from step 1 (Unix timestamp).

### 3. Stalled Tasks (`claimed` or `in_progress` for hours)

**Symptom:** A task stays in status 1 (`claimed`) or 2 (`in_progress`) beyond `task_timeout_minutes`.

**Steps:**
1. Identify stalled task UUID:
   ```powershell
   Get-Content tasks\*.json | jq -r 'select(.status == 1 or .status == 2) | "\(.id) worker=\(.worker_id) started=\(.started_at)"'
   ```
2. Check if the worker process is still running:
   ```powershell
   Get-Process -Name "supervisor" -ErrorAction SilentlyContinue
   ```
3. If the supervisor crashed, manually reset the task by editing its JSON:
   ```powershell
   $task = Get-Content tasks\<uuid>.json | ConvertFrom-Json
   $task.status = 0     # reset to pending
   $task.worker_id = "" # clear worker claim
   $task | ConvertTo-Json -Depth 10 | Set-Content tasks\<uuid>.json -Encoding utf8
   ```
4. Restart the supervisor. The task will be reclaimed automatically.

### 4. Quality Gate Failures

**Symptom:** `quality gate failed — tests: …` or similar in logs; task ends with status 5 (`failed`).

**Steps:**
1. Find the failing task:
   ```powershell
   Get-Content tasks\*.json | jq -r 'select(.error_msg | contains("quality gate")) | "\(.issue_number): \(.error_msg)"'
   ```
2. Inspect the worker's workspace:
   ```powershell
   # Worker workspaces are at: projects\<worker-id>\<owner>\<repo>\
   ls projects\gemini-flash-1\Mawar2\Kaimi\
   ```
3. Manually run the failing command inside the workspace to see full output:
   ```powershell
   Set-Location projects\gemini-flash-1\Mawar2\Kaimi
   go test ./...          # or whatever the test command is
   ```
4. If the issue is in the generated code, the task will be retried (up to `max_retry_attempts`). After exhausting retries it stays `failed` — handle manually or close the GitHub issue.
5. If quality gates are consistently failing for a project, review the `conventions_path` file to ensure `TestCommand`, `LintCommand`, and `FormatCommand` are correct.

### 5. Missing Fix Tasks (AI Review Feedback Not Detected)

**Symptom:** AI code review comments appear on a PR but no `pr_feedback` task is created.

**Steps:**
1. Confirm the review comment body starts with `## 🤖 AI Code Review (Gemini 2.5 Pro)` — the supervisor only detects this exact prefix.
2. Verify PR monitoring is enabled: the supervisor polls open PRs every 120 seconds. Allow up to 2 minutes after the review comment appears.
3. Check for deduplication: if a task was already created for this `review_comment_id`, no duplicate is created.
   ```powershell
   Get-Content tasks\*.json | jq -r 'select(.metadata.task_type == "pr_feedback") | "\(.issue_number) comment_id=\(.review_comment_id) iter=\(.review_iteration)"'
   ```
4. Check `max_retry_attempts`: if the parent task already hit 3 review iterations, no further fix tasks are created (by design).

### 6. Clone Failures (`destination path already exists`)

**Symptom:** `fatal: destination path '...' already exists and is not an empty directory`.

**Steps:**
1. This should not occur since per-worker isolation was implemented in commit `3cd2649`. If it does, a workspace was left in a partially-initialized state.
2. Identify and clean the broken workspace:
   ```powershell
   ls projects\
   # Find partially-cloned directories
   Remove-Item -Recurse -Force projects\<worker-id>\
   ```
3. The worker will re-clone on the next task.

### 7. Duplicate Tasks for the Same Issue

**Symptom:** Multiple task JSON files reference the same `issue_number`.

**Steps:**
1. The supervisor checks for an existing open PR before enqueuing. If GitHub search is slow or returns stale data, duplicates can occur.
2. Identify duplicates:
   ```powershell
   Get-Content tasks\*.json | jq -r '.issue_number' | Sort-Object | Get-Unique -AsString
   ```
3. Manually mark all but the most recent task as `failed`:
   ```powershell
   $task = Get-Content tasks\<uuid>.json | ConvertFrom-Json
   $task.status = 5
   $task.error_msg = "duplicate — cancelled manually"
   $task | ConvertTo-Json -Depth 10 | Set-Content tasks\<uuid>.json -Encoding utf8
   ```

### 8. Supervisor Exits Immediately

**Symptom:** Supervisor starts and exits within a second with a non-zero exit code.

**Steps:**
1. Run with visible stderr:
   ```powershell
   .\bin\supervisor.exe --config orchestrator.yml 2>&1
   ```
2. Common causes:
   - `orchestrator.yml` not found → copy from `orchestrator.example.yml`.
   - YAML syntax error → validate with `Get-Content orchestrator.yml | python -c "import sys,yaml; yaml.safe_load(sys.stdin)"` (if Python available) or use an online YAML validator.
   - No projects configured → ensure `projects:` list is non-empty.
   - `repo_owner` or `repo_name` missing → both fields are required for every project.

### 9. Build Fails: `-race requires cgo`

**Symptom:** `go test -race ./...` fails with `-race requires cgo`.

**Resolution:** CGO is disabled on this machine. Drop `-race` from all test invocations:

```powershell
go test -cover ./...   # correct
# go test -race ./...  # DO NOT USE — fails with CGO disabled
```

The `Makefile`'s `make test` target uses `-race` and will fail locally. Use the raw `go` command above.

### 10. Pre-Existing Lint Findings (Not Regressions)

**Symptom:** `golangci-lint run ./...` reports 4 findings even on a clean checkout.

**Context:** These are pre-existing issues in `internal/ticket/github_rest_client.go`:

- 3× unchecked `resp.Body.Close` error (`errcheck`)
- 1× `staticcheck QF1003`

These were not introduced by recent work. Do not mark a PR as failing because of them. Fix opportunistically when editing that file.

---

## 5. Monitoring

### Key Metrics — PowerShell + jq Queries

**Task throughput (completed in last 24 hours):**
```powershell
Get-Content tasks\*.json | jq -r 'select(.status == 4) | .completed_at' | Where-Object { $_ -gt (Get-Date).AddDays(-1).ToString("o") } | Measure-Object | Select-Object -ExpandProperty Count
```

**Overall failure rate:**
```powershell
$all = (Get-ChildItem tasks\*.json).Count
$failed = (Get-Content tasks\*.json | jq -r 'select(.status == 5)' | Measure-Object -Line).Lines
Write-Host "Failure rate: $failed / $all = $([math]::Round($failed/$all*100, 1))%"
```

**Quality gate failure rate:**
```powershell
Get-Content tasks\*.json | jq -r 'select(.error_msg | strings | contains("quality gate"))' | jq -s 'length'
```

**Review iteration distribution (expect: ~70% at 0, ~20% at 1, ~8% at 2, ~2% at 3):**
```powershell
Get-Content tasks\*.json | jq -r '.review_iteration' | Sort-Object | Group-Object | Select-Object Name, Count
```

**Tasks that hit the max-iteration limit (should be <5%):**
```powershell
Get-Content tasks\*.json | jq -r 'select(.error_msg | strings | contains("Max review iterations"))' | jq -s 'length'
```

**Backfilled tasks (enqueued via `cmd/backfill`):**
```powershell
Get-Content tasks\*.json | jq -r 'select(.metadata.source == "backfill")' | jq -s 'length'
```

**Active workers (tasks currently in-progress):**
```powershell
Get-Content tasks\*.json | jq -r 'select(.status == 2) | "\(.worker_id) → issue #\(.issue_number)"'
```

### Log Patterns Table

| Pattern to watch | Level | Action |
|------------------|-------|--------|
| `GitHub API returned status 401` | Error | Re-set `GITHUB_TOKEN`; verify scopes |
| `GitHub API returned status 429` | Warning | Increase `poll_interval_seconds`; wait for reset |
| `quality gate failed` | Warning | Review workspace; check conventions commands |
| `Max review iterations reached` | Warning | Human review required; close or manually fix issue |
| `Error claiming task` | Warning | Transient; worker retries automatically |
| `fatal: destination path` | Error | Clean workspace for that worker ID |
| `Supervisor error:` | Fatal | Supervisor exited; restart required |

### Alerting Thresholds

| Metric | Warning | Critical |
|--------|---------|----------|
| Failure rate | > 20% | > 40% |
| Max-iteration hit rate | > 5% | > 15% |
| Tasks in `claimed` > 30 min | Any | — |
| Consecutive 429 responses | 3+ | — |
| No completed tasks in 2 hours | — | Alert |

---

## 6. Maintenance

### Task Queue Cleanup

Tasks accumulate in `./tasks/`. Clean up periodically to keep disk usage manageable.

**Archive completed/failed tasks older than 7 days:**
```powershell
$cutoff = (Get-Date).AddDays(-7).ToString("o")
$archive = "tasks\archive"
New-Item -ItemType Directory -Force $archive | Out-Null
Get-Content tasks\*.json | jq -r --arg cutoff $cutoff 'select((.status == 4 or .status == 5) and .completed_at < $cutoff) | .id' | ForEach-Object {
    Move-Item "tasks\$_.json" "$archive\$_.json"
}
```

**Delete all terminal tasks (completed or failed) — destructive:**
```powershell
# CAUTION: permanent deletion
Get-Content tasks\*.json | jq -r 'select(.status == 4 or .status == 5) | .id' | ForEach-Object {
    Remove-Item "tasks\$_.json"
}
```

### Workspace Cleanup

Worker workspaces are cloned repos under `./projects/`. They persist between tasks to avoid re-cloning.

**Clean all workspaces (workers will re-clone on next task):**
```powershell
Remove-Item -Recurse -Force projects\
```

**Clean a single worker's workspace:**
```powershell
Remove-Item -Recurse -Force projects\gemini-flash-1\
```

**Disk usage by worker:**
```powershell
Get-ChildItem projects\ -Directory | ForEach-Object {
    $size = (Get-ChildItem $_.FullName -Recurse -File | Measure-Object -Property Length -Sum).Sum / 1MB
    [PSCustomObject]@{Worker=$_.Name; SizeMB=[math]::Round($size,1)}
} | Format-Table -AutoSize
```

### Updating the Supervisor Binary

After pulling new code:
```powershell
git pull origin master
go build -o bin/supervisor.exe ./cmd/supervisor
# Stop the running supervisor (Ctrl+C or kill the process), then restart:
.\bin\supervisor.exe --config orchestrator.yml
```

### Adding a New Project

1. Add an entry under `projects:` in `orchestrator.yml`:

   ```yaml
   - name: new-project
     repo_owner: YourOrg
     repo_name: YourRepo
     conventions_path: ./CLAUDE.md
     branch_pattern: "feature/{ticket}-{summary}"
     commit_pattern: "{ticket}: {description}"
     labels: []
   ```

2. Ensure the target repo has a `CLAUDE.md` (or the file named in `conventions_path`) with `TestCommand`, `LintCommand`, and `FormatCommand` defined. The conventions parser reads these to run quality gates.

3. Restart the supervisor.

4. Verify the new project appears in startup output:
   ```
   Monitoring 2 project(s):
     - Mawar2/Kaimi
     - YourOrg/YourRepo
   ```

### Using the Backfill Utility

The `cmd/backfill` binary enqueues existing open PRs from a repository as `StatusReview` tasks so the supervisor's feedback loop can pick them up. Use this to bootstrap the feedback loop for PRs created before the system was deployed.

```powershell
# Build the backfill binary
go build -o bin/backfill.exe ./cmd/backfill

# Run (requires GITHUB_TOKEN)
.\bin\backfill.exe
```

The backfill utility reads `GITHUB_TOKEN` from the environment and targets `Mawar2/Kaimi` by default. It is a one-shot operation — safe to re-run (tasks are only created for PRs that don't already have a task).

### Changing Worker Count

Edit `orchestrator.yml`:

```yaml
worker_tiers:
  gemini_flash:
    max_workers: 8   # increase from 5
```

Then restart the supervisor. More workers = higher throughput but also higher GitHub API usage.

---

## 7. Security

### Token Handling

- **Never echo `$env:GITHUB_TOKEN`** in terminal sessions that may be recorded.
- Store the token only in `$PROFILE` (PowerShell profile) — do not commit it to `orchestrator.yml` or any tracked file.
- Rotate tokens periodically and after any team member departure.
- Use a dedicated GitHub account for the orchestrator (not a personal account) so token permissions are scoped and revocable.

### Worker Permission Scope

Workers run Claude Code with `--dangerously-skip-permissions` to allow headless file editing and git operations inside the isolated per-worker workspace. This flag is intentional and required for autonomous operation.

To audit or restrict what workers can do without disabling automation, override via the environment variable:

```powershell
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"   # allows edits, prompts for shell commands
$env:CLAUDE_PERMISSION_MODE = "plan"           # dry-run only, no actual changes
# Unset (default) = --dangerously-skip-permissions (full autonomy)
```

Workers only operate inside their isolated `./projects/<worker-id>/` directory. They do not have access to the supervisor's own working directory or the task queue files.

### Log Hygiene

- Supervisor logs go to stdout/stderr. Do not pipe these to files that are world-readable.
- The `GITHUB_TOKEN` value is never logged by the supervisor or workers.
- Task JSON files (`./tasks/*.json`) contain issue titles and bodies from GitHub — treat them as internal data; do not expose publicly.
- Worker logs may contain generated code snippets. Store or rotate them accordingly.

### Dependency Auditing

```powershell
# Check for known vulnerabilities in Go dependencies
go list -m all | ForEach-Object { go list -m -json $_.Split(" ")[0] } 2>$null

# Using govulncheck (install once: go install golang.org/x/vuln/cmd/govulncheck@latest)
govulncheck ./...
```

Run dependency audits before deploying a new version of the supervisor.
