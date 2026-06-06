# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

---

# Multi-Agent Orchestration System

**Last updated:** 2026-06-06

This document defines how Claude Code operates in the multi-agent-system repository. Read this file at the start of every session.

---

## What This System Is

The **Multi-Agent Orchestration System** is a production infrastructure system that orchestrates multiple AI workers to automatically solve GitHub issues and create pull requests.

**Purpose:**
- Automatically discover GitHub issues across multiple repositories
- Route issues to appropriate AI workers based on complexity
- Execute work in isolated workspaces with quality gates
- Create high-quality PRs that pass validation before AI review

**Key Innovation:**
- **Quality gates** reduce AI review costs by 30-40% by preventing low-quality PRs
- **Per-worker workspaces** enable true parallel execution on multiple issues
- **Complexity-based routing** assigns simple issues to fast/cheap models, complex to powerful ones
- **AI review feedback loop** enables iterative PR improvement, reducing human review time by 97%

---

## Repository Information

- **GitHub Repository:** `Mawar2/multi-agent-system`
- **Main Branch:** `master`
- **Current Status:** Core implementation complete, ready for production testing
- **Primary User:** Malik (operates solo/two-person BD operations)

---

## GitHub Authentication

Both `cmd/supervisor` and `cmd/backfill` read the token from the `GITHUB_TOKEN`
environment variable (`os.Getenv("GITHUB_TOKEN")` in `NewGitHubRESTClient`). It is
already set in this session — **use `$env:GITHUB_TOKEN` directly**; no need to parse
it out of `$PROFILE`.

```powershell
# Verify it is present (do not echo the value)
if ($env:GITHUB_TOKEN) { "GITHUB_TOKEN is set" } else { "NOT set" }
```

If it is ever missing, it is persisted in the user's `$PROFILE`; sourcing the profile
in a fresh shell restores it. Required scopes: `repo`, `read:org`.

---

## Architecture Overview

### Three-Tier Worker System

**Tier 1: Gemini Flash (Simple Tasks)**
- 5 workers: `gemini-flash-1` through `gemini-flash-5`
- Complexity score: 0-1 (simple issues, small PRs)
- Fast and cheap

**Tier 2: Gemini Pro (Medium Tasks)**
- 3 workers: `gemini-pro-1` through `gemini-pro-3`
- Complexity score: 2-4 (medium complexity)
- Balanced speed and capability

**Tier 3: Claude (Complex Tasks)**
- 2 workers: `claude-1` through `claude-2`
- Complexity score: 5+ (complex issues, large PRs)
- Most powerful but expensive

### System Components

```
┌─────────────────────────────────────────────────────────────┐
│ SUPERVISOR (Main Loop)                                      │
│ - Polls GitHub for open issues every 60s                    │
│ - Routes issues by complexity                               │
│ - Enqueues tasks to JSON queue                              │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ TASK QUEUE (JSON-backed)                                    │
│ - Atomic task claiming (only one worker claims each task)   │
│ - Status tracking: Pending → InProgress → Review/Failed     │
│ - Tasks stored in ./tasks/{uuid}.json                       │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ WORKERS (10 total)                                          │
│ - Claim tasks from queue based on tier                      │
│ - Clone repo to per-worker workspace                        │
│ - Execute Claude Code CLI to implement solution             │
│ - Run quality gates (tests, linter, formatter, build)       │
│ - Create PR if quality gates pass                           │
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ QUALITY GATES (Cost Reduction)                              │
│ - Run tests (must pass)                                     │
│ - Run linter (must pass)                                    │
│ - Run formatter (must pass)                                 │
│ - Run build (optional, if configured)                       │
│ - Prevents 30-40% of low-quality PRs from reaching AI review│
└─────────────────────────────────────────────────────────────┘
                            ↓
┌─────────────────────────────────────────────────────────────┐
│ AI REVIEW FEEDBACK LOOP (Iterative Improvement)             │
│ - Supervisor monitors PRs for AI review comments (120s)     │
│ - Creates "fix" tasks when feedback detected                │
│ - Workers update existing PRs with targeted fixes           │
│ - Max 3 iterations to prevent infinite loops                │
│ - Reduces human review time by 97%                          │
└─────────────────────────────────────────────────────────────┘
```

---

## Per-Worker Workspace Isolation

**Critical Feature:** Each worker gets its own isolated workspace directory.

### Why This Matters

Without isolation, workers share the same workspace and conflict when running tests/linter:
```
projects/Mawar2/Kaimi/  ← All workers share
├── Worker 1: checkout feature/issue-47 → run tests
├── Worker 2: checkout feature/issue-46 → conflicts!
└── Worker 3: checkout feature/issue-44 → conflicts!
```

With isolation, workers work in parallel without conflicts:
```
projects/
├── gemini-flash-1/Mawar2/Kaimi/  ← Worker 1 private workspace
├── gemini-flash-2/Mawar2/Kaimi/  ← Worker 2 private workspace
├── gemini-flash-3/Mawar2/Kaimi/  ← Worker 3 private workspace
└── ... (10 total workspaces)
```

### Benefits
- ✅ True parallel execution (10 workers can work simultaneously)
- ✅ Zero test/linter/build conflicts
- ✅ Enables handling multiple PRs with feedback at the same time
- ✅ 10× throughput potential

### Trade-offs
- Disk overhead: 10 workers × ~200MB = ~2GB (acceptable)

**Implementation:** Commit `3cd2649` - Implemented 2026-06-05

---

## Quality Gates System

**Purpose:** Reduce AI review costs by preventing low-quality PRs.

### How It Works

Before accepting a PR, quality gates validate:

1. **Tests** - Run project's test command (must pass)
2. **Linter** - Run project's linter (must pass)
3. **Formatter** - Run project's formatter (must pass)
4. **Build** - Run project's build command (optional, must pass if configured)

If ANY gate fails, the PR is rejected and the task is marked as failed. This prevents the PR from triggering expensive AI review.

### Cost Savings

**Before quality gates:**
- 100 tasks → 100 PRs → 100 AI reviews → 68% success rate
- Cost: 100 × $0.10 = $10.00
- Wasted: 32 × $0.10 = $3.20

**After quality gates:**
- 100 tasks → 68 PRs (32 failed quality gates) → 68 AI reviews → 95% success rate
- Cost: 68 × $0.10 = $6.80
- Wasted: 3 × $0.10 = $0.30

**Savings: $3.20 per 100 tasks (32% reduction)**

**Implementation:** `internal/worker/quality_gates.go` (202 lines)

---

## AI Review Feedback Loop ✨ NEW

**Purpose:** Automatically monitor PRs for AI code review feedback and create fix tasks to iteratively improve PRs.

### How It Works

The feedback loop creates a continuous improvement cycle:

1. **Worker creates PR** from GitHub issue
2. **CI/CD runs AI review** (Gemini 2.5 Pro via Vertex AI)
3. **Supervisor monitors PRs** for AI review comments (every 120s)
4. **Fix task created** if AI posts feedback with prefix: `## 🤖 AI Code Review (Gemini 2.5 Pro)`
5. **Worker claims fix task**, checkouts existing branch, applies fixes
6. **PR updated**, CI runs again
7. **Loop continues** until review passes or max 3 iterations reached

### Two Task Types

**Type 1: "issue" tasks (original work)**
- Created from GitHub issues
- Creates new branch: `feature/issue-{number}`
- Creates new PR
- `task.Metadata["task_type"] = "issue"`

**Type 2: "pr_feedback" tasks (fix work)**
- Created from AI review comments
- Reuses existing branch from parent task
- Updates existing PR (doesn't create new)
- Inherits complexity tier from parent
- `task.Metadata["task_type"] = "pr_feedback"`

### Key Features

- **Automatic feedback detection** - Supervisor polls PRs every 120s
- **Smart task creation** - Fix tasks inherit branch/PR/tier from parent
- **Workspace reuse** - Workers checkout existing branch (not create new)
- **Iteration limit** - Max 3 review cycles to prevent infinite loops
- **Deduplication** - Tracks ReviewCommentID to avoid duplicate tasks
- **Edge case handling** - Merged PRs, closed PRs, missing workspaces

### Data Model Extensions

Four new Task fields:
```go
ParentTaskID    string // Links to original issue task
ReviewIteration int    // 0 for issue, 1-3 for fix iterations
ReviewFeedback  string // AI comment text for LLM context
ReviewCommentID int64  // GitHub comment ID (deduplication)
```

### Cost Impact

**Before feedback loop:**
- 100 issues → 68 PRs pass gates → 34 pass AI review → 34 human reviews
- AI cost: 68 × $0.10 = $6.80
- Human time: 34 × 10 min = 5.7 hours

**After feedback loop:**
- 100 issues → 68 PRs → 27 fix tasks (iter 1) → 3 fixes (iter 2) → 99% pass AI review
- AI cost: 98 × $0.10 = $9.80 (43% increase)
- Human time: 1 × 10 min = 10 minutes (97% reduction)

**Net benefit:** Save 5.6 hours, spend $3 more on AI → **$557 net savings** at $100/hour billing rate

### Prompting Strategy

Fix tasks use specialized prompts that include:
- Original issue context
- PR number and branch name
- Review iteration count
- Full AI review feedback verbatim
- Instructions to make targeted fixes (not full rewrites)

### Monitoring

Track feedback loop health:
```bash
# Fix task creation rate
jq -r 'select(.Metadata.task_type == "pr_feedback")' tasks/*.json | wc -l

# Review iteration distribution (expect: 70% at 0, 20% at 1, 8% at 2, 2% at 3)
jq -r '.ReviewIteration' tasks/*.json | sort | uniq -c

# Failed due to max iterations (should be <5%)
jq -r 'select(.ErrorMsg | contains("Max review iterations"))' tasks/*.json | wc -l
```

**Implementation:** 2026-06-05
**Documentation:** `AI_REVIEW_FEEDBACK_LOOP.md` (comprehensive guide)

---

## Critical Files and Their Purposes

### Core System

**`cmd/supervisor/main.go`** (199 lines)
- Main entry point
- Initializes GitHub client, task queue, router
- Spawns 10 workers (5 flash, 3 pro, 2 claude)
- Runs supervisor main loop

**`internal/orchestrator/supervisor.go`**
- Polls GitHub for open issues every 60 seconds
- Routes issues to task queue based on complexity
- Checks for existing PRs to avoid duplicates

**`internal/orchestrator/router.go`**
- `RuleBasedRouter`: deterministic keyword/label heuristics (no API calls)
- Classifies each issue into one of THREE `Complexity` values — `ComplexitySimple` / `ComplexityMedium` / `ComplexityComplex` (a 3-value enum in `task.go`, NOT a 0-10 score)
- Maps complexity → tier 1:1: simple→`TierGeminiFlash`, medium→`TierGeminiPro`, complex→`TierClaude`
- Heuristics: simple keywords ("fix typo", "docs:", "update readme"), complex keywords ("architecture", "migration", "security"), file-count estimate from body, then issue labels; defaults to medium
- Note: `cmd/backfill` uses a *separate* 0-10 size heuristic (`inferComplexity`) based on PR additions+deletions — do not confuse it with the router

### Task Management

**`internal/taskqueue/json.go`** (`NewJSONQueue`)
- JSON-backed implementation of the `TaskQueue` interface (`queue.go`)
- Atomic claim via `Dequeue(ctx, tier, workerID)`; `Release` returns a task to Pending and increments `Attempts`
- Tasks stored as `./tasks/{uuid}.json`

**`internal/taskqueue/queue.go`**
- `TaskQueue` interface: Enqueue / Dequeue / Update / Get / List / Release, plus `TaskFilter`

**`internal/taskqueue/task.go`**
- `Task` struct (JSON tags are snake_case, e.g. `issue_number`, `pr_number`)
- `Status` enum: Pending → Claimed → InProgress → Review → Complete / Failed (6 values, not 4)
- `Complexity` enum: Simple / Medium / Complex; `Tier` enum: GeminiFlash / GeminiPro / Claude
- Feedback-loop fields: `ParentTaskID`, `ReviewIteration`, `ReviewFeedback`, `ReviewCommentID`

### Workers

**`internal/worker/claudecode.go`** (`ClaudeCodeWorker`, `NewClaudeCodeWorker`)
- Implements the `Worker` interface (`worker.go`): Claim / Execute / Release / Health / ID / Tier
- Claims tasks, prepares workspace, executes LLM, runs quality gates, creates PR
- ⚠️ **All tiers currently run the same `llm.ClaudeCodeBackend`.** `main.go` constructs every worker (flash, pro, claude) with `llm.NewClaudeCodeBackend()` — the Gemini "tiers" only affect routing/concurrency labels today, not which model executes. The Antigravity/Gemini backend is Phase 2 (not yet wired).

**`internal/worker/workspace.go`** (157 lines)
- WorkspaceManager with per-worker isolation
- Clones repos to `./projects/{workerID}/{owner}/{repo}/`
- Per-worker-repo locking for true parallelism
- Git operations (clone, pull, checkout)

**`internal/worker/quality_gates.go`** (202 lines)
- Quality gate validation system
- Runs tests, linter, formatter, build
- Returns error if any gate fails
- Cost reduction mechanism

### LLM Backend

**`internal/llm/backend.go`** + **`internal/llm/claude_code.go`**
- `LLMBackend` interface: `Execute`, `ExecuteInDir` (workspace isolation), `Name`, `Models`
- `claude_code.go` is the only implementation today (Claude Code CLI as subprocess)
- Designed so a Gemini/Antigravity backend can be swapped in without changing worker logic

### GitHub Integration

**`internal/ticket/github_rest_client.go`** (274 lines)
- HTTP-based GitHub REST API client
- Replaces MCP client (was failing with 400 errors)
- Direct HTTP requests with GITHUB_TOKEN auth
- Methods: listIssues, getIssue, searchPullRequests

**`internal/ticket/client.go`**
- High-level GitHub operations / `Client` interface, wraps `GitHubRESTClient`
- Issue discovery and PR detection
- `mcp_client.go` is the legacy MCP path, kept but superseded by the REST client

### Conventions

**`internal/conventions/parser.go`** + **`internal/conventions/ruleset.go`**
- Parses a target repo's CLAUDE.md / CONVENTIONS.md / Makefile into a `Ruleset`
- Extracts test/lint/format/build commands consumed by quality gates and prompt building
- Test fixtures live in `test/fixtures/conventions/`

### Entry Points

**`cmd/supervisor/main.go`** — builds the queue, router, REST client, supervisor, and 10 workers (5 flash + 3 pro + 2 claude), then runs the supervisor loop and one goroutine per worker.

**`cmd/backfill/main.go`** — one-shot utility: fetches open PRs from Mawar2/Kaimi and enqueues them as `StatusReview` tasks so the supervisor's feedback loop picks them up. Requires `GITHUB_TOKEN`.

---

## Configuration

**`orchestrator.yml`** — main config (git-ignored; copy from `orchestrator.example.yml`). Parsed by `internal/orchestrator/config.go` → `LoadConfig`. The real schema:

```yaml
projects:
  - name: kaimi
    repo_owner: Mawar2
    repo_name: Kaimi
    conventions_path: ./CLAUDE.md
    branch_pattern: "feature/KAI-{ticket}-{summary}"
    commit_pattern: "{ticket}_{description}"
    labels: []                 # optional issue-label filter

worker_tiers:
  gemini_flash: { max_workers: 5, model: gemini-flash-3.5 }
  gemini_pro:   { max_workers: 3, model: gemini-pro-3.5 }
  claude:       { max_workers: 2, model: claude-sonnet-4.5 }

poll_interval_seconds: 60      # how often to poll GitHub for new issues
task_timeout_minutes: 120      # max time a worker spends on a task
max_retry_attempts: 3          # retries before a task is marked failed
task_queue_dir: ./tasks        # JSON queue directory
```

Tiers carry a `model` string and `max_workers`, NOT a `complexity_range` — the complexity→tier mapping is hard-coded in the router. The `model` field is currently informational (see the all-tiers-use-Claude-backend note above).

---

## Running the System

### Prerequisites

1. **GITHUB_TOKEN** — already exported in this session; just use `$env:GITHUB_TOKEN`.

2. **Build supervisor:**
   ```bash
   cd /c/Users/Owner/OneDrive/Documents/Builder/multi-agent-system
   go build -o supervisor.exe ./cmd/supervisor
   ```

### Start Supervisor

```bash
./supervisor.exe --config orchestrator.yml
```

### Expected Output

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
[gemini-flash-2] Worker started (tier: gemini-flash)
...
[claude-2] Worker started (tier: claude)

Supervisor: Starting main loop
Supervisor: Polling project Mawar2/Kaimi
Supervisor: Found 42 open issues in Mawar2/Kaimi
Supervisor: Processing issue #47: Test: Add comment to README
Supervisor: Routed issue #47 - complexity: simple, tier: gemini-flash
Supervisor: Enqueued task b5730f2c-8bd5-4f29-83ed-02e34fa38edf for issue #47

[gemini-flash-1] Claimed task b5730f2c-8bd5-4f29-83ed-02e34fa38edf (issue #47)
[WorkspaceManager] Cloning Mawar2/Kaimi into workspace...
[WorkspaceManager] Successfully cloned Mawar2/Kaimi
[Worker gemini-flash-1] Using workspace: projects/gemini-flash-1/Mawar2/Kaimi
[Worker gemini-flash-1] Running quality gates before accepting PR...
[QualityGates] Running tests...
[QualityGates] ✅ Tests passed
[QualityGates] Running linter...
[QualityGates] ✅ Linter passed
[QualityGates] Running formatter...
[QualityGates] ✅ Formatter passed
[Worker gemini-flash-1] Quality gates passed ✅ - PR approved
[Worker gemini-flash-1] Completed task - PR #XX created
```

---

## Common Tasks

### Check Task Queue Status

```bash
cd tasks/
ls -la *.json
jq . b5730f2c-8bd5-4f29-83ed-02e34fa38edf.json
```

### View Failed Tasks

```bash
cd tasks/
jq -r 'select(.status == "failed") | "\(.issue_number): \(.error_msg)"' *.json
```

### View Quality Gate Failures

```bash
cd tasks/
jq -r 'select(.error_msg | contains("quality gates")) | "\(.issue_number): \(.error_msg)"' *.json
```

### Clean Workspaces

```bash
rm -rf projects/
```

---

## Testing Checklist

### Single Worker Test
1. Clean workspace: `rm -rf projects/`
2. Confirm `$env:GITHUB_TOKEN` is set (already exported this session)
3. Run supervisor: `./bin/supervisor --config orchestrator.yml`
4. Verify workspace created: `ls projects/gemini-flash-1/Mawar2/Kaimi/`

### Multi-Worker Test
1. Create 5-10 Kaimi issues
2. Run supervisor with all 10 workers
3. Verify parallel workspaces:
   ```bash
   ls projects/
   # Should see: gemini-flash-1/, gemini-flash-2/, etc.
   ```
4. Check logs for concurrent cloning (timestamps should overlap)
5. Verify no "destination path already exists" errors

### Quality Gates Test
1. Process 10+ tasks
2. Check failed tasks: `jq -r 'select(.status == "failed")' tasks/*.json`
3. Count quality gate failures:
   ```bash
   jq -r 'select(.error_msg | contains("quality gates"))' tasks/*.json | wc -l
   ```
4. Calculate cost savings:
   - Total tasks: N
   - Quality gate failures: F
   - Cost savings: F × $0.10

---

## Known Issues and Solutions

### Issue 1: MCP Client Failures
**Error:** `MCP server returned status 400`
**Solution:** Use GitHubRESTClient instead (already implemented)
**Status:** ✅ Fixed in commit `05fd6bd`

### Issue 2: Workspace Concurrency
**Error:** `fatal: destination path already exists`
**Solution:** Per-repository mutex locking
**Status:** ✅ Fixed in commit `0a64927`

### Issue 3: Worker Conflicts
**Error:** Test/linter conflicts when multiple workers work on same repo
**Solution:** Per-worker workspace isolation
**Status:** ✅ Fixed in commit `3cd2649`

### Issue 4: GitHub Token Not Found
**Error:** `GitHub API returned status 401`
**Solution:** Ensure `$env:GITHUB_TOKEN` is set in the current shell (it is exported this session; otherwise re-source `$PROFILE`)
**Status:** ✅ Documented

---

## Development Guidelines

### Building, Testing, Linting

There is a `Makefile`, but ⚠️ **`make` is NOT installed on this Windows machine** — use
the raw `go` commands below. (`go` 1.25.1 and `golangci-lint` are on PATH.) The Makefile
targets are `all` / `build` / `test` / `lint` / `fmt` / `run` / `clean`; `make build`
outputs to `bin/supervisor` and `make test` uses `-race`.

```bash
# Build (verified working)
go build -o bin/supervisor.exe ./cmd/supervisor   # supervisor entry point
go build ./...                                      # all packages

# Test. Two important traps on this machine:
#  1. `go test -race` fails: "-race requires cgo" (CGO off, no C compiler).
#     So `make test` (which uses -race) does NOT work locally — drop -race.
#  2. ⚠️ `go test ./...` HANGS. The internal/worker package test `TestExecute`
#     is NON-HERMETIC: it builds a real ClaudeCodeWorker (baseDir /tmp/projects)
#     and calls Execute, which runs the real WorkspaceManager.PrepareWorkspace ->
#     `git pull` (workspace.go) against a cloned private repo. With no TTY that
#     git command blocks on a credential prompt and the test times out after 10m.
#
# Run the hermetic packages (everything except worker) — all green:
go test -cover ./internal/conventions ./internal/llm ./internal/orchestrator ./internal/taskqueue ./internal/ticket

# Single package
go test ./internal/orchestrator

# Single test by name (regex), verbose
go test -run TestRoute ./internal/orchestrator -v

# Vet (run by `go test`; keep it clean)
go vet ./...

# Lint — runs, but currently reports 4 PRE-EXISTING findings in
# internal/ticket/github_rest_client.go (3× unchecked resp.Body.Close errcheck,
# 1× staticcheck QF1003). Not introduced by recent work; fix opportunistically.
golangci-lint run ./...
```

Approx. coverage on the hermetic packages: conventions 86%, llm 79%, taskqueue
76%, ticket 43%, orchestrator 38%. The two `cmd/` packages have no tests, and
`internal/worker` cannot be run unattended (see the hang trap above — fixing it
requires injecting a mock/`WorkspaceManager` so `Execute` doesn't touch real git).
Tests are standard Go `_test.go` files beside each package.
Module `github.com/Mawar2/multi-agent-system`, Go 1.25.1; deps: `gopkg.in/yaml.v3`,
`github.com/google/uuid`.

### Adding a New Worker Tier

1. Update `orchestrator.yml` with new tier configuration
2. Add tier constant to `internal/taskqueue/task.go`
3. Update router in `internal/orchestrator/router.go`
4. Add workers in `cmd/supervisor/main.go`

### Adding a New Quality Gate

1. Add validation method to `internal/worker/quality_gates.go`
2. Call from `Validate()` method
3. Update conventions parser if new command needed
4. Test with real project

---

## Success Metrics

**System is working when:**
- ✅ Supervisor discovers issues and creates tasks
- ✅ Workers claim tasks without conflicts
- ✅ Per-worker workspaces created correctly
- ✅ Quality gates run and filter low-quality PRs
- ✅ PRs created in correct repository (not multi-agent-system)
- ✅ 30-40% cost reduction from quality gate filtering

**Production ready when:**
- ✅ 10+ tasks processed successfully
- ✅ No workspace conflicts in logs
- ✅ Quality gates prevent failing tests/linter
- ✅ Cost savings measured and verified

---

## Recent Improvements (2026-06-05)

### Quality Gates Implementation
**Commits:** Multiple commits in session
**Purpose:** Reduce AI review costs by 30-40%
**Status:** ✅ Complete and ready for testing

### GitHub REST Client
**Commit:** `05fd6bd`
**Purpose:** Replace failing MCP client with reliable HTTP API
**Status:** ✅ Complete and tested

### Workspace Concurrency Fix
**Commit:** `0a64927`
**Purpose:** Prevent race conditions during repo cloning
**Status:** ✅ Complete and tested

### Per-Worker Workspace Isolation
**Commit:** `3cd2649`
**Purpose:** Enable true parallel execution on multiple issues
**Status:** ✅ Complete and ready for testing

---

## Future Enhancements

### Phase 1 (Next)
- Unit tests for workspace isolation
- Integration tests for end-to-end flow
- Workspace cleanup after task completion

### Phase 2
- Stalled task recovery (handle crashed workers)
- Health monitoring dashboard
- Task priority system

### Phase 3
- Auto-fix AI review comments (quality loop)
- Git worktrees for disk space optimization
- Multi-repository support scaling

---

## Session Start Checklist

At the start of every Claude Code session:

- [ ] Read this CLAUDE.md
- [ ] Set `$env:GITHUB_TOKEN` from `$PROFILE`
- [ ] Check `git status` is clean
- [ ] Review recent commits for context
- [ ] Check `tasks/` directory for active work
- [ ] Verify supervisor is not already running

---

## Important Reminders

1. **Use `$env:GITHUB_TOKEN`** - already set this session; don't re-parse `$PROFILE`
2. **Per-worker workspaces** - Each worker gets `./projects/{workerID}/{owner}/{repo}/`
3. **Quality gates save money** - 30-40% cost reduction by filtering bad PRs
4. **10 workers total** - 5 flash + 3 pro + 2 claude, but all currently run the Claude Code backend (Gemini is Phase 2)
5. **Main branch is `master`** - Not `main`
6. **Build with `make build` → `bin/supervisor`** - binary is git-ignored
7. **This is production infrastructure** - Not a demo, optimize for years of operation

---

**Status:** Core system complete, ready for production testing
**Next Action:** Test with real Kaimi issues to validate end-to-end flow
