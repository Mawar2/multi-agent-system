package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/ticket"
)

// MockTaskQueue implements taskqueue.TaskQueue for testing.
type MockTaskQueue struct {
	enqueuedTasks []*taskqueue.Task
	tasksInQueue  map[string]*taskqueue.Task
	releasedTasks []string
}

func NewMockTaskQueue() *MockTaskQueue {
	return &MockTaskQueue{
		enqueuedTasks: make([]*taskqueue.Task, 0),
		tasksInQueue:  make(map[string]*taskqueue.Task),
		releasedTasks: make([]string, 0),
	}
}

func (m *MockTaskQueue) Enqueue(ctx context.Context, task *taskqueue.Task) error {
	m.enqueuedTasks = append(m.enqueuedTasks, task)
	m.tasksInQueue[task.ID] = task
	return nil
}

func (m *MockTaskQueue) Dequeue(ctx context.Context, tier taskqueue.Tier, workerID string) (*taskqueue.Task, error) {
	return nil, errors.New("dequeue not implemented in mock")
}

func (m *MockTaskQueue) Update(ctx context.Context, task *taskqueue.Task) error {
	if existing, ok := m.tasksInQueue[task.ID]; ok {
		*existing = *task
		return nil
	}
	return errors.New("task not found")
}

func (m *MockTaskQueue) Get(ctx context.Context, taskID string) (*taskqueue.Task, error) {
	if task, ok := m.tasksInQueue[taskID]; ok {
		return task, nil
	}
	return nil, errors.New("task not found")
}

func (m *MockTaskQueue) List(ctx context.Context, filter *taskqueue.TaskFilter) ([]*taskqueue.Task, error) {
	var result []*taskqueue.Task
	for _, task := range m.tasksInQueue {
		matches := true
		if filter.Status != nil && task.Status != *filter.Status {
			matches = false
		}
		if filter.Tier != nil && task.Tier != *filter.Tier {
			matches = false
		}
		if filter.WorkerID != "" && task.WorkerID != filter.WorkerID {
			matches = false
		}
		if matches {
			result = append(result, task)
		}
	}
	return result, nil
}

func (m *MockTaskQueue) Release(ctx context.Context, taskID string) error {
	m.releasedTasks = append(m.releasedTasks, taskID)
	if task, ok := m.tasksInQueue[taskID]; ok {
		task.Status = taskqueue.StatusPending
		task.WorkerID = ""
		task.Attempts++
		return nil
	}
	return errors.New("task not found")
}

// MockTicketClient implements ticket.Client for testing.
type MockTicketClient struct {
	issues   []*ticket.Issue
	prStatus map[int]*ticket.PRStatus // issueNumber -> PRStatus
	err      error
}

func NewMockTicketClient(issues []*ticket.Issue) *MockTicketClient {
	return &MockTicketClient{
		issues:   issues,
		prStatus: make(map[int]*ticket.PRStatus),
	}
}

func (m *MockTicketClient) FetchIssues(ctx context.Context, owner, repo string, labels []string) ([]*ticket.Issue, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.issues, nil
}

func (m *MockTicketClient) GetIssue(ctx context.Context, owner, repo string, number int) (*ticket.Issue, error) {
	for _, issue := range m.issues {
		if issue.Number == number {
			return issue, nil
		}
	}
	return nil, errors.New("issue not found")
}

func (m *MockTicketClient) ParseAcceptanceCriteria(body string) ([]string, error) {
	// Simple mock implementation
	return []string{"Criterion 1", "Criterion 2"}, nil
}

func (m *MockTicketClient) CheckPRStatus(ctx context.Context, owner, repo string, issueNumber int) (*ticket.PRStatus, error) {
	if status, ok := m.prStatus[issueNumber]; ok {
		return status, nil
	}
	return nil, nil // No PR found
}

func (m *MockTicketClient) SetPRStatus(issueNumber int, status *ticket.PRStatus) {
	m.prStatus[issueNumber] = status
}

func (m *MockTicketClient) SetError(err error) {
	m.err = err
}

// TestNewSupervisor tests supervisor construction.
func TestNewSupervisor(t *testing.T) {
	config := &Config{
		Projects: []ProjectConfig{
			{
				Name:      "test-project",
				RepoOwner: "testowner",
				RepoName:  "testrepo",
			},
		},
		PollIntervalSeconds: 60,
		TaskTimeoutMinutes:  120,
	}
	queue := NewMockTaskQueue()
	router := NewRuleBasedRouter()
	ticketClient := NewMockTicketClient([]*ticket.Issue{})

	supervisor := NewSupervisor(config, queue, router, ticketClient)

	if supervisor == nil {
		t.Fatal("NewSupervisor returned nil")
	}
	if supervisor.config != config {
		t.Error("config not set correctly")
	}
	if supervisor.queue != queue {
		t.Error("queue not set correctly")
	}
	if supervisor.router != router {
		t.Error("router not set correctly")
	}
	if supervisor.ticketClient != ticketClient {
		t.Error("ticketClient not set correctly")
	}
}

// TestProcessIssue_NewIssue tests processing a new issue.
func TestProcessIssue_NewIssue(t *testing.T) {
	config := &Config{
		Projects: []ProjectConfig{
			{
				Name:      "test-project",
				RepoOwner: "testowner",
				RepoName:  "testrepo",
			},
		},
	}
	queue := NewMockTaskQueue()
	router := NewRuleBasedRouter()
	ticketClient := NewMockTicketClient([]*ticket.Issue{})

	supervisor := NewSupervisor(config, queue, router, ticketClient)

	issue := &ticket.Issue{
		Number:    123,
		Title:     "Add logging",
		Body:      "Add logging to the system",
		RepoOwner: "testowner",
		RepoName:  "testrepo",
	}

	err := supervisor.processIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("processIssue failed: %v", err)
	}

	if len(queue.enqueuedTasks) != 1 {
		t.Fatalf("expected 1 task enqueued, got %d", len(queue.enqueuedTasks))
	}

	task := queue.enqueuedTasks[0]
	if task.IssueNumber != 123 {
		t.Errorf("expected IssueNumber 123, got %d", task.IssueNumber)
	}
	if task.Title != "Add logging" {
		t.Errorf("expected Title 'Add logging', got '%s'", task.Title)
	}
	if task.Status != taskqueue.StatusPending {
		t.Errorf("expected Status Pending, got %v", task.Status)
	}
	if task.Complexity != taskqueue.ComplexitySimple {
		t.Errorf("expected Complexity Simple (logging task), got %v", task.Complexity)
	}
	if task.Tier != taskqueue.TierGeminiFlash {
		t.Errorf("expected Tier GeminiFlash, got %v", task.Tier)
	}
}

// TestProcessIssue_SkipIssueWithPR tests skipping issues that have PRs.
func TestProcessIssue_SkipIssueWithPR(t *testing.T) {
	config := &Config{
		Projects: []ProjectConfig{
			{
				Name:      "test-project",
				RepoOwner: "testowner",
				RepoName:  "testrepo",
			},
		},
	}
	queue := NewMockTaskQueue()
	router := NewRuleBasedRouter()
	ticketClient := NewMockTicketClient([]*ticket.Issue{})

	// Set up PR status for issue 123
	ticketClient.SetPRStatus(123, &ticket.PRStatus{
		Number:  456,
		State:   "open",
		HTMLURL: "https://github.com/testowner/testrepo/pull/456",
		Merged:  false,
	})

	supervisor := NewSupervisor(config, queue, router, ticketClient)

	issue := &ticket.Issue{
		Number:    123,
		Title:     "Add logging",
		Body:      "Add logging to the system",
		RepoOwner: "testowner",
		RepoName:  "testrepo",
	}

	err := supervisor.processIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("processIssue failed: %v", err)
	}

	if len(queue.enqueuedTasks) != 0 {
		t.Fatalf("expected 0 tasks enqueued (should skip issue with PR), got %d", len(queue.enqueuedTasks))
	}
}

// TestProcessIssue_SkipAlreadyEnqueued tests skipping issues already in queue.
func TestProcessIssue_SkipAlreadyEnqueued(t *testing.T) {
	config := &Config{
		Projects: []ProjectConfig{
			{
				Name:      "test-project",
				RepoOwner: "testowner",
				RepoName:  "testrepo",
			},
		},
	}
	queue := NewMockTaskQueue()
	router := NewRuleBasedRouter()
	ticketClient := NewMockTicketClient([]*ticket.Issue{})

	// Pre-populate queue with task for issue 123
	existingTask := &taskqueue.Task{
		ID:          "existing-task-id",
		IssueNumber: 123,
		RepoOwner:   "testowner",
		RepoName:    "testrepo",
		Status:      taskqueue.StatusPending,
	}
	_ = queue.Enqueue(context.Background(), existingTask)

	supervisor := NewSupervisor(config, queue, router, ticketClient)

	issue := &ticket.Issue{
		Number:    123,
		Title:     "Add logging",
		Body:      "Add logging to the system",
		RepoOwner: "testowner",
		RepoName:  "testrepo",
	}

	// This should skip since task already exists
	err := supervisor.processIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("processIssue failed: %v", err)
	}

	if len(queue.enqueuedTasks) != 1 {
		t.Fatalf("expected 1 task enqueued (the existing one), got %d", len(queue.enqueuedTasks))
	}
}

// TestMonitorStalledTasks tests stall detection and release.
func TestMonitorStalledTasks(t *testing.T) {
	config := &Config{
		Projects: []ProjectConfig{
			{
				Name:      "test-project",
				RepoOwner: "testowner",
				RepoName:  "testrepo",
			},
		},
		TaskTimeoutMinutes: 1, // 1 minute timeout for testing
		MaxRetryAttempts:   3, // Allow retries
	}
	queue := NewMockTaskQueue()
	router := NewRuleBasedRouter()
	ticketClient := NewMockTicketClient([]*ticket.Issue{})

	supervisor := NewSupervisor(config, queue, router, ticketClient)

	// Add a stalled task (claimed 2 minutes ago)
	stalledTask := &taskqueue.Task{
		ID:          "stalled-task",
		IssueNumber: 123,
		RepoOwner:   "testowner",
		RepoName:    "testrepo",
		Status:      taskqueue.StatusClaimed,
		WorkerID:    "worker-1",
		ClaimedAt:   time.Now().Add(-2 * time.Minute),
	}
	_ = queue.Enqueue(context.Background(), stalledTask)

	// Add a fresh task (claimed 30 seconds ago)
	freshTask := &taskqueue.Task{
		ID:          "fresh-task",
		IssueNumber: 124,
		RepoOwner:   "testowner",
		RepoName:    "testrepo",
		Status:      taskqueue.StatusClaimed,
		WorkerID:    "worker-2",
		ClaimedAt:   time.Now().Add(-30 * time.Second),
	}
	_ = queue.Enqueue(context.Background(), freshTask)

	// Monitor stalled tasks
	err := supervisor.monitorStalledTasks(context.Background())
	if err != nil {
		t.Fatalf("monitorStalledTasks failed: %v", err)
	}

	// Check that only the stalled task was released
	if len(queue.releasedTasks) != 1 {
		t.Fatalf("expected 1 task released, got %d", len(queue.releasedTasks))
	}
	if queue.releasedTasks[0] != "stalled-task" {
		t.Errorf("expected 'stalled-task' released, got '%s'", queue.releasedTasks[0])
	}

	// Verify the stalled task is now pending
	stalledAfterRelease, err := queue.Get(context.Background(), "stalled-task")
	if err != nil {
		t.Fatalf("failed to get task: %v", err)
	}
	if stalledAfterRelease.Status != taskqueue.StatusPending {
		t.Errorf("expected stalled task to be Pending after release, got %v", stalledAfterRelease.Status)
	}
	if stalledAfterRelease.WorkerID != "" {
		t.Errorf("expected WorkerID to be cleared, got '%s'", stalledAfterRelease.WorkerID)
	}
	if stalledAfterRelease.Attempts != 1 {
		t.Errorf("expected Attempts to be incremented to 1, got %d", stalledAfterRelease.Attempts)
	}
}

// TestPollIssues tests issue polling.
func TestPollIssues(t *testing.T) {
	config := &Config{
		Projects: []ProjectConfig{
			{
				Name:      "test-project",
				RepoOwner: "testowner",
				RepoName:  "testrepo",
			},
		},
	}
	queue := NewMockTaskQueue()
	router := NewRuleBasedRouter()

	// Mock ticket client with 2 issues
	issues := []*ticket.Issue{
		{
			Number:    123,
			Title:     "Add logging",
			Body:      "Add logging to the system",
			RepoOwner: "testowner",
			RepoName:  "testrepo",
		},
		{
			Number:    124,
			Title:     "Fix typo",
			Body:      "Fix typo in README",
			RepoOwner: "testowner",
			RepoName:  "testrepo",
		},
	}
	ticketClient := NewMockTicketClient(issues)

	supervisor := NewSupervisor(config, queue, router, ticketClient)

	err := supervisor.pollIssues(context.Background())
	if err != nil {
		t.Fatalf("pollIssues failed: %v", err)
	}

	if len(queue.enqueuedTasks) != 2 {
		t.Fatalf("expected 2 tasks enqueued, got %d", len(queue.enqueuedTasks))
	}

	// Verify both issues were processed
	issueNumbers := []int{queue.enqueuedTasks[0].IssueNumber, queue.enqueuedTasks[1].IssueNumber}
	if !contains(issueNumbers, 123) || !contains(issueNumbers, 124) {
		t.Errorf("expected issue numbers 123 and 124, got %v", issueNumbers)
	}
}

// TestPollIssues_ErrorHandling tests graceful error handling during polling.
func TestPollIssues_ErrorHandling(t *testing.T) {
	config := &Config{
		Projects: []ProjectConfig{
			{
				Name:      "test-project",
				RepoOwner: "testowner",
				RepoName:  "testrepo",
			},
		},
	}
	queue := NewMockTaskQueue()
	router := NewRuleBasedRouter()
	ticketClient := NewMockTicketClient([]*ticket.Issue{})

	// Set error on ticket client
	ticketClient.SetError(errors.New("API error"))

	supervisor := NewSupervisor(config, queue, router, ticketClient)

	// Should log error but not crash
	err := supervisor.pollIssues(context.Background())
	if err == nil {
		t.Fatal("expected error from pollIssues, got nil")
	}
}

// TestShutdown tests graceful shutdown.
func TestShutdown(t *testing.T) {
	config := &Config{
		Projects: []ProjectConfig{
			{
				Name:      "test-project",
				RepoOwner: "testowner",
				RepoName:  "testrepo",
			},
		},
		PollIntervalSeconds: 60,
	}
	queue := NewMockTaskQueue()
	router := NewRuleBasedRouter()
	ticketClient := NewMockTicketClient([]*ticket.Issue{})

	supervisor := NewSupervisor(config, queue, router, ticketClient)

	// Start supervisor in goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- supervisor.Run(ctx)
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Now shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()

	err := supervisor.Shutdown(shutdownCtx)
	if err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Verify Run() completed
	select {
	case <-errCh:
		// Good - run completed
	case <-time.After(1 * time.Second):
		t.Fatal("Run() did not complete after shutdown")
	}
}

// Helper function to check if a slice contains an element.
func contains(slice []int, elem int) bool {
	for _, v := range slice {
		if v == elem {
			return true
		}
	}
	return false
}
