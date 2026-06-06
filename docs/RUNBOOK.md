# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06
**Audience:** Operators running the system day-to-day

This runbook covers the full operational lifecycle: getting started, normal operations, configuration, troubleshooting, monitoring, maintenance, and security.

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

| Requirement | Minimum Version | Notes |
|---|---|---|
| Go | 1.25+ | `go version` to verify |
| GitHub CLI (`gh`) | 2.x+ | `gh --version`; must be authenticated |
| `git` | 2.x+ | Must be on `PATH` |
| `golangci-lint` | 1.x+ | For linting; optional for runtime |
| `GITHUB_TOKEN` | — | Scopes: `repo`, `read:org` |

### Token Setup

The supervisor and backfill utility read the token from the `GITHUB_TOKEN` environment variable.

```powershell
# PowerShell — verify it is set (do NOT echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — source your profile" }

# If missing, restore from your PowerShell profile:
. $PROFILE
```

The token must have the `repo` and `read:org` scopes. Verify with:

```powershell
gh auth status
```

### GitHub CLI Authentication

Workers create PRs via the `gh` CLI. Ensure it is authenticated and `gh auth setup-git` has been run so git credential prompts do not hang workers:

```powershell
gh auth status          # should show your account
gh auth setup-git       # configures git credential helper
```

### Build

```powershell
# Build the supervisor binary
go build -o bin/supervisor.exe ./cmd/supervisor

# Build the backfill utility
go build -o bin/backfill.exe ./cmd/backfill

# Build everything (verify no compile errors)
go build ./...
```

Binaries are written to `bin/` (git-ignored). The `--config` flag defaults to `orchestrator.yml`.

### First-Run Verification

1. Copy the example config and customize it:

   ```powershell
   Copy-Item orchestrator.example.yml orchestrator.yml
   # Edit orchestrator.yml: set repo_owner, repo_name, branch_pattern, etc.
   ```

2. Verify the config loads cleanly:

   ```powershell
   .\bin\supervisor.exe --config orchestrator.yml
   # Should print "Loading configuration..." then "Supervisor running."
   # Ctrl+C to stop after verifying startup output.
   ```

3. Confirm the task queue directory was created:

   ```powershell
   Test-Path .\tasks    # should print True
   ```

4. Confirm worker workspaces will appear after the first task:

   ```powershell
   # After the first task is claimed, check:
   Get-ChildItem .\projects
   # Expect directories: gemini-flash-1, gemini-flash-2, gemini-pro-1, claude-1, etc.
   ```

---

## 2. Operation

### Normal State Indicators

When the system is healthy, the supervisor log shows:

```
Supervisor: Polling project Mawar2/Kaimi
Supervisor: Found N open issues in Mawar2/Kaimi
[gemini-flash-1] Worker started (tier: gemini-flash)
[gemini-flash-1] Claimed task <uuid> (issue #47)
[QualityGates] ✅ Tests passed
[QualityGates] ✅ Linter passed
[QualityGates] ✅ Formatter passed
[gemini-flash-1] Completed task <uuid> - PR #XX created
```

A quiet log (no "Claimed task" lines) means no actionable issues are in the queue — this is normal when all issues have PRs or are already claimed.

### Issue → PR Lifecycle

```
GitHub Issue (open)
       │
       ▼
Supervisor polls (every 60 s)
       │  router classifies complexity → tier
       ▼
Task: status=pending  (tasks/<uuid>.json)
       │
       ▼ Worker.Claim()
Task: status=claimed
       │
       ▼ Worker.Execute() begins
Task: status=in_progress
       │  Clone repo → checkout branch → run LLM → quality gates
       ▼ (gates pass)
Task: status=review   ← PR created on GitHub
       │
       ▼ CI/AI review posts comment
Supervisor detects review feedback
       │  creates pr_feedback task
       ▼
Task: status=pending  (fix task, iteration 1–3)
       │
       ▼ (fix applied, PR updated)
Task: status=review   ← PR updated
       │
       ▼ Human approves and merges
Task: status=complete
```

If any quality gate fails or the LLM errors, the task moves to `status=failed`.

### Worker Tiers

| Tier | Workers | IDs | Handles |
|---|---|---|---|
| `gemini-flash` | 5 | `gemini-flash-1` … `gemini-flash-5` | Simple issues (docs, typos, comments) |
| `gemini-pro` | 3 | `gemini-pro-1` … `gemini-pro-3` | Medium issues (features, refactors) |
| `claude` | 2 | `claude-1`, `claude-2` | Complex issues (architecture, security) |

All tiers currently use the Claude Code CLI backend (`claude --print --dangerously-skip-permissions`).

### Task Queue Commands

```powershell
# List all tasks and their statuses
Get-ChildItem .\tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json | Select-Object id, status, issue_number, worker_id }

# Count by status
Get-ChildItem .\tasks\*.json | ForEach-Object { (Get-Content $_ | ConvertFrom-Json).status } | Group-Object

# Inspect a specific task
Get-Content .\tasks\<uuid>.json | ConvertFrom-Json

# View failed tasks with error messages
Get-ChildItem .\tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -eq "failed") { "$($t.issue_number): $($t.error_msg)" }
}
```

### Day-to-Day Commands

```powershell
# Start the supervisor
.\bin\supervisor.exe --config orchestrator.yml

# Build (after code changes)
go build -o bin/supervisor.exe ./cmd/supervisor

# Run all tests (no -race; CGO is off on this machine)
go test -cover ./...

# Run a single package's tests
go test ./internal/orchestrator

# Lint
golangci-lint run ./...

# Format check (no diff = clean)
gofmt -l ./...

# Stop supervisor gracefully
# Press Ctrl+C — the supervisor handles SIGINT/SIGTERM
```

---

## 3. Configuration Reference

### Full `orchestrator.yml` Schema

```yaml
# ── Projects to monitor ──────────────────────────────────────────────────
projects:
  - name: kaimi                          # Logical name (used in logs)
    repo_owner: Mawar2                   # GitHub organization or user
    repo_name: Kaimi                     # GitHub repository name
    conventions_path: ./CLAUDE.md        # Path to conventions file in the target repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"   # Branch name template
    commit_pattern: "{ticket}_{description}"            # Commit message template
    labels: []                           # Issue labels to filter on (empty = all open issues)

  # Add additional projects here:
  # - name: other-project
  #   repo_owner: YourOrg
  #   repo_name: YourRepo
  #   conventions_path: ./PROJECT_RULES.md
  #   branch_pattern: "{ticket}-{summary}"
  #   commit_pattern: "[{ticket}] {description}"
  #   labels: ["orchestrator:pending"]

# ── Worker pools ─────────────────────────────────────────────────────────
worker_tiers:
  gemini_flash:
    max_workers: 5                       # Concurrent workers in this tier
    model: gemini-flash-3.5              # Informational; backend is Claude Code CLI

  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5

  claude:
    max_workers: 2
    model: claude-sonnet-4.5

# ── Supervisor settings ───────────────────────────────────────────────────
poll_interval_seconds: 60               # How often to poll GitHub for new issues
task_timeout_minutes: 120               # Max time a worker can spend on a task
max_retry_attempts: 3                   # Retries before a task is marked failed
task_queue_dir: ./tasks                 # Directory for JSON queue files
```

### Routing Heuristics

The `RuleBasedRouter` classifies each issue deterministically — no API calls.

| Signal | Complexity | Examples |
|---|---|---|
| Title/body contains simple keyword | Simple | `"fix typo"`, `"docs:"`, `"add comment"`, `"update readme"` |
| Title/body contains complex keyword | Complex | `"architecture"`, `"migration"`, `"security"`, `"breaking change"` |
| Body lists ≤ 3 files under `files:` / `affected files` | Simple | — |
| Body lists > 10 files | Complex | — |
| Label contains `"simple"` or `"easy"` | Simple | — |
| Label contains `"complex"` or `"hard"` | Complex | — |
| No clear signal | **Medium** | Default fallback |

Complexity maps to tier 1:1: Simple → `gemini-flash`, Medium → `gemini-pro`, Complex → `claude`.

### Branch and Commit Pattern Variables

| Variable | Replaced with |
|---|---|
| `{ticket}` | Issue number (e.g., `47`) |
| `{summary}` | Slug of the issue title (lowercase, hyphens) |
| `{description}` | Short description from the LLM |

### Label Filtering

Set `labels` on a project to restrict which issues are picked up:

```yaml
labels: ["orchestrator:pending"]   # Only issues with this label
labels: []                         # All open issues (default)
```

Workers never modify labels; label updates are the responsibility of upstream automation or humans.

---

## 4. Troubleshooting

### 4.1 GitHub API Returns 401

**Symptom:** Log line: `GitHub API returned status 401`

**Cause:** `GITHUB_TOKEN` is missing or has expired.

**Fix:**
```powershell
# Verify token is set
if ($env:GITHUB_TOKEN) { "set" } else { "MISSING" }

# If missing, restore from profile
. $PROFILE

# Verify token works
gh auth status
```

If the token is present but 401 still occurs, the token may have been revoked. Generate a new one at GitHub → Settings → Developer settings → Personal access tokens with scopes `repo` and `read:org`.

---

### 4.2 GitHub Rate Limit Hit

**Symptom:** Log line: `GitHub API returned status 403` or `rate limit exceeded`

**Cause:** The authenticated token has exhausted its GitHub API quota (5 000 requests/hour for PAT).

**Fix:** Check remaining quota:
```powershell
gh api rate_limit | ConvertFrom-Json | Select-Object -ExpandProperty rate
```

Reduce `poll_interval_seconds` (e.g., from 60 to 300) to lower request frequency. If you run multiple projects, each poll loop adds one request per project.

---

### 4.3 Stalled Tasks (Stuck in `in_progress`)

**Symptom:** A task has been `in_progress` or `claimed` for longer than `task_timeout_minutes`.

**Diagnosis:**
```powershell
# Find stalled tasks
Get-ChildItem .\tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -in @("in_progress","claimed")) {
        "$($t.id): issue #$($t.issue_number) claimed by $($t.worker_id) at $($t.claimed_at)"
    }
}
```

**Fix:** The supervisor automatically releases stalled tasks (based on `task_timeout_minutes`) back to `pending`. If it doesn't, manually edit the JSON file to set `"status": 0` (pending) and `"worker_id": ""` — then restart the supervisor.

---

### 4.4 Quality Gate Failures

**Symptom:** Tasks fail with error messages containing `"quality gates"`.

**Diagnosis:**
```powershell
Get-ChildItem .\tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -eq "failed" -and $t.error_msg -match "quality") {
        "Issue #$($t.issue_number): $($t.error_msg)"
    }
}
```

**Fix:**
- Review the failing test/linter command in the target repo's `CLAUDE.md` or `Makefile`.
- Confirm the conventions file at `conventions_path` in your config is accurate.
- Run the quality gate commands manually in the worker workspace to see the raw error:
  ```powershell
  cd .\projects\gemini-flash-1\Mawar2\Kaimi
  # Run the test command from the conventions file, e.g.:
  go test ./...
  ```
- If the gate is misconfigured (wrong command), update the `conventions_path` file in the target repo.

---

### 4.5 Fix Tasks Not Being Created After AI Review

**Symptom:** AI posts a review comment on a PR but no `pr_feedback` task appears in `./tasks/`.

**Diagnosis:**
1. Confirm the AI review comment begins with `## 🤖 AI Code Review (Gemini 2.5 Pro)` — supervisor only triggers on this prefix.
2. Check the supervisor log for `Supervisor: Checking PRs for feedback` lines.
3. Verify the PR's parent task is in status `review` (not `complete` or `failed`).
4. Check `review_comment_id` in the parent task — if it already matches the current comment ID, the task was already created (deduplication).

**Fix:** If the parent task status is wrong, manually set `"status": 3` (review) in the JSON file and restart the supervisor.

---

### 4.6 Clone Failures (`destination path already exists`)

**Symptom:** Log: `fatal: destination path '<path>' already exists and is not an empty directory`

**Cause:** A previous worker run left a partial clone; per-worker locking prevents conflicts across workers but a leftover corrupt clone blocks the same worker.

**Fix:**
```powershell
# Remove the specific worker's workspace for the affected repo
Remove-Item -Recurse -Force .\projects\gemini-flash-1\Mawar2\Kaimi

# Or remove all workspaces (nuclear option — forces re-clone for all workers)
Remove-Item -Recurse -Force .\projects
```

Workers re-clone automatically on the next task claim.

---

### 4.7 Duplicate Tasks for the Same Issue

**Symptom:** Multiple tasks with the same `issue_number` in the queue.

**Cause:** The supervisor checks for an existing open PR before enqueuing; if the PR was not yet created (race) or the query returned stale data, a duplicate may slip through.

**Prevention:** The supervisor's duplicate check runs on every poll. Duplicates are self-healing — once a worker creates a PR for issue #N, subsequent polls detect the PR and skip it.

**Fix (manual):** Mark extra pending tasks as failed:
```powershell
# Identify duplicates, then for each extra UUID:
$json = Get-Content .\tasks\<uuid>.json | ConvertFrom-Json
$json.status = 5   # 5 = failed
$json.error_msg = "duplicate — closed manually"
$json | ConvertTo-Json | Set-Content .\tasks\<uuid>.json
```

---

### 4.8 Supervisor Exits Immediately

**Symptom:** The supervisor binary starts and exits within a few seconds.

**Diagnosis:**
- Config validation error: check that `orchestrator.yml` has at least one project with `name`, `repo_owner`, and `repo_name` set.
- Missing `tasks/` directory write permission.
- `GITHUB_TOKEN` not set (REST client will 401 on first poll and may propagate a fatal error).

**Fix:**
```powershell
# Run with visible output and check the last line before exit
.\bin\supervisor.exe --config orchestrator.yml
```

---

### 4.9 Build Fails with `-race requires cgo`

**Symptom:** `go test -race ./...` fails with `cannot use -race flag without cgo enabled`.

**Cause:** CGO is disabled on this machine (no C compiler in PATH).

**Fix:** Drop the `-race` flag:
```powershell
go test -cover ./...      # correct on this machine
# Do NOT use: go test -race ./...
```

---

### 4.10 `golangci-lint` Reports Pre-existing Findings

**Symptom:** Lint output includes findings in `internal/ticket/github_rest_client.go` (unchecked `resp.Body.Close()` and `QF1003`).

**Context:** These are pre-existing findings not introduced by recent work. The CI gate is not blocked by them; fix opportunistically when touching that file.

---

## 5. Monitoring

### Key Metrics (jq Queries)

All queries run over `./tasks/*.json`. On Windows PowerShell, use `Get-Content` + `ConvertFrom-Json` as shown; the `jq` equivalents are provided for reference on systems where `jq` is available.

#### Throughput (tasks completed vs. failed)

```powershell
$tasks = Get-ChildItem .\tasks\*.json | ForEach-Object { Get-Content $_ | ConvertFrom-Json }
$completed = ($tasks | Where-Object { $_.status -eq 4 }).Count   # 4 = complete
$failed    = ($tasks | Where-Object { $_.status -eq 5 }).Count   # 5 = failed
"Completed: $completed | Failed: $failed | Total: $($tasks.Count)"
```

```bash
# jq equivalent
jq -r '.status' tasks/*.json | sort | uniq -c
# 0=pending 1=claimed 2=in_progress 3=review 4=complete 5=failed
```

#### Quality Gate Failure Rate

```powershell
$gateFailures = ($tasks | Where-Object { $_.status -eq 5 -and $_.error_msg -match "quality" }).Count
"Gate failures: $gateFailures / $($tasks.Count)"
```

```bash
jq -r 'select(.status == 5) | select(.error_msg | contains("quality"))' tasks/*.json | wc -l
```

#### Review Iteration Distribution

```powershell
$tasks | Group-Object review_iteration | Sort-Object Name | ForEach-Object {
    "Iteration $($_.Name): $($_.Count) tasks"
}
```

```bash
jq -r '.review_iteration' tasks/*.json | sort | uniq -c
# Expected: ~70% at 0, ~20% at 1, ~8% at 2, ~2% at 3
```

#### Backfilled Task Count

```powershell
($tasks | Where-Object { $_.metadata.backfilled -eq "true" }).Count
```

```bash
jq -r 'select(.metadata.backfilled == "true")' tasks/*.json | wc -l
```

#### Max Iterations Reached (should be < 5%)

```powershell
($tasks | Where-Object { $_.error_msg -match "Max review iterations" }).Count
```

```bash
jq -r 'select(.error_msg | contains("Max review iterations"))' tasks/*.json | wc -l
```

### Log Patterns Table

| Log Pattern | Meaning | Action |
|---|---|---|
| `Supervisor: Found N open issues` | Normal poll | None |
| `Claimed task <uuid> (issue #N)` | Worker picked up work | None |
| `✅ Tests passed` / `✅ Linter passed` | Quality gate OK | None |
| `Quality gates failed` | PR not created; task → failed | See §4.4 |
| `Completed task — PR #N created` | Success | None |
| `GitHub API returned status 401` | Auth failure | See §4.1 |
| `GitHub API returned status 403` | Rate limit | See §4.2 |
| `Error claiming task` | Queue error (transient) | Restart supervisor |
| `Supervisor: Creating fix task for PR #N` | Feedback loop triggered | Normal |
| `Max review iterations reached` | Loop limit hit | Review PR manually |

### Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Quality gate failure rate | > 20% | > 40% |
| Max iterations hit | > 5% | > 10% |
| Tasks stuck in `in_progress` | > 30 min | > `task_timeout_minutes` |
| Consecutive 401/403 errors | 3+ | 5+ |
| `failed` tasks in last 1 hour | > 5 | > 20 |

---

## 6. Maintenance

### Task Queue Cleanup

Tasks accumulate in `./tasks/` over time. Archive or delete terminal tasks periodically.

```powershell
# Archive completed/failed tasks older than 7 days
$cutoff = (Get-Date).AddDays(-7)
New-Item -ItemType Directory -Force .\tasks\archive | Out-Null

Get-ChildItem .\tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -in @(4, 5) -and [datetime]$t.completed_at -lt $cutoff) {
        Move-Item $_.FullName .\tasks\archive\
    }
}

# Hard-delete everything in archive (irreversible)
# Remove-Item -Recurse -Force .\tasks\archive
```

Leave `pending`, `claimed`, `in_progress`, and `review` tasks in place — the supervisor needs them.

### Workspace Cleanup

Worker workspaces grow over time (each clone is ~200 MB for a typical Go repo).

```powershell
# Remove all workspaces (workers re-clone on next task)
Remove-Item -Recurse -Force .\projects

# Remove a single worker's workspace
Remove-Item -Recurse -Force .\projects\gemini-flash-1
```

Safe to run while the supervisor is stopped. Do not delete workspaces while a worker is actively using them.

### Binary Update Procedure

1. Pull latest code:
   ```powershell
   git pull origin master
   ```
2. Stop the running supervisor (Ctrl+C or kill the process).
3. Rebuild:
   ```powershell
   go build -o bin/supervisor.exe ./cmd/supervisor
   go build -o bin/backfill.exe  ./cmd/backfill
   ```
4. Run tests:
   ```powershell
   go test -cover ./...
   ```
5. Restart the supervisor.

### Adding a New Project

1. Add a project entry in `orchestrator.yml`:
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
2. Ensure the target repo has a `CLAUDE.md` (or the file named by `conventions_path`) with test, lint, format, and build commands so quality gates can parse them.
3. Restart the supervisor — it picks up new projects on startup.

### Backfill Utility

The `backfill` command seeds the task queue with existing open PRs from Mawar2/Kaimi so the supervisor's feedback loop monitors them:

```powershell
# Ensure GITHUB_TOKEN is set, then:
.\bin\backfill.exe

# Output:
# Found N open PRs in Kaimi
# ✅ Created task <uuid> for PR #N: <title> (complexity: X, tier: Y)
# ...
# Backfill complete!
```

Run this once after initial deployment to catch PRs that predate the supervisor. Idempotent — if a PR is already in the queue it will be enqueued as a duplicate (the supervisor's dedup check prevents double-processing).

---

## 7. Security

### Token Handling

- Store `GITHUB_TOKEN` in your PowerShell profile or a secrets manager — never commit it to the repository.
- The token is read via `os.Getenv("GITHUB_TOKEN")` at startup; it is not written to disk by the system.
- Required scopes: `repo` (full control of private repos you own) and `read:org` (read org membership). Do not grant `admin:org` or `delete_repo` unless needed for future features.
- Rotate the token if it is exposed in logs or committed accidentally. Revoke the old token on GitHub before rotating.

### Worker Permission Scope

Workers run:
```
claude --print --dangerously-skip-permissions
```

This flag allows the Claude Code CLI to edit files, run git commands, and execute test/lint/build tools **without user confirmation** in the worker's isolated workspace clone. The blast radius is bounded by the per-worker workspace directory.

Override for a less permissive mode (dry-run or prompts):
```powershell
$env:CLAUDE_PERMISSION_MODE = "plan"      # plan only, no edits
$env:CLAUDE_PERMISSION_MODE = "acceptEdits"  # edits only, no shell commands
```

Restart the supervisor after changing this variable.

### Log Hygiene

- Supervisor and worker logs print task IDs and issue numbers — this is safe.
- Logs do **not** print the `GITHUB_TOKEN` value, PR descriptions in full, or LLM prompts.
- Redirect stdout/stderr to a log file with appropriate permissions if running as a service:
  ```powershell
  .\bin\supervisor.exe --config orchestrator.yml >> .\supervisor.log 2>&1
  ```
- Rotate and archive logs periodically to prevent unbounded disk growth.

### Dependency Auditing

```powershell
# Review direct dependencies
Get-Content go.mod

# Check for known vulnerabilities (requires govulncheck)
govulncheck ./...

# Update all dependencies (test after!)
go get -u ./...
go mod tidy
go test -cover ./...
```

The system's direct dependencies are minimal: `gopkg.in/yaml.v3` (config parsing) and `github.com/google/uuid` (task ID generation). The `gh` CLI and `claude` CLI are external binaries not managed by Go modules — update them independently via their respective package managers.
