# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06  
**Audience:** Operators who deploy, monitor, and maintain the system  
**Related:** [SYSTEM_DESIGN.md](SYSTEM_DESIGN.md) · [AI_REVIEW_FEEDBACK_LOOP.md](../AI_REVIEW_FEEDBACK_LOOP.md) · [CLAUDE.md](../CLAUDE.md)

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

| Requirement | Version | Check |
|---|---|---|
| Go | 1.25.1+ | `go version` |
| GitHub CLI | any | `gh version` |
| golangci-lint | any | `golangci-lint version` |
| GITHUB_TOKEN | `repo` + `read:org` scopes | see below |

> **Windows note:** `make` is not available. Use the raw `go` commands shown throughout this runbook.

### GitHub Token Setup

The token must be exported in the shell before starting the supervisor.

```powershell
# PowerShell — token is stored in $PROFILE
# Verify it is present (do not echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — source your profile first" }
```

If not set, re-source your profile:

```powershell
. $PROFILE
```

Required scopes: `repo` (full control of repositories), `read:org` (read org membership).

### Build

```bash
# Build the supervisor binary
go build -o bin/supervisor.exe ./cmd/supervisor

# Verify all packages compile
go build ./...
```

The binary is written to `bin/supervisor.exe`. The `bin/` directory is git-ignored.

### First Run

1. Copy the example config and customize it:

   ```bash
   cp orchestrator.example.yml orchestrator.yml
   # Edit orchestrator.yml — set repo_owner, repo_name, branch_pattern, etc.
   ```

2. Create the task queue directory (created automatically on first run, but you can pre-create):

   ```bash
   mkdir tasks
   ```

3. Start the supervisor:

   ```bash
   ./bin/supervisor.exe --config orchestrator.yml
   ```

4. Confirm startup output:

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

5. Stop with `Ctrl+C`. The supervisor drains in-progress tasks before exiting.

---

## 2. Operation

### Normal State

When everything is healthy you will see a repeating cycle in the logs:

```
Supervisor: Polling project Mawar2/Kaimi
Supervisor: Found N open issues in Mawar2/Kaimi
Supervisor: Processing issue #N: <title>
Supervisor: Routed issue #N - complexity: simple, tier: gemini-flash
Supervisor: Enqueued task <uuid> for issue #N
[gemini-flash-1] Claimed task <uuid> (issue #N)
[Worker gemini-flash-1] Quality gates passed ✅ - PR approved
[Worker gemini-flash-1] Completed task <uuid> - PR #N created
```

Between polls (default 60 s) the supervisor is quiet. Workers that have no tasks available sleep for 5 s between claim attempts.

### Issue → PR Lifecycle

```
GitHub Issue (open)
      ↓
Supervisor polls (every 60 s)
      ↓
Router classifies: simple / medium / complex
      ↓
Task enqueued → ./tasks/<uuid>.json  [status: pending]
      ↓
Worker claims task                   [status: claimed]
      ↓
Worker clones repo → ./projects/<workerID>/<owner>/<repo>/
      ↓
Claude Code CLI implements solution  [status: in_progress]
      ↓
Quality gates: tests + lint + format [fail → status: failed]
      ↓
PR created on GitHub                 [status: review]
      ↓
AI review comments detected (120 s poll)
      ↓  (if feedback found)
Fix task enqueued                    [review_iteration: 1–3]
      ↓
Worker applies targeted fix, pushes to same branch
      ↓
PR updated, AI re-reviews
      ↓  (when review passes or max 3 iterations)
Human reviews PR                     [status: complete after merge]
```

### Worker Tiers

| Tier | Workers | IDs | Use case |
|---|---|---|---|
| Gemini Flash | 5 | `gemini-flash-1` … `gemini-flash-5` | Simple issues: docs, typos, config |
| Gemini Pro | 3 | `gemini-pro-1` … `gemini-pro-3` | Medium issues: features, refactors |
| Claude | 2 | `claude-1` … `claude-2` | Complex issues: architecture, security |

> All tiers currently run the `ClaudeCodeWorker` with the Claude Code CLI backend. The Gemini bridge (`USE_GEMINI_WORKER=1`) is off by default and not production-ready.

### Task Queue

Tasks are stored as individual JSON files in `./tasks/`:

```
tasks/
├── b5730f2c-8bd5-4f29-83ed-02e34fa38edf.json   ← active task
├── a1c2d3e4-....json                             ← completed task
└── ...
```

Each file maps to one `Task` struct. Key fields:

| Field | Description |
|---|---|
| `id` | UUID |
| `issue_number` | GitHub Issue number |
| `status` | `pending` / `claimed` / `in_progress` / `review` / `complete` / `failed` |
| `tier` | `0`=gemini-flash · `1`=gemini-pro · `2`=claude |
| `complexity` | `0`=simple · `1`=medium · `2`=complex |
| `branch_name` | Feature branch created (e.g. `feature/issue-47`) |
| `pr_number` | GitHub PR number |
| `attempts` | Number of claim attempts |
| `error_msg` | Failure reason (if `status=failed`) |
| `review_iteration` | `0` = original issue task; `1`–`3` = fix iterations |
| `parent_task_id` | UUID of the original issue task (for `pr_feedback` tasks) |

### Common Dev Commands

```bash
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Run all tests (no -race; CGO is off on this machine)
go test -cover ./...

# Test one package
go test ./internal/orchestrator

# Test by name
go test -run TestRoute ./internal/orchestrator -v

# Lint
golangci-lint run ./...

# Vet
go vet ./...

# Check task queue status
ls tasks/
Get-Content tasks/<uuid>.json | jq .   # PowerShell

# Inspect a failed task
jq -r 'select(.status == "failed") | "\(.issue_number): \(.error_msg)"' tasks/*.json

# View all tasks in review
jq -r 'select(.status == "review") | "\(.pr_number): \(.title)"' tasks/*.json
```

---

## 3. Configuration Reference

### Full Schema (`orchestrator.yml`)

```yaml
# Projects to monitor — one entry per repository
projects:
  - name: kaimi                          # Logical project name (used in logs)
    repo_owner: Mawar2                   # GitHub org or user
    repo_name: Kaimi                     # Repository name
    conventions_path: ./CLAUDE.md        # Path to conventions file in the target repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"   # Branch naming (see variables below)
    commit_pattern: "{ticket}_{description}"            # Commit message format
    labels: []                           # Issue label filter; empty = all issues

# Worker tier pools
worker_tiers:
  gemini_flash:
    max_workers: 5                       # Number of concurrent flash workers
    model: gemini-flash-3.5              # Informational — actual model set in backend

  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5

  claude:
    max_workers: 2
    model: claude-sonnet-4.5

# Supervisor timing and queue settings
poll_interval_seconds: 60               # GitHub poll frequency (default: 60)
task_timeout_minutes: 120               # Worker timeout per task (default: 120)
max_retry_attempts: 3                   # Max claim attempts before marking failed (default: 3)
task_queue_dir: ./tasks                 # JSON task queue directory (default: ./tasks)
```

### Routing Rules

The `RuleBasedRouter` classifies each issue **without API calls** using these heuristics (evaluated in order):

1. **Simple** — title or body contains any of:
   `add comment`, `add godoc`, `add documentation`, `fix typo`, `update readme`,
   `format code`, `add logging`, `update version`, `docs:`, `[docs]`, `documentation`

2. **Complex** — title or body matches any regex:
   `architecture`, `design`, `refactor.*system`, `implement.*agent`, `new feature.*complex`,
   `database`, `migration`, `schema change`, `security`, `authentication`, `authorization`,
   `breaking change`, `api redesign`

3. **File count** — if body contains `files:` or `affected files`:
   ≤3 files → simple · >10 files → complex

4. **Labels** — label contains `simple` or `easy` → simple; `complex` or `hard` → complex

5. **Default** — medium (Gemini Pro tier)

Complexity maps directly to tier: `simple` → Gemini Flash · `medium` → Gemini Pro · `complex` → Claude.

### Branch and Commit Pattern Variables

| Variable | Value |
|---|---|
| `{ticket}` | GitHub Issue number (e.g. `47`) |
| `{summary}` | Slugified issue title (lowercase, hyphens) |
| `{description}` | Short description from issue body |

Example: `branch_pattern: "feature/KAI-{ticket}-{summary}"` + issue #47 "Add comment to README" → `feature/KAI-47-add-comment-to-readme`

### Label Filtering

Set `labels` in the project config to restrict which issues are picked up:

```yaml
labels:
  - orchestrator:pending   # Only pick up issues tagged for the orchestrator
```

Leave empty (`labels: []`) to process all open issues.

---

## 4. Troubleshooting

### T-1: GitHub API returns 401 Unauthorized

**Symptom:** `GitHub API returned status 401` in logs.

**Steps:**
1. Confirm token is set: `if ($env:GITHUB_TOKEN) { "set" } else { "NOT set" }`
2. Re-source profile: `. $PROFILE`
3. Verify token scopes via GitHub → Settings → Developer settings → Personal access tokens
4. Required scopes: `repo`, `read:org`
5. Confirm `gh auth status` shows the correct account

---

### T-2: No tasks being created despite open issues

**Symptom:** Supervisor polls but logs show 0 issues or 0 tasks enqueued.

**Steps:**
1. Check `labels` filter — empty means all issues; if set, confirm issues have the matching label.
2. Confirm `repo_owner` / `repo_name` are correct (case-sensitive).
3. Check whether issues already have open PRs — the supervisor skips issues with existing PRs:
   ```bash
   gh pr list --repo Mawar2/Kaimi --state open
   ```
4. Check for duplicate tasks: tasks with `status=pending` or `status=claimed` for the same `issue_number`.

---

### T-3: Worker claims task but workspace clone fails

**Symptom:** `fatal: destination path already exists` or `git clone` errors.

**Steps:**
1. Inspect the workspace directory:
   ```bash
   ls projects/
   ls projects/gemini-flash-1/Mawar2/Kaimi/
   ```
2. If stale from a previous run, clean it:
   ```bash
   Remove-Item -Recurse -Force projects/
   ```
3. Confirm `gh auth setup-git` has run — this configures the git credential helper so clone doesn't prompt.
4. Check `GIT_TERMINAL_PROMPT=0` is honored (set by `workspace.go`). If clone hangs, there is an auth gap; re-run `gh auth login`.

---

### T-4: Quality gates fail for every task

**Symptom:** Tasks enter `status=failed` with `error_msg` containing `quality gates`.

**Steps:**
1. Identify which gate is failing:
   ```bash
   jq -r 'select(.error_msg | contains("quality gates")) | "\(.issue_number): \(.error_msg)"' tasks/*.json
   ```
2. Go to the worker's workspace and run the gate manually:
   ```bash
   cd projects/gemini-flash-1/Mawar2/Kaimi
   git status
   # Run the failing command (test / lint / format) from conventions
   ```
3. Common causes:
   - LLM introduced a syntax error → inspect generated diff
   - Test suite itself is broken on the target branch → check upstream
   - Formatter config mismatch → confirm `conventions_path` points to correct file

---

### T-5: Fix tasks not created after AI review

**Symptom:** PRs have AI review comments but no `pr_feedback` tasks appear.

**Steps:**
1. Confirm the AI review comment begins with exactly: `## 🤖 AI Code Review (Gemini 2.5 Pro)`
2. Check supervisor feedback poll interval (120 s) — wait at least 2 minutes.
3. Check for duplicate detection: the supervisor tracks `review_comment_id` to avoid re-creating tasks.
   ```bash
   jq -r 'select(.metadata.task_type == "pr_feedback") | "\(.parent_task_id): iter \(.review_iteration)"' tasks/*.json
   ```
4. Check if the PR is already closed or merged — supervisor skips those.
5. Check `review_iteration` on the parent task — if it is already 3, no more fix tasks will be created (max iterations reached).

---

### T-6: Task stuck in `claimed` or `in_progress`

**Symptom:** Task has been in `claimed`/`in_progress` state longer than `task_timeout_minutes`.

**Steps:**
1. Find stalled tasks:
   ```bash
   jq -r 'select(.status == "claimed" or .status == "in_progress") | "\(.id): claimed_at=\(.claimed_at) worker=\(.worker_id)"' tasks/*.json
   ```
2. Check if the worker process is still running (look for `claude` subprocess or `git clone` in process list).
3. If the worker is dead, release the task manually by editing the JSON file:
   ```bash
   # Edit the task file: set status to "pending", clear worker_id, increment attempts
   # Then the next available worker will claim it
   ```
4. If `attempts` has reached `max_retry_attempts` (default 3), status will be set to `failed` automatically on the next claim attempt.

---

### T-7: Workspace disk usage is high

**Symptom:** Disk usage under `./projects/` is growing without bound.

**Steps:**
1. Check workspace sizes:
   ```bash
   Get-ChildItem projects/ | ForEach-Object { "$($_.Name): $(($_ | Get-ChildItem -Recurse | Measure-Object -Property Length -Sum).Sum / 1MB) MB" }
   ```
2. Expected: ~200 MB per worker × 10 workers = ~2 GB total.
3. Clean all workspaces (safe when supervisor is stopped):
   ```bash
   Remove-Item -Recurse -Force projects/
   ```
4. Workers re-clone on next task claim. A `git pull` is used if the workspace already exists.

---

### T-8: Supervisor exits with non-zero status

**Symptom:** Process exits with `Supervisor error: ...`.

**Common causes and fixes:**

| Error | Fix |
|---|---|
| `failed to read config file` | Confirm `orchestrator.yml` exists at the path passed to `--config` |
| `no projects configured` | Add at least one entry under `projects:` in config |
| `invalid config: project N: repo_owner is required` | Fill in missing required fields |
| `Error creating task queue` | Confirm the parent directory of `task_queue_dir` is writable |

---

### T-9: API quota exceeded (Gemini / GitHub)

**Symptom:** Tasks fail with quota or rate-limit errors.

**Steps:**
1. Identify the quota source from `error_msg`:
   - GitHub: `API rate limit exceeded` — wait ~1 h or use a token with higher limits
   - Gemini: quota errors in bridge logs
2. Reduce concurrency: lower `max_workers` in `orchestrator.yml` and restart.
3. Increase `poll_interval_seconds` to reduce GitHub API calls.

---

### T-10: Workers create PRs in the wrong repository

**Symptom:** PRs appear in `multi-agent-system` instead of `Kaimi`.

**Steps:**
1. Confirm worker workspace path: `projects/<workerID>/<repo_owner>/<repo_name>/`
2. The `gh pr create` command is run from inside the cloned workspace, so it targets the remote of that clone.
3. If the workspace is missing, `gh` may fall back to the current repo. Clean workspaces and re-run:
   ```bash
   Remove-Item -Recurse -Force projects/
   ```

---

## 5. Monitoring

### Key Metrics (jq queries)

Run these from the repository root while the supervisor is running (or after stopping it).

```bash
# Total tasks by status
jq -rs 'group_by(.status) | .[] | "\(.[0].status): \(length)"' tasks/*.json

# Fix task creation rate (pr_feedback tasks)
jq -rs '[.[] | select(.metadata.task_type == "pr_feedback")] | length' tasks/*.json

# Review iteration distribution (healthy: 70% at 0, 20% at 1, 8% at 2, 2% at 3)
jq -r '.review_iteration' tasks/*.json | sort | uniq -c

# Failed tasks with reasons
jq -r 'select(.status == "failed") | "\(.issue_number): \(.error_msg)"' tasks/*.json

# Quality gate failure rate
jq -rs '[.[] | select(.error_msg // "" | contains("quality gates"))] | length' tasks/*.json

# Tasks currently in progress
jq -r 'select(.status == "in_progress" or .status == "claimed") | "\(.worker_id): issue #\(.issue_number) (\(.title))"' tasks/*.json

# Average attempts per completed task
jq -rs '[.[] | select(.status == "complete") | .attempts] | if length > 0 then add/length else 0 end' tasks/*.json

# PRs in review (awaiting human merge)
jq -r 'select(.status == "review") | "PR #\(.pr_number): \(.title)"' tasks/*.json
```

### Log Patterns to Watch

| Pattern | Meaning | Action |
|---|---|---|
| `✅ Tests passed` | Quality gates passing normally | None |
| `quality gates failed` | Worker produced broken code | Check error_msg; inspect workspace diff |
| `Error claiming task` | Worker claim loop error | Usually transient; persistent → check queue file permissions |
| `GitHub API returned status 401` | Auth failure | Re-export `GITHUB_TOKEN` |
| `GitHub API returned status 403` | Scope missing or rate limited | Check token scopes or wait |
| `Max review iterations reached` | Feedback loop hit 3-iteration cap | Human review required |
| `Worker stopping` | Graceful shutdown in progress | Normal on Ctrl+C |

### Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Failed task rate | >20% of total | >40% of total |
| Tasks stuck in `claimed`/`in_progress` | >2× `task_timeout_minutes` | Any task older than 4 h |
| Quality gate failure rate | >35% | >50% |
| Fix tasks at `review_iteration=3` | >5% | >10% |
| Workspace disk usage | >3 GB | >5 GB |

---

## 6. Maintenance

### Task Queue Cleanup

Completed and failed tasks accumulate indefinitely. Archive or delete old tasks periodically:

```bash
# List all terminal-state tasks (complete or failed)
jq -r 'select(.status == "complete" or .status == "failed") | .id' tasks/*.json

# Archive completed tasks older than 7 days (PowerShell)
Get-ChildItem tasks/*.json | Where-Object { $_.LastWriteTime -lt (Get-Date).AddDays(-7) } | Move-Item -Destination archive/tasks/

# Or simply delete them
Get-ChildItem tasks/*.json | ForEach-Object {
    $task = Get-Content $_ | ConvertFrom-Json
    if ($task.status -eq "complete" -or $task.status -eq "failed") {
        Remove-Item $_
    }
}
```

### Workspace Cleanup

Worker workspaces grow with each cloned repo and feature branch. Safe to clean when the supervisor is stopped:

```bash
# Remove all worker workspaces
Remove-Item -Recurse -Force projects/

# Workers re-clone on next task claim — no data is lost
```

### Updating the Binary

```bash
# Pull latest code
git pull origin master

# Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor

# Restart supervisor (stop existing process with Ctrl+C first)
./bin/supervisor.exe --config orchestrator.yml
```

### Adding a New Project

1. Add an entry to `orchestrator.yml`:

   ```yaml
   projects:
     - name: new-project
       repo_owner: YourOrg
       repo_name: YourRepo
       conventions_path: ./CLAUDE.md
       branch_pattern: "feature/{ticket}-{summary}"
       commit_pattern: "[{ticket}] {description}"
       labels: []
   ```

2. Restart the supervisor — no rebuild required.

3. Confirm the new repo appears in startup output:

   ```
   Monitoring 2 project(s):
     - Mawar2/Kaimi
     - YourOrg/YourRepo
   ```

### Backfill Existing PRs

The `cmd/backfill` tool enqueues open PRs as `review`-status tasks so the AI review feedback loop can pick them up without re-doing the implementation work:

```bash
# Build the backfill tool
go build -o bin/backfill.exe ./cmd/backfill

# Run against a repo (reads GITHUB_TOKEN from environment)
./bin/backfill.exe --repo Mawar2/Kaimi
```

This is useful when:
- The supervisor was not running when PRs were created
- You want to retroactively apply the AI review feedback loop to existing PRs

### Running Tests

```bash
# Full suite (no -race; CGO is off on this machine)
go test -cover ./...

# Single package
go test ./internal/worker -v

# Single test
go test -run TestClaudeCodeWorker_Execute ./internal/worker -v
```

Pre-existing lint findings in `internal/ticket/github_rest_client.go` (3× unchecked `resp.Body.Close`, 1× `QF1003`) are known — fix opportunistically, not as blockers.

---

## 7. Security

### Token Handling

- The `GITHUB_TOKEN` is read from the environment via `os.Getenv("GITHUB_TOKEN")` — it is never written to disk by the supervisor.
- Do **not** commit `orchestrator.yml` if it contains secrets. The file is git-ignored by default.
- Rotate the token if it appears in logs, error messages, or is accidentally printed.
- Use a fine-grained personal access token scoped to only the repositories you are monitoring.

### Worker Permission Scope

- Workers run `claude --print --dangerously-skip-permissions` in an isolated workspace clone.
- The `--dangerously-skip-permissions` flag allows the Claude Code CLI to edit files and run shell commands without interactive prompts.
- This flag is **only safe** because workers operate in a fresh, per-worker clone that is not the production codebase.
- Workers never push directly to `master`/`main` — they always create a feature branch and open a PR.

### Log Hygiene

- Worker logs are written to stdout. Avoid redirecting them to files accessible by untrusted processes.
- The `error_msg` field in task JSON may contain partial output from the LLM — review before sharing externally.
- Do not log the `GITHUB_TOKEN` value. The codebase does not do this, but verify if you add custom logging.

### Dependency Auditing

```bash
# Review direct dependencies
cat go.mod

# Check for known vulnerabilities
go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Update dependencies
go get -u ./...
go mod tidy
```

Current direct dependencies: `gopkg.in/yaml.v3`, `github.com/google/uuid`. Keep them up to date.

---

*This runbook covers the operational lifecycle of the multi-agent orchestration system as of 2026-06-06. For architecture decisions and design rationale, see [SYSTEM_DESIGN.md](SYSTEM_DESIGN.md).*
