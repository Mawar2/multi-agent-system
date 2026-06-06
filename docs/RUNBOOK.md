# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06

This runbook covers the full operational lifecycle of the multi-agent system: initial setup, day-to-day operation, configuration, troubleshooting, monitoring, maintenance, and security.

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

| Requirement | Check |
|---|---|
| Go 1.25.1+ | `go version` |
| GitHub CLI | `gh --version` |
| golangci-lint | `golangci-lint --version` |
| `GITHUB_TOKEN` env var | `if ($env:GITHUB_TOKEN) { "set" }` |
| gh authenticated | `gh auth status` |

### Token Setup

The token must be set before the supervisor or backfill tool can reach GitHub.

```powershell
# Already exported in a running session — verify without revealing it:
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set" }

# If missing, it is persisted in $PROFILE — restore with:
. $PROFILE
```

Required GitHub token scopes: `repo`, `read:org`.

After setting the token, wire it into git credential helper:

```bash
gh auth setup-git
```

### Build Steps

```bash
# Build supervisor binary
go build -o bin/supervisor.exe ./cmd/supervisor

# Build backfill utility
go build -o bin/backfill.exe ./cmd/backfill

# Build and vet everything
go build ./...
go vet ./...
```

> **Note:** `make` is not available on this machine. Use the raw `go` commands above. The `Makefile` documents the equivalent targets for reference.

### First-Run Verification

1. Copy the example config:
   ```bash
   cp orchestrator.example.yml orchestrator.yml
   ```

2. Edit `orchestrator.yml` — set `repo_owner`, `repo_name`, and confirm `task_queue_dir`.

3. Start the supervisor:
   ```bash
   ./bin/supervisor.exe --config orchestrator.yml
   ```

4. Confirm healthy startup output:
   ```
   Loading configuration from orchestrator.yml...
   Initializing task queue at ./tasks...
   Started 10 workers
   Supervisor running. Press Ctrl+C to stop.
   ```

5. After one poll cycle (default 60 s) you should see:
   ```
   Supervisor: Polling project Mawar2/Kaimi
   Supervisor: Found N open issues in Mawar2/Kaimi
   ```

---

## 2. Operation

### Normal State Indicators

| Log line | Meaning |
|---|---|
| `Worker started (tier: gemini-flash)` | Worker pool healthy |
| `Polling project Mawar2/Kaimi` | Supervisor active |
| `Routed issue #N - complexity: simple, tier: gemini-flash` | Task classified and enqueued |
| `Claimed task <uuid>` | Worker picked up work |
| `Quality gates passed ✅` | PR approved for creation |
| `Completed task - PR #N created` | End-to-end success |

### Issue → PR Lifecycle

```
GitHub Issue (open)
       │
       ▼
Supervisor polls every 60 s
       │
       ▼
RuleBasedRouter → Complexity (Simple/Medium/Complex) → Tier (Flash/Pro/Claude)
       │
       ▼
Task enqueued  (status: Pending)
       │
       ▼
Worker claims  (status: Claimed → InProgress)
       │
       ▼
Workspace cloned  →  LLM executes  →  Quality gates
       │                                    │
       │                              gate fails → task Failed
       │
       ▼
PR created  (status: Review)
       │
       ▼
AI review posts comment  →  Supervisor detects (120 s poll)
       │
       ▼
Fix task enqueued  (pr_feedback, iter 1–3)
       │
       ▼
Worker applies fixes  →  PR updated
       │
       ▼
Human approves & merges  (status: Complete)
```

### Worker Tiers

| Tier | Workers | Complexity | Default Model |
|---|---|---|---|
| `gemini-flash` | 5 (flash-1 … flash-5) | Simple | gemini-flash-3.5 |
| `gemini-pro` | 3 (pro-1 … pro-3) | Medium | gemini-pro-3.5 |
| `claude` | 2 (claude-1, claude-2) | Complex | claude-sonnet-4.5 |

### Task Queue Commands

```bash
# List all task files
ls tasks/

# Inspect a specific task
cat tasks/<uuid>.json

# Count by status
jq -r '.status' tasks/*.json | sort | uniq -c

# List pending tasks
jq -r 'select(.status == "pending") | .id + " " + .title' tasks/*.json

# List failed tasks with reason
jq -r 'select(.status == "failed") | "\(.issue_number): \(.error_msg)"' tasks/*.json

# List tasks in review (awaiting human merge)
jq -r 'select(.status == "review") | "\(.issue_number) PR#\(.pr_number) \(.title)"' tasks/*.json
```

### Day-to-Day Commands

```bash
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Test (no -race; CGO is off on this machine)
go test -cover ./...

# Lint
golangci-lint run ./...

# Run supervisor
./bin/supervisor.exe --config orchestrator.yml

# Run backfill (ingest existing open PRs)
./bin/backfill.exe

# Stop supervisor
Ctrl+C   # graceful shutdown via SIGINT
```

---

## 3. Configuration Reference

### Full `orchestrator.yml` Schema

```yaml
# ─── Projects ──────────────────────────────────────────────────────────────────
projects:
  - name: kaimi                          # internal identifier (informational)
    repo_owner: Mawar2                   # GitHub org or username
    repo_name: Kaimi                     # repository name (case-sensitive)
    conventions_path: ./CLAUDE.md        # path inside target repo to conventions file
    branch_pattern: "feature/KAI-{ticket}-{summary}"
    # branch name template — variables: {ticket}, {summary}
    commit_pattern: "{ticket}_{description}"
    # commit message template — variables: {ticket}, {description}
    labels: []                           # optional: only process issues with these labels
    # e.g. labels: ["ai-ready", "bug"]

# ─── Worker Tiers ──────────────────────────────────────────────────────────────
worker_tiers:
  gemini_flash:
    max_workers: 5                       # concurrent worker goroutines for this tier
    model: gemini-flash-3.5              # informational; backend wired in main.go
  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5
  claude:
    max_workers: 2
    model: claude-sonnet-4.5

# ─── Global Settings ───────────────────────────────────────────────────────────
poll_interval_seconds: 60               # how often supervisor polls GitHub for new issues
task_timeout_minutes: 120               # max wall-clock time a worker may spend on a task
max_retry_attempts: 3                   # how many times a task is retried before → Failed
task_queue_dir: ./tasks                 # directory for JSON task files
```

### Routing Heuristics

The `RuleBasedRouter` classifies each issue without making API calls.

| Signal | Simple | Complex |
|---|---|---|
| Title keywords | "fix typo", "add comment", "add godoc", "add documentation", "update readme", "format code", "add logging", "update version" | "architecture", "design", "refactor.*system", "implement.*agent", "new feature.*complex", "database", "migration", "schema change", "security", "authentication", "authorization", "breaking change", "api redesign" |
| Labels | `simple`, `easy` | `complex`, `hard` |
| File count estimate (from body) | ≤ 3 files | > 10 files |
| Default (no signal) | — Medium — | |

Tier assignment is 1:1 with complexity: Simple → `gemini-flash`, Medium → `gemini-pro`, Complex → `claude`.

### Branch & Commit Pattern Variables

| Variable | Value |
|---|---|
| `{ticket}` | Issue number (e.g. `47`) |
| `{summary}` | Slug of issue title |
| `{description}` | Short description for commit message |

### Label Filtering

When `labels` is non-empty, the supervisor only processes issues that carry **at least one** of the listed labels. An empty list (the default) processes all open issues.

```yaml
labels: ["ai-ready"]   # only issues tagged ai-ready
labels: []             # all open issues (default)
```

---

## 4. Troubleshooting

### 1. GitHub API 401 Unauthorized

**Symptom:** `GitHub API returned status 401`

**Cause:** `GITHUB_TOKEN` is missing or expired.

**Fix:**
```powershell
# Verify token is present (do not print value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set" }

# If missing, restore from profile
. $PROFILE

# Verify scopes via CLI
gh auth status
```

---

### 2. GitHub API Rate Limit (403 / 429)

**Symptom:** `GitHub API returned status 403` with body containing `rate limit exceeded`.

**Cause:** Token exhausted GitHub's REST API quota (5 000 req/hour for authenticated requests).

**Fix:**
- Check remaining quota: `gh api rate_limit`
- Reduce `poll_interval_seconds` (increase the value) to lower request frequency.
- If running multiple supervisor instances, consolidate to one.

---

### 3. Stalled Tasks (stuck InProgress)

**Symptom:** Task has `status: in_progress` but no worker log activity for > `task_timeout_minutes`.

**Cause:** Worker crashed, context cancelled, or LLM subprocess hung.

**Fix:**
```bash
# Find stalled tasks (claimed_at older than timeout)
jq -r 'select(.status == "in_progress") | "\(.id) claimed=\(.claimed_at) worker=\(.worker_id)"' tasks/*.json

# Manually reset to pending (edit the file or delete and re-enqueue)
# Edit status field:
# "status": "pending"
# Clear worker_id, claimed_at, started_at
```

Restart the supervisor — it will re-claim the task up to `max_retry_attempts` times.

---

### 4. Quality Gate Failures

**Symptom:** Tasks fail with `error_msg` containing `quality gates failed`.

**Cause:** LLM-generated code didn't pass tests, linter, or formatter in the target repo.

**Fix:**
```bash
# See which gate failed
jq -r 'select(.error_msg | contains("quality gates")) | "\(.issue_number): \(.error_msg)"' tasks/*.json

# Inspect full logs
cat tasks/<uuid>.json | jq -r .logs_path
```

- If tests fail consistently for a specific issue type, consider increasing the issue complexity so a more capable model handles it.
- If the target repo's test suite is broken independently of this system, fix it there first.

---

### 5. Missing Fix Tasks (AI Review Feedback Not Picked Up)

**Symptom:** AI review comments appear on PRs but no `pr_feedback` tasks are created.

**Cause:** Review comment doesn't start with the expected prefix, or the supervisor poll hasn't run yet.

**Expected prefix:**
```
## 🤖 AI Code Review (Gemini 2.5 Pro)
```

**Fix:**
- Wait up to 120 s for the next supervisor poll.
- Confirm the AI reviewer posts comments with the exact prefix above.
- Check existing tasks for the PR to ensure `review_comment_id` deduplication isn't blocking re-creation:
  ```bash
  jq -r 'select(.pr_number == <PR_NUM>) | "\(.id) iter=\(.review_iteration) status=\(.status)"' tasks/*.json
  ```
- If the iteration counter reached 3, the task is intentionally blocked (max iterations). Manual review required.

---

### 6. Clone Failures

**Symptom:** `fatal: destination path already exists` or `fatal: repository not found`.

**Cause (already exists):** Stale workspace from a previous run that wasn't cleaned up.

**Cause (not found):** Token lacks `repo` scope, or repo name is wrong.

**Fix:**
```bash
# Clean stale workspaces for one worker
rm -rf projects/gemini-flash-1/

# Clean all workspaces
rm -rf projects/

# Verify token scope
gh auth status

# Verify repo is accessible
gh repo view Mawar2/Kaimi
```

---

### 7. Duplicate Tasks for the Same Issue

**Symptom:** Multiple `pending` tasks with the same `issue_number`.

**Cause:** Supervisor polled before the first task reached `review` or `complete` status.

**Fix:** The supervisor checks for existing open PRs before enqueuing. If duplicates appear, verify `searchPullRequests` is returning results. A common cause is a mismatched `branch_pattern` — the PR's branch won't match the search and the supervisor re-enqueues.

```bash
# Check branch pattern in config vs actual PR branch
jq -r '.branch_name' tasks/*.json | sort | uniq
gh pr list --repo Mawar2/Kaimi --state open --json headRefName | jq '.[].headRefName'
```

---

### 8. Supervisor Exits Immediately

**Symptom:** Supervisor starts and exits without polling.

**Cause:** Config file not found, YAML parse error, or missing required fields.

**Fix:**
```bash
# Validate YAML syntax
go run ./cmd/supervisor --config orchestrator.yml 2>&1 | head -20

# Ensure orchestrator.yml exists (not just orchestrator.example.yml)
ls orchestrator.yml
```

---

### 9. CGO / Race Detector Build Failure

**Symptom:** `go test -race ./...` fails with `-race requires cgo`.

**Cause:** CGO is disabled on this machine (no C compiler).

**Fix:** Drop `-race` flag:
```bash
go test -cover ./...   # correct invocation on this machine
```

---

### 10. Stale MCP Binary (Legacy)

**Symptom:** Errors referencing `MCP server returned status 400`.

**Cause:** Legacy MCP client path still invoked somewhere.

**Fix:** The system now uses `GitHubRESTClient` (commit `05fd6bd`). If this error appears, a code path is still calling the old MCP client. Search:
```bash
grep -r "mcp_client" internal/
```
Any remaining calls should be replaced with `GitHubRESTClient` methods.

---

## 5. Monitoring

### Key Metrics (jq Queries)

```bash
# Overall throughput: tasks completed today
jq -r 'select(.status == "complete") | .completed_at' tasks/*.json | cut -c1-10 | sort | uniq -c

# Failure rate
TOTAL=$(ls tasks/*.json | wc -l)
FAILED=$(jq -r 'select(.status == "failed")' tasks/*.json | grep -c '"id"')
echo "Failure rate: $FAILED / $TOTAL"

# Quality gate failure rate (cost proxy)
jq -r 'select(.error_msg // "" | contains("quality gates"))' tasks/*.json | grep -c '"id"'

# Review iteration distribution (health of feedback loop)
jq -r '.review_iteration' tasks/*.json | sort | uniq -c
# Healthy: ~70% at 0, ~20% at 1, ~8% at 2, ~2% at 3

# Tasks stuck in max iterations (should be < 5%)
jq -r 'select(.error_msg // "" | contains("Max review iterations"))' tasks/*.json | grep -c '"id"'

# Active workers right now
jq -r 'select(.status == "in_progress") | .worker_id' tasks/*.json | sort | uniq -c

# PRs awaiting human review
jq -r 'select(.status == "review") | "PR#\(.pr_number) issue#\(.issue_number) \(.title)"' tasks/*.json

# Backfilled tasks
jq -r 'select(.metadata.backfilled == "true") | .id' tasks/*.json | wc -l
```

### Log Patterns

| Pattern | Meaning | Action |
|---|---|---|
| `Quality gates passed ✅` | PR accepted | Normal |
| `quality gates failed` | LLM code rejected | Review logs, possibly increase tier |
| `Max review iterations` | Feedback loop capped | Manual PR review required |
| `GitHub API returned status 401` | Token expired | Rotate token |
| `GitHub API returned status 403` | Rate limited | Increase `poll_interval_seconds` |
| `fatal: destination path already exists` | Stale workspace | `rm -rf projects/<worker>/` |
| `context deadline exceeded` | Worker timed out | Check `task_timeout_minutes`; increase if needed |
| `Worker started (tier: ...)` | Pool healthy | Normal |

### Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Failure rate | > 20% | > 40% |
| Max-iteration tasks | > 5% | > 10% |
| Tasks stuck in `in_progress` > 2× timeout | Any | Any |
| API 401 errors | 1 | Any recurring |
| Quality gate failures | > 35% | > 50% |

---

## 6. Maintenance

### Task Queue Cleanup

Tasks accumulate in `./tasks/`. Archive completed/failed tasks periodically.

```bash
# Archive completed tasks older than 7 days (adjust date as needed)
mkdir -p tasks/archive
# Move completed tasks (PowerShell)
Get-ChildItem tasks/*.json | Where-Object {
    (Get-Content $_ | ConvertFrom-Json).status -in "complete","failed"
} | Move-Item -Destination tasks/archive/

# Delete archived tasks (irreversible — ensure you have backups)
rm tasks/archive/*.json
```

### Workspace Cleanup

Worker workspaces grow to ~200 MB each (~2 GB total for 10 workers).

```bash
# Clean all workspaces (safe when supervisor is stopped)
rm -rf projects/

# Clean a single worker workspace
rm -rf projects/gemini-flash-1/

# Supervisor re-clones on next task claim — no manual re-init needed
```

### Binary Update Procedure

```bash
# Pull latest code
git pull origin master

# Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor
go build -o bin/backfill.exe ./cmd/backfill

# Verify tests pass
go test -cover ./...

# Restart supervisor (stop running instance first with Ctrl+C)
./bin/supervisor.exe --config orchestrator.yml
```

### Adding a New Project

1. Add a new entry under `projects:` in `orchestrator.yml`:
   ```yaml
   - name: my-new-project
     repo_owner: MyOrg
     repo_name: MyRepo
     conventions_path: ./CLAUDE.md
     branch_pattern: "feature/{ticket}-{summary}"
     commit_pattern: "{ticket} {description}"
     labels: []
   ```

2. Ensure `GITHUB_TOKEN` has `repo` access to the new repository.

3. Restart the supervisor — it will begin polling the new repo on the next cycle.

4. Optionally run backfill to ingest existing open PRs:
   ```bash
   ./bin/backfill.exe
   ```

### Backfill Utility

Use `backfill` to ingest existing open PRs into the task queue so the supervisor's feedback loop can monitor them.

```bash
# Run backfill (reads GITHUB_TOKEN from environment)
./bin/backfill.exe

# Output example:
# Backfill complete: 12 created, 3 skipped (already in queue), 15 total
```

**Behavior:**
- Fetches all open PRs from the configured repository (currently `Mawar2/Kaimi`).
- Infers complexity from PR size (additions + deletions).
- Enqueues each PR as a task with `status: review` so the supervisor's AI-review monitoring loop picks it up.
- Skips PRs already in the queue (idempotent).
- Sets `metadata.backfilled = "true"` on all created tasks.

---

## 7. Security

### Token Handling

- **Never log or print `GITHUB_TOKEN`** — check for presence only:
  ```powershell
  if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set" }
  ```
- Store the token in `$PROFILE` (PowerShell user profile), not in any file tracked by git.
- `orchestrator.yml` is git-ignored — do not add tokens to it.
- Rotate the token if it is accidentally exposed in logs or committed to git.

### Worker Permission Scope

The Claude-tier backend runs:
```
claude --print --dangerously-skip-permissions
```

This flag grants the headless worker permission to edit files and run `git`/`gh`/tests inside its isolated clone without interactive prompts. It is intentional and required for autonomous operation.

**Scope is limited by workspace isolation:** each worker operates only inside `./projects/{workerID}/{owner}/{repo}/` — it cannot reach other workers' directories or the host system outside the project root.

To restrict permissions during testing or auditing:
```powershell
$env:CLAUDE_PERMISSION_MODE = "plan"    # dry-run only, no file edits
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"  # edits OK, no shell commands
# Unset to restore default (dangerously-skip-permissions)
Remove-Item Env:CLAUDE_PERMISSION_MODE
```

### Log Hygiene

- Supervisor logs go to stdout — redirect to a file if you need persistence:
  ```bash
  ./bin/supervisor.exe --config orchestrator.yml >> supervisor.log 2>&1
  ```
- Scan logs before sharing: ensure no tokens, credentials, or PII appear in worker output.
- Worker logs may contain issue descriptions — treat as potentially sensitive.

### Dependency Auditing

```bash
# Check for known vulnerabilities in dependencies
go list -m all | head -20

# Run govulncheck if installed
govulncheck ./...
```

Keep Go and dependencies up to date. Run `go get -u ./...` on a dedicated branch and validate tests before merging.

---

*For architecture details, see `ARCHITECTURE.md`. For the AI review feedback loop design, see `AI_REVIEW_FEEDBACK_LOOP.md`.*
