// Package integration_test contains end-to-end tests for the multi-agent orchestration
// pipeline. It exercises the full flow (supervisor → queue → worker) using mock GitHub
// and Claude CLI backends, with real JSONQueue, Supervisor, and RuleBasedRouter.
package integration_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/orchestrator"
	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/ticket"
	"github.com/google/uuid"
)

// ─── Mock ticket client (GitHub) ────────────────────────────────────────────

type mockTicketClient struct {
	mu         sync.Mutex
	issues     []*ticket.Issue
	prStatuses map[int]*ticket.PRStatus // issueNumber → status (nil key = no PR)
}

func newMockClient(issues ...*ticket.Issue) *mockTicketClient {
	return &mockTicketClient{
		issues:     issues,
		prStatuses: make(map[int]*ticket.PRStatus),
	}
}

func (m *mockTicketClient) FetchIssues(_ context.Context, owner, repo string, _ []string) ([]*ticket.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*ticket.Issue
	for _, iss := range m.issues {
		if iss.RepoOwner == owner && iss.RepoName == repo {
			out = append(out, iss)
		}
	}
	return out, nil
}

func (m *mockTicketClient) GetIssue(_ context.Context, owner, repo string, number int) (*ticket.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, iss := range m.issues {
		if iss.Number == number && iss.RepoOwner == owner && iss.RepoName == repo {
			return iss, nil
		}
	}
	return nil, fmt.Errorf("issue #%d not found", number)
}

func (m *mockTicketClient) ParseAcceptanceCriteria(_ string) ([]string, error) {
	return nil, nil
}

func (m *mockTicketClient) CheckPRStatus(_ context.Context, _ string, _ string, issueNumber int) (*ticket.PRStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if pr, ok := m.prStatuses[issueNumber]; ok {
		return pr, nil
	}
	return nil, nil
}

// ─── Mock worker (Claude CLI / LLM + workspace + quality gates) ─────────────

type mockWorker struct {
	id    string
	tier  taskqueue.Tier
	queue taskqueue.TaskQueue
}

func (w *mockWorker) claim(ctx context.Context) (*taskqueue.Task, error) {
	return w.queue.Dequeue(ctx, w.tier, w.id)
}

func (w *mockWorker) succeed(ctx context.Context, task *taskqueue.Task, branch string, prNum int) error {
	task.Status = taskqueue.StatusInProgress
	task.StartedAt = time.Now()
	if err := w.queue.Update(ctx, task); err != nil {
		return err
	}
	task.Status = taskqueue.StatusReview
	task.BranchName = branch
	task.PRNumber = prNum
	task.CompletedAt = time.Now()
	return w.queue.Update(ctx, task)
}

func (w *mockWorker) fail(ctx context.Context, task *taskqueue.Task, msg string) error {
	task.Status = taskqueue.StatusFailed
	task.ErrorMsg = msg
	task.CompletedAt = time.Now()
	return w.queue.Update(ctx, task)
}

// ─── Test helpers ────────────────────────────────────────────────────────────

const (
	testOwner = "Mawar2"
	testRepo  = "Kaimi"
)

func testConfig() *orchestrator.Config {
	return &orchestrator.Config{
		Projects: []orchestrator.ProjectConfig{
			{Name: "test", RepoOwner: testOwner, RepoName: testRepo},
		},
		PollIntervalSeconds: 3600, // effectively never – tests control polling manually
		TaskTimeoutMinutes:  120,
		MaxRetryAttempts:    3,
		TaskQueueDir:        "./tasks",
	}
}

func newQueue(t *testing.T) *taskqueue.JSONQueue {
	t.Helper()
	q, err := taskqueue.NewJSONQueue(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONQueue: %v", err)
	}
	return q
}

func newIssue(number int, title, body string, labels ...string) *ticket.Issue {
	return &ticket.Issue{
		Number:    number,
		Title:     title,
		Body:      body,
		Labels:    labels,
		RepoOwner: testOwner,
		RepoName:  testRepo,
	}
}

// runOnePoll creates a fresh Supervisor (Supervisor.done is one-shot and cannot
// be reused after Run exits), runs it until the initial synchronous poll
// completes, then cancels the context. Mock client has zero I/O latency so
// 100 ms is always sufficient.
func runOnePoll(t *testing.T, q taskqueue.TaskQueue, client ticket.Client) {
	t.Helper()
	sup := orchestrator.NewSupervisor(testConfig(), q, orchestrator.NewRuleBasedRouter(), client)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		sup.Run(ctx) //nolint:errcheck – context.Canceled is the expected return
	}()
	time.Sleep(100 * time.Millisecond) // allow initial pollIssues to complete
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("supervisor did not stop within 5s")
	}
}

// ─── Tests ───────────────────────────────────────────────────────────────────

// TestHappyPath: Issue discovered → task queued → worker claims → task reaches Review.
func TestHappyPath(t *testing.T) {
	q := newQueue(t)
	client := newMockClient(newIssue(1, "fix typo in README", "Small docs fix"))

	runOnePoll(t, q, client)

	ctx := context.Background()
	tasks, err := q.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after poll, got %d", len(tasks))
	}
	task := tasks[0]
	if task.IssueNumber != 1 {
		t.Errorf("IssueNumber: want 1, got %d", task.IssueNumber)
	}
	if task.Status != taskqueue.StatusPending {
		t.Errorf("Status: want Pending, got %v", task.Status)
	}

	w := &mockWorker{id: "w1", tier: task.Tier, queue: q}
	claimed, err := w.claim(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim: err=%v task=%v", err, claimed)
	}
	if err := w.succeed(ctx, claimed, "feature/issue-1-fix-typo", 99); err != nil {
		t.Fatalf("succeed: %v", err)
	}

	final, err := q.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != taskqueue.StatusReview {
		t.Errorf("final Status: want Review, got %v", final.Status)
	}
	if final.BranchName == "" {
		t.Error("BranchName should be set after worker succeeds")
	}
	if final.PRNumber == 0 {
		t.Error("PRNumber should be set after worker succeeds")
	}
}

// TestWorkerFailure: Worker marks task Failed; error message stored on task.
func TestWorkerFailure(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)

	task := &taskqueue.Task{
		ID:          uuid.New().String(),
		IssueNumber: 2,
		RepoOwner:   testOwner,
		RepoName:    testRepo,
		Title:       "implement database migration",
		Tier:        taskqueue.TierClaude,
		Status:      taskqueue.StatusPending,
		Metadata:    map[string]string{"task_type": "issue"},
	}
	if err := q.Enqueue(ctx, task); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	w := &mockWorker{id: "w1", tier: taskqueue.TierClaude, queue: q}
	claimed, err := w.claim(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}
	if err := w.fail(ctx, claimed, "test suite failed: 3 errors"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	got, err := q.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != taskqueue.StatusFailed {
		t.Errorf("Status: want Failed, got %v", got.Status)
	}
	if got.ErrorMsg == "" {
		t.Error("ErrorMsg should be set on failure")
	}
}

// TestWorkerReleasedOnCrash: Worker calls Release → Pending, Attempts incremented, re-claimable.
func TestWorkerReleasedOnCrash(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)

	task := &taskqueue.Task{
		ID:          uuid.New().String(),
		IssueNumber: 3,
		RepoOwner:   testOwner,
		RepoName:    testRepo,
		Title:       "add unit tests to service",
		Tier:        taskqueue.TierGeminiPro,
		Status:      taskqueue.StatusPending,
		Metadata:    map[string]string{"task_type": "issue"},
	}
	if err := q.Enqueue(ctx, task); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	w1 := &mockWorker{id: "w1", tier: taskqueue.TierGeminiPro, queue: q}
	claimed, err := w1.claim(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("claim: %v", err)
	}
	if claimed.Status != taskqueue.StatusClaimed {
		t.Errorf("after claim Status: want Claimed, got %v", claimed.Status)
	}

	// Simulate crash – release back to queue.
	if err := q.Release(ctx, task.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}

	released, err := q.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get after release: %v", err)
	}
	if released.Status != taskqueue.StatusPending {
		t.Errorf("after release Status: want Pending, got %v", released.Status)
	}
	if released.WorkerID != "" {
		t.Errorf("WorkerID should be cleared after release, got %q", released.WorkerID)
	}
	if released.Attempts != 1 {
		t.Errorf("Attempts: want 1, got %d", released.Attempts)
	}

	// Another worker reclaims the released task.
	w2 := &mockWorker{id: "w2", tier: taskqueue.TierGeminiPro, queue: q}
	reclaimed, err := w2.claim(ctx)
	if err != nil || reclaimed == nil {
		t.Fatalf("w2 could not reclaim released task: %v", err)
	}
	if reclaimed.ID != task.ID {
		t.Errorf("reclaimed task ID: want %s, got %s", task.ID, reclaimed.ID)
	}
}

// TestRouting: Heuristic routing maps issue content to correct tier.
func TestRouting(t *testing.T) {
	router := orchestrator.NewRuleBasedRouter()
	ctx := context.Background()

	tests := []struct {
		name           string
		issue          *ticket.Issue
		wantComplexity taskqueue.Complexity
		wantTier       taskqueue.Tier
	}{
		{
			name:           "simple",
			issue:          newIssue(10, "fix typo in README", ""),
			wantComplexity: taskqueue.ComplexitySimple,
			wantTier:       taskqueue.TierGeminiFlash,
		},
		{
			name:           "medium",
			issue:          newIssue(11, "add input validation to user form", ""),
			wantComplexity: taskqueue.ComplexityMedium,
			wantTier:       taskqueue.TierGeminiPro,
		},
		{
			name:           "complex",
			issue:          newIssue(12, "implement database migration for schema change", "breaking change requiring database migration"),
			wantComplexity: taskqueue.ComplexityComplex,
			wantTier:       taskqueue.TierClaude,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			complexity, tier, err := router.Route(ctx, tc.issue)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if complexity != tc.wantComplexity {
				t.Errorf("complexity: want %v, got %v", tc.wantComplexity, complexity)
			}
			if tier != tc.wantTier {
				t.Errorf("tier: want %v, got %v", tc.wantTier, tier)
			}
		})
	}
}

// TestRoutingByLabel: Label-based routing overrides keyword heuristic.
func TestRoutingByLabel(t *testing.T) {
	router := orchestrator.NewRuleBasedRouter()
	ctx := context.Background()

	tests := []struct {
		name     string
		issue    *ticket.Issue
		wantTier taskqueue.Tier
	}{
		{
			name:     "simple",
			issue:    newIssue(20, "implement some feature", "", "simple"),
			wantTier: taskqueue.TierGeminiFlash,
		},
		{
			name:     "complex",
			issue:    newIssue(21, "update a configuration file", "", "complex"),
			wantTier: taskqueue.TierClaude,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, tier, err := router.Route(ctx, tc.issue)
			if err != nil {
				t.Fatalf("Route: %v", err)
			}
			if tier != tc.wantTier {
				t.Errorf("tier: want %v, got %v", tc.wantTier, tier)
			}
		})
	}
}

// TestDuplicateDetection_AlreadyQueued: Same issue polled twice → only one task created.
func TestDuplicateDetection_AlreadyQueued(t *testing.T) {
	q := newQueue(t)
	client := newMockClient(newIssue(30, "add feature X", "Medium complexity feature"))

	runOnePoll(t, q, client)
	runOnePoll(t, q, client)

	ctx := context.Background()
	tasks, err := q.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Errorf("expected 1 task after 2 polls (duplicate detection failed), got %d", len(tasks))
	}
}

// TestDuplicateDetection_ExistingPR: Issue with open PR → task skipped entirely.
func TestDuplicateDetection_ExistingPR(t *testing.T) {
	q := newQueue(t)
	client := newMockClient(newIssue(31, "fix bug in handler", "Medium complexity bug fix"))
	client.prStatuses[31] = &ticket.PRStatus{Number: 100, State: "open"}

	runOnePoll(t, q, client)

	ctx := context.Background()
	tasks, err := q.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks (issue already has open PR), got %d", len(tasks))
	}
}

// TestMultipleIssues: Three issues in one poll → three tasks, each on the right tier.
func TestMultipleIssues(t *testing.T) {
	q := newQueue(t)
	client := newMockClient(
		newIssue(40, "fix typo in docs", ""),
		newIssue(41, "add input validation to API", ""),
		newIssue(42, "implement security authentication system", "security and authorization overhaul"),
	)

	runOnePoll(t, q, client)

	ctx := context.Background()
	tasks, err := q.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	tierCounts := map[taskqueue.Tier]int{}
	for _, task := range tasks {
		tierCounts[task.Tier]++
	}
	if tierCounts[taskqueue.TierGeminiFlash] != 1 {
		t.Errorf("GeminiFlash tasks: want 1, got %d", tierCounts[taskqueue.TierGeminiFlash])
	}
	if tierCounts[taskqueue.TierGeminiPro] != 1 {
		t.Errorf("GeminiPro tasks: want 1, got %d", tierCounts[taskqueue.TierGeminiPro])
	}
	if tierCounts[taskqueue.TierClaude] != 1 {
		t.Errorf("Claude tasks: want 1, got %d", tierCounts[taskqueue.TierClaude])
	}
}

// TestMultipleWorkers: Three concurrent workers race for three tasks; no double-claims.
func TestMultipleWorkers(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)

	for i := range 3 {
		task := &taskqueue.Task{
			ID:          uuid.New().String(),
			IssueNumber: 50 + i,
			RepoOwner:   testOwner,
			RepoName:    testRepo,
			Title:       fmt.Sprintf("task %d", i),
			Tier:        taskqueue.TierGeminiPro,
			Status:      taskqueue.StatusPending,
			Metadata:    map[string]string{"task_type": "issue"},
		}
		if err := q.Enqueue(ctx, task); err != nil {
			t.Fatalf("Enqueue task %d: %v", i, err)
		}
	}

	type claimResult struct {
		workerID string
		task     *taskqueue.Task
		err      error
	}
	results := make(chan claimResult, 3)

	for i := range 3 {
		wID := fmt.Sprintf("worker-%d", i)
		go func(id string) {
			task, err := q.Dequeue(ctx, taskqueue.TierGeminiPro, id)
			results <- claimResult{workerID: id, task: task, err: err}
		}(wID)
	}

	claimed := map[string]string{} // taskID → workerID
	for range 3 {
		r := <-results
		if r.err != nil {
			t.Errorf("worker %s Dequeue error: %v", r.workerID, r.err)
			continue
		}
		if r.task == nil {
			t.Errorf("worker %s got nil task (no tasks available)", r.workerID)
			continue
		}
		if prev, ok := claimed[r.task.ID]; ok {
			t.Errorf("task %s double-claimed by %s and %s", r.task.ID, prev, r.workerID)
		}
		claimed[r.task.ID] = r.workerID
	}
	if len(claimed) != 3 {
		t.Errorf("expected 3 unique tasks claimed, got %d", len(claimed))
	}
}

// TestStalledTaskRecovery: Released task is re-claimable by a new worker.
func TestStalledTaskRecovery(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)

	task := &taskqueue.Task{
		ID:          uuid.New().String(),
		IssueNumber: 60,
		RepoOwner:   testOwner,
		RepoName:    testRepo,
		Title:       "implement new feature",
		Tier:        taskqueue.TierGeminiFlash,
		Status:      taskqueue.StatusPending,
		Metadata:    map[string]string{"task_type": "issue"},
	}
	if err := q.Enqueue(ctx, task); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	w1 := &mockWorker{id: "w1", tier: taskqueue.TierGeminiFlash, queue: q}
	claimed, err := w1.claim(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("w1 claim: %v", err)
	}

	// Supervisor detects stall and releases task.
	if err := q.Release(ctx, task.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}

	w2 := &mockWorker{id: "w2", tier: taskqueue.TierGeminiFlash, queue: q}
	reclaimed, err := w2.claim(ctx)
	if err != nil || reclaimed == nil {
		t.Fatalf("w2 could not reclaim stalled task: %v", err)
	}
	if reclaimed.ID != task.ID {
		t.Errorf("reclaimed task ID: want %s, got %s", task.ID, reclaimed.ID)
	}
	if reclaimed.WorkerID != "w2" {
		t.Errorf("reclaimed WorkerID: want w2, got %s", reclaimed.WorkerID)
	}
}

// TestTaskMetadata: New tasks carry task_type=issue, ReviewIteration=0.
func TestTaskMetadata(t *testing.T) {
	q := newQueue(t)
	client := newMockClient(newIssue(70, "add logging to service", "Need structured logging"))

	runOnePoll(t, q, client)

	ctx := context.Background()
	tasks, err := q.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.Metadata["task_type"] != "issue" {
		t.Errorf("task_type: want %q, got %q", "issue", task.Metadata["task_type"])
	}
	if task.ReviewIteration != 0 {
		t.Errorf("ReviewIteration: want 0, got %d", task.ReviewIteration)
	}
}

// TestFeedbackTaskInheritance: pr_feedback tasks inherit branch/PR/tier/complexity from parent.
func TestFeedbackTaskInheritance(t *testing.T) {
	ctx := context.Background()
	q := newQueue(t)

	// Parent issue task already in Review (worker created a PR).
	parentID := uuid.New().String()
	parent := &taskqueue.Task{
		ID:              parentID,
		IssueNumber:     80,
		RepoOwner:       testOwner,
		RepoName:        testRepo,
		Title:           "implement feature Y",
		Complexity:      taskqueue.ComplexityMedium,
		Tier:            taskqueue.TierGeminiPro,
		Status:          taskqueue.StatusReview,
		BranchName:      "feature/issue-80",
		PRNumber:        200,
		ReviewIteration: 0,
		Metadata:        map[string]string{"task_type": "issue"},
	}
	if err := q.Enqueue(ctx, parent); err != nil {
		t.Fatalf("Enqueue parent: %v", err)
	}

	// Supervisor creates a pr_feedback task inheriting from the parent.
	fixTask := &taskqueue.Task{
		ID:              uuid.New().String(),
		IssueNumber:     parent.IssueNumber,
		RepoOwner:       parent.RepoOwner,
		RepoName:        parent.RepoName,
		Title:           "Fix AI review feedback - " + parent.Title,
		Complexity:      parent.Complexity,
		Tier:            parent.Tier,
		Status:          taskqueue.StatusPending,
		BranchName:      parent.BranchName,
		PRNumber:        parent.PRNumber,
		ParentTaskID:    parent.ID,
		ReviewIteration: parent.ReviewIteration + 1,
		ReviewFeedback:  "Please add more unit tests covering edge cases",
		ReviewCommentID: 12345,
		Attempts:        0,
		Metadata:        map[string]string{"task_type": "pr_feedback"},
	}
	if err := q.Enqueue(ctx, fixTask); err != nil {
		t.Fatalf("Enqueue fixTask: %v", err)
	}

	got, err := q.Get(ctx, fixTask.ID)
	if err != nil {
		t.Fatalf("Get fixTask: %v", err)
	}
	if got.Metadata["task_type"] != "pr_feedback" {
		t.Errorf("task_type: want pr_feedback, got %s", got.Metadata["task_type"])
	}
	if got.ParentTaskID != parentID {
		t.Errorf("ParentTaskID: want %s, got %s", parentID, got.ParentTaskID)
	}
	if got.BranchName != parent.BranchName {
		t.Errorf("BranchName: want %s, got %s", parent.BranchName, got.BranchName)
	}
	if got.PRNumber != parent.PRNumber {
		t.Errorf("PRNumber: want %d, got %d", parent.PRNumber, got.PRNumber)
	}
	if got.Tier != parent.Tier {
		t.Errorf("Tier: want %v, got %v", parent.Tier, got.Tier)
	}
	if got.Complexity != parent.Complexity {
		t.Errorf("Complexity: want %v, got %v", parent.Complexity, got.Complexity)
	}
	if got.ReviewIteration != 1 {
		t.Errorf("ReviewIteration: want 1, got %d", got.ReviewIteration)
	}
}

// TestQueuePersistence: Tasks survive JSONQueue reconstruction (simulates process restart).
func TestQueuePersistence(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	q1, err := taskqueue.NewJSONQueue(dir)
	if err != nil {
		t.Fatalf("NewJSONQueue q1: %v", err)
	}
	taskID := uuid.New().String()
	task := &taskqueue.Task{
		ID:          taskID,
		IssueNumber: 90,
		RepoOwner:   testOwner,
		RepoName:    testRepo,
		Title:       "persist across restart",
		Tier:        taskqueue.TierClaude,
		Status:      taskqueue.StatusPending,
		Metadata:    map[string]string{"task_type": "issue"},
	}
	if err := q1.Enqueue(ctx, task); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Reconstruct queue from same directory (simulating process restart).
	q2, err := taskqueue.NewJSONQueue(dir)
	if err != nil {
		t.Fatalf("NewJSONQueue q2: %v", err)
	}
	got, err := q2.Get(ctx, taskID)
	if err != nil {
		t.Fatalf("Get from q2: %v", err)
	}
	if got.ID != taskID {
		t.Errorf("ID: want %s, got %s", taskID, got.ID)
	}
	if got.IssueNumber != 90 {
		t.Errorf("IssueNumber: want 90, got %d", got.IssueNumber)
	}
	if got.Status != taskqueue.StatusPending {
		t.Errorf("Status: want Pending, got %v", got.Status)
	}
}
