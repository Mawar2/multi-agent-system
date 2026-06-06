# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06  
**Repository:** `Mawar2/multi-agent-system`  
**Audience:** Operators running or troubleshooting a live deployment

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
| Go | 1.25.1+ | `go version` to check |
| GitHub CLI (`gh`) | 2.x | `gh --version`; must be authenticated |
| `golangci-lint` | latest | Only needed for development |
| `GITHUB_TOKEN` | — | Required scopes: `repo`, `read:org` |

### Token Setup

The supervisor authenticates to GitHub using `GITHUB_TOKEN`. On this machine the token is persisted in your PowerShell profile:

```powershell
# Verify the token is available (do not echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — source your profile" }
```

If it is missing, re-source your profile:

```powershell
. $PROFILE
```

Required GitHub token scopes:
- `repo` — read issues, create branches, open PRs
- `read:org` — list organization repositories

### Build

> **Note:** `make` is not installed on Windows. Use the raw `go` commands below; the Makefile targets are listed for reference only.

```powershell
# From the repository root
go build -o bin/supervisor.exe ./cmd/supervisor
go build ./...                  # verify all packages compile
```

`bin/supervisor.exe` is git-ignored and must be rebuilt after each update.

### First Run

1. Copy and edit the configuration:
   ```powershell
   Copy-Item orchestrator.example.yml orchestrator.yml
   # Edit orchestrator.yml — add your project(s) and tune worker counts
   ```

2. Create the task queue directory (created automatically on first run, but confirm it is writable):
   ```powershell
   New-Item -ItemType Directory -Force tasks
   ```

3. Start the supervisor:
   ```powershell
   .\bin\supervisor.exe --config orchestrator.yml
   ```

4. Confirm healthy startup output:
   ```
   Loading configuration from orchestrator.yml...
   Initializing task queue at ./tasks...
   ...
   Started 10 workers
   Monitoring 1 project(s):
     - Mawar2/Kaimi
   Supervisor running. Press Ctrl+C to stop.
   ```

Stop with **Ctrl+C**. The supervisor drains gracefully and exits.

---

## 2. Operation

### Normal System State

When the system is healthy you will see log lines repeating every 60 seconds:

```
Supervisor: Polling project Mawar2/Kaimi
Supervisor: Found N open issues in Mawar2/Kaimi
```

Workers emit lines when they claim, execute, or complete work:

```
[gemini-flash-1] Claimed task <uuid> (issue #47)
[Worker gemini-flash-1] Quality gates passed ✅ - PR approved
[gemini-flash-1] Completed task <uuid> - PR #XX created
```

Silence (no log output for > 2 minutes) indicates a hang or crash. See [Troubleshooting](#4-troubleshooting).

### Issue → PR Lifecycle

```
GitHub Issue opened
       ↓
Supervisor polls (every 60 s)
       ↓
Router classifies complexity → simple / medium / complex
       ↓
Task enqueued in ./tasks/{uuid}.json  (status: pending)
       ↓
Worker claims task                    (status: claimed → in_progress)
       ↓
Worker clones repo to per-worker workspace
Worker executes LLM (claude --print --dangerously-skip-permissions)
Worker runs quality gates (tests, linter, formatter)
       ↓
Quality gates FAIL → task marked failed, no PR created
Quality gates PASS → PR opened on GitHub
       ↓
Task status: review
       ↓
CI runs AI code review (Gemini 2.5 Pro)
       ↓
AI review PASS → human review → merge → task: complete
AI review FAIL → supervisor creates pr_feedback task (up to 3 iterations)
```

### Worker Tiers

| Tier | Worker IDs | Handles | Complexity |
|---|---|---|---|
| Gemini Flash | `gemini-flash-1` … `gemini-flash-5` | Simple tasks (docs, typos, formatting) | `simple` |
| Gemini Pro | `gemini-pro-1` … `gemini-pro-3` | Medium tasks (features, refactors) | `medium` |
| Claude | `claude-1`, `claude-2` | Complex tasks (architecture, security, large refactors) | `complex` |

All tiers currently use the Claude Code CLI backend (`claude --print --dangerously-skip-permissions`). The Gemini backend (`USE_GEMINI_WORKER=1`) is experimental and off by default.

### Task Queue

Tasks are stored as JSON files in `./tasks/`. Each file is named `{uuid}.json`.

```powershell
# Count tasks by status
Get-ChildItem tasks\*.json | ForEach-Object { (Get-Content $_ | ConvertFrom-Json).status } | Group-Object | Sort-Object Name
```

```bash
# With jq (Git Bash or WSL)
jq -r '.status' tasks/*.json | sort | uniq -c
```

Task status flow: `pending` → `claimed` → `in_progress` → `review` → `complete` | `failed`

### Development Commands

```powershell
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Run all tests (drop -race — CGO is disabled on this machine)
go test -cover ./...

# Single package
go test ./internal/orchestrator

# Single test by name
go test -run TestRoute ./internal/orchestrator -v

# Vet
go vet ./...

# Lint (reports 4 pre-existing findings; not introduced by new work)
golangci-lint run ./...

# Format
gofmt -w .
```

---

## 3. Configuration Reference

### Full `orchestrator.yml` Schema

```yaml
# One entry per GitHub repository to monitor
projects:
  - name: kaimi                         # Human-readable name (used in logs)
    repo_owner: Mawar2                  # GitHub org or username
    repo_name: Kaimi                    # Repository name
    conventions_path: ./CLAUDE.md       # Path to conventions file inside the target repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"  # Branch name template
    commit_pattern: "{ticket}_{description}"           # Commit message template
    labels: []                          # Issue label filter (empty = all issues)

worker_tiers:
  gemini_flash:
    max_workers: 5                      # Parallel Gemini Flash workers
    model: gemini-flash-3.5             # Informational; routing is tier-based

  gemini_pro:
    max_workers: 3                      # Parallel Gemini Pro workers
    model: gemini-pro-3.5

  claude:
    max_workers: 2                      # Parallel Claude workers
    model: claude-sonnet-4.5

poll_interval_seconds: 60              # How often to poll GitHub for new issues
task_timeout_minutes: 120              # Max time a worker may hold a task before it is considered stalled
max_retry_attempts: 3                  # Failed tasks are retried this many times before being marked failed permanently
task_queue_dir: ./tasks                # Directory where task JSON files are stored
```

### Routing Rules

The `RuleBasedRouter` classifies each issue deterministically — no API calls:

| Signal | Result |
|---|---|
| Title/body contains: `add comment`, `add godoc`, `fix typo`, `update readme`, `format code`, `docs:`, `[docs]`, `documentation` | **simple** |
| Title/body matches: `architecture`, `design`, `refactor.*system`, `implement.*agent`, `database`, `migration`, `schema change`, `security`, `authentication`, `authorization`, `breaking change`, `api redesign` | **complex** |
| Body mentions `files:` / `affected files` and estimated file count ≤ 3 | **simple** |
| Body mentions `files:` / `affected files` and estimated file count > 10 | **complex** |
| Label contains `simple` or `easy` | **simple** |
| Label contains `complex` or `hard` | **complex** |
| No clear signal | **medium** (default) |

Complexity → tier mapping is 1-to-1: `simple` → Gemini Flash, `medium` → Gemini Pro, `complex` → Claude.

### Pattern Variables

| Variable | Expands to |
|---|---|
| `{ticket}` | Issue number (e.g., `47`) |
| `{summary}` | Slug derived from issue title |
| `{description}` | Short description for commit message |

Example with `branch_pattern: "feature/KAI-{ticket}-{summary}"` and issue #47 "Add logging":

```
feature/KAI-47-add-logging
```

### Label Filtering

Set `labels` to restrict which issues the supervisor processes:

```yaml
projects:
  - name: kaimi
    repo_owner: Mawar2
    repo_name: Kaimi
    labels:
      - "orchestrator:pending"    # Only process issues with this label
```

An empty `labels: []` processes all open issues.

### Per-Worker Workspaces

Each worker clones the target repo into its own directory:

```
./projects/{workerID}/{owner}/{repo}/
```

Example for `gemini-flash-1` working on `Mawar2/Kaimi`:

```
./projects/gemini-flash-1/Mawar2/Kaimi/
```

This prevents test/linter conflicts between workers running in parallel. Disk usage is approximately 10 workers × ~200 MB = ~2 GB.

---

## 4. Troubleshooting

### 4.1 GitHub API 401 Unauthorized

**Symptom:** `GitHub API returned status 401`

**Cause:** `GITHUB_TOKEN` is missing or expired.

**Fix:**
```powershell
# Check if set
if ($env:GITHUB_TOKEN) { "set" } else { "missing" }

# Re-source profile to restore it
. $PROFILE

# Confirm required scopes by calling the API
gh auth status
```

---

### 4.2 GitHub API 403 Forbidden / Rate Limited

**Symptom:** `GitHub API returned status 403` or `rate limit exceeded`

**Cause:** The token hit the GitHub REST API rate limit (5 000 requests/hour for authenticated calls).

**Fix:**
```powershell
# Check remaining quota
gh api rate_limit | ConvertFrom-Json | Select-Object -ExpandProperty rate
```

Reduce `poll_interval_seconds` to slow down polling, or wait for the quota window to reset (shown in the `reset` field of the rate limit response).

---

### 4.3 Stalled Tasks (in_progress for > 2 hours)

**Symptom:** A task stays `in_progress` past `task_timeout_minutes` with no log output.

**Cause:** Worker process crashed or the LLM call hung.

**Diagnosis:**
```powershell
# Find stalled tasks
Get-ChildItem tasks\*.json | Where-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    $t.status -eq "in_progress" -and $t.started_at -lt (Get-Date).AddHours(-2)
} | ForEach-Object { $_.Name }
```

**Fix:** The supervisor's stall-recovery logic is in the backlog (Phase 2). For now, manually release a stalled task by editing its JSON:

```powershell
# Set status back to pending and clear worker_id
$t = Get-Content tasks\<uuid>.json | ConvertFrom-Json
$t.status = "pending"
$t.worker_id = ""
$t | ConvertTo-Json -Depth 10 | Set-Content tasks\<uuid>.json
```

---

### 4.4 Quality Gate Failures

**Symptom:** Task marked failed with `error_msg` containing `quality gates`.

**Cause:** The LLM's code change broke tests, lint, or formatting in the target repo.

**Diagnosis:**
```bash
# List all quality gate failures
jq -r 'select(.error_msg | test("quality gates")) | "\(.issue_number): \(.error_msg)"' tasks/*.json
```

**Fix:** The task will be retried automatically up to `max_retry_attempts` times. If it fails repeatedly, inspect the worker logs at `task.logs_path` or manually review the generated branch in the target repo.

---

### 4.5 Missing AI Review Fix Tasks

**Symptom:** A PR received an AI review comment but no `pr_feedback` task was created.

**Causes:**
- The AI review comment does not start with `## 🤖 AI Code Review (Gemini 2.5 Pro)` — the supervisor ignores other comment formats.
- `ReviewCommentID` deduplication: the comment was already processed in a prior poll cycle.
- The PR was already at `ReviewIteration == 3` (max iterations reached).

**Diagnosis:**
```bash
# Check existing pr_feedback tasks for this PR
jq -r 'select(.metadata.task_type == "pr_feedback" and .pr_number == <PR_NUMBER>)' tasks/*.json

# Check review iteration count on parent task
jq -r 'select(.pr_number == <PR_NUMBER>) | "\(.id): iteration=\(.review_iteration)"' tasks/*.json
```

**Fix:** If a task was missed and iteration < 3, manually create a `pr_feedback` task by copying the parent task JSON and setting `task_type = "pr_feedback"`, `review_iteration`, `review_feedback`, and `status = "pending"`.

---

### 4.6 Worker Fails to Clone Repository

**Symptom:** `fatal: destination path already exists` or `authentication required`.

**Causes:**
- Leftover workspace from a previous crashed worker (destination exists error).
- `gh auth setup-git` credential helper not configured (auth error).

**Fix:**
```powershell
# Remove stale workspace for a specific worker
Remove-Item -Recurse -Force projects\gemini-flash-1\

# Re-configure git credentials
gh auth setup-git

# Or nuke all workspaces and start fresh
Remove-Item -Recurse -Force projects\
```

---

### 4.7 Duplicate Tasks for the Same Issue

**Symptom:** Multiple tasks exist with the same `issue_number`.

**Cause:** The supervisor checks for existing PRs to avoid duplicates but may enqueue a task if the PR hasn't been created yet and the supervisor polls twice before the worker claims the task.

**Diagnosis:**
```bash
jq -r '.issue_number' tasks/*.json | sort | uniq -d
```

**Fix:** Identify duplicate tasks and mark the extras as `failed` with an explanatory `error_msg`:

```bash
# Identify duplicates
jq -r '.issue_number' tasks/*.json | sort | uniq -d | while read n; do
  jq -r "select(.issue_number == $n) | .id" tasks/*.json
done
```

Then edit the duplicate JSON files to set `"status": "failed"` and `"error_msg": "duplicate — manually cancelled"`.

---

### 4.8 Supervisor Exits Immediately

**Symptom:** The binary starts and exits within a second, often with no output.

**Cause:** Usually a malformed `orchestrator.yml`.

**Diagnosis:**
```powershell
.\bin\supervisor.exe --config orchestrator.yml 2>&1
```

Common YAML mistakes:
- Missing required field `repo_owner` or `repo_name`
- Wrong indentation under `worker_tiers`
- `task_queue_dir` path that does not exist and cannot be created

**Fix:** Validate your YAML against `orchestrator.example.yml` and ensure all required fields are present.

---

### 4.9 Build Failure: `CGO` / Race Detector

**Symptom:** `go test -race ./...` fails with `-race requires cgo`.

**Cause:** CGO is disabled on this machine (no C compiler), making the race detector unavailable.

**Fix:** Drop `-race` from the test invocation:

```powershell
go test -cover ./...
```

The `Makefile`'s `make test` target uses `-race` — do not use it on this machine.

---

### 4.10 Supervisor Logs Show `MCP server returned status 400`

**Symptom:** Old log lines (or logs from a stale binary) contain MCP errors.

**Cause:** The MCP GitHub client was replaced with the REST client in commit `05fd6bd`. You are running an outdated binary.

**Fix:** Rebuild the binary:

```powershell
go build -o bin/supervisor.exe ./cmd/supervisor
```

---

## 5. Monitoring

### Key Metrics via `jq`

```bash
# Total tasks by status
jq -r '.status' tasks/*.json | sort | uniq -c

# Throughput: completed tasks in the last 24 hours
jq -r 'select(.status == "complete") | .completed_at' tasks/*.json \
  | awk -v cutoff="$(date -d '24 hours ago' -u +%Y-%m-%dT%H:%M)" '$0 > cutoff' \
  | wc -l

# Quality gate failure rate
echo "Total: $(jq -r '.status' tasks/*.json | wc -l)"
echo "Quality gate failures: $(jq -r 'select(.error_msg | test("quality gates"; "i"))' tasks/*.json | grep -c '"id"')"

# AI review feedback loop health
echo "pr_feedback tasks: $(jq -r 'select(.metadata.task_type == "pr_feedback")' tasks/*.json | grep -c '"id"')"

# Iteration distribution (expect: 70% at 0, 20% at 1, 8% at 2, 2% at 3)
jq -r '.review_iteration' tasks/*.json | sort | uniq -c

# Tasks that hit the max iteration limit (should be < 5%)
jq -r 'select(.error_msg | test("Max review iterations"; "i"))' tasks/*.json | grep -c '"id"'

# Failed tasks with error details
jq -r 'select(.status == "failed") | "\(.issue_number): \(.error_msg)"' tasks/*.json
```

### Log Patterns to Watch

| Pattern | Meaning | Action |
|---|---|---|
| `Worker stopped` for all workers | All workers exited | Restart the supervisor |
| `Error claiming task` repeating | Queue corruption or permissions issue | Check `tasks/` directory permissions |
| `GitHub API returned status 401` | Token expired | Re-export `GITHUB_TOKEN` |
| `GitHub API returned status 403` | Rate limited | Check quota; reduce `poll_interval_seconds` |
| `quality gates failed` | LLM produced broken code | Review failed task; check target repo test suite |
| No output for > 2 minutes | Supervisor or worker hung | Check process, restart if needed |

### Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Quality gate failure rate | > 40% | > 60% |
| Tasks stuck in `in_progress` > 2h | Any | > 3 |
| Tasks at max review iterations | > 5% | > 10% |
| GitHub 401/403 errors | Any | Any |

---

## 6. Maintenance

### Task Queue Cleanup

Completed and failed tasks accumulate in `./tasks/`. They are safe to archive or delete once you have recorded metrics.

```powershell
# Archive completed tasks older than 7 days
New-Item -ItemType Directory -Force tasks\archive
Get-ChildItem tasks\*.json | Where-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    $t.status -in @("complete","failed") -and $t.completed_at -lt (Get-Date).AddDays(-7)
} | Move-Item -Destination tasks\archive\

# Or simply remove them
Get-ChildItem tasks\*.json | Where-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    $t.status -in @("complete","failed")
} | Remove-Item
```

### Workspace Cleanup

Per-worker workspaces grow as repos accumulate history. Clean between runs or when disk is low:

```powershell
# Remove all worker workspaces
Remove-Item -Recurse -Force projects\

# Remove a single worker's workspace
Remove-Item -Recurse -Force projects\gemini-flash-1\
```

Workers re-clone automatically on their next task claim.

### Updating the Binary

After pulling new commits, rebuild before restarting:

```powershell
git pull
go build -o bin/supervisor.exe ./cmd/supervisor
# Restart supervisor
```

### Adding a New Project

1. Add an entry to `orchestrator.yml` under `projects:`:
   ```yaml
   projects:
     - name: my-new-project
       repo_owner: MyOrg
       repo_name: MyRepo
       conventions_path: ./CLAUDE.md
       branch_pattern: "feature/{ticket}-{summary}"
       commit_pattern: "{ticket}_{description}"
       labels: []
   ```

2. Ensure `GITHUB_TOKEN` has access to the new repository.

3. Restart the supervisor. It will begin polling the new repo on the next cycle.

### Backfill Utility

The `backfill` tool creates `review`-status tasks from existing open PRs so the AI review feedback loop can monitor them. Use this when you start the system against a repo that already has open PRs.

```powershell
# Build
go build -o bin/backfill.exe ./cmd/backfill

# Run (reads GITHUB_TOKEN from environment)
.\bin\backfill.exe
```

The tool fetches all open, non-draft PRs from `Mawar2/Kaimi`, infers complexity from PR size (additions + deletions), and enqueues each as a `StatusReview` task. The supervisor's feedback loop then picks them up automatically.

---

## 7. Security

### Token Handling

- **Never log the token value.** The supervisor uses `os.Getenv("GITHUB_TOKEN")` and does not print it.
- **Store the token in `$PROFILE`**, not in `orchestrator.yml` or any file committed to source control. `orchestrator.yml` is listed in `.gitignore`.
- **Rotate the token** if it is ever exposed. Revoke the old token immediately in GitHub Settings → Developer settings → Personal access tokens.
- **Minimum required scopes:** `repo` and `read:org`. Do not grant admin or write:org scopes.

### Worker Permission Scope

Workers run with `claude --print --dangerously-skip-permissions`. This flag allows the headless Claude process to read files, run tests, and call `git`/`gh` inside the per-worker workspace without interactive confirmation. The blast radius is limited to the target repository's workspace directory.

If you want to restrict this during a dry-run or review:

```powershell
$env:CLAUDE_PERMISSION_MODE = "plan"    # LLM proposes changes, does not apply
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"  # Applies edits, prompts before running commands
```

Remove the override to restore full autonomous operation:

```powershell
Remove-Item Env:\CLAUDE_PERMISSION_MODE
```

### Log Hygiene

Worker logs (`supervisor_test.log`, stdout) may contain issue titles and PR descriptions. These could include internal ticket content. Treat log files as internal artifacts:
- Do not commit log files (`*.log` is in `.gitignore`).
- Rotate or purge logs periodically — at least once per week in production.

### Dependency Auditing

The module has two direct dependencies (`gopkg.in/yaml.v3`, `github.com/google/uuid`). Check for known vulnerabilities periodically:

```powershell
go list -json -m all | Out-File deps.json
# Review deps.json or submit to a dependency scanner
```

Run `go get -u ./...` followed by `go test -cover ./...` to update dependencies. Pin to a specific version if a transitive dependency introduces a breaking change.
