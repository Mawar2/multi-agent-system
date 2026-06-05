# Worker Package

The worker package provides autonomous agent workers that claim and complete tasks from the queue.

## Overview

Workers are the core execution units in the multi-agent orchestration system. They:
- Claim tasks from the queue based on their tier (gemini-flash, gemini-pro, claude)
- Parse project conventions (CLAUDE.md, CONVENTIONS.md)
- Execute tasks using LLM backends (Claude Code CLI, Antigravity, etc.)
- Create feature branches following project patterns
- Implement solutions using TDD when required
- Run tests and linters
- Create pull requests with proper formatting
- Report health and completion statistics

## Components

### ClaudeCodeWorker

The primary worker implementation in Phase 1, using Claude Code CLI as the backend.

**Key Features:**
- Atomic task claiming from queue
- Convention-driven implementation (reads CLAUDE.md, CONVENTIONS.md)
- Detailed prompt construction with task context + conventions
- Response parsing to extract branch names and PR numbers
- Automatic task status updates (Claimed → InProgress → Review)
- Error handling with task release on failure
- Thread-safe statistics tracking

**Usage:**

```go
backend := llm.NewClaudeCodeBackend()
queue := taskqueue.NewJSONQueue("/path/to/queue.json")

worker := worker.NewClaudeCodeWorker(
    "claude-worker-1",
    taskqueue.TierClaude,
    queue,
    backend,
    "/path/to/projects",
)

// Claim task
task, err := worker.Claim(ctx)
if err != nil {
    log.Fatalf("Failed to claim: %v", err)
}

if task == nil {
    log.Println("No tasks available")
    return
}

// Execute task
result, err := worker.Execute(ctx, task)
if err != nil {
    log.Fatalf("Failed to execute: %v", err)
}

if result.Success {
    log.Printf("✅ PR created: #%d on branch %s", result.PRNumber, result.BranchName)
} else {
    log.Printf("❌ Task failed: %s", result.ErrorMsg)
}
```

## Prompt Construction

Workers build comprehensive prompts that include:

1. **Task Details**
   - Repository (owner/name)
   - Issue number and title
   - Full description with acceptance criteria

2. **Project Conventions**
   - Branch naming pattern (e.g., `feature/KAI-{ticket}-{summary}`)
   - Commit message format (e.g., `{ticket}_{description}`)
   - Forbidden files (e.g., `utils.go`, `helpers.go`)
   - Test command (e.g., `make test`)
   - Lint command (e.g., `make lint`)
   - Format command (e.g., `gofmt -w .`)
   - TDD requirement (if enforced)

3. **Implementation Instructions**
   - Step-by-step workflow
   - Create feature branch
   - Implement solution (TDD if required)
   - Run quality checks (test, lint, format)
   - Create pull request
   - Report results (branch name, PR number)

## Response Parsing

Workers extract structured data from LLM responses:

**Branch Name Extraction:**
- Looks for patterns: `Branch: feature/KAI-123-summary`
- Also handles: `Created branch feature/...`
- Standalone branch lines: `feature/KAI-456-update`

**PR Number Extraction:**
- Looks for patterns: `PR: #456` or `Pull Request: #123`
- Extracts number after `#` symbol

## Testing

Comprehensive test suite with 87.9% coverage:

- `TestNewClaudeCodeWorker` - Worker initialization
- `TestClaim` - Task claiming (success, no tasks, errors)
- `TestExecute` - Task execution (success, LLM failure, missing data)
- `TestRelease` - Task release (success, errors)
- `TestHealth` - Health status reporting
- `TestBuildPrompt` - Prompt construction
- `TestExtractBranchName` - Branch name parsing
- `TestExtractPRNumber` - PR number parsing

Run tests:

```bash
go test ./internal/worker/... -v
```

Run with coverage:

```bash
go test ./internal/worker/... -v -cover
```

## Error Handling

Workers handle errors gracefully:

1. **Convention Parsing Failure**
   - Falls back to sensible defaults (e.g., `go test ./...`)
   - Continues execution with default commands

2. **LLM Execution Failure**
   - Marks task as Failed
   - Updates error message in task
   - Increments tasksFailed counter
   - Returns Result with success=false

3. **Missing Response Data**
   - If branch name missing: task fails
   - If PR number missing: task succeeds with PRNumber=0 (can be fixed manually)

4. **Queue Update Failure**
   - Logs warning but continues
   - Best-effort update to avoid blocking worker

## Worker Lifecycle

```
┌─────────────┐
│   Worker    │
│  Created    │
└──────┬──────┘
       │
       ├─────► Claim Task ─────► No Tasks ────┐
       │                            Available   │
       ├─────► Task Claimed ──────────────────►│
       │                                        │
       ├─────► Execute Task                    │
       │          │                             │
       │          ├──► Success ─────────────►  │
       │          │                             │
       │          └──► Failure ──► Release ──► │
       │                                        │
       └────────────────────────────────────────┘
                   (Loop Continues)
```

## Statistics

Workers track:
- `tasksCompleted` - Number of successful executions
- `tasksFailed` - Number of failed executions
- Thread-safe with mutex protection

Query via Health():

```go
health, err := worker.Health(ctx)
fmt.Printf("Worker %s: %d completed, %d failed\n",
    health.WorkerID, health.TasksCompleted, health.TasksFailed)
```

## Phase 1 Scope

Current implementation:
- ✅ ClaudeCodeWorker with Claude Code CLI backend
- ✅ Convention parsing from CLAUDE.md, CONVENTIONS.md
- ✅ Detailed prompt construction
- ✅ Response parsing (branch name, PR number)
- ✅ Task status updates (Claimed → InProgress → Review)
- ✅ Error handling with task release
- ✅ Health status reporting
- ✅ Comprehensive test suite (87.9% coverage)

## Future Phases

Phase 2+:
- GeminiWorker using Antigravity backend
- Advanced health monitoring with metrics
- Worker pool management with auto-scaling
- Retry strategies with exponential backoff
- Integration with distributed queue (Firestore)

## Files

- `worker.go` - Worker interface definition
- `claudecode.go` - ClaudeCodeWorker implementation
- `claudecode_test.go` - Comprehensive test suite
- `doc.go` - Package documentation
- `README.md` - This file

## Dependencies

- `internal/taskqueue` - Task and queue models
- `internal/llm` - LLM backend abstraction
- `internal/conventions` - Convention parser
- Standard library: `context`, `fmt`, `strings`, `sync`, `time`
