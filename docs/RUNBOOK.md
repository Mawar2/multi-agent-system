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
| Go | 1.21+ | `go version` to verify |
| Git | any | Must be on PATH |
| GitHub CLI (`gh`) | latest | Must be authenticated |
| Claude Code CLI | latest | Required for Claude-tier workers |
| `golangci-lint` | latest | Required for linting |

### GitHub Token Setup

The supervisor authenticates to GitHub via `GITHUB_TOKEN`. The token must have `repo` and `read:org` scopes.

**Windows (PowerShell):**
```powershell
$env:GITHUB_TOKEN = "ghp_your_token_here"
# Persist across sessions by adding to $PROFILE
```

**Linux/macOS:**
```bash
export GITHUB_TOKEN="ghp_your_token_here"
# Persist by adding to ~/.bashrc or ~/.zshrc
```

Verify the token is set:
```powershell
# Windows — prints "GITHUB_TOKEN is set" without revealing the value
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set" }
```

### GitHub CLI Authentication

Workers use `gh` for PR creation and git credential helpers. Authenticate once:

```bash
gh auth login
gh auth setup-git   # configures git credential helper
```

Verify:
```bash
gh auth status
```

### Build

```bash
# Build the supervisor binary
go build -o bin/supervisor.exe ./cmd/supervisor

# Verify all packages compile
go build ./...
```

> On Windows, `make` is not installed. Use `go` commands directly (see [Development Commands](#development-commands)).

### First Run

1. Copy and edit the example config:
   ```bash
   cp orchestrator.example.yml orchestrator.yml
   # Edit orchestrator.yml for your project(s)
   ```

2. Start the supervisor:
   ```bash
   ./bin/supervisor.exe --config orchestrator.yml
   ```

3. Confirm startup output:
   ```
   Loading configuration from orchestrator.yml...
   Initializing task queue at ./tasks...
   Started 10 workers
   Supervisor running. Press Ctrl+C to stop.
   ```

4. Create a test GitHub issue in the monitored repo and watch it flow through.

---

## 2. Operation

### Normal Operating State

When running correctly, the supervisor produces log lines like:

```
Supervisor: Polling project Mawar2/Kaimi
Supervisor: Found 3 open issues in Mawar2/Kaimi
Supervisor: Routed issue #52 - complexity: simple, tier: gemini-flash
[gemini-flash-1] Claimed task a1b2c3d4 (issue #52)
[gemini-flash-1] Completed task a1b2c3d4 - PR #71 created
```

The system is idle (no issues) when you see only:
```
Supervisor: Polling project Mawar2/Kaimi
Supervisor: No new issues found
```

### Issue → PR Lifecycle

```
GitHub Issue opened
        ↓
Supervisor polls (every 60s) → routes by complexity
        ↓
Task enqueued (status: pending)
        ↓
Worker claims task (status: claimed → in_progress)
        ↓
Worker clones repo to per-worker workspace
        ↓
Worker implements solution (Claude Code or Gemini backend)
        ↓
Quality gates run (tests, lint, format, build)
        ↓  gate fails → task marked failed, worker moves on
PR created (status: review)
        ↓
CI runs AI review (Gemini 2.5 Pro via Vertex AI)
        ↓  no feedback → human reviews → merge → status: complete
Supervisor detects AI review comment (every 120s)
        ↓
Fix task created (pr_feedback, iteration 1..3)
        ↓
Worker applies fixes, updates PR
        ↓
Loop repeats until AI review passes or iteration 3 reached
```

### Stopping the Supervisor

Press `Ctrl+C`. The supervisor handles `SIGINT` and shuts down gracefully. Workers finish their current task or release it back to the queue.

If the process is killed ungracefully, in-progress tasks remain in `claimed` or `in_progress` state. They will be re-queued on the next startup when the stall timeout elapses.

### Worker Tiers

| Tier | Workers | Handles | Backend |
|------|---------|---------|---------|
| `gemini-flash` | 5 | Simple issues (≤3 files, docs, config) | Claude Code (default), Gemini via `USE_GEMINI_WORKER=1` |
| `gemini-pro` | 3 | Medium issues (features, refactors) | Claude Code (default) |
| `claude` | 2 | Complex issues (architecture, migrations) | Claude Code CLI |

> Gemini workers (`USE_GEMINI_WORKER=1`) are experimental. Keep the flag unset for production.

### Task Queue

Tasks are stored as JSON files in `./tasks/`. Each file is `{uuid}.json`.

```bash
# List all tasks
ls tasks/

# View a specific task
cat tasks/<uuid>.json

# Count by status (requires jq)
jq -r '.status' tasks/*.json | sort | uniq -c
```

### Development Commands

```bash
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Test (drop -race on Windows — CGO required)
go test -cover ./...

# Single package
go test ./internal/orchestrator

# Single test
go test -run TestRoute ./internal/orchestrator -v

# Vet
go vet ./...

# Lint
golangci-lint run ./...
```

---

## 3. Configuration Reference

### `orchestrator.yml` Schema

```yaml
projects:
  - name: kaimi                         # Logical project name
    repo_owner: Mawar2                  # GitHub org or user
    repo_name: Kaimi                    # Repository name
    conventions_path: ./CLAUDE.md       # Path to conventions file in the target repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"
    commit_pattern: "{ticket}_{description}"
    labels: []                          # Optional: filter issues by label

worker_tiers:
  gemini_flash:
    max_workers: 5
    model: gemini-flash-3.5             # Informational — routing uses tier, not model string
  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5
  claude:
    max_workers: 2
    model: claude-sonnet-4.5

poll_interval_seconds: 60              # GitHub polling interval (default: 60)
task_timeout_minutes: 120              # Max time per task before stall (default: 120)
max_retry_attempts: 3                  # Retries before marking failed (default: 3)
task_queue_dir: ./tasks                # JSON queue directory (default: ./tasks)
```

### Key Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `poll_interval_seconds` | 60 | How often the supervisor polls GitHub for new issues |
| `task_timeout_minutes` | 120 | A task in `in_progress` for longer than this is stalled and re-queued |
| `max_retry_attempts` | 3 | Task failures before permanently marking it `failed` |
| `task_queue_dir` | `./tasks` | Directory for the JSON task queue |

### Complexity → Tier Routing

The router in `internal/orchestrator/router.go` classifies issues into three tiers using keyword heuristics:

| Complexity | Keywords that trigger it | Tier |
|------------|------------------------|------|
| Simple | "fix typo", "docs:", "update readme", small file count | `gemini-flash` |
| Medium | Default for most issues | `gemini-pro` |
| Complex | "architecture", "migration", "security", large file count, labels | `claude` |

To change routing, edit `internal/orchestrator/router.go` and rebuild.

### Pattern Variables

`branch_pattern` and `commit_pattern` support these placeholders:

| Variable | Value |
|----------|-------|
| `{ticket}` | GitHub issue number |
| `{summary}` | Slugified issue title (first 40 chars) |
| `{description}` | Same as `{summary}` |

### Label Filtering

To only process issues with a specific label:

```yaml
projects:
  - name: kaimi
    labels: ["orchestrator:pending"]
```

Leave `labels: []` to process all open issues.

---

## 4. Troubleshooting

### "GitHub API returned status 401"

**Cause:** `GITHUB_TOKEN` is missing or expired.

**Fix:**
```powershell
# Windows
$env:GITHUB_TOKEN = "ghp_new_token"
# Restart supervisor
```

Verify token scopes: `repo` and `read:org` are required.

---

### "No tasks claimed"

**Symptoms:** Issues exist but no workers claim them.

**Debug steps:**
1. Confirm the issue is open (not closed or a draft PR).
2. Check label filtering — if `labels` is set, the issue must have the label.
3. Look for "Routed issue #X" in supervisor logs. If absent, routing is filtering it out.
4. Check that the task file was created in `tasks/`:
   ```bash
   ls tasks/ | tail -5
   jq '.status' tasks/*.json
   ```

---

### "Worker fails to create PR"

**Symptoms:** Task reaches `in_progress` but never creates a PR.

**Debug steps:**
1. Verify `gh auth status` is authenticated for `Mawar2`.
2. Check `gh auth setup-git` ran — workers clone via git using the credential helper.
3. Look for the task's error message:
   ```bash
   jq -r '.error_msg' tasks/<uuid>.json
   ```
4. Check if the feature branch already exists (duplicate PR attempt):
   ```bash
   gh pr list --repo Mawar2/Kaimi --head feature/issue-<number>
   ```

---

### "Quality gates failed"

**Symptoms:** Task fails with `error_msg` containing "quality gates".

**Debug steps:**
1. Check which gate failed:
   ```bash
   jq -r 'select(.error_msg | contains("quality gates")) | "\(.issue_number): \(.error_msg)"' tasks/*.json
   ```
2. Inspect the worker's workspace logs (path in `logs_path` field).
3. Confirm the target repo's `CLAUDE.md` has correct `test`, `lint`, and `format` commands.
4. Run quality gates manually in the worker workspace to reproduce.

---

### "fatal: destination path already exists"

**Cause:** Worker tried to clone a repo into a directory that already exists, or a previous clone left a partial directory.

**Fix:** Clean the affected workspace and restart:
```bash
rm -rf projects/gemini-flash-1/Mawar2/Kaimi   # or all workspaces
./bin/supervisor.exe --config orchestrator.yml
```

---

### Fix tasks not created from AI review

**Symptoms:** AI posts a review comment but no `pr_feedback` task appears.

**Debug steps:**
1. Confirm the comment starts with the exact prefix:
   ```
   ## 🤖 AI Code Review (Gemini 2.5 Pro)
   ```
2. Verify the original task is in `review` status:
   ```bash
   jq '.status' tasks/<uuid>.json
   ```
3. Check supervisor logs for PR monitoring:
   ```bash
   grep "Monitoring PRs\|PR monitoring" supervisor.log
   ```
4. Check comment ID isn't already processed (deduplication):
   ```bash
   jq '.review_comment_id' tasks/*.json
   ```

---

### Workers not updating PRs (fix tasks)

**Symptoms:** `pr_feedback` task claimed but PR not updated.

**Debug steps:**
1. Verify the parent task's branch still exists:
   ```bash
   gh pr view <pr-number> --repo Mawar2/Kaimi
   ```
2. Check worker logs for workspace preparation errors.
3. Confirm `branch_name` and `pr_number` fields are populated in the fix task:
   ```bash
   jq '{branch_name, pr_number}' tasks/<uuid>.json
   ```

---

### High GitHub API rate limit usage

**Symptoms:** Warnings about `X-RateLimit-Remaining` in logs, or 403 responses.

**Mitigation:**
- Increase `poll_interval_seconds` to 120 or 180.
- Reduce the number of projects being monitored.
- Check the number of PRs in `review` status — each is polled every 120s:
  ```bash
  jq -r 'select(.status == "review")' tasks/*.json | wc -l
  ```
- GitHub limit: 5,000 requests/hour for authenticated users.

---

### Stalled tasks after restart

**Symptoms:** Tasks stuck in `claimed` or `in_progress` after a crash or restart.

**Behavior:** The supervisor's stall-timeout mechanism re-queues these automatically when `task_timeout_minutes` has elapsed since `started_at`.

**Manual fix (if needed):**
```bash
# Reset a specific task to pending
jq '.status = "pending" | .worker_id = "" | .claimed_at = null' tasks/<uuid>.json > /tmp/fixed.json
mv /tmp/fixed.json tasks/<uuid>.json
```

---

## 5. Monitoring

### Key Metrics to Track

**Task throughput:**
```bash
# Tasks created today (requires jq + date)
jq -r 'select(.status != null)' tasks/*.json | wc -l

# Completed tasks
jq -r 'select(.status == "complete")' tasks/*.json | wc -l

# Failed tasks
jq -r 'select(.status == "failed")' tasks/*.json | wc -l
```

**Quality gate filter rate** (cost savings proxy):
```bash
jq -r 'select(.error_msg | strings | contains("quality gates"))' tasks/*.json | wc -l
# Target: 30-40% of tasks filtered
```

**AI review feedback loop health:**
```bash
# Fix tasks created
jq -r 'select(.metadata.task_type == "pr_feedback")' tasks/*.json | wc -l

# Review iteration distribution (expected: 70% at 0, 20% at 1, 8% at 2, 2% at 3)
jq -r '.review_iteration' tasks/*.json | sort | uniq -c

# Tasks that hit max iteration limit (target: <5%)
jq -r 'select(.error_msg | strings | contains("Max review iterations"))' tasks/*.json | wc -l
```

**Worker utilization:**
```bash
# Tasks currently in progress
jq -r 'select(.status == "in_progress") | .worker_id' tasks/*.json | sort | uniq -c
```

### Log Monitoring

The supervisor writes to stdout. Redirect to a file for persistence:

```bash
./bin/supervisor.exe --config orchestrator.yml >> supervisor.log 2>&1
```

Key log patterns to watch:

| Pattern | Meaning |
|---------|---------|
| `Supervisor: Found N open issues` | Normal polling |
| `Supervisor: Routed issue #N` | Issue accepted for processing |
| `Worker X] Completed task` | Successful PR created |
| `quality gates` in error | PR rejected before AI review |
| `PR monitoring failed` | GitHub API issue for PR polling |
| `X-RateLimit-Remaining: 0` | API quota exhausted |

### Alerting Thresholds

| Metric | Warning | Critical |
|--------|---------|----------|
| Failed tasks / hour | > 5 | > 20 |
| Tasks stuck `in_progress` | > 2 hours | > 4 hours |
| API rate limit remaining | < 500 | < 100 |
| Max iteration tasks | > 3% | > 10% |

---

## 6. Maintenance

### Cleaning the Task Queue

Completed and failed tasks accumulate in `./tasks/`. Archive or delete periodically:

```bash
# Archive completed tasks (older than 7 days — requires find)
mkdir -p tasks/archive
find tasks/ -name "*.json" -mtime +7 -exec mv {} tasks/archive/ \;

# Delete all failed tasks (destructive — verify first)
jq -r 'select(.status == "failed") | .id' tasks/*.json   # preview
# Then delete specific files after review
```

### Cleaning Worker Workspaces

Worker workspaces under `./projects/` can grow to ~2GB (10 workers × ~200MB). Clean them when:
- The system has been idle for a while.
- Disk space is low.
- You want to force fresh clones.

```bash
# Remove all worker workspaces
rm -rf projects/

# Remove workspace for a single worker
rm -rf projects/gemini-flash-1/
```

Workers re-clone on next task automatically.

### Updating the Supervisor

```bash
git pull origin master
go build -o bin/supervisor.exe ./cmd/supervisor
go test -cover ./...
./bin/supervisor.exe --config orchestrator.yml
```

### Adding a Monitored Project

1. Add to `orchestrator.yml`:
   ```yaml
   projects:
     - name: new-project
       repo_owner: MyOrg
       repo_name: MyRepo
       conventions_path: ./CLAUDE.md
       branch_pattern: "feature/{ticket}-{summary}"
       commit_pattern: "[{ticket}] {description}"
       labels: []
   ```

2. Ensure the target repo has a `CLAUDE.md` with `test`, `lint`, and `format` commands.

3. Restart the supervisor.

### Adjusting Worker Counts

Edit `orchestrator.yml` and restart:
```yaml
worker_tiers:
  gemini_flash:
    max_workers: 8    # was 5 — scale up for higher ticket volume
```

For persistent scaling, also update `cmd/supervisor/main.go` to spawn the right number of goroutines.

### Backfilling Existing PRs

Use the backfill utility to enqueue open PRs that predate the supervisor:

```bash
go build -o bin/backfill.exe ./cmd/backfill
./bin/backfill.exe
```

This fetches open PRs from `Mawar2/Kaimi` and enqueues them as `review` status tasks, so the AI feedback loop picks them up.

---

## 7. Security

### GitHub Token Handling

- **Never commit `GITHUB_TOKEN` to the repository.** The `.gitignore` excludes `orchestrator.yml` (which contains project names) but not shell profiles.
- Store the token in an environment variable or a secrets manager.
- Use the minimum required scopes: `repo`, `read:org`. Do not use a token with `admin` scope.
- Rotate the token every 90 days or immediately if it may have been exposed.

**Verify no token in git history:**
```bash
git log -p --all | grep -i "ghp_"
```

### Worker Autonomy and Permissions

Workers run `claude --print --dangerously-skip-permissions` inside per-worker workspace directories. This allows the Claude Code agent to:
- Edit files in the workspace.
- Run git commands.
- Execute tests and linters.

The `--dangerously-skip-permissions` flag bypasses Claude Code's interactive permission prompts. Limit blast radius by:
- Ensuring workers only have access to their own workspace directories.
- Setting `GITHUB_TOKEN` to a token with minimal required scopes.
- Reviewing the `conventions_path` file in each target repo before adding it — this file instructs the worker.

### `orchestrator.yml` Contains Sensitive Paths

`orchestrator.yml` is git-ignored. Do not commit it. Copy from `orchestrator.example.yml` and keep the real config local.

### Network Access

The supervisor makes outbound HTTPS calls to:
- `api.github.com` — issue polling, PR creation, comment fetching.
- `github.com` — git clone/push operations.

No inbound network access is required. The supervisor does not expose any HTTP server.

### Dependency Audit

```bash
# Check for known vulnerabilities in Go dependencies
go list -m all
govulncheck ./...   # requires: go install golang.org/x/vuln/cmd/govulncheck@latest
```

### Log Hygiene

Supervisor logs contain issue titles and PR numbers but not authentication tokens. Review log retention policies if issue content is sensitive. Avoid piping logs to external services without reviewing what's included.

---

## Appendix: Quick Reference

### Start / Stop

```bash
# Start
./bin/supervisor.exe --config orchestrator.yml

# Stop
Ctrl+C
```

### Build

```bash
go build -o bin/supervisor.exe ./cmd/supervisor
go test -cover ./...
golangci-lint run ./...
```

### Task Queue Queries

```bash
# All task statuses
jq -r '.status' tasks/*.json | sort | uniq -c

# Failed tasks with reasons
jq -r 'select(.status == "failed") | "\(.issue_number): \(.error_msg)"' tasks/*.json

# PRs awaiting review
jq -r 'select(.status == "review") | "\(.issue_number) → PR #\(.pr_number)"' tasks/*.json

# Quality gate failures
jq -r 'select(.error_msg | strings | contains("quality gates")) | .issue_number' tasks/*.json
```

### Workspace Paths

```
projects/
├── gemini-flash-1/Mawar2/Kaimi/   ← Worker 1
├── gemini-flash-2/Mawar2/Kaimi/   ← Worker 2
...
└── claude-2/Mawar2/Kaimi/         ← Worker 10
```
