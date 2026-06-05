package worker

import (
	"context"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
)

// Worker represents an autonomous agent that claims and completes tasks.
// Workers follow project conventions (CLAUDE.md, CONVENTIONS.md) and create PRs.
// Phase 1: Uses Claude Code Task tool to spawn sub-agents
// Phase 2+: Can use Antigravity/Vertex API via backend abstraction
type Worker interface {
	// Claim attempts to claim a task from the queue for this worker's tier.
	// Returns the claimed task, or nil if no tasks available.
	Claim(ctx context.Context) (*taskqueue.Task, error)

	// Execute performs the work for a claimed task.
	// Steps:
	// 1. Read project conventions (CLAUDE.md, CONVENTIONS.md)
	// 2. Create feature branch
	// 3. Implement solution following conventions (TDD)
	// 4. Run tests + linter
	// 5. Create pull request
	// Returns Result with success status, branch name, PR number.
	Execute(ctx context.Context, task *taskqueue.Task) (*Result, error)

	// Release returns a task back to the queue (called if Execute fails).
	// Task goes back to Pending status for another worker to try.
	Release(ctx context.Context, taskID string) error

	// Health returns current health status of this worker.
	Health(ctx context.Context) (*HealthStatus, error)

	// ID returns this worker's unique identifier.
	ID() string

	// Tier returns which tier this worker operates in.
	Tier() taskqueue.Tier
}

// Result represents the outcome of a worker executing a task.
type Result struct {
	TaskID     string // Task that was executed
	Success    bool   // Whether task completed successfully
	BranchName string // Feature branch created (e.g., "feature/KAI-123-summary")
	PRNumber   int    // Pull request number created
	ErrorMsg   string // Error message if failed
	LogsPath   string // Path to execution logs
}

// HealthStatus describes a worker's current operational state.
type HealthStatus struct {
	WorkerID       string         // Worker's unique ID
	Tier           taskqueue.Tier // Worker's tier (gemini-flash, gemini-pro, claude)
	Healthy        bool           // Whether worker is operational
	TasksCompleted int            // Number of tasks completed by this worker
	TasksFailed    int            // Number of tasks failed by this worker
	LastHeartbeat  string         // ISO 8601 timestamp of last heartbeat
	ErrorMsg       string         // Error message if unhealthy
}
