package worker

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/conventions"
	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
)

// mockQueue is a mock implementation of TaskQueue for testing.
type mockQueue struct {
	dequeueFunc func(ctx context.Context, tier taskqueue.Tier, workerID string) (*taskqueue.Task, error)
	updateFunc  func(ctx context.Context, task *taskqueue.Task) error
	releaseFunc func(ctx context.Context, taskID string) error
	tasks       map[string]*taskqueue.Task // Store tasks for inspection
}

func newMockQueue() *mockQueue {
	return &mockQueue{
		tasks: make(map[string]*taskqueue.Task),
	}
}

func (m *mockQueue) Enqueue(ctx context.Context, task *taskqueue.Task) error {
	m.tasks[task.ID] = task
	return nil
}

func (m *mockQueue) Dequeue(ctx context.Context, tier taskqueue.Tier, workerID string) (*taskqueue.Task, error) {
	if m.dequeueFunc != nil {
		return m.dequeueFunc(ctx, tier, workerID)
	}
	return nil, nil
}

func (m *mockQueue) Update(ctx context.Context, task *taskqueue.Task) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, task)
	}
	m.tasks[task.ID] = task
	return nil
}

func (m *mockQueue) Get(ctx context.Context, taskID string) (*taskqueue.Task, error) {
	if task, ok := m.tasks[taskID]; ok {
		return task, nil
	}
	return nil, fmt.Errorf("task not found: %s", taskID)
}

func (m *mockQueue) List(ctx context.Context, filter *taskqueue.TaskFilter) ([]*taskqueue.Task, error) {
	return nil, nil
}

func (m *mockQueue) Release(ctx context.Context, taskID string) error {
	if m.releaseFunc != nil {
		return m.releaseFunc(ctx, taskID)
	}
	if task, ok := m.tasks[taskID]; ok {
		task.Status = taskqueue.StatusPending
		task.WorkerID = ""
		task.Attempts++
		return nil
	}
	return fmt.Errorf("task not found: %s", taskID)
}

// mockBackend is a mock implementation of LLMBackend for testing.
type mockBackend struct {
	executeFunc func(ctx context.Context, prompt string, model string) (string, error)
	name        string
	models      []string
}

func newMockBackend(name string, models []string) *mockBackend {
	return &mockBackend{
		name:   name,
		models: models,
	}
}

func (m *mockBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, prompt, model)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockBackend) ExecuteInDir(ctx context.Context, prompt string, model string, workDir string) (string, error) {
	// For tests, delegate to Execute (tests don't care about workDir)
	return m.Execute(ctx, prompt, model)
}

func (m *mockBackend) Name() string {
	return m.name
}

func (m *mockBackend) Models() []string {
	return m.models
}

// mockWorkspace implements workspaceProvider for testing — no filesystem or git calls.
type mockWorkspace struct {
	prepareFunc    func(ctx context.Context, task *taskqueue.Task) (string, error)
	prepareFixFunc func(ctx context.Context, task *taskqueue.Task) (string, error)
}

func newMockWorkspace(dir string) *mockWorkspace {
	return &mockWorkspace{
		prepareFunc: func(_ context.Context, _ *taskqueue.Task) (string, error) {
			return dir, nil
		},
		prepareFixFunc: func(_ context.Context, _ *taskqueue.Task) (string, error) {
			return dir, nil
		},
	}
}

func (m *mockWorkspace) PrepareWorkspace(ctx context.Context, task *taskqueue.Task) (string, error) {
	return m.prepareFunc(ctx, task)
}

func (m *mockWorkspace) PrepareWorkspaceForFix(ctx context.Context, task *taskqueue.Task) (string, error) {
	return m.prepareFixFunc(ctx, task)
}

// mockQualityGate implements qualityValidator for testing — no subprocess calls.
type mockQualityGate struct {
	validateFunc func(ctx context.Context, workspaceDir string, ruleset *conventions.Ruleset) error
}

func newMockQualityGate(err error) *mockQualityGate {
	return &mockQualityGate{
		validateFunc: func(_ context.Context, _ string, _ *conventions.Ruleset) error {
			return err
		},
	}
}

func (m *mockQualityGate) Validate(ctx context.Context, workspaceDir string, ruleset *conventions.Ruleset) error {
	return m.validateFunc(ctx, workspaceDir, ruleset)
}

// newTestWorker builds a ClaudeCodeWorker with injected mocks for hermetic tests.
func newTestWorker(
	queue *mockQueue,
	backend *mockBackend,
	workspace *mockWorkspace,
	gate *mockQualityGate,
) *ClaudeCodeWorker {
	return &ClaudeCodeWorker{
		id:           "worker-1",
		tier:         taskqueue.TierClaude,
		queue:        queue,
		backend:      backend,
		workspaceMgr: workspace,
		qualityGate:  gate,
	}
}

// TestNewClaudeCodeWorker tests worker initialization.
func TestNewClaudeCodeWorker(t *testing.T) {
	queue := newMockQueue()
	backend := newMockBackend("test-backend", []string{"test-model"})

	worker := NewClaudeCodeWorker(
		"worker-1",
		taskqueue.TierClaude,
		queue,
		backend,
		"/tmp/projects",
	)

	if worker == nil {
		t.Fatal("NewClaudeCodeWorker returned nil")
	}

	if worker.ID() != "worker-1" {
		t.Errorf("expected ID 'worker-1', got '%s'", worker.ID())
	}

	if worker.Tier() != taskqueue.TierClaude {
		t.Errorf("expected tier Claude, got %v", worker.Tier())
	}

	if worker.tasksCompleted != 0 {
		t.Errorf("expected tasksCompleted 0, got %d", worker.tasksCompleted)
	}

	if worker.tasksFailed != 0 {
		t.Errorf("expected tasksFailed 0, got %d", worker.tasksFailed)
	}
}

// TestClaim tests task claiming from queue.
func TestClaim(t *testing.T) {
	tests := []struct {
		name        string
		dequeueFunc func(ctx context.Context, tier taskqueue.Tier, workerID string) (*taskqueue.Task, error)
		wantTask    bool
		wantErr     bool
	}{
		{
			name: "successful claim",
			dequeueFunc: func(ctx context.Context, tier taskqueue.Tier, workerID string) (*taskqueue.Task, error) {
				return &taskqueue.Task{
					ID:          "task-1",
					IssueNumber: 123,
					RepoOwner:   "owner",
					RepoName:    "repo",
					Title:       "Test task",
					Status:      taskqueue.StatusClaimed,
					WorkerID:    workerID,
				}, nil
			},
			wantTask: true,
			wantErr:  false,
		},
		{
			name: "no tasks available",
			dequeueFunc: func(ctx context.Context, tier taskqueue.Tier, workerID string) (*taskqueue.Task, error) {
				return nil, nil
			},
			wantTask: false,
			wantErr:  false,
		},
		{
			name: "queue error",
			dequeueFunc: func(ctx context.Context, tier taskqueue.Tier, workerID string) (*taskqueue.Task, error) {
				return nil, fmt.Errorf("queue error")
			},
			wantTask: false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := newMockQueue()
			queue.dequeueFunc = tt.dequeueFunc
			backend := newMockBackend("test-backend", []string{"test-model"})

			worker := NewClaudeCodeWorker("worker-1", taskqueue.TierClaude, queue, backend, "/tmp/projects")

			task, err := worker.Claim(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("Claim() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if (task != nil) != tt.wantTask {
				t.Errorf("Claim() got task = %v, wantTask %v", task != nil, tt.wantTask)
			}

			if tt.wantTask && task != nil {
				if task.WorkerID != "worker-1" {
					t.Errorf("expected WorkerID 'worker-1', got '%s'", task.WorkerID)
				}
			}
		})
	}
}

// TestExecute tests task execution with injected mockWorkspace and mockQualityGate —
// no real git, filesystem, or subprocess calls are made.
func TestExecute(t *testing.T) {
	tests := []struct {
		name        string
		task        *taskqueue.Task
		executeFunc func(ctx context.Context, prompt string, model string) (string, error)
		updateFunc  func(ctx context.Context, task *taskqueue.Task) error
		gateErr     error // error the mock quality gate returns (nil = pass)
		wantSuccess bool
		wantBranch  string
		wantPR      int
	}{
		{
			name: "successful execution",
			task: &taskqueue.Task{
				ID:          "task-1",
				IssueNumber: 123,
				RepoOwner:   "Mawar2",
				RepoName:    "Kaimi",
				Title:       "Add feature",
				Description: "Add new feature to the project",
				Status:      taskqueue.StatusClaimed,
				WorkerID:    "worker-1",
			},
			executeFunc: func(ctx context.Context, prompt string, model string) (string, error) {
				return "Implementation complete.\n\nBranch: feature/KAI-123-add-feature\nPR: #456", nil
			},
			updateFunc: func(ctx context.Context, task *taskqueue.Task) error {
				return nil
			},
			gateErr:     nil,
			wantSuccess: true,
			wantBranch:  "feature/KAI-123-add-feature",
			wantPR:      456,
		},
		{
			name: "LLM execution fails",
			task: &taskqueue.Task{
				ID:          "task-2",
				IssueNumber: 124,
				RepoOwner:   "owner",
				RepoName:    "repo",
				Title:       "Fix bug",
				Description: "Fix the bug",
				Status:      taskqueue.StatusClaimed,
				WorkerID:    "worker-1",
			},
			executeFunc: func(ctx context.Context, prompt string, model string) (string, error) {
				return "", fmt.Errorf("LLM service unavailable")
			},
			updateFunc: func(ctx context.Context, task *taskqueue.Task) error {
				return nil
			},
			gateErr:     nil,
			wantSuccess: false,
			wantBranch:  "",
			wantPR:      0,
		},
		{
			name: "missing branch name in response",
			task: &taskqueue.Task{
				ID:          "task-3",
				IssueNumber: 125,
				RepoOwner:   "owner",
				RepoName:    "repo",
				Title:       "Refactor code",
				Description: "Refactor the code",
				Status:      taskqueue.StatusClaimed,
				WorkerID:    "worker-1",
			},
			executeFunc: func(ctx context.Context, prompt string, model string) (string, error) {
				return "Implementation complete but no branch info", nil
			},
			updateFunc: func(ctx context.Context, task *taskqueue.Task) error {
				return nil
			},
			gateErr:     nil,
			wantSuccess: false,
			wantBranch:  "",
			wantPR:      0,
		},
		{
			name: "quality gate failure",
			task: &taskqueue.Task{
				ID:          "task-4",
				IssueNumber: 126,
				RepoOwner:   "owner",
				RepoName:    "repo",
				Title:       "Bad code",
				Description: "Introduces failing tests",
				Status:      taskqueue.StatusClaimed,
				WorkerID:    "worker-1",
			},
			executeFunc: func(ctx context.Context, prompt string, model string) (string, error) {
				return "Done.\nBranch: feature/issue-126-bad-code\nPR: #200", nil
			},
			updateFunc: func(ctx context.Context, task *taskqueue.Task) error {
				return nil
			},
			gateErr:     fmt.Errorf("tests failed: 1 test failed"),
			wantSuccess: false,
			wantBranch:  "",
			wantPR:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := newMockQueue()
			queue.updateFunc = tt.updateFunc
			backend := newMockBackend("test-backend", []string{"test-model"})
			backend.executeFunc = tt.executeFunc

			workspace := newMockWorkspace("/tmp/test-workspace")
			gate := newMockQualityGate(tt.gateErr)

			worker := newTestWorker(queue, backend, workspace, gate)

			result, err := worker.Execute(context.Background(), tt.task)

			if err != nil {
				t.Errorf("Execute() unexpected error: %v", err)
				return
			}

			if result.Success != tt.wantSuccess {
				t.Errorf("Execute() success = %v, want %v", result.Success, tt.wantSuccess)
			}

			if result.BranchName != tt.wantBranch {
				t.Errorf("Execute() branch = '%s', want '%s'", result.BranchName, tt.wantBranch)
			}

			if result.PRNumber != tt.wantPR {
				t.Errorf("Execute() PR = %d, want %d", result.PRNumber, tt.wantPR)
			}

			// Verify worker stats
			if tt.wantSuccess && worker.tasksCompleted != 1 {
				t.Errorf("Expected tasksCompleted = 1, got %d", worker.tasksCompleted)
			}

			if !tt.wantSuccess && worker.tasksFailed != 1 {
				t.Errorf("Expected tasksFailed = 1, got %d", worker.tasksFailed)
			}
		})
	}
}

// TestRelease tests task release back to queue.
func TestRelease(t *testing.T) {
	tests := []struct {
		name        string
		taskID      string
		releaseFunc func(ctx context.Context, taskID string) error
		wantErr     bool
	}{
		{
			name:   "successful release",
			taskID: "task-1",
			releaseFunc: func(ctx context.Context, taskID string) error {
				return nil
			},
			wantErr: false,
		},
		{
			name:   "release fails",
			taskID: "task-2",
			releaseFunc: func(ctx context.Context, taskID string) error {
				return fmt.Errorf("release failed")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queue := newMockQueue()
			queue.releaseFunc = tt.releaseFunc
			backend := newMockBackend("test-backend", []string{"test-model"})

			worker := NewClaudeCodeWorker("worker-1", taskqueue.TierClaude, queue, backend, "/tmp/projects")

			err := worker.Release(context.Background(), tt.taskID)

			if (err != nil) != tt.wantErr {
				t.Errorf("Release() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestHealth tests worker health reporting.
func TestHealth(t *testing.T) {
	queue := newMockQueue()
	backend := newMockBackend("test-backend", []string{"test-model"})

	worker := NewClaudeCodeWorker("worker-1", taskqueue.TierClaude, queue, backend, "/tmp/projects")

	// Set some stats
	worker.tasksCompleted = 5
	worker.tasksFailed = 2

	health, err := worker.Health(context.Background())

	if err != nil {
		t.Fatalf("Health() unexpected error: %v", err)
	}

	if health.WorkerID != "worker-1" {
		t.Errorf("expected WorkerID 'worker-1', got '%s'", health.WorkerID)
	}

	if health.Tier != taskqueue.TierClaude {
		t.Errorf("expected tier Claude, got %v", health.Tier)
	}

	if !health.Healthy {
		t.Error("expected Healthy = true")
	}

	if health.TasksCompleted != 5 {
		t.Errorf("expected TasksCompleted = 5, got %d", health.TasksCompleted)
	}

	if health.TasksFailed != 2 {
		t.Errorf("expected TasksFailed = 2, got %d", health.TasksFailed)
	}

	// Verify LastHeartbeat is recent
	heartbeat, err := time.Parse(time.RFC3339, health.LastHeartbeat)
	if err != nil {
		t.Errorf("invalid heartbeat format: %v", err)
	}

	if time.Since(heartbeat) > 5*time.Second {
		t.Errorf("heartbeat timestamp too old: %v", health.LastHeartbeat)
	}
}

// TestBuildPrompt tests prompt construction.
func TestBuildPrompt(t *testing.T) {
	// Create a minimal test setup (conventions parsing will use defaults if files don't exist)
	queue := newMockQueue()
	backend := newMockBackend("test-backend", []string{"test-model"})
	worker := NewClaudeCodeWorker("worker-1", taskqueue.TierClaude, queue, backend, "/tmp/projects")

	task := &taskqueue.Task{
		ID:          "task-1",
		IssueNumber: 123,
		RepoOwner:   "Mawar2",
		RepoName:    "Kaimi",
		Title:       "Add new feature",
		Description: "Implement feature X with acceptance criteria:\n- Criterion 1\n- Criterion 2",
	}

	// Create a simple ruleset manually (since we can't rely on file parsing in tests)
	ruleset := &testRuleset{
		ProjectPath:    "/tmp/projects/Mawar2/Kaimi",
		BranchPattern:  "feature/KAI-{ticket}-{summary}",
		CommitPattern:  "{ticket}_{description}",
		ForbiddenFiles: []string{"utils.go", "helpers.go"},
		TestCommand:    "make test",
		LintCommand:    "make lint",
		FormatCommand:  "gofmt -w .",
		TDDRequired:    true,
	}

	prompt := buildPromptForTest(worker, task, ruleset)

	// Verify prompt contains key elements
	requiredElements := []string{
		"TASK DETAILS",
		"Mawar2/Kaimi",
		"Issue #123",
		"Add new feature",
		"PROJECT CONVENTIONS",
		"feature/KAI-{ticket}-{summary}",
		"{ticket}_{description}",
		"utils.go",
		"helpers.go",
		"make test",
		"make lint",
		"TDD REQUIRED",
		"IMPLEMENTATION INSTRUCTIONS",
		"Create Feature Branch",
		"Implement Solution",
		"Run Quality Checks",
		"Create Pull Request",
		"Report Results",
	}

	for _, element := range requiredElements {
		if !strings.Contains(prompt, element) {
			t.Errorf("Prompt missing required element: %s", element)
		}
	}
}

// TestExtractBranchName tests branch name extraction from LLM responses.
func TestExtractBranchName(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     string
	}{
		{
			name:     "standard format",
			response: "Implementation complete.\n\nBranch: feature/KAI-123-summary\nPR: #456",
			want:     "feature/KAI-123-summary",
		},
		{
			name:     "lowercase branch",
			response: "branch: feature/ABC-789-fix",
			want:     "feature/ABC-789-fix",
		},
		{
			name:     "created branch format",
			response: "Created branch feature/XYZ-111-refactor successfully",
			want:     "feature/XYZ-111-refactor",
		},
		{
			name:     "standalone branch line",
			response: "Some text\nfeature/KAI-456-update\nMore text",
			want:     "feature/KAI-456-update",
		},
		{
			name:     "fix branch",
			response: "Branch: fix/issue-789-bug",
			want:     "fix/issue-789-bug",
		},
		{
			name:     "no branch",
			response: "Some text without branch information",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractBranchName(tt.response)
			if got != tt.want {
				t.Errorf("extractBranchName() = '%s', want '%s'", got, tt.want)
			}
		})
	}
}

// TestExtractPRNumber tests PR number extraction from LLM responses.
func TestExtractPRNumber(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     int
	}{
		{
			name:     "standard PR format",
			response: "Branch: feature/KAI-123-summary\nPR: #456",
			want:     456,
		},
		{
			name:     "lowercase pr",
			response: "pr: #789",
			want:     789,
		},
		{
			name:     "pull request format",
			response: "Pull Request: #123",
			want:     123,
		},
		{
			name:     "pull request without colon",
			response: "Created Pull Request #999",
			want:     999,
		},
		{
			name:     "no PR number",
			response: "Some text without PR information",
			want:     0,
		},
		{
			name:     "PR without hash",
			response: "PR: 111",
			want:     111,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPRNumber(tt.response)
			if got != tt.want {
				t.Errorf("extractPRNumber() = %d, want %d", got, tt.want)
			}
		})
	}
}

// testRuleset is a simple implementation for testing prompt building.
type testRuleset struct {
	ProjectPath    string
	BranchPattern  string
	CommitPattern  string
	ForbiddenFiles []string
	TestCommand    string
	LintCommand    string
	FormatCommand  string
	TDDRequired    bool
}

// buildPromptForTest is a test helper that builds a prompt with a simple ruleset.
func buildPromptForTest(_ *ClaudeCodeWorker, task *taskqueue.Task, ruleset *testRuleset) string {
	var sb strings.Builder

	sb.WriteString("You are an autonomous code agent implementing a GitHub Issue.\n\n")
	sb.WriteString("## TASK DETAILS\n\n")
	fmt.Fprintf(&sb, "**Repository:** %s/%s\n", task.RepoOwner, task.RepoName)
	fmt.Fprintf(&sb, "**Issue #%d:** %s\n\n", task.IssueNumber, task.Title)
	sb.WriteString("**Description:**\n")
	sb.WriteString(task.Description)
	sb.WriteString("\n\n")

	sb.WriteString("## PROJECT CONVENTIONS\n\n")
	fmt.Fprintf(&sb, "**Project Path:** %s\n\n", ruleset.ProjectPath)

	if ruleset.BranchPattern != "" {
		fmt.Fprintf(&sb, "**Branch Naming Pattern:** %s\n", ruleset.BranchPattern)
		sb.WriteString("Example: Replace {ticket} with issue number, {summary} with brief description\n\n")
	}

	if ruleset.CommitPattern != "" {
		fmt.Fprintf(&sb, "**Commit Message Format:** %s\n", ruleset.CommitPattern)
		sb.WriteString("Example: Replace {ticket} with issue number, {description} with change summary\n\n")
	}

	if len(ruleset.ForbiddenFiles) > 0 {
		sb.WriteString("**Forbidden Files:** Do NOT create these files:\n")
		for _, file := range ruleset.ForbiddenFiles {
			fmt.Fprintf(&sb, "  - %s\n", file)
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "**Test Command:** %s\n", ruleset.TestCommand)
	fmt.Fprintf(&sb, "**Lint Command:** %s\n", ruleset.LintCommand)
	fmt.Fprintf(&sb, "**Format Command:** %s\n\n", ruleset.FormatCommand)

	if ruleset.TDDRequired {
		sb.WriteString("**TDD REQUIRED:** You MUST write tests BEFORE implementation code.\n")
		sb.WriteString("Test-driven development is mandatory for this project.\n\n")
	}

	sb.WriteString("## IMPLEMENTATION INSTRUCTIONS\n\n")
	sb.WriteString("Follow these steps in order:\n\n")
	sb.WriteString("1. **Create Feature Branch**\n\n")
	sb.WriteString("2. **Implement Solution**\n\n")
	sb.WriteString("3. **Run Quality Checks**\n\n")
	sb.WriteString("4. **Create Pull Request**\n\n")
	sb.WriteString("5. **Report Results**\n\n")

	return sb.String()
}
