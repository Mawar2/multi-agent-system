package taskqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewJSONQueue_CreateDirectory(t *testing.T) {
	dir := t.TempDir()
	queueDir := filepath.Join(dir, "tasks")

	queue, err := NewJSONQueue(queueDir)
	if err != nil {
		t.Fatalf("NewJSONQueue failed: %v", err)
	}

	// Verify queue was created and directory exists
	if queue == nil {
		t.Fatal("queue is nil")
	}
	if _, err := os.Stat(queueDir); os.IsNotExist(err) {
		t.Fatal("tasks directory was not created")
	}
}

func TestNewJSONQueue_DefaultDirectory(t *testing.T) {
	// Save current directory
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current directory: %v", err)
	}
	defer func() {
		if err := os.Chdir(oldCwd); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	}()

	// Change to temp directory
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to change working directory: %v", err)
	}

	queue, err := NewJSONQueue("")
	if err != nil {
		t.Fatalf("NewJSONQueue failed: %v", err)
	}

	if queue == nil {
		t.Fatal("queue is nil")
	}

	// Verify default directory was created
	defaultDir := filepath.Join(dir, "./tasks")
	if _, err := os.Stat(defaultDir); os.IsNotExist(err) {
		t.Fatal("default tasks directory was not created")
	}
}

func TestNewJSONQueue_NotADirectory(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not_a_dir")

	// Create a file instead of a directory
	if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	queue, err := NewJSONQueue(filePath)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if queue != nil {
		t.Fatal("queue should be nil on error")
	}
}

func TestEnqueue(t *testing.T) {
	queue, dir := createTestQueue(t)

	ctx := context.Background()
	task := &Task{
		ID:          "task-1",
		IssueNumber: 100,
		RepoOwner:   "Mawar2",
		RepoName:    "Kaimi",
		Title:       "Test Task",
		Status:      StatusPending,
		Tier:        TierGeminiFlash,
		Complexity:  ComplexitySimple,
	}

	err := queue.Enqueue(ctx, task)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Verify file was created
	filePath := filepath.Join(dir, "task-1.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("task file was not created")
	}

	// Verify file content
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read task file: %v", err)
	}

	var savedTask Task
	if err := json.Unmarshal(data, &savedTask); err != nil {
		t.Fatalf("failed to unmarshal task: %v", err)
	}

	if savedTask.ID != task.ID {
		t.Fatalf("task ID mismatch: expected %s, got %s", task.ID, savedTask.ID)
	}
}

func TestEnqueue_NilTask(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	err := queue.Enqueue(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil task")
	}
}

func TestEnqueue_EmptyID(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	task := &Task{
		ID:     "",
		Status: StatusPending,
	}

	err := queue.Enqueue(ctx, task)
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
}

func TestGet(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	task := &Task{
		ID:          "task-1",
		IssueNumber: 100,
		RepoOwner:   "Mawar2",
		RepoName:    "Kaimi",
		Title:       "Test Task",
		Status:      StatusPending,
		Tier:        TierGeminiFlash,
		Complexity:  ComplexitySimple,
	}

	// Enqueue the task
	err := queue.Enqueue(ctx, task)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Retrieve it
	retrieved, err := queue.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved == nil {
		t.Fatal("retrieved task is nil")
	}
	if retrieved.ID != task.ID {
		t.Fatalf("task ID mismatch: expected %s, got %s", task.ID, retrieved.ID)
	}
	if retrieved.IssueNumber != task.IssueNumber {
		t.Fatalf("issue number mismatch: expected %d, got %d", task.IssueNumber, retrieved.IssueNumber)
	}
}

func TestGet_NotFound(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	_, err := queue.Get(ctx, "nonexistent-task")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestGet_EmptyID(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	_, err := queue.Get(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
}

func TestUpdate(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	task := &Task{
		ID:          "task-1",
		IssueNumber: 100,
		Title:       "Test Task",
		Status:      StatusPending,
		Tier:        TierGeminiFlash,
	}

	// Enqueue the task
	err := queue.Enqueue(ctx, task)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Update it
	task.Status = StatusInProgress
	task.WorkerID = "worker-1"
	task.BranchName = "feature/test"

	err = queue.Update(ctx, task)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify update
	retrieved, err := queue.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Status != StatusInProgress {
		t.Fatalf("status not updated: expected %v, got %v", StatusInProgress, retrieved.Status)
	}
	if retrieved.WorkerID != "worker-1" {
		t.Fatalf("worker ID not updated: expected worker-1, got %s", retrieved.WorkerID)
	}
	if retrieved.BranchName != "feature/test" {
		t.Fatalf("branch name not updated: expected feature/test, got %s", retrieved.BranchName)
	}
}

func TestUpdate_NilTask(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	err := queue.Update(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil task")
	}
}

func TestDequeue_SingleTask(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	// Enqueue a task
	task := &Task{
		ID:     "task-1",
		Status: StatusPending,
		Tier:   TierGeminiFlash,
	}
	err := queue.Enqueue(ctx, task)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Dequeue it
	claimed, err := queue.Dequeue(ctx, TierGeminiFlash, "worker-1")
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if claimed == nil {
		t.Fatal("claimed task is nil")
	}
	if claimed.ID != "task-1" {
		t.Fatalf("claimed task ID mismatch: expected task-1, got %s", claimed.ID)
	}
	if claimed.Status != StatusClaimed {
		t.Fatalf("status not updated: expected %v, got %v", StatusClaimed, claimed.Status)
	}
	if claimed.WorkerID != "worker-1" {
		t.Fatalf("worker ID not set: expected worker-1, got %s", claimed.WorkerID)
	}
	if claimed.ClaimedAt.IsZero() {
		t.Fatal("ClaimedAt not set")
	}
}

func TestDequeue_NoTasks(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	// Dequeue from empty queue
	claimed, err := queue.Dequeue(ctx, TierGeminiFlash, "worker-1")
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if claimed != nil {
		t.Fatal("expected nil for empty queue")
	}
}

func TestDequeue_EmptyWorkerID(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	_, err := queue.Dequeue(ctx, TierGeminiFlash, "")
	if err == nil {
		t.Fatal("expected error for empty worker ID")
	}
}

func TestDequeue_FiltersbyTier(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	// Enqueue tasks for different tiers
	task1 := &Task{
		ID:     "task-1",
		Status: StatusPending,
		Tier:   TierGeminiFlash,
	}
	task2 := &Task{
		ID:     "task-2",
		Status: StatusPending,
		Tier:   TierGeminiPro,
	}

	if err := queue.Enqueue(ctx, task1); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	if err := queue.Enqueue(ctx, task2); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Dequeue from GeminiPro tier
	claimed, err := queue.Dequeue(ctx, TierGeminiPro, "worker-1")
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if claimed.ID != "task-2" {
		t.Fatalf("dequeued wrong task: expected task-2, got %s", claimed.ID)
	}
	if claimed.Tier != TierGeminiPro {
		t.Fatalf("tier mismatch: expected %v, got %v", TierGeminiPro, claimed.Tier)
	}
}

func TestDequeue_AtomicClaiming(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	// Enqueue one task
	task := &Task{
		ID:     "task-1",
		Status: StatusPending,
		Tier:   TierGeminiFlash,
	}
	if err := queue.Enqueue(ctx, task); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Have multiple workers try to claim the same task concurrently
	numWorkers := 10
	var claimedCount atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			claimed, err := queue.Dequeue(ctx, TierGeminiFlash, fmt.Sprintf("worker-%d", workerID))
			if err != nil {
				t.Errorf("Dequeue failed: %v", err)
				return
			}
			if claimed != nil {
				claimedCount.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// Only one worker should have successfully claimed the task
	if claimedCount.Load() != 1 {
		t.Fatalf("expected 1 claim, got %d", claimedCount.Load())
	}

	// Verify the task is marked as claimed
	retrieved, err := queue.Get(ctx, "task-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.Status != StatusClaimed {
		t.Fatalf("task not claimed: status is %v", retrieved.Status)
	}
	if retrieved.WorkerID == "" {
		t.Fatal("task has no worker ID")
	}
}

func TestList_AllTasks(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	// Enqueue multiple tasks
	tasks := []*Task{
		{ID: "task-1", Status: StatusPending, Tier: TierGeminiFlash, RepoOwner: "Mawar2", RepoName: "Kaimi"},
		{ID: "task-2", Status: StatusPending, Tier: TierGeminiPro, RepoOwner: "Mawar2", RepoName: "Kaimi"},
		{ID: "task-3", Status: StatusInProgress, Tier: TierClaude, RepoOwner: "other", RepoName: "repo"},
	}

	for _, task := range tasks {
		err := queue.Enqueue(ctx, task)
		if err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// List all tasks
	listed, err := queue.List(ctx, nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(listed) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(listed))
	}
}

func TestList_FilterByStatus(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	tasks := []*Task{
		{ID: "task-1", Status: StatusPending, Tier: TierGeminiFlash},
		{ID: "task-2", Status: StatusPending, Tier: TierGeminiFlash},
		{ID: "task-3", Status: StatusInProgress, Tier: TierGeminiFlash},
		{ID: "task-4", Status: StatusComplete, Tier: TierGeminiFlash},
	}

	for _, task := range tasks {
		if err := queue.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	status := StatusPending
	filter := &TaskFilter{Status: &status}

	listed, err := queue.List(ctx, filter)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(listed) != 2 {
		t.Fatalf("expected 2 pending tasks, got %d", len(listed))
	}

	for _, task := range listed {
		if task.Status != StatusPending {
			t.Fatalf("filter didn't work: got task with status %v", task.Status)
		}
	}
}

func TestList_FilterByTier(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	tasks := []*Task{
		{ID: "task-1", Status: StatusPending, Tier: TierGeminiFlash},
		{ID: "task-2", Status: StatusPending, Tier: TierGeminiPro},
		{ID: "task-3", Status: StatusPending, Tier: TierClaude},
		{ID: "task-4", Status: StatusPending, Tier: TierGeminiPro},
	}

	for _, task := range tasks {
		if err := queue.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	tier := TierGeminiPro
	filter := &TaskFilter{Tier: &tier}

	listed, err := queue.List(ctx, filter)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(listed) != 2 {
		t.Fatalf("expected 2 GeminiPro tasks, got %d", len(listed))
	}

	for _, task := range listed {
		if task.Tier != TierGeminiPro {
			t.Fatalf("filter didn't work: got task with tier %v", task.Tier)
		}
	}
}

func TestList_FilterByRepoOwner(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	tasks := []*Task{
		{ID: "task-1", RepoOwner: "Mawar2", RepoName: "Kaimi"},
		{ID: "task-2", RepoOwner: "other", RepoName: "repo"},
		{ID: "task-3", RepoOwner: "Mawar2", RepoName: "other-repo"},
	}

	for _, task := range tasks {
		if err := queue.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	filter := &TaskFilter{RepoOwner: "Mawar2"}

	listed, err := queue.List(ctx, filter)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(listed) != 2 {
		t.Fatalf("expected 2 Mawar2 tasks, got %d", len(listed))
	}

	for _, task := range listed {
		if task.RepoOwner != "Mawar2" {
			t.Fatalf("filter didn't work: got task with owner %s", task.RepoOwner)
		}
	}
}

func TestList_FilterByComplexity(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	tasks := []*Task{
		{ID: "task-1", Complexity: ComplexitySimple},
		{ID: "task-2", Complexity: ComplexityMedium},
		{ID: "task-3", Complexity: ComplexityComplex},
		{ID: "task-4", Complexity: ComplexityMedium},
	}

	for _, task := range tasks {
		if err := queue.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	complexity := ComplexityMedium
	filter := &TaskFilter{Complexity: &complexity}

	listed, err := queue.List(ctx, filter)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(listed) != 2 {
		t.Fatalf("expected 2 medium complexity tasks, got %d", len(listed))
	}

	for _, task := range listed {
		if task.Complexity != ComplexityMedium {
			t.Fatalf("filter didn't work: got task with complexity %v", task.Complexity)
		}
	}
}

func TestList_MultipleFilters(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	tasks := []*Task{
		{ID: "task-1", Status: StatusPending, Tier: TierGeminiFlash, RepoOwner: "Mawar2"},
		{ID: "task-2", Status: StatusPending, Tier: TierGeminiPro, RepoOwner: "Mawar2"},
		{ID: "task-3", Status: StatusInProgress, Tier: TierGeminiFlash, RepoOwner: "Mawar2"},
		{ID: "task-4", Status: StatusPending, Tier: TierGeminiFlash, RepoOwner: "other"},
	}

	for _, task := range tasks {
		if err := queue.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	status := StatusPending
	tier := TierGeminiFlash
	filter := &TaskFilter{
		Status:    &status,
		Tier:      &tier,
		RepoOwner: "Mawar2",
	}

	listed, err := queue.List(ctx, filter)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(listed) != 1 {
		t.Fatalf("expected 1 task matching all filters, got %d", len(listed))
	}

	if listed[0].ID != "task-1" {
		t.Fatalf("expected task-1, got %s", listed[0].ID)
	}
}

func TestRelease(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	// Create and enqueue a task
	task := &Task{
		ID:       "task-1",
		Status:   StatusPending,
		Tier:     TierGeminiFlash,
		Attempts: 1,
	}

	if err := queue.Enqueue(ctx, task); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Claim it
	claimed, err := queue.Dequeue(ctx, TierGeminiFlash, "worker-1")
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Verify it's claimed
	if claimed.Status != StatusClaimed {
		t.Fatalf("task should be claimed, got status %v", claimed.Status)
	}

	// Release it
	err = queue.Release(ctx, "task-1")
	if err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	// Verify it's back to pending
	released, err := queue.Get(ctx, "task-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if released.Status != StatusPending {
		t.Fatalf("status not released: expected %v, got %v", StatusPending, released.Status)
	}
	if released.WorkerID != "" {
		t.Fatalf("worker ID not cleared: expected empty, got %s", released.WorkerID)
	}
	if released.Attempts != 2 {
		t.Fatalf("attempts not incremented: expected 2, got %d", released.Attempts)
	}
}

func TestRelease_NotFound(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	err := queue.Release(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent task")
	}
}

func TestRelease_EmptyID(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	err := queue.Release(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty task ID")
	}
}

func TestConcurrentOperations(t *testing.T) {
	queue, _ := createTestQueue(t)
	ctx := context.Background()

	numWorkers := 5
	tasksPerWorker := 10
	var wg sync.WaitGroup

	// Enqueue tasks
	for i := 0; i < numWorkers*tasksPerWorker; i++ {
		task := &Task{
			ID:     fmt.Sprintf("task-%d", i),
			Tier:   Tier(i % 3), // Distribute across tiers
			Status: StatusPending,
		}
		if err := queue.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Have workers claim and update tasks concurrently
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < tasksPerWorker; i++ {
				// Try to dequeue from each tier
				for tier := 0; tier < 3; tier++ {
					claimed, err := queue.Dequeue(ctx, Tier(tier), fmt.Sprintf("worker-%d", workerID))
					if err != nil {
						t.Errorf("Dequeue failed: %v", err)
						return
					}
					if claimed == nil {
						continue
					}

					// Update the task
					claimed.Status = StatusInProgress
					if err := queue.Update(ctx, claimed); err != nil {
						t.Errorf("Update failed: %v", err)
						return
					}

					time.Sleep(time.Millisecond) // Simulate work

					// Mark as complete
					claimed.Status = StatusComplete
					if err := queue.Update(ctx, claimed); err != nil {
						t.Errorf("Final update failed: %v", err)
						return
					}
					break
				}
			}
		}(w)
	}

	wg.Wait()

	// Verify all tasks are either complete or still pending
	allTasks, err := queue.List(ctx, nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	for _, task := range allTasks {
		if task.Status != StatusComplete && task.Status != StatusPending {
			t.Fatalf("unexpected task status: %v", task.Status)
		}
	}
}

// createTestQueue is a helper function that creates a JSONQueue with a temporary directory.
func createTestQueue(t *testing.T) (*JSONQueue, string) {
	dir := t.TempDir()
	queue, err := NewJSONQueue(dir)
	if err != nil {
		t.Fatalf("failed to create queue: %v", err)
	}
	return queue, dir
}
