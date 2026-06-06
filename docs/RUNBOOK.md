# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06  
**Audience:** Operators running the supervisor in production

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

| Requirement | Version | Purpose |
|---|---|---|
| Go | 1.25.1+ | Build the supervisor binary |
| `gh` CLI | Any recent | PR creation, git auth helper |
| `git` | Any | Workspace cloning |
| `golangci-lint` | Any | Code linting in workers |
| GitHub token | — | API access (see below) |

### GitHub Token Setup

The token must have `repo` and `read:org` scopes.

**PowerShell (Windows):**
```powershell
# Verify token is set
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — source your profile" }

# If missing, re-source your profile
. $PROFILE
```

**Authenticate the gh CLI** (one-time setup):
```bash
gh auth login
gh auth setup-git   # configures git credential helper for private repo clones
```

### Build Steps

```bash
# Build the supervisor binary (output: bin/supervisor.exe on Windows)
go build -o bin/supervisor.exe ./cmd/supervisor

# Build the backfill utility (optional)
go build -o bin/backfill.exe ./cmd/backfill

# Verify all packages compile
go build ./...
```

> **Note:** `make` is not required. Use the `go` commands above directly.

### Configuration

Copy the example config and customize it:
```bash
cp orchestrator.example.yml orchestrator.yml
# Edit orchestrator.yml with your project settings
```

### First-Run Verification

```bash
# 1. Ensure task queue directory exists (created automatically on first run)
ls tasks/ 2>/dev/null || echo "tasks/ will be created on first run"

# 2. Start the supervisor
./bin/supervisor.exe --config orchestrator.yml

# 3. Verify expected startup log lines appear:
#    "Loading configuration from orchestrator.yml..."
#    "Started 10 workers"
#    "Supervisor running. Press Ctrl+C to stop."
#    "[gemini-flash-1] Worker started (tier: gemini-flash)"

# 4. After the first poll (60s), verify a task was created:
ls tasks/*.json | head -5
```

---

## 2. Operation

### Normal State Indicators

When the system is running correctly, you will see:

```
Supervisor: Polling project Mawar2/Kaimi
Supervisor: Found N open issues in Mawar2/Kaimi
[gemini-flash-1] Claimed task <uuid> (issue #N)
[QualityGates] ✅ All quality checks passed - safe to create PR
[gemini-flash-1] Completed task <uuid> - PR #N created
```

Absence of `[QualityGates]` lines for several minutes after task claim may indicate a stalled worker.

### Issue → PR Lifecycle

```
GitHub Issue (open)
      │
      ▼ poll every 60s
Supervisor.Run()
      │ route by complexity
      ▼
Task enqueued (status: pending)
      │
      ▼ worker polls every 5s
Worker.Claim()     → status: claimed
      │
      ▼
Worker.Execute()   → status: in_progress
  ├── Clone / update workspace
  ├── Create feature branch
  ├── Run Claude Code CLI
  ├── Run quality gates
  │     ├── tests pass?  → continue
  │     ├── linter pass? → continue
  │     ├── fmt pass?    → continue
  │     └── build pass?  → continue (if configured)
  └── Create PR via gh
      │
      ▼
status: review
      │
      ▼ supervisor polls PRs every 120s
AI review comment detected?
  ├── No  → Human reviews PR manually
  └── Yes → Create pr_feedback task (iter 1-3)
              └── Worker fixes PR → loop
```

### Worker Tiers

| Tier | Worker IDs | Max Workers | Handles |
|---|---|---|---|
| `gemini-flash` | `gemini-flash-1` … `gemini-flash-5` | 5 | Simple issues (docs, typos, small changes) |
| `gemini-pro` | `gemini-pro-1` … `gemini-pro-3` | 3 | Medium issues (features, refactors) |
| `claude` | `claude-1`, `claude-2` | 2 | Complex issues (architecture, security) |

### Task Queue Commands

```bash
# List all tasks and their status
jq -r '[.id[:8], .status, .issue_number, .title] | @tsv' tasks/*.json

# Count tasks by status
jq -r '.status' tasks/*.json | sort | uniq -c

# Show pending tasks
jq -r 'select(.status == "pending") | "\(.id[:8])  issue #\(.issue_number)  \(.title)"' tasks/*.json

# Show in-progress tasks
jq -r 'select(.status == "in_progress") | "\(.id[:8])  \(.worker_id)  issue #\(.issue_number)"' tasks/*.json

# Show completed tasks (today)
jq -r 'select(.status == "complete") | "\(.id[:8])  PR #\(.pr_number)  \(.title)"' tasks/*.json
```

### Dev Commands

```bash
# Run tests (no -race on this machine — CGO is off)
go test -cover ./...

# Run linter
golangci-lint run ./...

# Vet all packages
go vet ./...
```

---

## 3. Configuration Reference

### Full `orchestrator.yml` Schema

```yaml
# One or more GitHub repositories to monitor
projects:
  - name: kaimi                          # Internal project name (used in logs)
    repo_owner: Mawar2                   # GitHub org or user
    repo_name: Kaimi                     # Repository name
    conventions_path: ./CLAUDE.md        # Path to conventions file IN the target repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"  # Branch naming template
    commit_pattern: "{ticket}_{description}"           # Commit message template
    labels: []                           # Issue label filter; empty = all issues

# Worker pool sizes and model assignments
worker_tiers:
  gemini_flash:
    max_workers: 5                       # Number of Gemini Flash workers
    model: gemini-flash-3.5              # Model identifier (informational for now)

  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5

  claude:
    max_workers: 2
    model: claude-sonnet-4.5

# Supervisor tuning
poll_interval_seconds: 60               # How often to poll GitHub for new issues
task_timeout_minutes: 120               # Max time a worker can hold a task
max_retry_attempts: 3                   # Retries before marking task failed
task_queue_dir: ./tasks                 # Directory for JSON task files
```

### Routing Heuristics

The `RuleBasedRouter` classifies each issue without making API calls:

| Signal | Simple | Complex | Default |
|---|---|---|---|
| Title/body keywords | `fix typo`, `docs:`, `add comment`, `update readme`, `add logging` | `architecture`, `migration`, `security`, `authentication`, `breaking change`, `api redesign` | — |
| File count in body | ≤ 3 files mentioned | > 10 files mentioned | — |
| Issue labels | `simple`, `easy` | `complex`, `hard` | — |
| Fallback | — | — | Medium |

### Branch and Commit Pattern Variables

| Variable | Value |
|---|---|
| `{ticket}` | Issue number (e.g., `47`) |
| `{summary}` | Slugified issue title (e.g., `add-logging`) |
| `{description}` | Short description for commit message |

### Label Filtering

Set `labels` in a project config to restrict which issues the supervisor picks up:

```yaml
projects:
  - name: kaimi
    repo_owner: Mawar2
    repo_name: Kaimi
    labels:
      - "orchestrator:pending"   # Only issues with this label
```

Leave `labels: []` to process all open issues.

---

## 4. Troubleshooting

### 4.1 GitHub API 401 Unauthorized

**Symptom:** `GitHub API returned status 401`

**Steps:**
1. Verify the token is set: `if ($env:GITHUB_TOKEN) { "set" } else { "missing" }`
2. If missing, re-source your profile: `. $PROFILE`
3. Verify token scopes: `gh auth status`
4. If token expired, generate a new one at GitHub → Settings → Developer settings → Personal access tokens (need `repo` + `read:org`)

### 4.2 GitHub API 403 Rate Limited

**Symptom:** `GitHub API returned status 403` or `rate limit exceeded`

**Steps:**
1. Check remaining rate limit: `gh api rate_limit`
2. If exhausted, wait until `reset` timestamp
3. Reduce `poll_interval_seconds` to avoid frequent polling (try 120 or 300)
4. Check for runaway supervisor instances: `tasklist | findstr supervisor`

### 4.3 Stalled Tasks (In-Progress Too Long)

**Symptom:** A task stays in `in_progress` for > 2 hours

**Steps:**
1. Identify the stalled task:
   ```bash
   jq -r 'select(.status == "in_progress") | "\(.id)  started: \(.started_at)  worker: \(.worker_id)"' tasks/*.json
   ```
2. Check if the worker process is still running
3. If the supervisor was restarted mid-task, the task is stuck — release it manually:
   ```bash
   # Edit the task JSON directly
   jq '.status = "pending" | .worker_id = "" | .attempts += 1' tasks/<uuid>.json > tmp.json
   mv tmp.json tasks/<uuid>.json
   ```
4. The supervisor will pick it up on the next poll

### 4.4 Quality Gate Failures

**Symptom:** Task fails with `quality gate failed - tests` / `linter` / `formatter`

**Steps:**
1. Find failed tasks and their error messages:
   ```bash
   jq -r 'select(.error_msg | test("quality gate")) | "issue #\(.issue_number): \(.error_msg)"' tasks/*.json
   ```
2. Inspect the worker's workspace:
   ```bash
   ls projects/<worker-id>/<owner>/<repo>/
   ```
3. Run the failing gate manually in the workspace:
   ```bash
   cd projects/gemini-flash-1/Mawar2/Kaimi
   go test ./...        # or the project's test command
   golangci-lint run    # or the project's lint command
   ```
4. If the gate fails on the base branch too, the target repo has pre-existing failures — either fix them upstream or remove that gate from the project's conventions file

### 4.5 Missing AI Review Fix Tasks (Feedback Loop Not Triggering)

**Symptom:** PRs have AI review comments but no `pr_feedback` tasks are created

**Steps:**
1. Verify the review comment starts with the expected prefix:
   ```
   ## 🤖 AI Code Review (Gemini 2.5 Pro)
   ```
2. Check that the supervisor PR-monitoring loop is running (it polls every 120s — watch logs)
3. Check for duplicate detection — the comment ID may already be recorded:
   ```bash
   jq -r 'select(.review_comment_id != null) | .review_comment_id' tasks/*.json
   ```
4. Verify the parent task is in `review` status (not `complete` or `failed`)

### 4.6 Clone Failures

**Symptom:** `fatal: repository not found` or `fatal: destination path already exists`

**Steps:**
1. Verify `gh auth setup-git` has been run (sets the git credential helper)
2. Verify the token has `repo` scope for private repos
3. If "destination path already exists", a prior run left a partial clone:
   ```bash
   rm -rf projects/<worker-id>/<owner>/<repo>
   ```
4. The worker will re-clone on the next attempt

### 4.7 Duplicate Tasks for the Same Issue

**Symptom:** Multiple tasks exist for the same issue number

**Steps:**
1. Find duplicates:
   ```bash
   jq -r '.issue_number' tasks/*.json | sort | uniq -d
   ```
2. The supervisor checks for existing PRs before enqueuing — if the PR was not yet created when the supervisor re-polled, it may enqueue twice
3. Safe fix: mark the duplicate as `failed` so only one task runs:
   ```bash
   jq '.status = "failed" | .error_msg = "duplicate"' tasks/<duplicate-uuid>.json > tmp.json
   mv tmp.json tasks/<duplicate-uuid>.json
   ```

### 4.8 Supervisor Exits Immediately

**Symptom:** Supervisor starts and exits with no output or with `Error loading config`

**Steps:**
1. Verify the config file exists: `ls orchestrator.yml`
2. Validate YAML syntax: `python -c "import yaml; yaml.safe_load(open('orchestrator.yml'))"`
3. Check required fields are present: `projects`, `worker_tiers`, `task_queue_dir`
4. Run with explicit config path: `./bin/supervisor.exe --config ./orchestrator.yml`

### 4.9 Build Fails: "race requires cgo"

**Symptom:** `go test -race ./...` fails with `requires cgo`

**Resolution:** CGO is disabled on this machine (no C compiler). Drop the `-race` flag:
```bash
go test -cover ./...   # works — no race detector
```

### 4.10 Stale MCP Binary / `MCP server returned status 400`

**Symptom:** Logs contain `MCP server returned status 400`

**Resolution:** The system uses the `GitHubRESTClient` (direct HTTP), not the MCP client. If you see this error, a stale code path is executing — verify `cmd/supervisor/main.go` calls `ticket.NewGitHubRESTClient()`, not the MCP client.

---

## 5. Monitoring

### Key Metrics via `jq`

```bash
# Task throughput: completed in last 24h
jq -r 'select(.status == "complete" and (.completed_at > "2026-06-05T00:00:00Z")) | .id' tasks/*.json | wc -l

# Failure rate (all time)
total=$(ls tasks/*.json | wc -l)
failed=$(jq -r 'select(.status == "failed")' tasks/*.json | grep -c '"id"')
echo "Failure rate: $failed / $total"

# Quality gate failure breakdown
jq -r 'select(.error_msg | test("quality gate failed")) | .error_msg' tasks/*.json | \
  sed 's/.*quality gate failed - //' | sort | uniq -c | sort -rn

# Review iteration distribution (expect ~70% at 0, ~20% at 1, ~8% at 2, ~2% at 3)
jq -r '.review_iteration' tasks/*.json | sort | uniq -c

# Fix tasks created by feedback loop
jq -r 'select(.metadata.task_type == "pr_feedback") | .id' tasks/*.json | wc -l

# Tasks stuck in_progress longer than 2 hours (requires GNU date)
jq -r 'select(.status == "in_progress") | .id + " " + .started_at' tasks/*.json
```

### Log Patterns Table

| Log Pattern | Meaning |
|---|---|
| `Supervisor: Found N open issues` | Normal poll — N issues visible |
| `Supervisor: Issue #N already has PR` | Skipping; dedup working |
| `Supervisor: Enqueued task <uuid>` | New task created |
| `[worker] Claimed task <uuid>` | Worker picked up the task |
| `[QualityGates] ✅ All quality checks passed` | PR will be created |
| `quality gate failed - tests` | Tests failed; PR suppressed |
| `[worker] Completed task - PR #N created` | Success |
| `Supervisor: Creating fix task for PR #N` | Feedback loop triggered |
| `Max review iterations reached` | PR exhausted 3 fix attempts |

### Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Failure rate | > 20% | > 40% |
| Tasks stuck `in_progress` | > 30 min | > 2 hours |
| Review iterations hitting max (3) | > 5% of PRs | > 10% |
| Zero completed tasks in 60 min | — | Investigate immediately |

---

## 6. Maintenance

### Task Queue Cleanup

Tasks accumulate indefinitely in `./tasks/`. Clean up periodically:

```bash
# Archive completed tasks older than 30 days
mkdir -p tasks/archive
find tasks/ -maxdepth 1 -name "*.json" -mtime +30 \
  -exec sh -c 'jq -e ".status == \"complete\" or .status == \"failed\"" "$1" > /dev/null && mv "$1" tasks/archive/' _ {} \;

# Or delete failed tasks (irreversible)
find tasks/ -maxdepth 1 -name "*.json" \
  -exec sh -c 'jq -e ".status == \"failed\"" "$1" > /dev/null && rm "$1"' _ {} \;
```

### Workspace Cleanup

Worker workspaces under `./projects/` can grow to ~2 GB (10 workers × ~200 MB each). Clean up when not running:

```bash
# Remove all worker workspaces (safe when supervisor is stopped)
rm -rf projects/

# Remove a single worker's workspace
rm -rf projects/gemini-flash-1/
```

Workspaces are re-created automatically on the next task.

### Binary Update Procedure

```bash
# 1. Stop the running supervisor (Ctrl+C or kill the process)

# 2. Pull latest code
git pull origin master

# 3. Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor

# 4. Run tests
go test -cover ./...

# 5. Restart
./bin/supervisor.exe --config orchestrator.yml
```

### Adding a New Project

1. Add the project entry to `orchestrator.yml`:
   ```yaml
   projects:
     - name: new-project
       repo_owner: YourOrg
       repo_name: YourRepo
       conventions_path: ./CLAUDE.md
       branch_pattern: "feature/issue-{ticket}-{summary}"
       commit_pattern: "{ticket}: {description}"
       labels: []
   ```
2. Ensure the target repo has a `CLAUDE.md` (or `CONVENTIONS.md`) that specifies test/lint/format commands
3. Restart the supervisor to pick up the new project

### Backfill Utility

Use `cmd/backfill` to seed the task queue with existing open PRs from Kaimi so the feedback loop can monitor them:

```bash
# Build backfill (if not already built)
go build -o bin/backfill.exe ./cmd/backfill

# Run backfill (requires GITHUB_TOKEN)
./bin/backfill.exe

# Verify tasks created
jq -r 'select(.metadata.backfilled == "true") | "PR #\(.pr_number): \(.title)"' tasks/*.json
```

Backfilled tasks start in `status: review` so the supervisor immediately monitors them for AI review comments without running quality gates again.

---

## 7. Security

### Token Handling

- **Never** commit `GITHUB_TOKEN` to any file in the repository
- Store the token in your shell profile (`$PROFILE`) or a secrets manager, not in `orchestrator.yml`
- Rotate tokens at least every 90 days
- Use fine-grained personal access tokens scoped to only the monitored repositories when possible

### Worker Permission Scope

Workers run Claude Code CLI with `--dangerously-skip-permissions` so the headless agent can edit files, run git, and call `gh` without human confirmation. This is intentional for autonomous operation.

To restrict permissions during debugging or dry runs, set:
```bash
$env:CLAUDE_PERMISSION_MODE = "plan"      # Claude proposes changes, does not apply
$env:CLAUDE_PERMISSION_MODE = "acceptEdits" # Claude edits files but does not run commands
```

Remove or unset the variable to return to fully autonomous mode.

### Log Hygiene

- Worker logs may contain issue descriptions and PR content — treat them as potentially sensitive
- Do not forward raw `tasks/*.json` files to external services; they may contain API responses with repository content
- The `GITHUB_TOKEN` value is never written to task files or logs by the supervisor (it is read from the environment only)

### Dependency Auditing

```bash
# Check for known vulnerabilities in Go dependencies
go list -m all | govulncheck ./...

# Review indirect dependencies
go mod graph | head -50
```

Run `go get -u ./...` followed by `go test -cover ./...` before deploying dependency upgrades to production.

---

*For architecture details, see [`docs/SYSTEM_DESIGN.md`](SYSTEM_DESIGN.md).  
For the AI review feedback loop design, see [`AI_REVIEW_FEEDBACK_LOOP.md`](../AI_REVIEW_FEEDBACK_LOOP.md).*
