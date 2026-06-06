# Operator Runbook — Multi-Agent Orchestration System

**Last updated:** 2026-06-06
**Audience:** Solo operators and small-team SREs running the system in production.

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

| Requirement | Minimum Version | Check Command |
|-------------|-----------------|---------------|
| Go | 1.25.1 | `go version` |
| Git | 2.x | `git --version` |
| GitHub CLI | 2.x | `gh --version` |
| golangci-lint | 1.x | `golangci-lint --version` |
| GITHUB_TOKEN env var | — | `if ($env:GITHUB_TOKEN) { "set" } else { "NOT set" }` |

### Setting `GITHUB_TOKEN`

The supervisor and backfill utility both read the token from `$env:GITHUB_TOKEN`. The token must have `repo` and `read:org` scopes.

```powershell
# Verify the token is present (do not echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set — check your `$PROFILE`" }
```

If the token is missing, it is persisted in `$PROFILE`. Re-source the profile in a fresh shell:

```powershell
. $PROFILE
```

### Authenticating the GitHub CLI

Workers create pull requests via `gh`. Authenticate once per machine:

```powershell
gh auth login
gh auth setup-git   # configures git credential helper so workers clone/push without prompting
```

Verify:

```powershell
gh auth status
```

### Building the Supervisor

```powershell
# From the repository root
go build -o bin/supervisor.exe ./cmd/supervisor

# Verify all packages compile
go build ./...
```

The `Makefile` is present but `make` is not available on Windows — use the raw `go` commands above.

### First-Run Verification

1. Copy the example config and edit it for your projects:

   ```powershell
   Copy-Item orchestrator.example.yml orchestrator.yml
   # Edit orchestrator.yml — set repo_owner, repo_name, branch_pattern
   ```

2. Start the supervisor:

   ```powershell
   .\bin\supervisor.exe --config orchestrator.yml
   ```

3. Expected console output (first 30 seconds):

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

   [gemini-flash-1] Worker started (tier: gemini-flash)
   ...
   [claude-2] Worker started (tier: claude)

   Supervisor: Starting main loop
   Supervisor: Polling project Mawar2/Kaimi
   Supervisor: Found N open issues in Mawar2/Kaimi
   ```

4. Stop with `Ctrl+C`. The supervisor performs a graceful shutdown.

---

## 2. Operation

### Normal-State Log Indicators

| Log fragment | Meaning | Action |
|---|---|---|
| `Supervisor: Polling project ...` | Healthy poll cycle | None |
| `Supervisor: Found 0 open issues` | No eligible issues | Check label filters in config |
| `Worker started (tier: ...)` | Worker pool healthy | None |
| `Claimed task ... (issue #N)` | Work in progress | None |
| `Completed task ... - PR #N created` | Success path | None |
| `Quality gates passed ✅` | PR approved for creation | None |
| `quality gate failed` | PR suppressed, task marked failed | See §4 Quality Gate Failures |
| `Error claiming task` | Queue I/O error | Check disk space / `./tasks/` permissions |

### Issue → PR Lifecycle

```
GitHub Issue (open)
       │
       ▼
Supervisor polls every 60 s
       │  classifyComplexity()
       ▼
Task enqueued (status: pending)
       │
       ▼
Worker claims task (status: claimed → in_progress)
  ├─ Clone repo to ./projects/{workerID}/{owner}/{repo}/
  ├─ Execute LLM (Claude Code CLI)
  ├─ Run quality gates (tests → linter → formatter → build)
  │    ├─ FAIL → task marked failed, PR not created
  │    └─ PASS ──────────────────────────────────────────┐
  └────────────────────────────────────────────────────── ▼
                                                  PR created (status: review)
                                                         │
                                                  CI runs AI review (120 s)
                                                         │
                                          ┌──────────────┴──────────────────┐
                                          │ AI posts review comment          │ No comment
                                          ▼                                  ▼
                                  Fix task created                  Human reviews PR
                                  (status: pending)
                                          │  up to 3 iterations
                                          └──── (loop back to Worker) ───────┘
```

### Worker Tiers

| Tier | Worker IDs | Issues Routed | Backend |
|---|---|---|---|
| `gemini-flash` | `gemini-flash-1` … `gemini-flash-5` | Simple | ClaudeCodeWorker (Claude CLI) |
| `gemini-pro` | `gemini-pro-1` … `gemini-pro-3` | Medium | ClaudeCodeWorker (Claude CLI) |
| `claude` | `claude-1`, `claude-2` | Complex | ClaudeCodeWorker (Claude CLI) |

> Note: All tiers currently run `ClaudeCodeWorker` backed by the Claude Code CLI. `USE_GEMINI_WORKER=1` enables the experimental Gemini plan-execute path but it is not production-ready.

### Checking the Task Queue (PowerShell)

```powershell
# List all tasks and their status
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    "$($t.id.Substring(0,8))  status=$($t.status)  issue=#$($t.issue_number)  worker=$($t.worker_id)"
}

# Count by status
Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_ | ConvertFrom-Json).status
} | Group-Object | Select-Object Name, Count

# View a single task in full
Get-Content tasks\<uuid>.json | ConvertFrom-Json | Format-List
```

### Day-to-Day Commands

```powershell
# Build
go build -o bin/supervisor.exe ./cmd/supervisor

# Test (drop -race; CGO is disabled on this machine)
go test -cover ./...

# Lint
golangci-lint run ./...

# Format check
gofmt -l ./...

# Run supervisor
.\bin\supervisor.exe --config orchestrator.yml
```

---

## 3. Configuration Reference

### Full Annotated `orchestrator.yml`

```yaml
projects:
  - name: kaimi                          # Internal label (used in logs)
    repo_owner: Mawar2                   # GitHub org or username
    repo_name: Kaimi                     # Repository name
    conventions_path: ./CLAUDE.md        # Path to project's CLAUDE.md inside the cloned repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"  # Branch naming template
    commit_pattern: "{ticket}_{description}"           # Commit message template
    labels: []                           # Optional: filter issues by GitHub label strings
                                         # e.g. ["orchestrator:pending", "bug"]
                                         # Empty list = all open issues

worker_tiers:
  gemini_flash:
    max_workers: 5                       # Concurrent workers in this tier
    model: gemini-flash-3.5             # Informational; actual backend is Claude CLI
  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5
  claude:
    max_workers: 2
    model: claude-sonnet-4.5

poll_interval_seconds: 60               # How often supervisor polls GitHub for new issues
task_timeout_minutes: 120               # Max wall-clock minutes a worker can spend per task
max_retry_attempts: 3                   # Task retried this many times before marked failed
task_queue_dir: ./tasks                 # Directory where JSON task files are stored
```

### Routing Heuristics

The `RuleBasedRouter` (`internal/orchestrator/router.go`) classifies issues deterministically — no API calls.

| Signal | Complexity assigned |
|---|---|
| Title/body contains: `add comment`, `add godoc`, `fix typo`, `update readme`, `format code`, `add logging`, `update version`, `docs:`, `[docs]`, `documentation` | Simple |
| Title/body matches: `architecture`, `design`, `refactor.*system`, `implement.*agent`, `database`, `migration`, `schema change`, `security`, `authentication`, `authorization`, `breaking change`, `api redesign` | Complex |
| Body mentions "files:" or "affected files" with ≤3 file references | Simple |
| Body mentions "files:" or "affected files" with >10 file references | Complex |
| Label contains `simple` or `easy` | Simple |
| Label contains `complex` or `hard` | Complex |
| No clear signal | Medium (default) |

Complexity → Tier mapping (hard-coded, not configurable):

| Complexity | Tier |
|---|---|
| Simple | `gemini-flash` |
| Medium | `gemini-pro` |
| Complex | `claude` |

### Branch and Commit Pattern Variables

| Variable | Replaced with |
|---|---|
| `{ticket}` | Issue number (e.g. `47`) |
| `{summary}` | Slugified issue title (e.g. `add-comment-to-readme`) |
| `{description}` | Short commit description derived from issue title |

### Label Filtering

If `labels` is non-empty, only issues that carry **all** listed labels are enqueued. Use this to gate automation:

```yaml
labels: ["orchestrator:pending"]
```

Add that label to an issue to opt it into automated processing.

---

## 4. Troubleshooting

### 401 Unauthorized from GitHub API

**Symptoms:** `GitHub API returned status 401` in logs; tasks stay pending.

**Steps:**
1. Verify token is set: `if ($env:GITHUB_TOKEN) { "set" } else { "NOT set" }`
2. Check token scopes: `gh auth status`
3. Re-source profile if missing: `. $PROFILE`
4. Confirm token has not expired in GitHub → Settings → Developer settings → Tokens

---

### GitHub API Rate Limit (403 / 429)

**Symptoms:** Log lines containing `rate limit exceeded` or HTTP 403/429.

**Steps:**
1. Check remaining rate: `gh api rate_limit`
2. Primary limit resets every hour. Wait, then restart the supervisor.
3. Reduce `poll_interval_seconds` (e.g. to `120`) to halve request volume.
4. If persistent, split projects across multiple tokens using separate supervisor instances.

---

### Stalled Tasks (Worker Claims but Never Completes)

**Symptoms:** Task status stays `in_progress` longer than `task_timeout_minutes`.

**Steps:**
1. Identify stalled tasks:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
       $t = Get-Content $_ | ConvertFrom-Json
       if ($t.status -eq 2) {   # 2 = in_progress
           $started = [datetime]$t.started_at
           $age = (Get-Date) - $started
           if ($age.TotalMinutes -gt 120) {
               "STALLED: $($t.id.Substring(0,8)) age=$([int]$age.TotalMinutes)m issue=#$($t.issue_number)"
           }
       }
   }
   ```
2. Stop the supervisor (`Ctrl+C`).
3. Manually reset the stalled task to pending by editing its JSON file:
   - Set `"status": 0` (pending)
   - Clear `"worker_id": ""`
   - Clear `"claimed_at"` and `"started_at"` fields
4. Restart the supervisor.

---

### Quality Gate Failures

**Symptoms:** Log contains `quality gate failed`; task status = `failed`.

**Steps:**
1. Find failed tasks and their errors:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
       $t = Get-Content $_ | ConvertFrom-Json
       if ($t.status -eq 5) {   # 5 = failed
           "Issue #$($t.issue_number): $($t.error_msg)"
       }
   }
   ```
2. Common causes:
   - **Tests failed:** LLM introduced a regression. Check the worker's workspace at `./projects/{workerID}/{owner}/{repo}/` for the diff.
   - **Linter errors:** New code has lint violations. Inspect the branch pushed by the worker.
   - **Formatter changes detected:** Code was not gofmt'd. The formatter auto-fixes and then git status shows dirty files.
3. Fix the underlying issue in the target repo's `CLAUDE.md`/conventions to give the LLM better guidance, then re-open the GitHub issue to re-trigger the task.

---

### Fix Tasks Not Being Created (Feedback Loop Broken)

**Symptoms:** PRs have AI review comments but no `pr_feedback` fix tasks appear in `./tasks/`.

**Steps:**
1. Verify the AI review comment begins with exactly: `## 🤖 AI Code Review (Gemini 2.5 Pro)`.
   The supervisor matches this prefix — any deviation stops detection.
2. Check supervisor is polling PRs (log line: `Supervisor: Checking PR #N for feedback`).
3. Verify `ReviewCommentID` uniqueness — if the same comment ID was already processed, a duplicate task is not created (by design).
4. Confirm the parent task JSON has `"status": 3` (review) and a valid `"pr_number"`.

---

### Clone Failures (`destination path already exists`)

**Symptoms:** `fatal: destination path already exists` in worker logs.

**Cause:** Per-worker workspace directory was left over from a previous run that was interrupted before cleanup.

**Fix:**
```powershell
Remove-Item -Recurse -Force projects\
```

Workers re-clone on the next task claim. This is safe — the source of truth is GitHub.

---

### Duplicate Tasks for the Same Issue

**Symptoms:** Multiple tasks with the same `issue_number` in pending state.

**Cause:** Supervisor polled while a task was already in flight and did not detect the existing task.

**Fix:**
1. Identify duplicates:
   ```powershell
   Get-ChildItem tasks\*.json | ForEach-Object {
       (Get-Content $_ | ConvertFrom-Json)
   } | Group-Object issue_number | Where-Object { $_.Count -gt 1 } |
   ForEach-Object { "Issue #$($_.Name): $($_.Count) tasks" }
   ```
2. Stop the supervisor.
3. Delete all but one pending task JSON for the affected issue.
4. Restart the supervisor.

---

### Supervisor Exits Immediately

**Symptoms:** Process exits right after "Supervisor running" with no further output.

**Steps:**
1. Check for config errors: `.\bin\supervisor.exe --config orchestrator.yml` — any YAML parse error or missing required field is printed to stderr.
2. Verify `./tasks/` directory is writable.
3. Confirm `GITHUB_TOKEN` is set.

---

### Build Fails with `-race requires cgo`

**Symptoms:** `go test -race ./...` or `make test` fails with `CGO disabled` / `-race requires cgo`.

**Cause:** CGO is disabled on this machine; the race detector requires a C compiler.

**Fix:** Drop the `-race` flag:
```powershell
go test -cover ./...
```

---

### Pre-existing Lint Findings in `github_rest_client.go`

**Symptoms:** `golangci-lint run ./...` reports 4 findings in `internal/ticket/github_rest_client.go` (3× unchecked `resp.Body.Close` errcheck, 1× `QF1003` staticcheck). These are **pre-existing** and not introduced by recent changes.

**Action:** Fix opportunistically when touching that file. Do not block other work on them.

---

## 5. Monitoring

### Key Metrics (PowerShell Queries)

**Throughput — tasks completed in last 24 hours:**
```powershell
$since = (Get-Date).AddHours(-24)
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -eq 4 -and [datetime]$t.completed_at -gt $since) { $t }
} | Measure-Object | Select-Object -ExpandProperty Count
```

**Failure rate:**
```powershell
$all   = (Get-ChildItem tasks\*.json).Count
$failed = (Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_ | ConvertFrom-Json).status } | Where-Object { $_ -eq 5 }).Count
if ($all -gt 0) { "Failure rate: $([math]::Round($failed/$all*100, 1))% ($failed/$all)" }
```

**Review iteration distribution (expect: ~70% at 0, ~20% at 1, ~8% at 2, ~2% at 3):**
```powershell
Get-ChildItem tasks\*.json | ForEach-Object {
    (Get-Content $_ | ConvertFrom-Json).review_iteration
} | Group-Object | Sort-Object Name | Select-Object Name, Count
```

**Backfilled tasks (created by `cmd/backfill`):**
```powershell
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.metadata.backfilled -eq "true") { $t }
} | Measure-Object | Select-Object -ExpandProperty Count
```

**Tasks that hit max review iterations (should be <5%):**
```powershell
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.error_msg -like "*Max review iterations*") { $t.id.Substring(0,8) + " issue=#" + $t.issue_number }
}
```

**Quality gate failures today:**
```powershell
$since = (Get-Date).Date
Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    if ($t.status -eq 5 -and $t.error_msg -like "*quality gate*" -and [datetime]$t.completed_at -gt $since) { $t }
} | Measure-Object | Select-Object -ExpandProperty Count
```

### Log Patterns Table

| Pattern | Severity | Meaning |
|---|---|---|
| `quality gate failed` | WARN | PR suppressed; task failed |
| `Error claiming task` | ERROR | Queue I/O problem |
| `Error executing task` | ERROR | Worker crashed mid-task |
| `GitHub API returned status 401` | ERROR | Token missing or expired |
| `GitHub API returned status 403` | WARN | Rate limit approaching |
| `Max review iterations` | WARN | PR needs human attention |
| `Supervisor error` | FATAL | Main loop crashed; restart required |

### Alerting Thresholds

| Metric | Warning | Critical |
|---|---|---|
| Failure rate | > 15% | > 30% |
| Tasks hitting max review iterations | > 5% | > 10% |
| Tasks stalled > 120 min | Any | > 3 |
| GitHub API 401 | Any | — |

---

## 6. Maintenance

### Task Queue Archive / Cleanup

Completed and failed tasks accumulate in `./tasks/`. Archive periodically:

```powershell
# Archive terminal tasks older than 7 days to tasks/archive/
$cutoff = (Get-Date).AddDays(-7)
New-Item -ItemType Directory -Force tasks\archive | Out-Null

Get-ChildItem tasks\*.json | ForEach-Object {
    $t = Get-Content $_ | ConvertFrom-Json
    $terminal = $t.status -eq 4 -or $t.status -eq 5
    $old = $t.completed_at -and [datetime]$t.completed_at -lt $cutoff
    if ($terminal -and $old) {
        Move-Item $_.FullName "tasks\archive\$($_.Name)"
    }
}
```

Do **not** delete tasks that are pending (0), claimed (1), or in_progress (2) — those are live work.

### Workspace Cleanup

Per-worker workspaces are re-used across tasks but never auto-deleted. Free disk space when needed:

```powershell
# Stop supervisor first, then:
Remove-Item -Recurse -Force projects\
```

Workers re-clone on next task claim (~200 MB per worker × 10 = ~2 GB total when all cloned).

### Binary Update Procedure

```powershell
# Pull latest code
git pull origin master

# Rebuild
go build -o bin/supervisor.exe ./cmd/supervisor

# Run tests to verify
go test -cover ./...

# Restart supervisor (stop existing instance first with Ctrl+C)
.\bin\supervisor.exe --config orchestrator.yml
```

### Adding a New Project

1. Add an entry under `projects:` in `orchestrator.yml`:
   ```yaml
   - name: newproject
     repo_owner: Mawar2
     repo_name: NewProject
     conventions_path: ./CLAUDE.md
     branch_pattern: "feature/NP-{ticket}-{summary}"
     commit_pattern: "{ticket}_{description}"
     labels: []
   ```
2. Ensure the target repo has a `CLAUDE.md` (or whichever path `conventions_path` points to) with test/lint/format commands.
3. Restart the supervisor — it picks up new projects on the next poll cycle.

### Backfill Utility

Use `cmd/backfill` to enqueue existing open PRs (from Mawar2/Kaimi) so the AI feedback loop monitors them:

```powershell
# Build
go build -o bin/backfill.exe ./cmd/backfill

# Run (requires GITHUB_TOKEN)
.\bin\backfill.exe
```

Output shows created task IDs and skipped draft PRs. Tasks are created with `"status": 3` (review) so the supervisor's feedback loop picks them up immediately.

### Dependency Audit

```powershell
# List all dependencies
go list -m all

# Check for known vulnerabilities
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

---

## 7. Security

### Token Handling

- `GITHUB_TOKEN` is read from `$env:GITHUB_TOKEN` at process startup.
- **Never log or print the token.** Use `if ($env:GITHUB_TOKEN) { "set" }` to verify presence without exposing the value.
- Store the token in `$PROFILE` (not in `orchestrator.yml` or any committed file).
- Rotate the token in GitHub → Settings → Developer settings → Personal access tokens if compromised.
- Required scopes: `repo` (full repo access) and `read:org`.

### Worker Permission Scope

Workers run `claude --print --dangerously-skip-permissions`. This grants the headless Claude agent full file-system and shell access inside its isolated workspace. Mitigations:

- Workers operate inside `./projects/{workerID}/` — a directory they created by cloning the target repo. They have no access to the host system outside that directory.
- The `--dangerously-skip-permissions` flag is scoped to the subprocess; it does not affect the supervisor process.
- Override permission mode without rebuilding:
  ```powershell
  $env:CLAUDE_PERMISSION_MODE = "acceptEdits"   # prompts for shell commands
  $env:CLAUDE_PERMISSION_MODE = "plan"          # dry-run only
  ```

### Log Hygiene

- Worker logs (`task.logs_path`) may contain snippets of issue bodies. Do not ship raw logs to third-party log aggregators without scrubbing PII.
- Avoid logging full AI review comment text at INFO level; keep it in the task JSON (`review_feedback` field) which is local-only.

### Dependency Auditing

Run `govulncheck ./...` before each binary update. The module graph uses:
- `gopkg.in/yaml.v3` — config parsing
- `github.com/google/uuid` — task ID generation

Both are well-maintained. Watch for upstream advisories.
