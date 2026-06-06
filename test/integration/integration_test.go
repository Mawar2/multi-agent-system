// Package integration_test exercises the full multi-agent orchestration pipeline
// without real GitHub API or Claude CLI. It uses a mockTicketClient (GitHub) and
// mockWorker (LLM + workspace + quality gates) defined locally, while the real
// JSONQueue, Supervisor, and RuleBasedRouter are used unchanged.
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
	"github.com/Mawar2/multi-agent-system/internal/worker"
)

// compile-time interface satisfaction checks
var _ ticket.Client = (*mockTicketClient)(nil)
var _ worker.Worker = (*mockWorker)(nil)

// ─── mockTicketClient ─────────────────────────────────────────────────────────

type mockTicketClient struct {
	mu         sync.Mutex
	issues     []*ticket.Issue
	prStatus   *ticket.PRStatus // returned by CheckPRStatus for every issue
	pollNotify chan struct{}     // fires once per FetchIssues call
}

func newMockClient(issues ...*ticket.Issue) *mockTicketClient {
	return &mockTicketClient{
		issues:     issues,
		pollNotify: make(chan struct{}, 1),
	}
}

func (m *mockTicketClient) FetchIssues(ctx context.Context, owner, repo string, labels []string) ([]*ticket.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case m.pollNotify <- struct{}{}:
	default:
	}
	out := make([]*ticket.Issue, len(m.issues))
	for i, iss := range m.issues {
		cp := *iss
		if cp.RepoOwner == "" {
			cp.RepoOwner = owner
		}
		if cp.RepoName == "" {
			cp.RepoName = repo
		}
		out[i] = &cp
	}
	return out, nil
}

func (m *mockTicketClient) GetIssue(ctx context.Context, owner, repo string, number int) (*ticket.Issue, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, iss := range m.issues {
		if iss.Number == number {
			cp := *iss
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("issue #%d not found", number)
}

func (m *mockTicketClient) ParseAcceptanceCriteria(body string) ([]string, error) {
	return []string{}, nil
}

func (m *mockTicketClient) CheckPRStatus(ctx context.Context, owner, repo string, issueNumber int) (*ticket.PRStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.prStatus, nil
}

// ─── mockWorker ───────────────────────────────────────────────────────────────

type mockWorker struct {
	id      string
	tier    taskqueue.Tier
	queue   taskqueue.TaskQueue
	execErr error

	mu         sync.Mutex
	claimedIDs []string
}

func newMockWorker(id string, tier taskqueue.Tier, q taskqueue.TaskQueue) *mockWorker {
	return &mockWorker{id: id, tier: tier, queue: q}
}

func (w *mockWorker) Claim(ctx context.Context) (*taskqueue.Task, error) {
	task, err := w.queue.Dequeue(ctx, w.tier, w.id)
	if err != nil {
		return nil, err
	}
	if task != nil {
		w.mu.Lock()
		w.claimedIDs = append(w.claimedIDs, task.ID)
		w.mu.Unlock()
	}
	return task, nil
}

func (w *mockWorker) Execute(ctx context.Context, task *taskqueue.Task) (*worker.Result, error) {
	if w.execErr != nil {
		task.Status = taskqueue.StatusFailed
		task.ErrorMsg = w.execErr.Error()
		task.CompletedAt = time.Now()
		_ = w.queue.Update(ctx, task)
		return &worker.Result{TaskID: task.ID, Success: false, ErrorMsg: w.execErr.Error()}, nil
	}

	task.Status = taskqueue.StatusInProgress
	task.StartedAt = time.Now()
	_ = w.queue.Update(ctx, task)

	branchName := fmt.Sprintf("feature/issue-%d-fix", task.IssueNumber)
	prNumber := 100 + task.IssueNumber

	task.Status = taskqueue.StatusReview
	task.BranchName = branchName
	task.PRNumber = prNumber
	task.CompletedAt = time.Now()
	_ = w.queue.Update(ctx, task)

	return &worker.Result{
		TaskID:     task.ID,
		Success:    true,
		BranchName: branchName,
		PRNumber:   prNumber,
	}, nil
}

func (w *mockWorker) Release(ctx context.Context, taskID string) error {
	return w.queue.Release(ctx, taskID)
}

func (w *mockWorker) Health(ctx context.Context) (*worker.HealthStatus, error) {
	return &worker.HealthStatus{WorkerID: w.id, Tier: w.tier, Healthy: true}, nil
}

func (w *mockWorker) ID() string           { return w.id }
func (w *mockWorker) Tier() taskqueue.Tier { return w.tier }

// ─── Helpers ──────────────────────────────────────────────────────────────────

func newQueue(t *testing.T) taskqueue.TaskQueue {
	t.Helper()
	q, err := taskqueue.NewJSONQueue(t.TempDir())
	if err != nil {
		t.Fatalf("NewJSONQueue: %v", err)
	}
	return q
}

func newSupervisor(q taskqueue.TaskQueue, mock *mockTicketClient) *orchestrator.Supervisor {
	cfg := &orchestrator.Config{
		Projects: []orchestrator.ProjectConfig{
			{Name: "test", RepoOwner: "TestOrg", RepoName: "test-repo"},
		},
		PollIntervalSeconds: 60,
		TaskTimeoutMinutes:  120,
		MaxRetryAttempts:    3,
		TaskQueueDir:        "./tasks",
	}
	return orchestrator.NewSupervisor(cfg, q, orchestrator.NewRuleBasedRouter(), mock)
}

// runPoll triggers the supervisor's automatic initial poll and waits until it
// has fully processed every issue before returning.
//
// The supervisor calls pollIssues synchronously before entering its ticker
// loop. We signal on mock.pollNotify (fired inside FetchIssues), then cancel
// the context. Because neither the mock methods nor JSONQueue check context
// cancellation, processIssue calls complete and tasks are persisted before
// Run() sees ctx.Done() and returns.
func runPoll(t *testing.T, sup *orchestrator.Supervisor, mock *mockTicketClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	done := make(chan error, 1)
	go func() { done <- sup.Run(ctx) }()

	select {
	case <-mock.pollNotify:
	case <-ctx.Done():
		t.Fatal("timeout: supervisor did not poll issues within 5s")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			t.Fatalf("supervisor.Run returned unexpected error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("supervisor did not stop within 3s after context cancel")
	}
}

func allTasks(t *testing.T, q taskqueue.TaskQueue) []*taskqueue.Task {
	t.Helper()
	tasks, err := q.List(context.Background(), nil)
	if err != nil {
		t.Fatalf("queue.List: %v", err)
	}
	return tasks
}

// ─── Tests ────────────────────────────────────────────────────────────────────

// TestHappyPath: Issue discovered → task queued → worker claims → task reaches
// Review status with BranchName and PRNumber set.
func TestHappyPath(t *testing.T) {
	issue := &ticket.Issue{Number: 47, Title: "fix typo in README", Body: "Fix the typo."}
	mock := newMockClient(issue)
	q := newQueue(t)
	sup := newSupervisor(q, mock)
	runPoll(t, sup, mock)

	tasks := allTasks(t, q)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after poll, got %d", len(tasks))
	}
	if tasks[0].Status != taskqueue.StatusPending {
		t.Errorf("status: want Pending, got %v", tasks[0].Status)
	}

	// "fix typo in README" → simple → TierGeminiFlash
	w := newMockWorker("flash-1", taskqueue.TierGeminiFlash, q)
	claimed, err := w.Claim(context.Background())
	if err != nil || claimed == nil {
		t.Fatalf("Claim: err=%v task=%v", err, claimed)
	}

	result, err := w.Execute(context.Background(), claimed)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.Success {
		t.Fatalf("Execute: expected success, got failure: %s", result.ErrorMsg)
	}

	final, err := q.Get(context.Background(), claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if final.Status != taskqueue.StatusReview {
		t.Errorf("final status: want Review, got %v", final.Status)
	}
	if final.BranchName == "" {
		t.Error("BranchName should be set after Execute")
	}
	if final.PRNumber == 0 {
		t.Error("PRNumber should be set after Execute")
	}
}

// TestWorkerFailure: Worker marks task Failed; ErrorMsg is stored on the task.
func TestWorkerFailure(t *testing.T) {
	// "broken task" has no routing signals → medium → TierGeminiPro
	issue := &ticket.Issue{Number: 10, Title: "broken task", Body: "It broke."}
	mock := newMockClient(issue)
	q := newQueue(t)
	sup := newSupervisor(q, mock)
	runPoll(t, sup, mock)

	w := newMockWorker("pro-1", taskqueue.TierGeminiPro, q)
	w.execErr = fmt.Errorf("build failed: exit status 1")

	claimed, err := w.Claim(context.Background())
	if err != nil || claimed == nil {
		t.Fatalf("Claim: err=%v task=%v", err, claimed)
	}

	result, _ := w.Execute(context.Background(), claimed)
	if result.Success {
		t.Fatal("expected Execute to report failure")
	}

	final, _ := q.Get(context.Background(), claimed.ID)
	if final.Status != taskqueue.StatusFailed {
		t.Errorf("status: want Failed, got %v", final.Status)
	}
	if final.ErrorMsg == "" {
		t.Error("ErrorMsg should be set on a failed task")
	}
}

// TestWorkerReleasedOnCrash: Worker calls Release → task returns to Pending
// with Attempts incremented, and is re-claimable by another worker.
func TestWorkerReleasedOnCrash(t *testing.T) {
	// "some task" → no routing signals → medium → TierGeminiPro
	issue := &ticket.Issue{Number: 20, Title: "some task"}
	mock := newMockClient(issue)
	q := newQueue(t)
	sup := newSupervisor(q, mock)
	runPoll(t, sup, mock)

	w1 := newMockWorker("pro-1", taskqueue.TierGeminiPro, q)
	claimed, err := w1.Claim(context.Background())
	if err != nil || claimed == nil {
		t.Fatalf("Claim: err=%v task=%v", err, claimed)
	}

	// Simulate crash: release without executing
	if err := w1.Release(context.Background(), claimed.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}

	after, _ := q.Get(context.Background(), claimed.ID)
	if after.Status != taskqueue.StatusPending {
		t.Errorf("after release: want Pending, got %v", after.Status)
	}
	if after.Attempts != 1 {
		t.Errorf("after release: want Attempts=1, got %d", after.Attempts)
	}

	// A new worker must be able to re-claim it
	w2 := newMockWorker("pro-2", taskqueue.TierGeminiPro, q)
	reclaimed, err := w2.Claim(context.Background())
	if err != nil || reclaimed == nil {
		t.Fatalf("re-Claim: err=%v task=%v", err, reclaimed)
	}
	if reclaimed.ID != claimed.ID {
		t.Errorf("expected same task %s to be re-claimed, got %s", claimed.ID, reclaimed.ID)
	}
}

// TestRouting verifies the RuleBasedRouter maps issue content to the correct
// complexity/tier for simple, medium, and complex issues.
func TestRouting(t *testing.T) {
	cases := []struct {
		name        string
		issue       *ticket.Issue
		wantTier    taskqueue.Tier
		wantComplex taskqueue.Complexity
	}{
		{
			name:        "simple",
			issue:       &ticket.Issue{Number: 1, Title: "fix typo in readme"},
			wantTier:    taskqueue.TierGeminiFlash,
			wantComplex: taskqueue.ComplexitySimple,
		},
		{
			name:        "medium",
			issue:       &ticket.Issue{Number: 2, Title: "Add user profile page"},
			wantTier:    taskqueue.TierGeminiPro,
			wantComplex: taskqueue.ComplexityMedium,
		},
		{
			name:        "complex",
			issue:       &ticket.Issue{Number: 3, Title: "Refactor database migration architecture"},
			wantTier:    taskqueue.TierClaude,
			wantComplex: taskqueue.ComplexityComplex,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := newMockClient(tc.issue)
			q := newQueue(t)
			sup := newSupervisor(q, mock)
			runPoll(t, sup, mock)

			tasks := allTasks(t, q)
			if len(tasks) != 1 {
				t.Fatalf("expected 1 task, got %d", len(tasks))
			}
			if tasks[0].Tier != tc.wantTier {
				t.Errorf("tier: want %v, got %v", tc.wantTier, tasks[0].Tier)
			}
			if tasks[0].Complexity != tc.wantComplex {
				t.Errorf("complexity: want %v, got %v", tc.wantComplex, tasks[0].Complexity)
			}
		})
	}
}

// TestRoutingByLabel verifies that issue labels override title-based heuristics
// to route issues to the correct tier.
func TestRoutingByLabel(t *testing.T) {
	cases := []struct {
		name        string
		labels      []string
		wantTier    taskqueue.Tier
		wantComplex taskqueue.Complexity
	}{
		{
			name:        "simple",
			labels:      []string{"easy"},
			wantTier:    taskqueue.TierGeminiFlash,
			wantComplex: taskqueue.ComplexitySimple,
		},
		{
			name:        "complex",
			labels:      []string{"complex"},
			wantTier:    taskqueue.TierClaude,
			wantComplex: taskqueue.ComplexityComplex,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issue := &ticket.Issue{
				Number: 5,
				Title:  "Generic task title", // no title-based routing signals
				Labels: tc.labels,
			}
			mock := newMockClient(issue)
			q := newQueue(t)
			sup := newSupervisor(q, mock)
			runPoll(t, sup, mock)

			tasks := allTasks(t, q)
			if len(tasks) != 1 {
				t.Fatalf("expected 1 task, got %d", len(tasks))
			}
			if tasks[0].Tier != tc.wantTier {
				t.Errorf("tier: want %v, got %v", tc.wantTier, tasks[0].Tier)
			}
			if tasks[0].Complexity != tc.wantComplex {
				t.Errorf("complexity: want %v, got %v", tc.wantComplex, tasks[0].Complexity)
			}
		})
	}
}

// TestDuplicateDetection_AlreadyQueued: polling the same issue twice produces
// only one task in the queue.
func TestDuplicateDetection_AlreadyQueued(t *testing.T) {
	issue := &ticket.Issue{
		Number: 47, Title: "fix typo",
		RepoOwner: "TestOrg", RepoName: "test-repo",
	}
	mock := newMockClient(issue)
	q := newQueue(t)

	// Pre-enqueue a task for this issue so the supervisor sees it as "already queued"
	pre := &taskqueue.Task{
		ID:          "pre-existing-task",
		IssueNumber: 47,
		RepoOwner:   "TestOrg",
		RepoName:    "test-repo",
		Title:       "fix typo",
		Status:      taskqueue.StatusPending,
		Tier:        taskqueue.TierGeminiFlash,
		Metadata:    map[string]string{"task_type": "issue"},
	}
	if err := q.Enqueue(context.Background(), pre); err != nil {
		t.Fatalf("pre-enqueue: %v", err)
	}

	sup := newSupervisor(q, mock)
	runPoll(t, sup, mock)

	tasks := allTasks(t, q)
	if len(tasks) != 1 {
		t.Errorf("expected 1 task (no duplicate), got %d", len(tasks))
	}
}

// TestDuplicateDetection_ExistingPR: issues with an open PR are skipped
// entirely; no task is created.
func TestDuplicateDetection_ExistingPR(t *testing.T) {
	issue := &ticket.Issue{Number: 47, Title: "fix typo"}
	mock := newMockClient(issue)
	mock.prStatus = &ticket.PRStatus{Number: 99, State: "open"}

	q := newQueue(t)
	sup := newSupervisor(q, mock)
	runPoll(t, sup, mock)

	tasks := allTasks(t, q)
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks (issue already has open PR), got %d", len(tasks))
	}
}

// TestMultipleIssues: a single poll with three issues at different complexity
// levels produces three tasks each routed to the correct tier.
func TestMultipleIssues(t *testing.T) {
	issues := []*ticket.Issue{
		{Number: 1, Title: "fix typo in readme"},                      // simple → Flash
		{Number: 2, Title: "Add user profile page"},                   // medium → Pro
		{Number: 3, Title: "Refactor database migration architecture"}, // complex → Claude
	}
	mock := newMockClient(issues...)
	q := newQueue(t)
	sup := newSupervisor(q, mock)
	runPoll(t, sup, mock)

	tasks := allTasks(t, q)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	tierByIssue := make(map[int]taskqueue.Tier)
	for _, task := range tasks {
		tierByIssue[task.IssueNumber] = task.Tier
	}
	if tierByIssue[1] != taskqueue.TierGeminiFlash {
		t.Errorf("issue 1: want GeminiFlash, got %v", tierByIssue[1])
	}
	if tierByIssue[2] != taskqueue.TierGeminiPro {
		t.Errorf("issue 2: want GeminiPro, got %v", tierByIssue[2])
	}
	if tierByIssue[3] != taskqueue.TierClaude {
		t.Errorf("issue 3: want Claude, got %v", tierByIssue[3])
	}
}

// TestMultipleWorkers: three concurrent workers racing for three tasks produce
// no double-claims — every task is claimed by exactly one worker.
func TestMultipleWorkers(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		task := &taskqueue.Task{
			ID:          fmt.Sprintf("task-%d", i),
			IssueNumber: i,
			RepoOwner:   "TestOrg",
			RepoName:    "test-repo",
			Title:       fmt.Sprintf("Task %d", i),
			Status:      taskqueue.StatusPending,
			Tier:        taskqueue.TierGeminiPro,
			Complexity:  taskqueue.ComplexityMedium,
			Metadata:    map[string]string{"task_type": "issue"},
		}
		if err := q.Enqueue(ctx, task); err != nil {
			t.Fatalf("enqueue task-%d: %v", i, err)
		}
	}

	workers := []*mockWorker{
		newMockWorker("pro-1", taskqueue.TierGeminiPro, q),
		newMockWorker("pro-2", taskqueue.TierGeminiPro, q),
		newMockWorker("pro-3", taskqueue.TierGeminiPro, q),
	}

	var wg sync.WaitGroup
	for _, w := range workers {
		wg.Add(1)
		go func(w *mockWorker) {
			defer wg.Done()
			task, err := w.Claim(ctx)
			if err != nil {
				t.Errorf("Claim error: %v", err)
				return
			}
			if task == nil {
				return
			}
			_, _ = w.Execute(ctx, task)
		}(w)
	}
	wg.Wait()

	// Each task ID should appear in exactly one worker's claimedIDs
	claimCount := make(map[string]int) // taskID → number of claimers
	for _, w := range workers {
		w.mu.Lock()
		for _, id := range w.claimedIDs {
			claimCount[id]++
		}
		w.mu.Unlock()
	}
	for id, count := range claimCount {
		if count > 1 {
			t.Errorf("task %s was claimed %d times (double-claim detected)", id, count)
		}
	}

	totalClaimed := 0
	for _, c := range claimCount {
		totalClaimed += c
	}
	if totalClaimed != 3 {
		t.Errorf("expected 3 total claims across all workers, got %d", totalClaimed)
	}
}

// TestStalledTaskRecovery: a released task is re-claimable by a new worker
// and can be completed successfully.
func TestStalledTaskRecovery(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	task := &taskqueue.Task{
		ID:          "stalled-task",
		IssueNumber: 99,
		RepoOwner:   "TestOrg",
		RepoName:    "test-repo",
		Title:       "Stalled task",
		Status:      taskqueue.StatusPending,
		Tier:        taskqueue.TierGeminiPro,
		Complexity:  taskqueue.ComplexityMedium,
		Metadata:    map[string]string{"task_type": "issue"},
	}
	if err := q.Enqueue(ctx, task); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Worker 1 claims but then crashes (releases)
	w1 := newMockWorker("pro-1", taskqueue.TierGeminiPro, q)
	claimed, err := w1.Claim(ctx)
	if err != nil || claimed == nil {
		t.Fatalf("Claim: err=%v task=%v", err, claimed)
	}
	if err := w1.Release(ctx, claimed.ID); err != nil {
		t.Fatalf("Release: %v", err)
	}

	after, _ := q.Get(ctx, claimed.ID)
	if after.Status != taskqueue.StatusPending {
		t.Errorf("after release: want Pending, got %v", after.Status)
	}
	if after.Attempts != 1 {
		t.Errorf("after release: want Attempts=1, got %d", after.Attempts)
	}

	// Worker 2 picks up the released task and completes it
	w2 := newMockWorker("pro-2", taskqueue.TierGeminiPro, q)
	reclaimed, err := w2.Claim(ctx)
	if err != nil || reclaimed == nil {
		t.Fatalf("re-Claim: err=%v task=%v", err, reclaimed)
	}

	result, err := w2.Execute(ctx, reclaimed)
	if err != nil || !result.Success {
		t.Fatalf("Execute: err=%v success=%v", err, result.Success)
	}

	final, _ := q.Get(ctx, claimed.ID)
	if final.Status != taskqueue.StatusReview {
		t.Errorf("final status: want Review, got %v", final.Status)
	}
}

// TestTaskMetadata: tasks created from issues carry task_type="issue" and
// ReviewIteration=0 in their metadata.
func TestTaskMetadata(t *testing.T) {
	issue := &ticket.Issue{Number: 7, Title: "Add logging to service"}
	mock := newMockClient(issue)
	q := newQueue(t)
	sup := newSupervisor(q, mock)
	runPoll(t, sup, mock)

	tasks := allTasks(t, q)
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
	if task.IssueNumber != 7 {
		t.Errorf("IssueNumber: want 7, got %d", task.IssueNumber)
	}
}

// TestFeedbackTaskInheritance: pr_feedback tasks inherit BranchName, PRNumber,
// Tier, and Complexity from the parent issue task; ReviewIteration is parent+1.
func TestFeedbackTaskInheritance(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()

	// Simulate a completed issue task that produced a PR
	parentTask := &taskqueue.Task{
		ID:              "parent-task-id",
		IssueNumber:     42,
		RepoOwner:       "TestOrg",
		RepoName:        "test-repo",
		Title:           "Implement authentication",
		Complexity:      taskqueue.ComplexityComplex,
		Tier:            taskqueue.TierClaude,
		Status:          taskqueue.StatusReview,
		BranchName:      "feature/issue-42-implement-authentication",
		PRNumber:        123,
		ReviewIteration: 0,
		Metadata:        map[string]string{"task_type": "issue"},
	}
	if err := q.Enqueue(ctx, parentTask); err != nil {
		t.Fatalf("enqueue parent: %v", err)
	}

	// Construct the fix task exactly as supervisor.createFixTask does
	fixTask := &taskqueue.Task{
		ID:              "fix-task-id",
		IssueNumber:     parentTask.IssueNumber,
		RepoOwner:       parentTask.RepoOwner,
		RepoName:        parentTask.RepoName,
		Title:           fmt.Sprintf("Fix AI review feedback - %s", parentTask.Title),
		Complexity:      parentTask.Complexity,
		Tier:            parentTask.Tier,
		Status:          taskqueue.StatusPending,
		BranchName:      parentTask.BranchName,
		PRNumber:        parentTask.PRNumber,
		ParentTaskID:    parentTask.ID,
		ReviewIteration: parentTask.ReviewIteration + 1,
		ReviewFeedback:  "Please add error handling to the login function",
		ReviewCommentID: 9001,
		Metadata:        map[string]string{"task_type": "pr_feedback"},
	}
	if err := q.Enqueue(ctx, fixTask); err != nil {
		t.Fatalf("enqueue fix task: %v", err)
	}

	// Retrieve and verify inherited fields survive queue round-trip
	retrieved, err := q.Get(ctx, fixTask.ID)
	if err != nil {
		t.Fatalf("Get fix task: %v", err)
	}

	if retrieved.Tier != parentTask.Tier {
		t.Errorf("Tier: want %v, got %v", parentTask.Tier, retrieved.Tier)
	}
	if retrieved.Complexity != parentTask.Complexity {
		t.Errorf("Complexity: want %v, got %v", parentTask.Complexity, retrieved.Complexity)
	}
	if retrieved.BranchName != parentTask.BranchName {
		t.Errorf("BranchName: want %q, got %q", parentTask.BranchName, retrieved.BranchName)
	}
	if retrieved.PRNumber != parentTask.PRNumber {
		t.Errorf("PRNumber: want %d, got %d", parentTask.PRNumber, retrieved.PRNumber)
	}
	if retrieved.ReviewIteration != 1 {
		t.Errorf("ReviewIteration: want 1, got %d", retrieved.ReviewIteration)
	}
	if retrieved.ParentTaskID != parentTask.ID {
		t.Errorf("ParentTaskID: want %q, got %q", parentTask.ID, retrieved.ParentTaskID)
	}
	if retrieved.Metadata["task_type"] != "pr_feedback" {
		t.Errorf("task_type: want %q, got %q", "pr_feedback", retrieved.Metadata["task_type"])
	}
}

// TestQueuePersistence: tasks survive JSONQueue reconstruction, simulating a
// process restart with the same task directory.
func TestQueuePersistence(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	// First queue instance: enqueue two tasks
	q1, err := taskqueue.NewJSONQueue(dir)
	if err != nil {
		t.Fatalf("NewJSONQueue (first): %v", err)
	}
	for i := 1; i <= 2; i++ {
		task := &taskqueue.Task{
			ID:          fmt.Sprintf("persist-task-%d", i),
			IssueNumber: i,
			RepoOwner:   "TestOrg",
			RepoName:    "test-repo",
			Title:       fmt.Sprintf("Persisted task %d", i),
			Status:      taskqueue.StatusPending,
			Tier:        taskqueue.TierGeminiPro,
			Metadata:    map[string]string{"task_type": "issue"},
		}
		if err := q1.Enqueue(ctx, task); err != nil {
			t.Fatalf("enqueue task %d: %v", i, err)
		}
	}

	// Second queue instance pointing to the same directory (simulates restart)
	q2, err := taskqueue.NewJSONQueue(dir)
	if err != nil {
		t.Fatalf("NewJSONQueue (second): %v", err)
	}

	tasks, err := q2.List(ctx, nil)
	if err != nil {
		t.Fatalf("List after restart: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks after restart, got %d", len(tasks))
	}

	ids := make(map[string]bool)
	for _, task := range tasks {
		ids[task.ID] = true
	}
	if !ids["persist-task-1"] {
		t.Error("persist-task-1 missing after restart")
	}
	if !ids["persist-task-2"] {
		t.Error("persist-task-2 missing after restart")
	}
}
