# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06
**Audience:** Operators responsible for running and maintaining the supervisor.

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
|---|---|---|
| Go | 1.25.1+ | `go version` to verify |
| GitHub CLI (`gh`) | Any recent | `gh version` to verify; must be authenticated |
| `golangci-lint` | Any recent | `golangci-lint --version` to verify |
| `GITHUB_TOKEN` | — | Scopes: `repo`, `read:org` |
| Git | Any recent | `gh auth setup-git` wires credential helper |

### Token Setup

`GITHUB_TOKEN` is read from the environment by both `cmd/supervisor` and `cmd/backfill` via `os.Getenv("GITHUB_TOKEN")`.

```powershell
# PowerShell — verify it is set (do not echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — source your profile" }
```

If missing, the token is stored in `$PROFILE`. Re-source the profile in a fresh shell to restore it.

### Build

```powershell
# Build the supervisor binary
go build -o bin/supervisor.exe ./cmd/supervisor

# Build everything (validates all packages compile)
go build ./...

# Build the backfill utility
go build -o bin/backfill.exe ./cmd/backfill
```

Binaries are written to `bin/` (git-ignored). The Makefile defines `all`, `build`, `test`, `lint`, `fmt`, `run`, and `clean` targets, but **`make` is not installed on Windows** — use the raw `go` commands above instead.

### First-Run Verification

```powershell
# 1. Confirm token is set
if ($env:GITHUB_TOKEN) { "ok" } else { "NOT set" }

# 2. Confirm gh is authenticated
gh auth status

# 3. Build
go build -o bin/supervisor.exe ./cmd/supervisor

# 4. Dry-run: start supervisor, Ctrl+C after seeing "Supervisor running"
./bin/supervisor.exe --config orchestrator.yml
```

Expected startup lines:

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

### Normal State Indicators

| Log pattern | Meaning |
|---|---|
| `Supervisor: Found N open issues` | GitHub poll succeeded |
| `Supervisor: Skipping issue #N - already has PR` | No duplicate work created |
| `Supervisor: Skipping issue #N - already in queue` | Idempotent deduplication |
| `[gemini-flash-1] Claimed task …` | Worker picked up work |
| `[QualityGates] ✅ All quality checks passed` | PR is safe to create |
| `[workerID] Completed task … - PR #N created` | Successful end-to-end |
| `Supervisor: PR #N merged, marking task … as complete` | Lifecycle complete |

### Issue → PR Lifecycle

```
GitHub Issue (open)
        │
        ▼
Supervisor polls (every 60 s)
        │  Router classifies: Simple / Medium / Complex
        │  Tier assigned:    GeminiFlash / GeminiPro / Claude
        ▼
Task created  →  Status: Pending
        │
        ▼  Worker claims
Task claimed  →  Status: Claimed
        │
        ▼  Worker begins
In Progress   →  Status: InProgress
        │
        ├─► Quality gates FAIL  →  Status: Failed
        │
        ▼  Quality gates PASS
PR Created    →  Status: Review
        │
        ├─► AI review feedback  →  Fix task created  →  Status: Pending (iteration +1)
        │
        ├─► PR closed/rejected  →  Status: Failed
        │
        └─► PR merged           →  Status: Complete
```

### Worker Tiers

| Tier | Workers | IDs | Complexity |
|---|---|---|---|
| Gemini Flash | 5 | `gemini-flash-1` … `gemini-flash-5` | Simple |
| Gemini Pro | 3 | `gemini-pro-1` … `gemini-pro-3` | Medium |
| Claude | 2 | `claude-1`, `claude-2` | Complex |

### Task Queue Quick-Reference

All queue files live in `./tasks/` as `{uuid}.json`.

```powershell
# List all tasks and their statuses
Get-ChildItem tasks\*.json | ForEach-Object { (Get-Content $_) | ConvertFrom-Json | Select-Object id, issue_number, status, worker_id }

# Count tasks by status
Get-ChildItem tasks\*.json | ForEach-Object { (Get-Content $_) | ConvertFrom-Json } | Group-Object status | Select-Object Name, Count

# View a specific task
Get-Content tasks\<uuid>.json | ConvertFrom-Json

# All failed tasks with error messages
Get-ChildItem tasks\*.json | ForEach-Object { (Get-Content $_) | ConvertFrom-Json } | Where-Object { $_.status -eq "failed" } | Select-Object issue_number, error_msg
```

On Linux/macOS (or Git Bash):

```bash
# All task statuses
jq -r '.status' tasks/*.json | sort | uniq -c

# Failed tasks
jq -r 'select(.status == "failed") | "\(.issue_number): \(.error_msg)"' tasks/*.json

# Pending tasks by tier
jq -r 'select(.status == "pending") | "\(.tier): issue #\(.issue_number)"' tasks/*.json
```

### Day-to-Day Commands

```bash
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Test (drop -race: CGO off on this machine)
go test -cover ./...

# Lint
golangci-lint run ./...

# Format check
go fmt ./...
go vet ./...

# Start supervisor
./bin/supervisor.exe --config orchestrator.yml

# Backfill open PRs into queue
./bin/backfill.exe
```

---

## 3. Configuration Reference

### Full `orchestrator.yml` Schema

```yaml
# --------------------------------------------------------------------------
# Projects to monitor. Add one entry per GitHub repository.
# --------------------------------------------------------------------------
projects:
  - name: kaimi                       # Logical name (used in logs)
    repo_owner: Mawar2                # GitHub organization or user
    repo_name: Kaimi                  # Repository name
    conventions_path: ./CLAUDE.md     # Path to conventions file in the repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"  # Branch name template
    commit_pattern: "{ticket}_{description}"            # Commit message template
    labels: []                        # Optional: filter issues by these labels
                                      # e.g., ["orchestrator:pending"]

# --------------------------------------------------------------------------
# Worker tier settings. Each tier handles a complexity class.
# --------------------------------------------------------------------------
worker_tiers:
  gemini_flash:
    max_workers: 5                    # Concurrent workers for simple tasks
    model: gemini-flash-3.5           # Model identifier (informational)

  gemini_pro:
    max_workers: 3                    # Concurrent workers for medium tasks
    model: gemini-pro-3.5

  claude:
    max_workers: 2                    # Concurrent workers for complex tasks
    model: claude-sonnet-4.5

# --------------------------------------------------------------------------
# Supervisor tuning knobs.
# --------------------------------------------------------------------------
poll_interval_seconds: 60             # GitHub poll frequency (default: 60)
task_timeout_minutes: 120             # Max time per task before stall recovery (default: 120)
max_retry_attempts: 3                 # Max attempts before task is marked failed (default: 3)
task_queue_dir: ./tasks               # JSON queue directory (default: ./tasks)
```

### Branch and Commit Pattern Variables

| Variable | Replaced with |
|---|---|
| `{ticket}` | Issue number (e.g., `47`) |
| `{summary}` | Slug derived from issue title |
| `{description}` | Commit description text |

### Routing Heuristics

The `RuleBasedRouter` (`internal/orchestrator/router.go`) classifies issues with no API calls:

| Priority | Signal | Complexity |
|---|---|---|
| 1 | Title/body contains: `add comment`, `fix typo`, `update readme`, `docs:`, `documentation`, `add logging`, `update version` | Simple |
| 2 | Title/body matches: `architecture`, `design`, `refactor.*system`, `database`, `migration`, `security`, `breaking change`, `api redesign` | Complex |
| 3 | Body mentions file count ≤ 3 | Simple |
| 3 | Body mentions file count > 10 | Complex |
| 4 | Label contains `simple` or `easy` | Simple |
| 4 | Label contains `complex` or `hard` | Complex |
| 5 | _(default)_ | Medium |

### Label Filtering

Set `labels` in a project config to only process issues tagged with those labels. An empty list (`[]`) processes all open issues.

```yaml
projects:
  - name: kaimi
    repo_owner: Mawar2
    repo_name: Kaimi
    labels: ["orchestrator:pending"]  # Only pick up labelled issues
```

---

## 4. Troubleshooting

### 4.1 GitHub 401 Unauthorized

**Symptom:** `GitHub API returned status 401`

**Diagnosis:**
```powershell
if ($env:GITHUB_TOKEN) { "set" } else { "missing" }
gh auth status
```

**Fix:** Re-source the profile, then re-export the token:
```powershell
. $PROFILE
```
Required scopes: `repo`, `read:org`. Regenerate the token in GitHub Settings if scopes are missing.

---

### 4.2 GitHub 403 / Rate Limit

**Symptom:** `GitHub API returned status 403` or `rate limit exceeded`

**Diagnosis:** Check `X-RateLimit-Remaining` header. The REST client uses authenticated requests (5 000/hr). Each poll costs 1 request per project plus 1 per issue for PR-status check.

**Fix:**
- Increase `poll_interval_seconds` to reduce polling frequency.
- If truly rate-limited, wait for the reset (shown in `X-RateLimit-Reset` header as Unix timestamp).

---

### 4.3 Stalled Tasks

**Symptom:** Tasks stay in `claimed` or `in_progress` indefinitely.

**Diagnosis:**
```bash
jq -r 'select(.status == "claimed" or .status == "in_progress") | "\(.id[:8]) issue #\(.issue_number) claimed_at=\(.claimed_at) attempts=\(.attempts)"' tasks/*.json
```

**Cause:** Worker crashed or was killed while holding a task.

**Recovery:** The supervisor's stall monitor runs every 30 seconds. A task is released back to `pending` when `time.Since(started_at) > task_timeout_minutes`. After `max_retry_attempts` releases, the task is marked `failed`.

To force-release immediately, edit the JSON file directly:
```bash
# Reset a stalled task to pending
jq '.status = "pending" | .worker_id = "" | .attempts += 1' tasks/<uuid>.json > /tmp/fix.json
mv /tmp/fix.json tasks/<uuid>.json
```

---

### 4.4 Quality Gate Failures

**Symptom:** `quality gate failed - tests:` / `quality gate failed - linter:` in worker logs. Task moves to `failed`.

**Diagnosis:**
```bash
jq -r 'select(.error_msg | contains("quality gate")) | "\(.issue_number): \(.error_msg)"' tasks/*.json
```

**Cause:** The LLM-generated change broke tests or introduced lint errors.

**Fix options:**
- The task will be retried up to `max_retry_attempts` times automatically.
- If the issue is consistently failing, investigate the target repo's test/lint commands in its `CLAUDE.md` or `conventions_path` file.
- If the conventions file has wrong commands, update it in the target repo.

---

### 4.5 Missing Fix Tasks (Feedback Loop Not Triggering)

**Symptom:** PRs have AI review comments but no `pr_feedback` tasks are created.

**Diagnosis:**
1. Confirm PR is in `review` status in the queue.
2. Confirm the AI review comment starts with `## 🤖 AI Code Review (Gemini 2.5 Pro)`.
3. Check supervisor logs for `Supervisor: Found N tasks in Review status`.

**Common causes:**
- Task `status` is `complete` or `failed` rather than `review` — supervisor only monitors `review` tasks.
- `ReviewCommentID` already exists in another task (deduplication fired).
- PR was closed before the 120-second monitoring tick.

**Check deduplication:**
```bash
jq -r 'select(.review_comment_id != 0) | "\(.id[:8]) comment_id=\(.review_comment_id)"' tasks/*.json
```

---

### 4.6 Clone Failures

**Symptom:** `fatal: destination path already exists` or `authentication failed`.

**Destination path exists:**
- Workers use per-worker paths `./projects/{workerID}/{owner}/{repo}/`. This error should not occur with isolation enabled. If seen, a leftover workspace from a previous run is interfering.
- Fix: `Remove-Item -Recurse -Force projects\` (Windows) or `rm -rf projects/` (bash).

**Authentication failed:**
- `gh auth setup-git` must be run once to wire git's credential helper.
- Workers set `GIT_TERMINAL_PROMPT=0`, so any auth gap fails immediately.
- Fix: run `gh auth setup-git` and verify with `gh auth status`.

---

### 4.7 Duplicate Tasks

**Symptom:** Multiple tasks with the same `issue_number` in non-terminal states.

**Cause:** Supervisor created a task before a previous task's status updated.

**Diagnosis:**
```bash
jq -r '.issue_number' tasks/*.json | sort | uniq -d
```

**Fix:** Manually mark one as failed:
```bash
jq '.status = "failed" | .error_msg = "duplicate — manually resolved"' tasks/<uuid>.json > /tmp/fix.json
mv /tmp/fix.json tasks/<uuid>.json
```

---

### 4.8 Supervisor Exits Immediately

**Symptom:** Process starts but exits with non-zero code.

**Common causes:**

| Exit message | Fix |
|---|---|
| `Error loading config: …` | Check `orchestrator.yml` exists and is valid YAML |
| `invalid config: no projects configured` | Add at least one `projects` entry |
| `project X: repo_owner is required` | Ensure `repo_owner` and `repo_name` are set |
| `Error creating task queue: …` | Ensure `task_queue_dir` is writable |

---

### 4.9 CGO / Race Detector Build Failure

**Symptom:** `go test -race ./...` fails with `-race requires cgo`.

**Cause:** CGO is disabled on this machine (no C compiler configured).

**Fix:** Drop `-race` from test invocations:
```bash
go test -cover ./...   # works
go test -race ./...    # fails — do not use
```

---

### 4.10 Stale MCP Binary

**Symptom:** `MCP server returned status 400` in logs.

**Cause:** The legacy `mcp_client.go` path is used instead of the REST client.

**Status:** This is a known historical issue. The system now uses `GitHubRESTClient` exclusively. If you see 400 errors, confirm `cmd/supervisor/main.go` calls `ticket.NewGitHubRESTClient()` (not the MCP client). This was fixed in commit `05fd6bd`.

---

## 5. Monitoring

### Key Metrics via `jq`

```bash
# Task throughput: complete tasks
jq -r 'select(.status == "complete")' tasks/*.json | grep -c '"status"'

# Overall failure rate
TOTAL=$(ls tasks/*.json | wc -l)
FAILED=$(jq -r 'select(.status == "failed")' tasks/*.json | grep -c '"status"')
echo "Failure rate: $FAILED / $TOTAL"

# Quality gate failure rate
jq -r 'select(.error_msg | test("quality gate"))' tasks/*.json | grep -c '"error_msg"'

# Review iteration distribution (expect: ~70% at 0, ~20% at 1, ~8% at 2, ~2% at 3)
jq -r '.review_iteration' tasks/*.json | sort | uniq -c

# Tasks that hit max iteration limit
jq -r 'select(.error_msg | test("Max review iterations"))' tasks/*.json | grep -c '"error_msg"'

# Backfilled tasks
jq -r 'select(.metadata.backfilled == "true")' tasks/*.json | grep -c '"backfilled"'

# Fix tasks created by feedback loop
jq -r 'select(.metadata.task_type == "pr_feedback")' tasks/*.json | grep -c '"task_type"'

# Average attempts per completed task
jq -r 'select(.status == "complete") | .attempts' tasks/*.json | awk '{s+=$1; c++} END {print s/c}'
```

### Log Patterns Table

| Pattern | Severity | Action |
|---|---|---|
| `Poll failed` | Warning | Transient GitHub error; retries automatically |
| `quality gate failed` | Info | Expected; task retried up to max_retry_attempts |
| `Task stalled` | Warning | Worker died; task released back to queue |
| `has exceeded max retry attempts` | Error | Investigate: check `error_msg` in task JSON |
| `Max review iterations (3) exceeded` | Warning | AI review feedback loop didn't converge |
| `Failed to get PR` | Warning | GitHub API issue; retries next monitoring tick |
| `PR closed without merging` | Info | Human rejected PR |
| `PR merged, marking task as complete` | Info | Success |

### Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Failure rate (all causes) | > 20% | > 40% |
| Quality gate failure rate | > 40% | > 60% |
| Stalled tasks (active at any time) | > 3 | > 5 |
| Max-iteration failures | > 5% | > 10% |
| Tasks pending > 30 minutes | > 5 | > 15 |

---

## 6. Maintenance

### Task Queue Cleanup

Tasks accumulate in `./tasks/`. Archive or delete terminal tasks periodically.

```bash
# List terminal tasks older than 7 days (bash)
find tasks/ -name "*.json" -mtime +7 | xargs jq -r 'select(.status == "complete" or .status == "failed") | .id'

# Archive completed tasks to a dated folder
mkdir -p tasks/archive/$(date +%Y-%m-%d)
jq -r 'select(.status == "complete" or .status == "failed") | .id' tasks/*.json \
  | xargs -I{} mv tasks/{}.json tasks/archive/$(date +%Y-%m-%d)/

# Delete failed tasks with known resolution
jq -r 'select(.status == "failed" and (.error_msg | test("quality gate"))) | .id' tasks/*.json \
  | xargs -I{} rm tasks/{}.json
```

**PowerShell equivalent:**
```powershell
# Archive all terminal tasks
$archive = "tasks\archive\$(Get-Date -Format 'yyyy-MM-dd')"
New-Item -ItemType Directory -Force $archive | Out-Null
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = (Get-Content $_) | ConvertFrom-Json
    if ($t.status -eq "complete" -or $t.status -eq "failed") {
        Move-Item $_.FullName $archive
    }
}
```

### Workspace Cleanup

Per-worker workspaces live in `./projects/`. Clean them when disk is low or before a fresh test run.

```powershell
# Windows
Remove-Item -Recurse -Force projects\

# Bash
rm -rf projects/
```

Workers re-clone on next task claim. Expected disk usage: ~200 MB per worker × 10 workers = ~2 GB.

### Updating the Supervisor Binary

```powershell
# Rebuild from source
go build -o bin/supervisor.exe ./cmd/supervisor

# Verify it builds cleanly
go build ./...
go vet ./...
```

No migrations needed — the JSON task queue is forward-compatible with new fields (they default to zero values on read).

### Adding a New Project

1. Add an entry to `orchestrator.yml`:
   ```yaml
   projects:
     - name: new-project
       repo_owner: YourOrg
       repo_name: YourRepo
       conventions_path: ./CLAUDE.md
       branch_pattern: "{ticket}-{summary}"
       commit_pattern: "[{ticket}] {description}"
       labels: []
   ```
2. Ensure `GITHUB_TOKEN` has `repo` access to `YourOrg/YourRepo`.
3. Restart the supervisor.

### Backfill Utility

Use `cmd/backfill` to seed the task queue with open PRs from Kaimi that predate the supervisor. This allows the AI review feedback loop to monitor existing PRs.

```powershell
# Build and run backfill
go build -o bin/backfill.exe ./cmd/backfill
./bin/backfill.exe
```

The backfill tool:
- Fetches all open (non-draft) PRs from `Mawar2/Kaimi`
- Creates tasks with `status: review` so the supervisor's PR monitor picks them up
- Sets `metadata.backfilled = "true"` for tracking
- Skips draft PRs
- Infers complexity from PR size (lines added + deleted)

Run backfill once after initial deployment or after a supervisor downtime to catch up.

---

## 7. Security

### Token Handling

- `GITHUB_TOKEN` must never appear in logs, task files, or source code.
- The supervisor reads it via `os.Getenv("GITHUB_TOKEN")` at startup — it is not written to disk.
- Task JSON files (`./tasks/*.json`) do not contain the token.
- Store the token in your shell profile (`$PROFILE`) or a secrets manager; never commit it.

### Worker Permission Scope

Workers execute:
```
claude --print --dangerously-skip-permissions
```

The `--dangerously-skip-permissions` flag allows the headless agent to edit files, run git, run tests, and call `gh` within its isolated workspace (`./projects/{workerID}/`). The scope is limited to that workspace directory.

To audit or restrict worker actions without running in full autonomous mode:
```powershell
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"   # prompts for edits only
$env:CLAUDE_PERMISSION_MODE = "plan"          # dry-run, no file changes
```

Remove the variable (or set to `""`) to restore full autonomous mode.

### Log Hygiene

- Do not redirect supervisor stdout to a file without ensuring the file is not world-readable.
- `supervisor_test.log` in the repo root is git-ignored; rotate or delete it periodically.
- The `logs_path` field in task JSON points to worker log files — ensure those paths are not publicly accessible.

### Dependency Auditing

```bash
# Check for known vulnerabilities in Go dependencies
go list -m all | head -20    # survey direct + indirect deps

# govulncheck (install once: go install golang.org/x/vuln/cmd/govulncheck@latest)
govulncheck ./...
```

Run dependency audits before each production deployment and after `go get` updates.
