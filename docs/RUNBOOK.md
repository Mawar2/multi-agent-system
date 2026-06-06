# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06
**Audience:** Operators running or maintaining the orchestration system in production.

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
| GitHub CLI (`gh`) | authenticated as the account that will open PRs |
| `golangci-lint` | on PATH; required for quality gates in target repos |
| `GITHUB_TOKEN` env var | `repo` + `read:org` scopes |
| Git credential helper | configured via `gh auth setup-git` |
| Claude Code CLI (`claude`) | on PATH; used by Claude-tier workers |

### 1.1 Set Up GitHub Authentication

The supervisor reads the GitHub token from `$env:GITHUB_TOKEN`. It must be set before running.

```powershell
# Verify token is present (never echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — check your `$PROFILE`" }
```

If not set, source your profile:

```powershell
. $PROFILE
```

Also configure git's credential helper so workers can clone and push without prompts:

```powershell
gh auth setup-git
```

Verify GitHub CLI authentication:

```powershell
gh auth status
```

Expected output includes `Logged in to github.com as <your-account>`.

### 1.2 Build the Supervisor

```powershell
# From repository root
go build -o bin/supervisor.exe ./cmd/supervisor
```

The binary is git-ignored. Rebuild after any source change.

To verify all packages compile:

```powershell
go build ./...
```

### 1.3 Configure the System

Copy the example config and edit for your environment:

```powershell
Copy-Item orchestrator.example.yml orchestrator.yml
```

Edit `orchestrator.yml` — see [Section 3](#3-configuration-reference) for the full schema.

Minimum required change: set `repo_owner` and `repo_name` under `projects`.

### 1.4 First Run

```powershell
.\bin\supervisor.exe --config orchestrator.yml
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

[gemini-flash-1] Worker started (tier: gemini-flash)
...
[claude-2] Worker started (tier: claude)

Supervisor: Starting main loop
Supervisor: Polling project Mawar2/Kaimi
```

Verify first task by checking the task queue directory:

```powershell
Get-ChildItem .\tasks\ -Filter *.json | Measure-Object | Select-Object -ExpandProperty Count
```

Expected: a positive number after the first poll cycle (60s).

---

## 2. Operation

### 2.1 Normal-State Log Indicators

| Log line | Meaning |
|---|---|
| `Supervisor: Polling project Mawar2/Kaimi` | Healthy poll cycle started |
| `Supervisor: Found N open issues` | Issues discovered; N may be 0 between cycles |
| `Supervisor: Routed issue #N - complexity: X, tier: Y` | Issue accepted and enqueued |
| `[worker-id] Claimed task UUID (issue #N)` | Worker picked up work |
| `[WorkspaceManager] Successfully cloned Mawar2/Kaimi` | First-time clone complete |
| `[QualityGates] ✅ All quality checks passed` | PR will be created |
| `[worker-id] Completed task UUID - PR #N created` | Success — PR is open |
| `Supervisor: PR #N already exists for issue #N` | Duplicate guard; normal |
| `Supervisor: Monitoring PRs for AI review feedback` | Feedback loop polling |

### 2.2 Issue → PR Lifecycle

```
GitHub Issue
     │
     ▼  (every 60s)
Supervisor polls GitHub
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
  └── Clones repo to ./projects/{worker-id}/{owner}/{repo}/
  └── Creates branch feature/issue-{number}
  └── Runs Claude Code CLI to implement solution
  └── Runs quality gates (tests, linter, formatter, build)
     │
     ├─── FAIL → task marked failed, PR NOT created
     │
     └─── PASS → PR created on GitHub (status: review)
               │
               ▼  (every 120s)
          Supervisor checks PR for AI review comment
               │
               ├─ No feedback → no action
               │
               └─ Feedback detected → pr_feedback task enqueued
                        │
                        ▼
                   Worker claims fix task
                   └── Checks out existing branch
                   └── Applies targeted fixes
                   └── Updates PR (status: review again)
                        │
                        └─ Max 3 iterations, then task fails
```

### 2.3 Worker Tiers

| Tier | Workers | Handles | Backend |
|---|---|---|---|
| `gemini-flash` | 5 (`gemini-flash-1..5`) | Simple issues | Claude Code CLI (Gemini bridge pending) |
| `gemini-pro` | 3 (`gemini-pro-1..3`) | Medium issues | Claude Code CLI (Gemini bridge pending) |
| `claude` | 2 (`claude-1..2`) | Complex issues | Claude Code CLI |

> Note: All tiers currently use the Claude Code CLI backend. The Gemini Antigravity bridge (`USE_GEMINI_WORKER=1`) is available but not production-ready.

### 2.4 Task Queue Inspection (PowerShell)

```powershell
# Count tasks by status
Get-ChildItem .\tasks\ -Filter *.json |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
  Group-Object status | Select-Object Name, Count

# List pending tasks
Get-ChildItem .\tasks\ -Filter *.json |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
  Where-Object { $_.status -eq 0 } |
  Select-Object id, issue_number, title, tier

# List in-progress tasks
Get-ChildItem .\tasks\ -Filter *.json |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
  Where-Object { $_.status -eq 2 } |
  Select-Object id, worker_id, issue_number, started_at

# View a specific task
Get-Content .\tasks\<uuid>.json | ConvertFrom-Json | Format-List
```

Status integer values: `0=pending`, `1=claimed`, `2=in_progress`, `3=review`, `4=complete`, `5=failed`.

### 2.5 Day-to-Day Commands

```powershell
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Test (drop -race; CGO is off on this machine)
go test -cover ./...

# Lint
golangci-lint run ./...

# Vet
go vet ./...

# Start supervisor
.\bin\supervisor.exe --config orchestrator.yml

# Graceful stop
Ctrl+C
```

---

## 3. Configuration Reference

### 3.1 Full Annotated Schema

```yaml
# orchestrator.yml
projects:
  - name: kaimi                           # Short identifier used in logs
    repo_owner: Mawar2                    # GitHub org or user
    repo_name: Kaimi                      # GitHub repository name
    conventions_path: ./CLAUDE.md         # Path to CLAUDE.md in THIS repo (used for prompt building)
    branch_pattern: "feature/KAI-{ticket}-{summary}"  # Branch naming pattern
    commit_pattern: "{ticket}_{description}"           # Commit message pattern
    labels: []                            # Issue label filter; empty = all issues

worker_tiers:
  gemini_flash:
    max_workers: 5                        # Parallel simple-task workers
    model: gemini-flash-3.5               # Informational; actual model set by backend
  gemini_pro:
    max_workers: 3                        # Parallel medium-task workers
    model: gemini-pro-3.5
  claude:
    max_workers: 2                        # Parallel complex-task workers
    model: claude-sonnet-4.5

poll_interval_seconds: 60                 # GitHub poll frequency (default: 60)
task_timeout_minutes: 120                 # Worker timeout per task (default: 120)
max_retry_attempts: 3                     # Retries before task is marked failed (default: 3)
task_queue_dir: ./tasks                   # JSON queue directory (default: ./tasks)
```

### 3.2 Branch and Commit Pattern Variables

| Variable | Replaced with |
|---|---|
| `{ticket}` | GitHub issue number |
| `{summary}` | Sanitized lowercase issue title (spaces→hyphens) |
| `{description}` | Same as `{summary}` |

Example with `branch_pattern: "feature/KAI-{ticket}-{summary}"` and issue #47 "Add logging":
→ `feature/KAI-47-add-logging`

### 3.3 Routing Heuristics

The `RuleBasedRouter` (`internal/orchestrator/router.go`) classifies issues in this order:

| Priority | Signal | Result |
|---|---|---|
| 1 | Title/body contains: `add comment`, `fix typo`, `update readme`, `docs:`, `[docs]`, `documentation`, `add logging`, `format code`, `update version`, `add godoc` | Simple |
| 2 | Title/body matches (regex): `architecture`, `design`, `refactor.*system`, `implement.*agent`, `database`, `migration`, `schema change`, `security`, `authentication`, `authorization`, `breaking change`, `api redesign` | Complex |
| 3 | Body contains `files:` or `affected files` and extracted count ≤ 3 | Simple |
| 4 | Body contains `files:` or `affected files` and extracted count > 10 | Complex |
| 5 | Issue label contains `simple` or `easy` | Simple |
| 6 | Issue label contains `complex` or `hard` | Complex |
| 7 | (no match) | Medium (default) |

Tier mapping: `simple → gemini-flash`, `medium → gemini-pro`, `complex → claude`.

### 3.4 Label Filtering

Setting `labels` in `orchestrator.yml` restricts which issues the supervisor picks up:

```yaml
projects:
  - name: kaimi
    repo_owner: Mawar2
    repo_name: Kaimi
    labels: ["orchestrator:pending"]   # Only process issues with this label
```

Leave `labels: []` (or omit) to process all open issues.

---

## 4. Troubleshooting

### 4.1 401 Unauthorized — GitHub API

**Symptom:** `GitHub API returned status 401` in logs.

**Steps:**
1. Verify token is set: `if ($env:GITHUB_TOKEN) { "set" } else { "missing" }`
2. Re-source profile: `. $PROFILE`
3. Confirm token scopes include `repo` and `read:org`:
   ```powershell
   gh auth status
   ```
4. If token is expired, generate a new one at GitHub → Settings → Developer settings → Personal access tokens.

### 4.2 GitHub Rate Limit Exceeded

**Symptom:** `GitHub API returned status 403` with rate-limit body, or supervisor poll loop slows.

**Steps:**
1. Check remaining quota:
   ```powershell
   gh api rate_limit | ConvertFrom-Json | Select-Object -ExpandProperty rate
   ```
2. Increase `poll_interval_seconds` in `orchestrator.yml` to reduce API calls (e.g., 120 or 300).
3. If using label filtering, ensure labels match exactly — mismatches cause extra list calls.

### 4.3 Stalled Tasks (in_progress for > 2 hours)

**Symptom:** Task stuck at `status=2` (`in_progress`) long after `task_timeout_minutes`.

**Steps:**
1. Find stalled tasks:
   ```powershell
   $timeout = (Get-Date).AddHours(-2)
   Get-ChildItem .\tasks\ -Filter *.json |
     ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
     Where-Object { $_.status -eq 2 -and [datetime]$_.started_at -lt $timeout } |
     Select-Object id, worker_id, issue_number, started_at
   ```
2. Check if the worker process is still running (look for `claude` subprocess).
3. To manually release a stalled task back to pending:
   ```powershell
   $task = Get-Content .\tasks\<uuid>.json | ConvertFrom-Json
   $task.status = 0          # pending
   $task.worker_id = ""
   $task | ConvertTo-Json -Depth 10 | Set-Content .\tasks\<uuid>.json -Encoding utf8
   ```
4. Increase `task_timeout_minutes` in config if tasks legitimately take longer.

### 4.4 Quality Gate Failures

**Symptom:** Task fails with `quality gate failed - tests:` or similar.

**Steps:**
1. List quality gate failures:
   ```powershell
   Get-ChildItem .\tasks\ -Filter *.json |
     ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
     Where-Object { $_.error_msg -like "*quality gate*" } |
     Select-Object issue_number, error_msg
   ```
2. Inspect the full error message for which check failed and its output.
3. Check if the target repo's test/lint commands are correctly set in its `CLAUDE.md` / `CONVENTIONS.md`.
4. Run the quality gate manually in the worker workspace to reproduce:
   ```powershell
   cd .\projects\gemini-flash-1\Mawar2\Kaimi\
   go test ./...
   golangci-lint run ./...
   ```
5. If the issue is a pre-existing failure in the target repo (not caused by the worker), add the issue to the target repo's known-issues list or exclude it from the orchestrator with a label filter.

### 4.5 AI Review Feedback Tasks Not Being Created

**Symptom:** PRs receive AI review comments but no `pr_feedback` tasks appear in `./tasks/`.

**Steps:**
1. Confirm the AI review comment starts with the expected prefix:
   `## 🤖 AI Code Review (Gemini 2.5 Pro)`
   The supervisor matches this prefix exactly.
2. Check supervisor logs for `Supervisor: Monitoring PRs for AI review feedback`.
3. Verify `GITHUB_TOKEN` has read access to PR comments.
4. Check if `review_comment_id` is already recorded (deduplication):
   ```powershell
   Get-ChildItem .\tasks\ -Filter *.json |
     ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
     Where-Object { $_.review_comment_id -ne $null -and $_.review_comment_id -ne 0 } |
     Select-Object id, pr_number, review_comment_id, review_iteration
   ```
5. Check that the parent task is in `status=3` (review) — the feedback loop only monitors review-status tasks.

### 4.6 Clone Failures

**Symptom:** `fatal: destination path already exists` or `fatal: repository not found`.

**Destination already exists:**
- This indicates a leftover workspace from a previous run.
- Clean up: `Remove-Item -Recurse -Force .\projects\`
- The per-worker mutex prevents this during normal operation; it only occurs after abnormal termination.

**Repository not found:**
- Confirm `repo_owner` and `repo_name` in `orchestrator.yml` are correct.
- Confirm `gh auth status` shows the account has read access to the repo.
- Test manually: `gh repo clone Mawar2/Kaimi`

**GIT_TERMINAL_PROMPT=0 hangs avoided:**
Workers set `GIT_TERMINAL_PROMPT=0` so auth failures fail fast. If cloning hangs, check that `gh auth setup-git` has run.

### 4.7 Duplicate Tasks for Same Issue

**Symptom:** Multiple tasks exist for the same issue number.

**Steps:**
1. Find duplicates:
   ```powershell
   Get-ChildItem .\tasks\ -Filter *.json |
     ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json } |
     Group-Object issue_number |
     Where-Object { $_.Count -gt 1 } |
     Select-Object Name, Count
   ```
2. The supervisor guards against this via `searchPullRequests` — if a PR already exists for the issue, no new task is enqueued. This can fail if:
   - PR was created outside the system (different branch name pattern).
   - GitHub search has indexing delay.
3. Cancel the duplicate task by setting its status to `5` (failed):
   ```powershell
   $task = Get-Content .\tasks\<duplicate-uuid>.json | ConvertFrom-Json
   $task.status = 5
   $task.error_msg = "Duplicate — cancelled manually"
   $task | ConvertTo-Json -Depth 10 | Set-Content .\tasks\<duplicate-uuid>.json -Encoding utf8
   ```

### 4.8 Supervisor Exits Immediately

**Symptom:** Supervisor starts and exits without error.

**Steps:**
1. Run with verbose output and check stderr:
   ```powershell
   .\bin\supervisor.exe --config orchestrator.yml 2>&1
   ```
2. Common causes:
   - `orchestrator.yml` missing or malformed → `Error loading config`
   - No projects configured → `no projects configured`
   - `task_queue_dir` not writable → `Error creating task queue`
3. Verify config file exists and parses:
   ```powershell
   Get-Content orchestrator.yml
   ```

### 4.9 Build Fails: "requires cgo" / Race Detector

**Symptom:** `go test -race` fails with `-race requires cgo`.

**Cause:** CGO is disabled on this machine; no C compiler is installed.

**Fix:** Drop the `-race` flag:
```powershell
go test -cover ./...
```

The `Makefile` target `make test` uses `-race` — do not use `make test` on this machine. Use the raw `go test` command above.

### 4.10 Pre-existing Lint Findings (golangci-lint)

**Symptom:** `golangci-lint run ./...` reports findings not related to recent changes.

**Known pre-existing findings** (as of 2026-06-06):
- `internal/ticket/github_rest_client.go`: 3× `errcheck` (unchecked `resp.Body.Close`)
- `internal/ticket/github_rest_client.go`: 1× `staticcheck QF1003`

These findings pre-date current work. Fix them opportunistically, but do not block PRs on them.

---

## 5. Monitoring

### 5.1 Key Metrics (PowerShell Queries)

```powershell
# Total tasks by status
$tasks = Get-ChildItem .\tasks\ -Filter *.json |
  ForEach-Object { Get-Content $_.FullName | ConvertFrom-Json }

# Throughput: completed tasks
($tasks | Where-Object { $_.status -eq 4 }).Count

# Failure rate
$failed = ($tasks | Where-Object { $_.status -eq 5 }).Count
$total  = $tasks.Count
if ($total -gt 0) { [math]::Round($failed / $total * 100, 1) }

# Quality gate failure rate
($tasks | Where-Object { $_.error_msg -like "*quality gate*" }).Count

# Review iteration distribution (0=original, 1-3=fix rounds)
$tasks | Group-Object review_iteration | Sort-Object Name | Select-Object Name, Count

# Backfilled task count
($tasks | Where-Object { $_.metadata.backfilled -eq "true" }).Count

# Max-iterations hit (feedback loop exhausted)
($tasks | Where-Object { $_.error_msg -like "*Max review iterations*" }).Count

# Active workers (in-progress tasks)
($tasks | Where-Object { $_.status -eq 2 }) | Select-Object worker_id, issue_number, started_at
```

### 5.2 Log Patterns and Meanings

| Pattern | Severity | Meaning |
|---|---|---|
| `Error claiming task` | Warning | Worker poll error; retries automatically |
| `Error executing task` | Warning | Task execution failed; task released |
| `Error releasing task` | Error | Task may be orphaned; check manually |
| `quality gate failed` | Info | PR rejected by gate; expected behavior |
| `GitHub API returned status 401` | Error | Token expired or missing |
| `GitHub API returned status 403` | Error | Rate limit or permission denied |
| `GitHub API returned status 404` | Error | Repo or resource not found |
| `Max review iterations` | Warning | Feedback loop hit limit; manual review needed |
| `Supervisor error:` | Fatal | Main loop crashed; restart supervisor |

### 5.3 Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Failure rate | > 20% | > 40% |
| Quality gate failure rate | > 35% | > 50% |
| Max-iterations rate | > 5% | > 10% |
| Stalled tasks (in_progress > 2h) | > 2 | > 5 |
| Tasks pending > 30 min | > 10 | > 25 |

---

## 6. Maintenance

### 6.1 Task Queue Archive and Cleanup

Tasks accumulate in `./tasks/`. Archive completed tasks periodically:

```powershell
# Archive completed and failed tasks older than 7 days
$cutoff = (Get-Date).AddDays(-7)
New-Item -ItemType Directory -Force .\tasks\archive\

Get-ChildItem .\tasks\ -Filter *.json |
  ForEach-Object {
    $task = Get-Content $_.FullName | ConvertFrom-Json
    $isTerminal = $task.status -eq 4 -or $task.status -eq 5
    $isOld = [datetime]$task.completed_at -lt $cutoff
    if ($isTerminal -and $isOld) {
      Move-Item $_.FullName .\tasks\archive\
    }
  }
```

### 6.2 Workspace Cleanup

Worker workspaces live in `./projects/`. They persist between runs for faster subsequent clones (pull instead of full clone). Clean them when disk space is low or after major repo restructuring:

```powershell
Remove-Item -Recurse -Force .\projects\
```

Workers will re-clone on the next task. Expect slower first tasks after cleanup.

### 6.3 Update Supervisor Binary

```powershell
# Pull latest code
git pull origin master

# Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor

# Restart (stop existing supervisor first with Ctrl+C)
.\bin\supervisor.exe --config orchestrator.yml
```

### 6.4 Adding a New Project

1. Add a new entry under `projects` in `orchestrator.yml`:
   ```yaml
   - name: my-new-project
     repo_owner: MyOrg
     repo_name: MyRepo
     conventions_path: ./CLAUDE.md
     branch_pattern: "feature/{ticket}-{summary}"
     commit_pattern: "{ticket}_{description}"
     labels: []
   ```
2. Ensure the repository has a `CLAUDE.md` or `CONVENTIONS.md` with test/lint/format/build commands (used by quality gates and prompt building).
3. Restart the supervisor.

New project issues are picked up on the next poll cycle.

### 6.5 Backfill Existing PRs Into the Feedback Loop

To bring existing open PRs under feedback loop monitoring, use the backfill utility:

```powershell
# Build backfill tool
go build -o bin/backfill.exe ./cmd/backfill

# Run (reads GITHUB_TOKEN from env, queue from ./tasks)
.\bin\backfill.exe
```

The backfill tool:
- Fetches open PRs from `Mawar2/Kaimi` (hardcoded)
- Skips draft PRs
- Creates `StatusReview` tasks so the supervisor's feedback loop monitors them
- Tags tasks with `backfilled: "true"` metadata

Output example:
```
Fetching open PRs from Mawar2/Kaimi...
Found 12 open PRs in Kaimi

✅ Created task abc12345 for PR #47: feat: add logging (complexity: 1, tier: gemini-flash)
...

============================================================
Backfill complete!
  Created: 11 tasks
  Skipped: 1 tasks (drafts)
  Total:   12 PRs
============================================================
```

### 6.6 Dependency Audit

```powershell
# List direct dependencies
go list -m all

# Check for known vulnerabilities
go list -json -m all | nancy sleuth   # requires nancy tool
# or
govulncheck ./...                      # requires govulncheck tool
```

Current dependencies:
- `gopkg.in/yaml.v3` — config parsing
- `github.com/google/uuid` — task ID generation

---

## 7. Security

### 7.1 GitHub Token Handling

- `GITHUB_TOKEN` is read from the environment variable only (`os.Getenv("GITHUB_TOKEN")`).
- **Never log or print the token value.** Verify presence with `if ($env:GITHUB_TOKEN) { "set" }`.
- Store the token in the PowerShell profile (`$PROFILE`) or a secrets manager; never commit it to the repository.
- Token is git-ignored via `.gitignore` patterns; `orchestrator.yml` does not contain the token.
- Required scopes: `repo`, `read:org`. Do not grant broader scopes than necessary.

### 7.2 Worker Permission Scope

Claude-tier workers run:

```
claude --print --dangerously-skip-permissions
```

This allows the headless agent to read/write files and execute shell commands within its isolated workspace (`./projects/{worker-id}/`). Implications:

- Workers operate on isolated clones, not the orchestrator's own source tree.
- A compromised worker could run arbitrary code inside its workspace directory.
- Workers should not be granted host-level network access beyond what git/gh require.

To operate with reduced permissions during testing:

```powershell
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"   # prompts for shell commands
$env:CLAUDE_PERMISSION_MODE = "plan"          # read-only dry run
```

Unset this variable (or leave it unset) for production autonomy.

### 7.3 Log Hygiene

- Do not log issue titles or PR bodies verbatim if they may contain secrets or PII.
- Task JSON files in `./tasks/` contain issue descriptions — restrict directory permissions to the operator account.
- Worker logs in `./projects/{worker-id}/` may contain code changes and command output — treat them as internal artifacts.

### 7.4 Dependency Security

```powershell
# Verify go.sum is not tampered (run after any dependency change)
go mod verify

# Check for known vulnerabilities
govulncheck ./...
```

Keep Go toolchain and dependencies up to date. Use `go get -u ./...` (test first in a branch) when security patches are available.
