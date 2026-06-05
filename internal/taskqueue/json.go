package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// JSONQueue implements the TaskQueue interface using JSON files on disk.
//
// Each task is stored as a separate JSON file in the tasks directory,
// named by its ID (e.g., tasks/{taskID}.json). This implementation is
// suitable for Phase 1 (local multi-agent system) and can be swapped for
// Firestore in Phase 2+ for distributed workers without changing the interface.
//
// Thread-safety: All operations are protected by a RWMutex, allowing multiple
// concurrent readers or a single writer. The Dequeue operation is atomic,
// ensuring that only one worker can claim a task even under concurrent access.
type JSONQueue struct {
	tasksDir string       // Directory where task JSON files are stored
	mu       sync.RWMutex // Protects concurrent access to the file system
}

// NewJSONQueue creates a new JSON file-backed TaskQueue.
//
// The queue will create the tasks directory if it doesn't exist.
// Each task will be saved as {tasksDir}/{taskID}.json.
//
// Parameters:
//   - directory: The directory where task JSON files will be stored.
//     If empty, defaults to "./tasks/". Will be created if it doesn't exist.
//
// Returns an error if the directory cannot be created or accessed.
func NewJSONQueue(directory string) (*JSONQueue, error) {
	if directory == "" {
		directory = "./tasks"
	}

	// Create directory if it doesn't exist
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create tasks directory: %w", err)
	}

	// Verify it's actually a directory
	info, err := os.Stat(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to stat tasks directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("tasks path %s is not a directory", directory)
	}

	return &JSONQueue{
		tasksDir: directory,
	}, nil
}

// Enqueue adds a new task to the queue with Status = Pending.
//
// The task is written to {tasksDir}/{taskID}.json. If a task with the same
// ID already exists, it will be overwritten.
//
// Returns an error if:
//   - task is nil
//   - task.ID is empty
//   - the file cannot be written
//
// Thread-safety: Acquires a write lock for the duration of the operation.
func (q *JSONQueue) Enqueue(ctx context.Context, task *Task) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}
	if task.ID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Marshal task to JSON with indentation for readability
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// Write to file
	filePath := q.taskFilePath(task.ID)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write task file: %w", err)
	}

	return nil
}

// Dequeue atomically claims and returns the next available task for the given tier.
//
// This operation is atomic: it reads all pending tasks for the tier, selects the
// oldest one (by creation time), updates its status to Claimed, and persists the
// change. Only one worker will successfully claim any given task.
//
// Returns:
//   - The claimed task if one is available
//   - nil if no tasks are available for this tier
//   - An error if the operation fails
//
// Sets the claimed task's Status = Claimed, WorkerID to the claiming worker,
// and ClaimedAt to the current time.
//
// Thread-safety: Acquires a write lock for the entire operation to ensure atomicity.
// While locked, no other Dequeue or Update operations can proceed, preventing
// double-claims.
func (q *JSONQueue) Dequeue(ctx context.Context, tier Tier, workerID string) (*Task, error) {
	if workerID == "" {
		return nil, fmt.Errorf("worker ID cannot be empty")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Find all pending tasks for this tier
	entries, err := os.ReadDir(q.tasksDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read tasks directory: %w", err)
	}

	var candidates []*Task
	for _, entry := range entries {
		// Skip non-JSON files and directories
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		// Read and parse the task file
		filePath := filepath.Join(q.tasksDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			// Skip files that can't be read
			continue
		}

		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			// Skip files with invalid JSON
			continue
		}

		// Only consider pending tasks for this tier
		if task.Status == StatusPending && task.Tier == tier {
			candidates = append(candidates, &task)
		}
	}

	if len(candidates) == 0 {
		return nil, nil
	}

	// Select the oldest task (by claim time, or by creation if never claimed)
	// For this implementation, we use ID ordering as a stable way to pick consistently
	oldestTask := candidates[0]
	for _, task := range candidates[1:] {
		if task.ID < oldestTask.ID {
			oldestTask = task
		}
	}

	// Atomically claim the task
	oldestTask.Status = StatusClaimed
	oldestTask.WorkerID = workerID
	oldestTask.ClaimedAt = time.Now()

	// Persist the updated task
	data, err := json.MarshalIndent(oldestTask, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal claimed task: %w", err)
	}

	filePath := q.taskFilePath(oldestTask.ID)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return nil, fmt.Errorf("failed to persist claimed task: %w", err)
	}

	return oldestTask, nil
}

// Update updates an existing task's state.
//
// Used by workers to update progress (Status, BranchName, PRNumber, etc.).
// The updated task is written to {tasksDir}/{taskID}.json, overwriting
// the previous version.
//
// Returns an error if:
//   - task is nil
//   - task.ID is empty
//   - the file cannot be written
//
// Thread-safety: Acquires a write lock for the duration of the operation.
func (q *JSONQueue) Update(ctx context.Context, task *Task) error {
	if task == nil {
		return fmt.Errorf("task cannot be nil")
	}
	if task.ID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Marshal task to JSON with indentation
	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task: %w", err)
	}

	// Write to file
	filePath := q.taskFilePath(task.ID)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return fmt.Errorf("failed to write task file: %w", err)
	}

	return nil
}

// Get retrieves a specific task by ID.
//
// Returns an error if:
//   - taskID is empty
//   - the task doesn't exist
//   - the file cannot be read
//   - the JSON cannot be parsed
//
// Thread-safety: Acquires a read lock for the duration of the operation.
func (q *JSONQueue) Get(ctx context.Context, taskID string) (*Task, error) {
	if taskID == "" {
		return nil, fmt.Errorf("task ID cannot be empty")
	}

	q.mu.RLock()
	defer q.mu.RUnlock()

	filePath := q.taskFilePath(taskID)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("task %s not found", taskID)
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read task file: %w", err)
	}

	// Unmarshal JSON
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("failed to unmarshal task: %w", err)
	}

	return &task, nil
}

// List returns tasks matching the filter criteria.
//
// If filter is nil, returns all tasks.
// Returns an empty slice if no tasks match the filter.
//
// Filter criteria (all are AND'ed together):
//   - Status: if non-nil, filters by task status
//   - Tier: if non-nil, filters by task tier
//   - Complexity: if non-nil, filters by task complexity
//   - RepoOwner: if non-empty, filters by repository owner
//   - RepoName: if non-empty, filters by repository name
//   - WorkerID: if non-empty, filters by assigned worker
//
// Thread-safety: Acquires a read lock for the duration of the operation.
func (q *JSONQueue) List(ctx context.Context, filter *TaskFilter) ([]*Task, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	// Read all JSON files from tasks directory
	entries, err := os.ReadDir(q.tasksDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read tasks directory: %w", err)
	}

	var tasks []*Task

	for _, entry := range entries {
		// Skip non-JSON files and directories
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		// Read and parse the file
		filePath := filepath.Join(q.tasksDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			// Skip files that can't be read
			continue
		}

		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			// Skip files with invalid JSON
			continue
		}

		// Apply filter if provided
		if filter != nil && !q.matchesFilter(&task, filter) {
			continue
		}

		tasks = append(tasks, &task)
	}

	return tasks, nil
}

// Release returns a task back to Pending status.
//
// Used when a worker crashes or times out while processing a task.
// Sets Status = Pending, clears WorkerID, and increments Attempts.
//
// Returns an error if:
//   - taskID is empty
//   - the task doesn't exist
//   - the file cannot be read or written
//
// Thread-safety: Acquires a write lock for the duration of the operation.
func (q *JSONQueue) Release(ctx context.Context, taskID string) error {
	if taskID == "" {
		return fmt.Errorf("task ID cannot be empty")
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	filePath := q.taskFilePath(taskID)

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read task file: %w", err)
	}

	// Unmarshal JSON
	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return fmt.Errorf("failed to unmarshal task: %w", err)
	}

	// Reset task to pending state
	task.Status = StatusPending
	task.WorkerID = ""
	task.Attempts++

	// Persist the released task
	data, err = json.MarshalIndent(&task, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal released task: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return fmt.Errorf("failed to persist released task: %w", err)
	}

	return nil
}

// taskFilePath returns the full file path for a task ID.
func (q *JSONQueue) taskFilePath(taskID string) string {
	return filepath.Join(q.tasksDir, taskID+".json")
}

// matchesFilter checks if a task matches all provided filter criteria.
func (q *JSONQueue) matchesFilter(task *Task, filter *TaskFilter) bool {
	// Filter by status
	if filter.Status != nil && task.Status != *filter.Status {
		return false
	}

	// Filter by tier
	if filter.Tier != nil && task.Tier != *filter.Tier {
		return false
	}

	// Filter by complexity
	if filter.Complexity != nil && task.Complexity != *filter.Complexity {
		return false
	}

	// Filter by repository owner
	if filter.RepoOwner != "" && task.RepoOwner != filter.RepoOwner {
		return false
	}

	// Filter by repository name
	if filter.RepoName != "" && task.RepoName != filter.RepoName {
		return false
	}

	// Filter by worker ID
	if filter.WorkerID != "" && task.WorkerID != filter.WorkerID {
		return false
	}

	return true
}
