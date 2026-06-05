# Team Tasks for Multi-Agent Orchestration Support

**16 GitHub issues ready to create for team members**

Copy/paste these into GitHub Issues, or use the script at the bottom to create all at once.

---

## 1. Fix GitHub PR search timeout ⚡ CRITICAL

**Title:** Fix GitHub PR search timeout - increase from 30s to 120s

**Labels:** `priority:critical`, `bug`, `infrastructure`

**Body:**
```markdown
## Summary
GitHub PR status checks are timing out on every request, preventing the supervisor from detecting existing PRs. This causes workers to attempt duplicate work.

## Problem
Every PR status check fails with:
```
context deadline exceeded (Client.Timeout exceeded while awaiting headers)
```

The current 30-second timeout in `internal/ticket/github_rest_client.go:30` is too short for GitHub's search API.

## Solution
Increase HTTP client timeout from 30s to 120s in GitHubRESTClient.

## Acceptance Criteria
- [ ] Timeout increased to 120 seconds in `internal/ticket/github_rest_client.go`
- [ ] PR status checks succeed without timeout errors
- [ ] Supervisor logs show successful PR detection
- [ ] No regression in other API calls

## Files to Modify
- `internal/ticket/github_rest_client.go` (line 30)

**Skills:** Go, HTTP clients
**Effort:** 30 minutes
**Priority:** **CRITICAL** - Currently blocking duplicate work detection
```

---

## 2. Improve response parsing robustness

**Title:** Improve response parsing - add more extraction patterns

**Labels:** `enhancement`, `worker`, `reliability`

**Body:**
```markdown
## Summary
Some workers fail with "could not extract branch name from LLM response" even when Claude CLI returns valid work. Need more robust parsing patterns.

## Problem
Current extraction patterns in `internal/worker/claudecode.go` have 6 strategies for branch names and 4 for PR numbers, but still miss some valid response formats.

## Solution
Add additional extraction patterns to catch edge cases:
- Branch names in code blocks
- PR numbers in summary sections
- Alternative git output formats
- URL variations

## Acceptance Criteria
- [ ] At least 3 new branch extraction patterns added
- [ ] At least 2 new PR number extraction patterns added
- [ ] Unit tests added for each new pattern
- [ ] All existing tests still pass
- [ ] Documentation updated with pattern examples

## Files to Modify
- `internal/worker/claudecode.go` (lines 342-424)
- `internal/worker/claudecode_test.go` (add new test cases)

**Skills:** Go, regex
**Effort:** 2 hours
```

---

## 3. Build observability dashboard

**Title:** Build observability dashboard for supervisor and workers

**Labels:** `feature`, `infrastructure`, `observability`

**Body:**
```markdown
## Summary
Create a web dashboard to monitor the multi-agent orchestration system in real-time.

## Features
**Dashboard should display:**
- Active workers and their current status
- Task queue depth by tier (Flash/Pro/Claude)
- Success/failure rates (overall and per-tier)
- Average task completion time
- Recent completions (last 10 PRs created)
- Error log (recent failures with details)

**Technical Stack:**
- Backend: REST API endpoint in supervisor (Go)
- Frontend: Web UI (React/Vue/vanilla JS)
- Updates: WebSocket or SSE for real-time updates

## Acceptance Criteria
- [ ] REST API endpoint `/api/status` returns JSON with all metrics
- [ ] Web UI displays all required metrics
- [ ] Updates in real-time (< 5 second lag)
- [ ] Responsive design (works on mobile)
- [ ] Documentation for running dashboard

**Skills:** Full-stack, Go REST APIs, React/Vue
**Effort:** 1 week
```

---

## 4. Implement worker health checks

**Title:** Implement worker health checks and auto-restart

**Labels:** `reliability`, `infrastructure`, `worker`

**Body:**
```markdown
## Summary
Add heartbeat mechanism to detect and auto-restart crashed or hung workers.

## Problem
Workers can hang indefinitely if Claude CLI subprocess freezes or crashes. No mechanism currently detects or recovers from this.

## Solution
**Heartbeat System:**
1. Workers send heartbeat every 30 seconds
2. Supervisor tracks last heartbeat per worker
3. If worker silent for 5+ minutes → mark as unhealthy
4. If worker working on task for 20+ minutes → timeout and release task

**Auto-Restart:**
1. Supervisor kills hung worker goroutine
2. Releases claimed task back to queue
3. Spawns replacement worker with same ID

## Acceptance Criteria
- [ ] Worker heartbeat mechanism implemented
- [ ] Supervisor detects unhealthy workers within 5 minutes
- [ ] Hung workers auto-restarted
- [ ] Tasks released back to queue on worker failure
- [ ] Health status visible in logs
- [ ] Unit tests for health check logic

**Skills:** Go concurrency, process management
**Effort:** 3 days
```

---

## 5. Replace fmt.Printf with structured logging

**Title:** Replace fmt.Printf with structured logging (zerolog or zap)

**Labels:** `enhancement`, `infrastructure`, `logging`

**Body:**
```markdown
## Summary
Replace all `fmt.Printf` and `fmt.Fprintf` calls with structured logger for better production debugging.

## Why
Current logging uses unstructured prints, making it hard to:
- Filter logs by severity
- Parse logs programmatically
- Add correlation IDs for request tracing
- Output JSON for log aggregation tools

## Solution
Adopt structured logging library (recommend zerolog for performance):

\`\`\`go
logger.Info().
    Str("worker_id", w.id).
    Str("task_id", task.ID).
    Int("issue_number", task.IssueNumber).
    Msg("Claimed task")
\`\`\`

## Acceptance Criteria
- [ ] Logging library chosen and added to go.mod (zerolog or zap)
- [ ] All fmt.Printf/Fprintf replaced with structured logger
- [ ] Log levels used appropriately (Debug, Info, Warn, Error)
- [ ] Consistent field names across codebase
- [ ] JSON output mode configurable via flag
- [ ] Documentation for log fields and format

**Skills:** Go logging frameworks
**Effort:** 2 days
```

---

## 6. Smart issue filtering

**Title:** Smart issue filtering - skip issues that shouldn't be auto-worked

**Labels:** `enhancement`, `orchestrator`, `quality`

**Body:**
```markdown
## Summary
Filter out GitHub issues that shouldn't be automatically worked by agents.

## Problem
System currently attempts to work on ALL open issues, including:
- Issues that already have PRs
- Issues marked as needing human design input
- Issues missing acceptance criteria
- Meta/planning issues not meant for implementation

## Solution
Add filtering logic in supervisor before enqueuing tasks.

**Skip issues if:**
1. Already has an open/merged PR (check via GitHub API)
2. Has label `needs-human-design` or `planning`
3. Missing acceptance criteria checklist in body
4. Has label `duplicate` or `wontfix`
5. Title starts with "Meta:" or "[Discussion]"

## Acceptance Criteria
- [ ] Issue filtering implemented in supervisor
- [ ] Configuration options in orchestrator.yml
- [ ] Skipped issues logged with reason
- [ ] PR existence check working (depends on timeout fix)
- [ ] Acceptance criteria validation working
- [ ] Unit tests for filtering logic

**Skills:** Go, GitHub API
**Effort:** 1 week
```

---

## 7. Build GeminiWorker (Phase 2)

**Title:** Build GeminiWorker with plan-execute pattern (Phase 2)

**Labels:** `feature`, `worker`, `phase-2`

**Body:**
```markdown
## Summary
Implement separate worker type that uses Gemini API directly instead of Claude CLI subprocess.

## Why
Current system uses Claude Code CLI for ALL tiers, which is expensive and slow. Gemini workers will be faster and cheaper for simple/medium tasks.

## Architecture
\`\`\`
GeminiWorker → Gemini API (get plan) → GitExecutor (execute plan) → GitHub API (create PR)
\`\`\`

## Acceptance Criteria
- [ ] `internal/worker/gemini.go` - GeminiWorker implementation
- [ ] `internal/worker/executor.go` - GitExecutor for git operations
- [ ] `internal/llm/gemini.go` - Gemini API client
- [ ] Plan parsing from Gemini responses
- [ ] Git operations (branch, commit, push)
- [ ] PR creation via GitHub API
- [ ] Unit tests for each component
- [ ] Integration test with mocked Gemini API
- [ ] Documentation in README

## Reference
See approved design in `.claude/plans/tingly-mapping-graham.md` (Fix 4 & Fix 5)

**Skills:** Go, Gemini API, git automation
**Effort:** 2 weeks
```

---

## 8. Add PR quality gates

**Title:** Add PR quality gates - verify work before marking complete

**Labels:** `enhancement`, `quality`, `worker`

**Body:**
```markdown
## Summary
Before marking a task as complete, verify that the claimed work was actually done.

## Problem
Current implementation trusts LLM response parsing without verifying:
- Branch actually exists on GitHub
- PR actually created
- PR references the correct issue
- CI checks are passing

## Solution
Add verification step before marking task complete.

## Acceptance Criteria
- [ ] Branch existence verification via GitHub API
- [ ] PR existence and issue reference verification
- [ ] Optional CI status check (configurable)
- [ ] Failed verifications logged with details
- [ ] Tasks fail if verification fails
- [ ] Unit tests with mocked GitHub API
- [ ] Configuration option in orchestrator.yml

**Skills:** Go, GitHub API
**Effort:** 1 week
```

---

## 9. Implement caching layer

**Title:** Implement caching layer for GitHub API responses

**Labels:** `optimization`, `infrastructure`, `github-api`

**Body:**
```markdown
## Summary
Cache GitHub API responses to reduce API calls and avoid rate limiting.

## Problem
Supervisor polls GitHub every 60 seconds, making identical API calls. With 13 issues × 60-second poll = ~780 API calls/hour just for polling.

## Solution
Add in-memory cache with TTL:
- Issue list: 5 minute TTL
- PR details: 2 minute TTL
- Bust cache on write operations (create PR, update issue)

## Acceptance Criteria
- [ ] Cache implementation in `internal/ticket/cache.go`
- [ ] GitHubRESTClient uses cache for GET requests
- [ ] TTL configurable in orchestrator.yml
- [ ] Cache hit/miss metrics logged
- [ ] Thread-safe (uses sync.RWMutex)
- [ ] Unit tests for cache logic
- [ ] Documentation for cache behavior

**Skills:** Go, in-memory caching (sync.Map or custom)
**Effort:** 2 days
```

---

## 10. Multi-project support

**Title:** Multi-project support - monitor multiple repositories simultaneously

**Labels:** `enhancement`, `orchestrator`, `multi-project`

**Body:**
```markdown
## Summary
Enable supervisor to monitor and route work for multiple GitHub repositories concurrently with per-project worker allocation and fair scheduling.

## Acceptance Criteria
- [ ] Per-project worker allocation working
- [ ] Priority queue implementation
- [ ] Fair scheduling across projects
- [ ] Metrics per project (tasks completed, PRs created)
- [ ] Tested with 2+ active projects
- [ ] Documentation for multi-project setup

**Skills:** Go, orchestration logic, priority queues
**Effort:** 1 week
```

---

## 11. Add task prioritization

**Title:** Add task prioritization based on issue labels

**Labels:** `enhancement`, `orchestrator`, `priority`

**Body:**
```markdown
## Summary
Route high-priority issues to the front of the queue instead of FIFO processing.

## Priority Levels (from issue labels)
1. **P0 / Critical** - `priority:critical`, `bug:critical`
2. **P1 / High** - `priority:high`, `blocked-by`
3. **P2 / Normal** - Default (no label)
4. **P3 / Low** - `priority:low`, `nice-to-have`

## Acceptance Criteria
- [ ] Priority queue implementation
- [ ] Issue labels mapped to priority levels
- [ ] Dequeue respects priority (P0 before P1 before P2 before P3)
- [ ] Same-priority tasks still FIFO
- [ ] Priority configurable in orchestrator.yml
- [ ] Unit tests for priority logic
- [ ] Documentation for label conventions

**Skills:** Go, priority queue/heap implementation
**Effort:** 3 days
```

---

## 12. Build integration test suite

**Title:** Build integration test suite with mocked GitHub and Claude CLI

**Labels:** `testing`, `infrastructure`, `quality`

**Body:**
```markdown
## Summary
Create end-to-end integration tests covering the full orchestration flow.

## Test Scenarios
1. **Happy path** - 3 issues discovered, routed, worked, PRs created
2. **Worker failure** - Worker crashes, task released and retried
3. **Routing logic** - Issues routed to correct tiers by complexity
4. **Duplicate detection** - Issue already in queue skipped
5. **PR exists** - Issue with existing PR skipped
6. **Failed extraction** - Worker fails to parse response, task marked failed

## Acceptance Criteria
- [ ] Mock GitHub client implemented
- [ ] Mock Claude backend implemented
- [ ] At least 6 integration test scenarios
- [ ] Tests run in CI (fast, no external deps)
- [ ] Code coverage report generated
- [ ] Documentation for adding new tests

**Skills:** Go testing, mocking, test fixtures
**Effort:** 1 week
```

---

## 13. Load testing

**Title:** Load testing - test with 100+ issues and 50+ workers

**Labels:** `testing`, `performance`, `reliability`

**Body:**
```markdown
## Summary
Stress test the orchestration system to find performance bottlenecks and bugs.

## Test Scenarios
1. **High volume** - 100 issues, 10 workers, run for 1 hour
2. **High concurrency** - 10 issues, 50 workers (worker competition)
3. **Long running** - 20 issues, 5 workers, run for 24 hours
4. **Failure recovery** - Inject worker crashes, verify recovery

## Tools
- `go test -race` - Detect race conditions
- `pprof` - CPU and memory profiling
- Custom load generator for mocked GitHub/Claude

## Acceptance Criteria
- [ ] Load test suite implemented
- [ ] All 4 test scenarios automated
- [ ] No race conditions detected
- [ ] No goroutine leaks (goroutine count stable)
- [ ] No memory leaks (memory usage stable after warmup)
- [ ] Performance benchmarks documented
- [ ] Bottlenecks identified and documented
- [ ] CI job runs subset of load tests on PR

**Skills:** Go performance testing, profiling (pprof)
**Effort:** 3 days
```

---

## 14. Containerization

**Title:** Containerization - create Dockerfile and docker-compose.yml

**Labels:** `infrastructure`, `deployment`, `docker`

**Body:**
```markdown
## Summary
Package the supervisor as a Docker container for consistent deployment.

## Deliverables
- Multi-stage Dockerfile producing small image (<50MB)
- docker-compose.yml for local development
- Environment variable documentation
- Health check endpoint configuration

## Acceptance Criteria
- [ ] Dockerfile builds successfully
- [ ] Multi-stage build produces small image (<50MB)
- [ ] docker-compose.yml for local development
- [ ] Environment variables documented
- [ ] Volume mounts for config and task queue
- [ ] Health check endpoint works
- [ ] README updated with Docker instructions

**Skills:** Docker, multi-stage builds
**Effort:** 2 days
```

---

## 15. CI/CD pipeline

**Title:** CI/CD pipeline - GitHub Actions for test, build, deploy

**Labels:** `infrastructure`, `ci-cd`, `automation`

**Body:**
```markdown
## Summary
Automate testing, building, and deployment with GitHub Actions.

## Workflows to Create
1. **PR Workflow** - Run tests, linter, build, coverage on every PR
2. **Main Workflow** - Deploy to staging on merge to main
3. **Release Workflow** - Create GitHub releases and deploy to production

## Acceptance Criteria
- [ ] PR workflow runs on every PR
- [ ] Main workflow deploys to staging on merge
- [ ] Release workflow creates GitHub releases
- [ ] All workflows use caching for dependencies
- [ ] Test results posted as PR comments
- [ ] Docker images tagged with git SHA and semver
- [ ] Secrets managed via GitHub Secrets
- [ ] Documentation for CI/CD process

**Skills:** GitHub Actions, Docker, Go builds
**Effort:** 3 days
```

---

## 16. Documentation - operator runbook

**Title:** Documentation - write operator runbook for production operations

**Labels:** `documentation`, `operations`

**Body:**
```markdown
## Summary
Create comprehensive documentation for operators running the multi-agent orchestration system in production.

## Runbook Sections
1. **Getting Started** - Prerequisites, installation, configuration
2. **Operation** - Starting/stopping, monitoring worker health, queue status
3. **Configuration** - Adding projects, adjusting workers, timeouts
4. **Troubleshooting** - Workers stuck, queue backing up, API rate limits
5. **Monitoring** - Key metrics, alert thresholds, dashboard
6. **Maintenance** - Updating, backup/restore, scaling
7. **Security** - Token management, secrets, rate limiting

## Acceptance Criteria
- [ ] `docs/RUNBOOK.md` created with all sections
- [ ] Code examples for common operations
- [ ] Screenshots/diagrams for architecture
- [ ] Troubleshooting decision tree
- [ ] Alert runbook (if X happens, do Y)
- [ ] Reviewed by someone who didn't write it
- [ ] Linked from main README.md

**Skills:** Technical writing
**Effort:** 2 days
```

---

## Quick Create Script

Once you have `gh` CLI installed, run this to create all 16 issues at once:

```bash
# Save this as create_team_issues.sh
chmod +x create_team_issues.sh
./create_team_issues.sh
```

The script content is in the repository at `scripts/create_team_issues.sh` (to be created).

---

## Summary by Priority

**CRITICAL (do first):**
- #1: Fix GitHub PR search timeout (30 min)

**High Impact:**
- #2: Improve response parsing (2 hours)
- #4: Worker health checks (3 days)
- #6: Smart issue filtering (1 week)

**Infrastructure:**
- #3: Observability dashboard (1 week)
- #5: Structured logging (2 days)
- #9: Caching layer (2 days)

**Phase 2:**
- #7: GeminiWorker (2 weeks) - Major feature

**Quality:**
- #8: PR quality gates (1 week)
- #12: Integration tests (1 week)
- #13: Load testing (3 days)

**Ops:**
- #14: Docker (2 days)
- #15: CI/CD (3 days)
- #16: Documentation (2 days)

**Enhancements:**
- #10: Multi-project (1 week)
- #11: Task prioritization (3 days)
