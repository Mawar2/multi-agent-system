package taskqueue

import "time"

// Task represents a unit of work to be completed by a worker.
// Tasks are created from GitHub Issues and flow through the system:
// Pending → Claimed → InProgress → Review → Complete
type Task struct {
	// Core identification
	ID          string `json:"id"`           // Unique task ID (UUID)
	IssueNumber int    `json:"issue_number"` // GitHub Issue number
	RepoOwner   string `json:"repo_owner"`   // Repository owner (e.g., "Mawar2")
	RepoName    string `json:"repo_name"`    // Repository name (e.g., "Kaimi")

	// Task details
	Title       string `json:"title"`       // Issue title
	Description string `json:"description"` // Full issue body with acceptance criteria

	// Classification
	Complexity Complexity `json:"complexity"` // Simple, Medium, Complex
	Tier       Tier       `json:"tier"`       // Gemini Flash, Gemini Pro, Claude

	// State tracking
	Status      Status    `json:"status"`       // Pending, Claimed, InProgress, Review, Complete, Failed
	WorkerID    string    `json:"worker_id"`    // ID of worker that claimed this task
	BranchName  string    `json:"branch_name"`  // Feature branch created by worker
	PRNumber    int       `json:"pr_number"`    // Pull request number
	ClaimedAt   time.Time `json:"claimed_at"`   // When task was claimed
	StartedAt   time.Time `json:"started_at"`   // When work began
	CompletedAt time.Time `json:"completed_at"` // When PR was created or task failed
	Attempts    int       `json:"attempts"`     // Number of times this task has been attempted

	// Metadata
	ErrorMsg string            `json:"error_msg,omitempty"` // Error message if failed
	LogsPath string            `json:"logs_path,omitempty"` // Path to worker logs
	Metadata map[string]string `json:"metadata,omitempty"`  // Additional key-value data
}

// Complexity represents how difficult a task is to implement.
type Complexity int

const (
	// ComplexitySimple: ≤3 files changed, docs/config only, clear patterns
	ComplexitySimple Complexity = iota

	// ComplexityMedium: 4-10 files, features/refactors, standard patterns
	ComplexityMedium

	// ComplexityComplex: >10 files, architecture changes, novel problems
	ComplexityComplex
)

// String returns the string representation of Complexity.
func (c Complexity) String() string {
	return [...]string{"simple", "medium", "complex"}[c]
}

// Tier represents which worker pool should handle this task.
type Tier int

const (
	// TierGeminiFlash: Gemini Flash 3.5 via Antigravity (simple tasks, fast, free)
	TierGeminiFlash Tier = iota

	// TierGeminiPro: Gemini Pro 3.5 via Antigravity (moderate tasks, free)
	TierGeminiPro

	// TierClaude: Claude via Claude Code CLI (complex tasks, local, free)
	TierClaude
)

// String returns the string representation of Tier.
func (t Tier) String() string {
	return [...]string{"gemini-flash", "gemini-pro", "claude"}[t]
}

// Status represents the current state of a task in its lifecycle.
type Status int

const (
	// StatusPending: Task is in queue, available to be claimed
	StatusPending Status = iota

	// StatusClaimed: Worker has claimed task but not yet started work
	StatusClaimed

	// StatusInProgress: Worker is actively working on task
	StatusInProgress

	// StatusReview: PR created, awaiting human review
	StatusReview

	// StatusComplete: PR merged, task done
	StatusComplete

	// StatusFailed: Task failed after max retry attempts
	StatusFailed
)

// String returns the string representation of Status.
func (s Status) String() string {
	return [...]string{"pending", "claimed", "in_progress", "review", "complete", "failed"}[s]
}

// IsTerminal returns true if this status indicates task is finished (complete or failed).
func (s Status) IsTerminal() bool {
	return s == StatusComplete || s == StatusFailed
}

// CanClaim returns true if this task can be claimed by a worker.
func (t *Task) CanClaim() bool {
	return t.Status == StatusPending && t.WorkerID == ""
}

// IsStalled returns true if task has been in progress too long without updates.
// Stalled tasks should be released back to the queue.
func (t *Task) IsStalled(timeout time.Duration) bool {
	if t.Status != StatusClaimed && t.Status != StatusInProgress {
		return false
	}

	lastUpdate := t.StartedAt
	if lastUpdate.IsZero() {
		lastUpdate = t.ClaimedAt
	}

	return time.Since(lastUpdate) > timeout
}
