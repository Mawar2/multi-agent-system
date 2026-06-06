// Package integration_test exercises the full multi-agent orchestration pipeline
// using a real JSONQueue and Supervisor with mocked GitHub and worker backends.
package integration_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/orchestrator"
	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/ticket"
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

// ── mockTicketClient ─────────────────────────────────────────────────────────

type mockTicketClient struct {
	mu       sync.Mutex
	issues   []*ticket.Issue
	prStatus map[int]*ticket.PRStatus
	fetchErr error
}

func newMockTicketClient(issues ...*ticket.Issue) *mockTicketClient {
	return &mockTicketClient{
		issues:   issues,
		prStatus: make(map[int]*ticket.PRStatus),
	}
}

func (m *mockTicketClient) FetchIssues(_ context.Context, _, _ string, _ []string) ([]*ticket.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fetchErr != nil {
		return nil, m.fetchErr
	}
	return m.issues, nil
}

func (m *mockTicketClient) GetIssue(_ context.Context, _, _ string, number int) (*ticket.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, iss := range m.issues {
		if iss.Number == number {
			return iss, nil
		}
	}
	return nil, fmt.Errorf("issue #%d not found", number)
}

func (m *mockTicketClient) ParseAcceptanceCriteria(_ string) ([]string, error) {
	return nil, nil
}

func (m *mockTicketClient) CheckPRStatus(_ context.Context, _, _ string, issueNumber int) (*ticket.PRStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prStatus[issueNumber], nil
}

func (m *mockTicketClient) setPRStatus(issueNumber int, s *ticket.PRStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prStatus[issueNumber] = s
}

// ── mockWorker ───────────────────────────────────────────────────────────────

// mockWorker is a fake Worker that delegates Claim/Release to the real queue
// and lets tests control Execute behaviour via executeFunc.
type mockWorker struct {
	id          string
	tier        taskqueue.Tier
	queue       taskqueue.TaskQueue
	executeFunc func(ctx context.Context, task *taskqueue.Task) (*worker.Result, error)
}

func newMockWorker(id string, tier taskqueue.Tier, q taskqueue.TaskQueue) *mockWorker {
	return &mockWorker{id: id, tier: tier, queue: q}
}

func (w *mockWorker) Claim(ctx context.Context) (*taskqueue.Task, error) {
	return w.queue.Dequeue(ctx, w.tier, w.id)
}

func (w *mockWorker) Execute(ctx context.Context, task *taskqueue.Task) (*worker.Result, error) {
	if w.executeFunc != nil {
		return w.executeFunc(ctx, task)
	}
	// Default: success – mark InProgress then Review, add branch+PR.
	task.Status = taskqueue.StatusInProgress
	task.WorkerID = w.id
	task.StartedAt = time.Now()
	if err := w.queue.Update(ctx, task); err != nil {
		return nil, err
	}
	task.Status = taskqueue.StatusReview
	task.BranchName = fmt.Sprintf("feature/issue-%d", task.IssueNumber)
	task.PRNumber = 100 + task.IssueNumber
	task.CompletedAt = time.Now()
	if err := w.queue.Update(ctx, task); err != nil {
		return nil, err
	}
	return &worker.Result{
		TaskID:     task.ID,
		Success:    true,
		BranchName: task.BranchName,
		PRNumber:   task.PRNumber,
	}, nil
}

func (w *mockWorker) Release(ctx context.Context, taskID string) error {
	return w.queue.Release(ctx, taskID)
}

func (w *mockWorker) Health(_ context.Context) (*worker.HealthStatus, error) {
	return &worker.HealthStatus{WorkerID: w.id, Tier: w.tier, Healthy: true}, nil
}

func (w *mockWorker) ID() string            { return w.id }
func (w *mockWorker) Tier() taskqueue.Tier  { return w.tier }

// ── helpers ──────────────────────────────────────────────────────────────────

func testConfig(owner, repo string) *orchestrator.Config {
	return &orchestrator.Config{
		Projects: []orchestrator.ProjectConfig{
			{Name: "test", RepoOwner: owner, RepoName: repo},
		},
		PollIntervalSeconds: 3600,
		TaskTimeoutMinutes:  120,
		MaxRetryAttempts:    3,
	}
}

func newQueue(t *testing.T) taskqueue.TaskQueue {
	t.Helper()
	q, err := taskqueue.NewJSONQueue(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONQueue: %v", err)
	}
	return q
}

// ── tests ────────────────────────────────────────────────────────────────────

// TestHappyPath: issue discovered → task queued → worker claims → Review with branch+PR.
func TestHappyPath(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)
	issue := &ticket.Issue{Number: 10, Title: "Fix typo in docs", Body: "fix typo", RepoOwner: "Org", RepoName: "Repo"}
	tc := newMockTicketClient(issue)
	sup := orchestrator.NewSupervisor(testConfig("Org", "Repo"), q, orchestrator.NewRuleBasedRouter(), tc)

	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("PollIssues: %v", err)
	}

	w := newMockWorker("w1", taskqueue.TierGeminiFlash, q)
	task, err := w.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if task == nil {
		t.Fatal("expected a task to claim, got nil")
	}

	result, err := w.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got failure: %s", result.ErrorMsg)
	}

	final, err := q.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != taskqueue.StatusReview {
		t.Errorf("expected StatusReview, got %v", final.Status)
	}
	if final.BranchName == "" {
		t.Error("expected BranchName to be set")
	}
	if final.PRNumber == 0 {
		t.Error("expected PRNumber to be set")
	}
}

// TestWorkerFailure: worker marks task Failed; error message stored.
func TestWorkerFailure(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)
	issue := &ticket.Issue{Number: 20, Title: "Some task", Body: "do something", RepoOwner: "Org", RepoName: "Repo"}
	tc := newMockTicketClient(issue)
	sup := orchestrator.NewSupervisor(testConfig("Org", "Repo"), q, orchestrator.NewRuleBasedRouter(), tc)

	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("PollIssues: %v", err)
	}

	w := newMockWorker("w1", taskqueue.TierGeminiPro, q)
	w.executeFunc = func(ctx context.Context, task *taskqueue.Task) (*worker.Result, error) {
		task.Status = taskqueue.StatusFailed
		task.ErrorMsg = "compilation failed"
		task.CompletedAt = time.Now()
		_ = q.Update(ctx, task)
		return &worker.Result{TaskID: task.ID, Success: false, ErrorMsg: "compilation failed"}, nil
	}

	task, err := w.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if task == nil {
		t.Fatal("expected a task")
	}

	result, err := w.Execute(ctx, task)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure result")
	}

	final, err := q.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != taskqueue.StatusFailed {
		t.Errorf("expected StatusFailed, got %v", final.Status)
	}
	if final.ErrorMsg == "" {
		t.Error("expected ErrorMsg to be set")
	}
}

// TestWorkerReleasedOnCrash: Release → Pending, Attempts incremented, re-claimable.
func TestWorkerReleasedOnCrash(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)
	issue := &ticket.Issue{Number: 30, Title: "Some task", Body: "do something", RepoOwner: "Org", RepoName: "Repo"}
	tc := newMockTicketClient(issue)
	sup := orchestrator.NewSupervisor(testConfig("Org", "Repo"), q, orchestrator.NewRuleBasedRouter(), tc)

	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("PollIssues: %v", err)
	}

	w1 := newMockWorker("w1", taskqueue.TierGeminiPro, q)
	task, err := w1.Claim(ctx)
	if err != nil || task == nil {
		t.Fatalf("Claim: err=%v task=%v", err, task)
	}
	if task.Status != taskqueue.StatusClaimed {
		t.Fatalf("expected StatusClaimed, got %v", task.Status)
	}

	// Simulate crash: release the task back to the queue.
	if err := w1.Release(ctx, task.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}

	released, err := q.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if released.Status != taskqueue.StatusPending {
		t.Errorf("expected StatusPending after release, got %v", released.Status)
	}
	if released.WorkerID != "" {
		t.Errorf("expected WorkerID cleared, got %q", released.WorkerID)
	}
	if released.Attempts != 1 {
		t.Errorf("expected Attempts=1, got %d", released.Attempts)
	}

	// Task must be re-claimable by a second worker.
	w2 := newMockWorker("w2", taskqueue.TierGeminiPro, q)
	task2, err := w2.Claim(ctx)
	if err != nil || task2 == nil {
		t.Fatalf("second Claim: err=%v task=%v", err, task2)
	}
	if task2.ID != task.ID {
		t.Errorf("expected same task ID %s, got %s", task.ID, task2.ID)
	}
}

// TestRouting verifies the rule-based router maps titles to the right tier.
func TestRouting(t *testing.T) {
	cases := []struct {
		name  string
		title string
		body  string
		tier  taskqueue.Tier
	}{
		{"simple", "fix typo in readme", "update readme", taskqueue.TierGeminiFlash},
		{"medium", "Add pagination to API", "add pagination feature", taskqueue.TierGeminiPro},
		{"complex", "Migrate database schema to new architecture", "database migration", taskqueue.TierClaude},
	}

	ctx := context.Background()
	router := orchestrator.NewRuleBasedRouter()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := &ticket.Issue{Number: 1, Title: tc.title, Body: tc.body, RepoOwner: "Org", RepoName: "Repo"}
			_, tier, err := router.Route(ctx, issue)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if tier != tc.tier {
				t.Errorf("expected tier %v, got %v", tc.tier, tier)
			}
		})
	}
}

// TestRoutingByLabel verifies that issue labels override the default routing.
func TestRoutingByLabel(t *testing.T) {
	cases := []struct {
		name   string
		labels []string
		tier   taskqueue.Tier
	}{
		{"simple", []string{"simple"}, taskqueue.TierGeminiFlash},
		{"complex", []string{"complex"}, taskqueue.TierClaude},
	}

	ctx := context.Background()
	router := orchestrator.NewRuleBasedRouter()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := &ticket.Issue{
				Number: 1, Title: "Generic task", Body: "some work",
				Labels: tc.labels, RepoOwner: "Org", RepoName: "Repo",
			}
			_, tier, err := router.Route(ctx, issue)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if tier != tc.tier {
				t.Errorf("expected tier %v, got %v", tc.tier, tier)
			}
		})
	}
}

// TestDuplicateDetection_AlreadyQueued: polling the same issue twice creates only one task.
func TestDuplicateDetection_AlreadyQueued(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)
	issue := &ticket.Issue{Number: 40, Title: "Fix typo", Body: "fix typo", RepoOwner: "Org", RepoName: "Repo"}
	tc := newMockTicketClient(issue)
	sup := orchestrator.NewSupervisor(testConfig("Org", "Repo"), q, orchestrator.NewRuleBasedRouter(), tc)

	// First poll.
	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("first PollIssues: %v", err)
	}
	// Second poll – same issues returned.
	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("second PollIssues: %v", err)
	}

	tasks, err := q.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var issueCount int
	for _, task := range tasks {
		if task.IssueNumber == 40 {
			issueCount++
		}
	}
	if issueCount != 1 {
		t.Errorf("expected 1 task for issue #40, got %d", issueCount)
	}
}

// TestDuplicateDetection_ExistingPR: issue with an open PR is skipped entirely.
func TestDuplicateDetection_ExistingPR(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)
	issue := &ticket.Issue{Number: 50, Title: "Fix bug", Body: "fix it", RepoOwner: "Org", RepoName: "Repo"}
	tc := newMockTicketClient(issue)
	tc.setPRStatus(50, &ticket.PRStatus{Number: 99, State: "open"})
	sup := orchestrator.NewSupervisor(testConfig("Org", "Repo"), q, orchestrator.NewRuleBasedRouter(), tc)

	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("PollIssues: %v", err)
	}

	tasks, err := q.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, task := range tasks {
		if task.IssueNumber == 50 {
			t.Errorf("expected issue #50 to be skipped (has open PR), but found task %s", task.ID)
		}
	}
}

// TestMultipleIssues: three issues → three tasks on correct tiers.
func TestMultipleIssues(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)
	issues := []*ticket.Issue{
		{Number: 1, Title: "fix typo in readme", Body: "fix typo", RepoOwner: "Org", RepoName: "Repo"},
		{Number: 2, Title: "Add new feature to API", Body: "add feature", RepoOwner: "Org", RepoName: "Repo"},
		{Number: 3, Title: "Migrate database schema", Body: "database migration", RepoOwner: "Org", RepoName: "Repo"},
	}
	tc := newMockTicketClient(issues...)
	sup := orchestrator.NewSupervisor(testConfig("Org", "Repo"), q, orchestrator.NewRuleBasedRouter(), tc)

	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("PollIssues: %v", err)
	}

	tasks, err := q.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	tierByIssue := make(map[int]taskqueue.Tier)
	for _, task := range tasks {
		tierByIssue[task.IssueNumber] = task.Tier
	}
	if tierByIssue[1] != taskqueue.TierGeminiFlash {
		t.Errorf("issue #1 (simple): expected GeminiFlash, got %v", tierByIssue[1])
	}
	if tierByIssue[2] != taskqueue.TierGeminiPro {
		t.Errorf("issue #2 (medium): expected GeminiPro, got %v", tierByIssue[2])
	}
	if tierByIssue[3] != taskqueue.TierClaude {
		t.Errorf("issue #3 (complex): expected Claude, got %v", tierByIssue[3])
	}
}

// TestMultipleWorkers: three concurrent workers race for three tasks; no double-claims.
func TestMultipleWorkers(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)

	// Enqueue 3 GeminiFlash tasks directly.
	for i := 1; i <= 3; i++ {
		task := &taskqueue.Task{
			ID:          fmt.Sprintf("task-%d", i),
			IssueNumber: i,
			RepoOwner:   "Org",
			RepoName:    "Repo",
			Status:      taskqueue.StatusPending,
			Tier:        taskqueue.TierGeminiFlash,
		}
		if err := q.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}

	var claimedCount atomic.Int32
	var mu sync.Mutex
	claimedIDs := make(map[string]string) // taskID → workerID

	var wg sync.WaitGroup
	for i := 1; i <= 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			w := newMockWorker(fmt.Sprintf("worker-%d", id), taskqueue.TierGeminiFlash, q)
			task, err := w.Claim(ctx)
			if err != nil {
				t.Errorf("worker-%d Claim: %v", id, err)
				return
			}
			if task == nil {
				return
			}
			claimedCount.Add(1)
			mu.Lock()
			claimedIDs[task.ID] = w.id
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	if int(claimedCount.Load()) != 3 {
		t.Errorf("expected 3 claims, got %d", claimedCount.Load())
	}
	if len(claimedIDs) != 3 {
		t.Errorf("expected 3 unique task IDs claimed, got %d", len(claimedIDs))
	}
}

// TestStalledTaskRecovery: a released task is re-claimable by a new worker.
func TestStalledTaskRecovery(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)
	issue := &ticket.Issue{Number: 60, Title: "Some task", Body: "do work", RepoOwner: "Org", RepoName: "Repo"}
	tc := newMockTicketClient(issue)
	sup := orchestrator.NewSupervisor(testConfig("Org", "Repo"), q, orchestrator.NewRuleBasedRouter(), tc)

	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("PollIssues: %v", err)
	}

	// First worker claims but stalls (simulate stall via Release).
	w1 := newMockWorker("w1", taskqueue.TierGeminiPro, q)
	task, err := w1.Claim(ctx)
	if err != nil || task == nil {
		t.Fatalf("w1 Claim: err=%v task=%v", err, task)
	}
	if err := w1.Release(ctx, task.ID); err != nil {
		t.Fatalf("w1 Release: %v", err)
	}

	// Second worker should be able to claim the same task.
	w2 := newMockWorker("w2", taskqueue.TierGeminiPro, q)
	task2, err := w2.Claim(ctx)
	if err != nil {
		t.Fatalf("w2 Claim: %v", err)
	}
	if task2 == nil {
		t.Fatal("expected w2 to claim the released task, got nil")
	}
	if task2.ID != task.ID {
		t.Errorf("expected task ID %s, got %s", task.ID, task2.ID)
	}
	if task2.Attempts != 1 {
		t.Errorf("expected Attempts=1 after one release, got %d", task2.Attempts)
	}
}

// TestTaskMetadata: new tasks carry task_type=issue and ReviewIteration=0.
func TestTaskMetadata(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)
	issue := &ticket.Issue{Number: 70, Title: "Add logging", Body: "add logging", RepoOwner: "Org", RepoName: "Repo"}
	tc := newMockTicketClient(issue)
	sup := orchestrator.NewSupervisor(testConfig("Org", "Repo"), q, orchestrator.NewRuleBasedRouter(), tc)

	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("PollIssues: %v", err)
	}

	tasks, err := q.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]

	if task.Metadata["task_type"] != "issue" {
		t.Errorf("expected task_type=issue, got %q", task.Metadata["task_type"])
	}
	if task.ReviewIteration != 0 {
		t.Errorf("expected ReviewIteration=0, got %d", task.ReviewIteration)
	}
}

// TestFeedbackTaskInheritance: pr_feedback tasks inherit branch/PR/tier/complexity from parent.
func TestFeedbackTaskInheritance(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)

	// Enqueue a parent "issue" task that has already created a PR (in Review).
	parent := &taskqueue.Task{
		ID:              "parent-task",
		IssueNumber:     80,
		RepoOwner:       "Org",
		RepoName:        "Repo",
		Title:           "Add feature",
		Complexity:      taskqueue.ComplexityMedium,
		Tier:            taskqueue.TierGeminiPro,
		Status:          taskqueue.StatusReview,
		BranchName:      "feature/issue-80-add-feature",
		PRNumber:        180,
		ReviewIteration: 0,
		Metadata:        map[string]string{"task_type": "issue"},
	}
	if err := q.Enqueue(ctx, parent); err != nil {
		t.Fatalf("Enqueue parent: %v", err)
	}

	// Manually create the pr_feedback task as the supervisor would.
	fixTask := &taskqueue.Task{
		ID:              "fix-task",
		IssueNumber:     parent.IssueNumber,
		RepoOwner:       parent.RepoOwner,
		RepoName:        parent.RepoName,
		Title:           fmt.Sprintf("Fix AI review feedback - %s", parent.Title),
		Complexity:      parent.Complexity,
		Tier:            parent.Tier,
		Status:          taskqueue.StatusPending,
		BranchName:      parent.BranchName,
		PRNumber:        parent.PRNumber,
		ParentTaskID:    parent.ID,
		ReviewIteration: parent.ReviewIteration + 1,
		ReviewFeedback:  "## 🤖 AI Code Review\nPlease add error handling.",
		ReviewCommentID: 12345,
		Metadata:        map[string]string{"task_type": "pr_feedback"},
	}
	if err := q.Enqueue(ctx, fixTask); err != nil {
		t.Fatalf("Enqueue fixTask: %v", err)
	}

	retrieved, err := q.Get(ctx, "fix-task")
	if err != nil {
		t.Fatalf("Get fix-task: %v", err)
	}

	if retrieved.ParentTaskID != parent.ID {
		t.Errorf("ParentTaskID: expected %s, got %s", parent.ID, retrieved.ParentTaskID)
	}
	if retrieved.BranchName != parent.BranchName {
		t.Errorf("BranchName: expected %s, got %s", parent.BranchName, retrieved.BranchName)
	}
	if retrieved.PRNumber != parent.PRNumber {
		t.Errorf("PRNumber: expected %d, got %d", parent.PRNumber, retrieved.PRNumber)
	}
	if retrieved.Tier != parent.Tier {
		t.Errorf("Tier: expected %v, got %v", parent.Tier, retrieved.Tier)
	}
	if retrieved.Complexity != parent.Complexity {
		t.Errorf("Complexity: expected %v, got %v", parent.Complexity, retrieved.Complexity)
	}
	if retrieved.ReviewIteration != 1 {
		t.Errorf("ReviewIteration: expected 1, got %d", retrieved.ReviewIteration)
	}
	if retrieved.Metadata["task_type"] != "pr_feedback" {
		t.Errorf("task_type: expected pr_feedback, got %q", retrieved.Metadata["task_type"])
	}

	// Verify the fix task is claimable by a GeminiPro worker.
	w := newMockWorker("w1", taskqueue.TierGeminiPro, q)
	claimed, err := w.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim fix task: %v", err)
	}
	if claimed == nil {
		t.Fatal("expected fix task to be claimable, got nil")
	}
	if claimed.ID != fixTask.ID {
		t.Errorf("expected fix-task to be claimed, got %s", claimed.ID)
	}
}

// TestQueuePersistence: tasks survive JSONQueue reconstruction (process restart).
func TestQueuePersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// First queue instance: enqueue tasks.
	q1, err := taskqueue.NewJSONQueue(dir)
	if err != nil {
		t.Fatalf("NewJSONQueue (first): %v", err)
	}
	issue := &ticket.Issue{Number: 90, Title: "Fix typo", Body: "fix typo", RepoOwner: "Org", RepoName: "Repo"}
	tc := newMockTicketClient(issue)
	sup := orchestrator.NewSupervisor(testConfig("Org", "Repo"), q1, orchestrator.NewRuleBasedRouter(), tc)
	if err := sup.PollIssues(ctx); err != nil {
		t.Fatalf("PollIssues: %v", err)
	}
	tasks1, err := q1.List(ctx, nil)
	if err != nil {
		t.Fatalf("List (first queue): %v", err)
	}
	if len(tasks1) != 1 {
		t.Fatalf("expected 1 task after first poll, got %d", len(tasks1))
	}
	taskID := tasks1[0].ID

	// Second queue instance pointing at the same directory: task must still be there.
	q2, err := taskqueue.NewJSONQueue(dir)
	if err != nil {
		t.Fatalf("NewJSONQueue (second): %v", err)
	}
	tasks2, err := q2.List(ctx, nil)
	if err != nil {
		t.Fatalf("List (second queue): %v", err)
	}
	if len(tasks2) != 1 {
		t.Fatalf("expected 1 task in second queue, got %d", len(tasks2))
	}
	if tasks2[0].ID != taskID {
		t.Errorf("task ID mismatch: expected %s, got %s", taskID, tasks2[0].ID)
	}
	if tasks2[0].Status != taskqueue.StatusPending {
		t.Errorf("expected StatusPending after reload, got %v", tasks2[0].Status)
	}
}
