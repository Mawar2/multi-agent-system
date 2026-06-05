# Multi-Agent Orchestration System - Implementation Plan

## Context

**Project Location:** `C:\Users\Owner\OneDrive\Documents\Builder\Pulse\multi-agent-system`

This system will be reusable across multiple projects (Kaimi, others). Kaimi currently requires manual developer intervention to work through tickets. This plan implements a **multi-agent orchestration system** where:

- A **supervisor agent** monitors GitHub Issues and routes tickets to worker agents
- **Worker agents** (Local/Gemini/Claude tiers) autonomously claim tickets, implement solutions following project conventions, and create PRs
- **Humans approve all merges** - agents never auto-merge to main
- The system is **reusable across projects** by reading each project's conventions (CLAUDE.md, CONVENTIONS.md)

**Why this matters:** This transforms the developer's role from writing all code to being a Product Owner who writes detailed tickets and reviews PRs. Estimated 10x productivity increase through parallel autonomous workers.

**Success criteria:**
- Workers complete tickets autonomously following CLAUDE.md conventions
- PRs created with correct branch naming, commit format, and tests
- CI passes (tests + linter + AI review)
- Human reviews and merges
- System works on Kaimi first, then generalizes to other projects

---

## Architecture Overview

```
┌──────────────┐
│  SUPERVISOR  │  Polls GitHub Issues, routes to workers
└──────┬───────┘
       │
       ▼
┌──────────────────────────────────────────────────┐
│              TASK ROUTER                         │
│  Classifies complexity → Routes to tier          │
└──────┬───────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────┐
│            TASK QUEUE (Store pattern)            │
│  JSON-backed (Phase 1) → Firestore (Phase 2+)    │
└──────┬───────────────────────────────────────────┘
       │
   ┌───┴────┬─────────┬──────────┐
   ▼        ▼         ▼          ▼
┌──────┐┌──────┐┌───────┐┌────────┐
│Local ││Local ││Gemini ││Claude  │  Worker Pools
│(Free)││(Free)││(Cheap)││($$$)   │  - Claim tickets
└──────┘└──────┘└───────┘└────────┘  - Follow conventions
   │        │        │        │      - Create PRs
   └────────┴────────┴────────┘      - Never merge
              │
              ▼
        ┌──────────┐
        │GitHub PR │  Human reviews & merges
        └──────────┘
```

**Three-tier worker strategy (all using existing subscriptions):**
- **Gemini Flash (via Antigravity):** $0 (existing subscription) - Simple tasks (docs, formatting, simple tests)
- **Gemini 3.5 (via Antigravity):** $0 (existing subscription) - Moderate tasks (features, refactors, integrations)
- **Claude (via Claude Code CLI):** $0 (local instance) - Complex tasks (architecture, novel problems)

**Architecture is swappable:** Workers use abstraction layer that can swap between Claude Code CLI (Phase 1) and Vertex AI API (Phase 2+) without changing supervisor logic.

---

## Data Flow: Ticket → PR

```
GitHub Issue → Supervisor polls → Router classifies complexity →
Task enqueued with tier assignment → Worker (Local/Gemini/Claude) claims →
Worker reads conventions (CLAUDE.md) → Creates feature branch →
Implements solution (TDD) → Runs tests + linter →
Creates pull request → CI runs (tests/lint/AI review) →
Human reviews → Human merges → Supervisor marks complete →
Worker claims next task
```

---

## Implementation Phases

### Phase 1: MVP - Prove It Works on Kaimi

**Goal:** Single supervisor + workers using Claude Code CLI + JSON queue

**Deliverables:**
- Supervisor polls GitHub Issues (Kaimi repo only)
- Simple rule-based router (file count, keywords)
- Worker spawner using **Claude Code Task tool** (not separate processes)
- JSON-backed task queue (reuses Store interface pattern)
- Convention parser reads CLAUDE.md, CONVENTIONS.md
- Workers create branches, commits, PRs following conventions
- Human merges (no auto-merge)

**Worker implementation:**
- Uses Claude Code CLI's Task tool to spawn sub-agents
- Each worker is a Claude Code agent with ticket context injected
- **No additional API costs** - uses your local Claude Code instance

**Success metric:** Worker completes 1 simple ticket end-to-end with correct format

**Estimated files: ~12 new Go files** (fewer because no LLM client implementations)

---

### Phase 2: Add Antigravity Integration & Swappable Backend

**Goal:** Use Gemini via Antigravity, make backend swappable

**Deliverables:**
- **Antigravity integration** (Gemini 3.5 Flash via your subscription)
- **Swappable LLM backend** - abstraction layer allows Claude Code CLI OR Vertex API
- LLM-based complexity analysis (Gemini Flash via Antigravity)
- Worker pool manager (3-5 workers per tier)
- Health monitoring and task reassignment
- Retry logic for failed tasks

**Worker backend abstraction:**
```go
type LLMBackend interface {
    Execute(ctx context.Context, prompt string, model string) (string, error)
}

// Phase 1: ClaudeCodeBackend (uses Task tool)
// Phase 2+: AntigravityBackend (Gemini via your subscription)
// Phase 2+: VertexAIBackend (direct API, if needed)
```

**Cost:** $0 - uses existing Gemini subscription via Antigravity

**Success metric:** 10 tickets routed to appropriate tiers, completed in parallel

**Estimated files: ~8 additional Go files**

---

### Phase 3: Generalization & Multi-Project

**Goal:** Make system reusable across any project

**Deliverables:**
- Project configuration file (orchestrator.yml)
- Convention parser supports arbitrary project conventions
- Supervisor supports multiple repos
- Firestore-backed queue for distributed workers
- Metrics/observability (completion rates, worker utilization)

**Success metric:** Works on Kaimi + 1 other project with different conventions

**Estimated files: ~10 additional Go files + config templates**

---

## Critical Files to Create

### Phase 1 Core Files

**Package: internal/taskqueue/** (Task queue abstraction)
- `task.go` - Task data model (ID, issue number, complexity, tier, status, worker ID, branch name, PR number, attempts)
- `queue.go` - TaskQueue interface (Enqueue, Dequeue, Update, Get, List) - mirrors Store pattern
- `json.go` - JSON file-backed implementation (Phase 1)

**Package: internal/orchestrator/** (Supervisor logic)
- `supervisor.go` - Main supervisor loop (poll GitHub, route tasks, monitor workers)
- `router.go` - Task complexity classifier (rules-based Phase 1, LLM-based Phase 2)
- `config.go` - Orchestrator configuration struct

**Package: internal/worker/** (Worker implementations)
- `worker.go` - Worker interface (Claim, Execute, Release, Health methods)
- `claudecode.go` - Claude Code Task tool-based worker (Phase 1)
- `pool.go` - Worker pool management

**Package: internal/conventions/** (Convention parsing)
- `parser.go` - Reads CLAUDE.md, CONVENTIONS.md, extracts rules
- `ruleset.go` - Ruleset data model (branch pattern, commit pattern, test requirements)

**Package: internal/llm/** (LLM backend abstraction - swappable)
- `backend.go` - LLMBackend interface (prompt → response)
- `claudecode.go` - Claude Code Task tool backend (Phase 1)
- `antigravity.go` - Antigravity Gemini backend (Phase 2)
- `vertex.go` - Vertex AI direct backend (Phase 2+, if needed)

**Package: internal/ticket/** (GitHub integration)
- `client.go` - GitHub API client (fetch issues, parse acceptance criteria)

**Entry points:**
- `multi-agent-system/cmd/supervisor/main.go` - Supervisor binary entry point
- **Note:** No separate worker binary needed - workers spawn via Task tool from supervisor

**Project structure:**
```
multi-agent-system/
├── cmd/supervisor/        # Supervisor entry point
├── internal/
│   ├── taskqueue/        # Task queue (Store pattern)
│   ├── orchestrator/     # Supervisor & router logic
│   ├── worker/           # Worker implementations
│   ├── conventions/      # Convention parser
│   ├── llm/              # LLM backend abstraction
│   └── ticket/           # GitHub integration
├── docs/
│   └── PLAN.md           # This implementation plan
├── go.mod
├── go.sum
└── README.md
```

---

## Key Design Decisions

### 1. Workers via Claude Code Task Tool (not separate binaries)
- **Why:** Leverage existing Claude Code infrastructure, no additional API costs
- **How:** Supervisor spawns workers using Task tool with injected context
- **Benefit:** Simpler deployment, uses local Claude Code instance
- **Future:** Abstraction layer allows swapping to Antigravity/Vertex API later

### 2. Pull-Based Coordination (workers poll queue)
- **Why:** More resilient than push (worker crash doesn't lose task)
- **Benefit:** Workers self-organize, simpler supervisor logic

### 3. Store Interface Pattern for Queue
- **Why:** Proven pattern in Kaimi (see internal/store/)
- **Phase 1:** JSON files (simple, fast to build)
- **Phase 2+:** Firestore (distributed, scalable)

### 4. Hybrid Complexity Routing
- **Phase 1:** Rules-based (file count, keywords) - fast, deterministic
- **Phase 2:** LLM-based (Gemini Flash scoring) - more accurate

### 5. Convention-Driven Design
- **Why:** Makes system reusable across projects
- **How:** Parser reads CLAUDE.md/CONVENTIONS.md at task start
- **Benefit:** Workers automatically adapt to each project's rules

### 6. Never Auto-Merge
- **Why:** Critical safety requirement from CLAUDE.md
- **How:** Workers create PRs, human approves and merges
- **Benefit:** Human always has final say on code changes

---

## Integration with Existing Kaimi Code

### Patterns to Reuse

**1. Store Interface Pattern (internal/store/store.go)**
```go
// TaskQueue mirrors Store interface design
type TaskQueue interface {
    Enqueue(ctx context.Context, task *Task) error
    Dequeue(ctx context.Context, tier Tier) (*Task, error)
    Update(ctx context.Context, task *Task) error
    Get(ctx context.Context, taskID string) (*Task, error)
    List(ctx context.Context, filter *TaskFilter) ([]*Task, error)
}

// JSON implementation (Phase 1) mirrors internal/store/json.go
// Firestore implementation (Phase 2+) mirrors future store/firestore.go
```

**2. GCP/Gemini Integration (from .github/workflows/ci.yml)**
- Reuse existing Vertex AI setup (project: kaimi-seeker, region: us-east4)
- Reuse service account authentication (GOOGLE_APPLICATION_CREDENTIALS)
- Gemini worker uses same auth as AI code review

**3. Convention Enforcement**
- Workers enforce existing branch pattern: `feature/KAI-XXX-summary`
- Workers enforce commit format: `XXX_feature_description`
- Workers run `make test` and `make lint` before creating PR

**4. CI/CD Integration**
- Worker-created PRs trigger existing CI pipeline (.github/workflows/ci.yml)
- Tests, linter, and AI code review run automatically
- No changes to CI needed

### Files That DON'T Change

- `internal/opportunity/` - Untouched (agents work on tickets, not opportunities)
- `internal/samgov/` - Untouched (Hunter agent independent)
- `internal/store/` - Untouched (queue uses same pattern, separate package)
- `cmd/hunter/` - Untouched (existing agent continues to work)
- `.github/workflows/ci.yml` - Untouched (works with orchestrator PRs automatically)

### New Dependencies to Add

**Phase 1:**
```go
// go.mod additions
require (
    github.com/google/go-github/v60 v60.0.0         // GitHub API (if not using MCP)
    gopkg.in/yaml.v3 v3.0.1                         // Config parsing
)
```

**Phase 2 (only if adding Antigravity HTTP client):**
```go
require (
    // Potentially HTTP client for Antigravity integration
    // Or continue using Claude Code Task tool with model selection
)
```

**Note:** No Ollama, Anthropic, or direct Vertex AI SDKs needed in Phase 1 - using Claude Code CLI Task tool instead

---

## Verification Plan

### Phase 1 Testing

**Unit tests (per WORKFLOW.md TDD requirement):**
```bash
# Test each component in isolation
go test ./internal/taskqueue/...    # Queue operations, atomic claims
go test ./internal/orchestrator/... # Router, supervisor logic
go test ./internal/worker/...       # Worker interface implementations
go test ./internal/conventions/...  # Parser extracts correct rules
```

**Integration test (end-to-end with mocks):**
```bash
# Create test GitHub Issue
# Start supervisor
# Start local worker
# Verify task flows: enqueued → claimed → in-progress → review
# Verify PR created with correct format
# Verify CI would pass (run tests locally)
```

**Manual E2E test:**
1. Create simple GitHub Issue on Kaimi repo (e.g., "Add godoc comment to function X")
2. Start supervisor: `./bin/supervisor --config orchestrator.yml`
3. Supervisor spawns worker via Task tool
4. Watch logs: supervisor polls issue → routes to worker → worker (Claude Code agent) claims → implements → creates PR
5. Review PR: verify branch name `feature/KAI-N-summary`, commit format `N_description`, tests pass
6. Human merges PR
7. Supervisor detects merge, marks task complete

**Success criteria for Phase 1:**
- [ ] Worker completes 1 simple ticket end-to-end
- [ ] Branch name follows convention: `feature/KAI-XXX-*`
- [ ] Commit format follows convention: `XXX_feature_description`
- [ ] PR includes tests (TDD followed)
- [ ] `make test` passes
- [ ] `make lint` passes
- [ ] CI pipeline passes (tests + linter + AI review)
- [ ] Human reviews and merges
- [ ] Supervisor correctly tracks state transitions

### Phase 2 Testing

**Multi-tier routing test:**
1. Create 10 GitHub Issues: 5 simple, 3 medium, 2 complex
2. Start supervisor + 2 local + 2 gemini + 1 claude workers
3. Verify router correctly classifies complexity
4. Verify tasks distributed across tiers
5. Verify all complete without conflicts

**Concurrent worker test:**
1. Create 5 identical simple issues
2. Start 5 local workers
3. Verify atomic claiming (no double-assignment)
4. Verify all complete successfully

**Retry test:**
1. Inject worker failure (kill worker mid-task)
2. Verify task returns to queue
3. Verify another worker claims and completes

### Phase 3 Testing

**Multi-project test:**
1. Set up orchestrator with 2 projects (Kaimi + test repo)
2. Create issues on both repos
3. Verify workers adapt conventions per project
4. Verify correct branch patterns for each project

---

## Rollout Strategy

**Week 1: Build Phase 1 MVP**
- Create core packages (taskqueue, orchestrator, worker, conventions)
- Implement JSON queue + local worker + supervisor
- Write tests for each component

**Week 2: Test & Iterate**
- Run Phase 1 manual E2E test
- Fix bugs found during testing
- Tune router rules based on real tickets

**Week 3: Add Multi-Tier (Phase 2)**
- Implement Gemini + Claude workers
- Add LLM-based routing
- Test with 10-20 real Kaimi tickets

**Week 4: Measure & Optimize**
- Track metrics: completion rate, time per ticket, cost per tier
- Optimize routing rules based on results
- Document operational runbook

**Month 2+: Generalize (Phase 3)**
- Test on second project
- Build multi-project configuration
- Transition to Firestore queue if needed

---

## Cost Estimates

**Phase 1 (Claude Code CLI only):** $0/day

**Phase 2 (Multi-tier, 20 tickets/day):**
- 12 tickets → Gemini Flash (Antigravity, existing subscription) = $0
- 6 tickets → Gemini 3.5 (Antigravity, existing subscription) = $0
- 2 tickets → Claude (local Claude Code CLI) = $0
- **Total: $0/day**

**Cost optimization:**
- All tiers use services you already pay for (Gemini subscription, local Claude Code)
- No per-ticket API charges
- Only costs are infrastructure you already have

---

## Risks & Mitigations

| Risk | Mitigation |
|------|-----------|
| Workers create buggy code | CI enforces tests + linter; AI review provides feedback; human final gate |
| Worker claims ticket, then crashes | Task has timeout (2hr); supervisor reassigns if stalled |
| Tickets too complex for workers | Max 3 retry attempts, then escalate to human |
| Workers conflict on same files | Atomic task claiming; workers on separate branches |
| Convention parser misreads rules | Parser has unit tests with fixtures; fallback to defaults if parse fails |
| Cost spirals (too many Claude calls) | Router enforces tier limits; metrics track cost per ticket |

---

## Open Questions (To Clarify with User)

*None - plan is complete and ready for implementation.*

The architecture follows Kaimi's established patterns (Store interface, TDD, convention enforcement), integrates with existing infrastructure (GCP/Gemini, CI/CD, GitHub Issues), and provides a clear phased rollout path from MVP to multi-project system.
