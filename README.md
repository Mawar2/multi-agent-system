# Multi-Agent Orchestration System

**A reusable multi-agent system that autonomously processes GitHub Issues across multiple projects.**

## Overview

This system transforms developers into Product Owners by autonomously handling ticket implementation:

- **Supervisor** monitors GitHub Issues and routes to workers
- **Workers** (Gemini Flash/3.5 via Antigravity, Claude via CLI) implement solutions
- **Convention-driven** - reads each project's CLAUDE.md/CONVENTIONS.md
- **Human-in-loop** - creates PRs, never auto-merges
- **Cost-optimized** - $0/day using existing subscriptions

## Architecture

```
Supervisor → Task Router → Task Queue → Worker Pool → GitHub PRs → AI Review → Fix Loop → Human Review
```

**Three-tier worker strategy:**
- Gemini Flash: Simple tasks (docs, formatting)
- Gemini 3.5: Moderate tasks (features, refactors)
- Claude: Complex tasks (architecture, novel problems)

**AI Review Feedback Loop:**
- Supervisor monitors PRs for AI code review comments (every 120s)
- Automatically creates "fix" tasks when feedback detected
- Workers iteratively improve PRs based on review feedback
- Max 3 review iterations to prevent infinite loops
- See [AI_REVIEW_FEEDBACK_LOOP.md](AI_REVIEW_FEEDBACK_LOOP.md) for details

## Quick Start

```bash
# Build supervisor
cd cmd/supervisor
go build -o ../../bin/supervisor

# Configure
cp orchestrator.example.yml orchestrator.yml
# Edit orchestrator.yml with your project settings

# Run
./bin/supervisor --config orchestrator.yml
```

## Project Structure

```
multi-agent-system/
├── cmd/supervisor/          # Supervisor entry point
├── internal/
│   ├── taskqueue/          # Task queue (Store pattern)
│   ├── orchestrator/       # Supervisor & router
│   ├── worker/             # Worker implementations
│   ├── conventions/        # Convention parser
│   ├── llm/                # LLM backend abstraction
│   └── ticket/             # GitHub integration
├── docs/
│   └── PLAN.md             # Full implementation plan
├── test/                   # Integration tests
├── go.mod
└── README.md
```

## Implementation Status

- [ ] Phase 1: MVP (Claude Code CLI workers)
- [ ] Phase 2: Antigravity integration (Gemini workers)
- [ ] Phase 3: Multi-project support

See [docs/PLAN.md](docs/PLAN.md) for full implementation plan.

## How It Works

### Issue → PR Flow
1. Supervisor polls GitHub Issues from configured repos (every 60s)
2. Task router classifies complexity (simple/moderate/complex)
3. Task enqueued in JSON queue with tier assignment ("issue" type)
4. Worker (appropriate tier) claims task
5. Worker reads project conventions (CLAUDE.md)
6. Worker implements solution following conventions (TDD)
7. Worker creates PR with correct branch/commit format
8. Task marked as "Review" status

### AI Review Feedback Loop
9. CI runs (tests + linter + AI review via Gemini 2.5 Pro)
10. Supervisor monitors PRs for AI review comments (every 120s)
11. If feedback detected: Creates "pr_feedback" task (iteration 1)
12. Worker claims fix task, checkouts existing branch, applies fixes
13. Updates existing PR, CI runs again
14. Loop continues until review passes or max 3 iterations
15. Human reviews final PR and merges
16. Supervisor marks complete, worker claims next task

## Configuration

`orchestrator.yml` example:

```yaml
projects:
  - name: kaimi
    repo: Mawar2/Kaimi
    conventions_path: ./CLAUDE.md
    branch_pattern: "feature/KAI-{ticket}-{summary}"
    commit_pattern: "{ticket}_{description}"

worker_tiers:
  gemini_flash:
    max_workers: 5
    model: gemini-flash-3.5
  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5
  claude:
    max_workers: 2
    model: claude-sonnet-4.5
```

## Development

```bash
# Run tests
go test ./...

# Run linter
golangci-lint run

# Build all
make all
```

## Cost

**$0/day** - uses existing services:
- Gemini via Antigravity subscription
- Claude via local Claude Code CLI
- No per-ticket API charges

## License

MIT
