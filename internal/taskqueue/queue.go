package taskqueue

import "context"

// TaskQueue manages the queue of work items.
// Mirrors the Store interface pattern from Kaimi (internal/store/store.go).
// Phase 1: JSON file-backed implementation
// Phase 2+: Firestore-backed for distributed workers
type TaskQueue interface {
	// Enqueue adds a new task to the queue with Status = Pending.
	Enqueue(ctx context.Context, task *Task) error

	// Dequeue atomically claims and returns the next available task for the given tier.
	// Returns nil if no tasks available for this tier.
	// Sets Status = Claimed and WorkerID to the claiming worker.
	Dequeue(ctx context.Context, tier Tier, workerID string) (*Task, error)

	// Update updates an existing task's state.
	// Used by workers to update progress (Status, BranchName, PRNumber, etc.)
	Update(ctx context.Context, task *Task) error

	// Get retrieves a specific task by ID.
	Get(ctx context.Context, taskID string) (*Task, error)

	// List returns tasks matching the filter.
	List(ctx context.Context, filter *TaskFilter) ([]*Task, error)

	// Release returns a task back to Pending status (e.g., if worker crashes).
	// Clears WorkerID, increments Attempts.
	Release(ctx context.Context, taskID string) error
}

// TaskFilter specifies criteria for filtering tasks in List queries.
type TaskFilter struct {
	Status     *Status     // Filter by status (nil = don't filter)
	Tier       *Tier       // Filter by tier (nil = don't filter)
	Complexity *Complexity // Filter by complexity (nil = don't filter)
	RepoOwner  string      // Filter by repository owner (empty = don't filter)
	RepoName   string      // Filter by repository name (empty = don't filter)
	WorkerID   string      // Filter by assigned worker (empty = don't filter)
}
