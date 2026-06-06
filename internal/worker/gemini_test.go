package worker

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mawar2/multi-agent-system/internal/conventions"
	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
)

// fakePRCreator records the arguments it was called with and returns a fixed PR
// number, so Execute can be tested without shelling out to the gh CLI.
type fakePRCreator struct {
	owner, repo, head, base, title, body string
	prNum                                int
	err                                  error
	called                               bool
}

func (f *fakePRCreator) CreatePR(ctx context.Context, owner, repo, head, base, title, body string) (int, error) {
	f.called = true
	f.owner, f.repo, f.head, f.base, f.title, f.body = owner, repo, head, base, title, body
	return f.prNum, f.err
}

func TestExtractAndParsePlan(t *testing.T) {
	t.Run("json fenced block", func(t *testing.T) {
		resp := "Here is the plan:\n```json\n{\"summary\":\"s\",\"operations\":[{\"path\":\"a.txt\",\"action\":\"write\",\"content\":\"x\"}],\"pr_title\":\"t\",\"pr_body\":\"b\"}\n```\nDone."
		plan, err := parseGeminiPlan(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plan.Summary != "s" || len(plan.Operations) != 1 || plan.Operations[0].Path != "a.txt" {
			t.Fatalf("unexpected plan: %+v", plan)
		}
		if plan.PRTitle != "t" || plan.PRBody != "b" {
			t.Fatalf("unexpected PR fields: %+v", plan)
		}
	})

	t.Run("bare object", func(t *testing.T) {
		resp := `{"summary":"s","operations":[{"path":"b.txt","action":"write","content":"y"}]}`
		plan, err := parseGeminiPlan(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(plan.Operations) != 1 || plan.Operations[0].Path != "b.txt" {
			t.Fatalf("unexpected plan: %+v", plan)
		}
	})

	t.Run("surrounding prose", func(t *testing.T) {
		resp := "Sure! I'll do that.\n{\"summary\":\"s\",\"operations\":[{\"path\":\"c.txt\",\"action\":\"delete\"}]}\nHope that helps!"
		plan, err := parseGeminiPlan(resp)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(plan.Operations) != 1 || plan.Operations[0].Action != "delete" {
			t.Fatalf("unexpected plan: %+v", plan)
		}
	})

	t.Run("garbage", func(t *testing.T) {
		if _, err := parseGeminiPlan("no json here at all"); err == nil {
			t.Fatal("expected error for garbage input")
		}
	})

	t.Run("invalid json object", func(t *testing.T) {
		if _, err := parseGeminiPlan("{not valid json}"); err == nil {
			t.Fatal("expected error for invalid json")
		}
	})
}

func TestApplyOps(t *testing.T) {
	t.Run("write creates nested files", func(t *testing.T) {
		root := t.TempDir()
		ops := []geminiFileOp{
			{Path: "dir/sub/file.txt", Action: "write", Content: "hello"},
		}
		if err := applyOps(root, ops, conventions.NewRuleset(root)); err != nil {
			t.Fatalf("applyOps error: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(root, "dir", "sub", "file.txt"))
		if err != nil {
			t.Fatalf("read error: %v", err)
		}
		if string(got) != "hello" {
			t.Fatalf("got %q, want %q", string(got), "hello")
		}
	})

	t.Run("delete removes file", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "gone.txt")
		if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		ops := []geminiFileOp{{Path: "gone.txt", Action: "delete"}}
		if err := applyOps(root, ops, conventions.NewRuleset(root)); err != nil {
			t.Fatalf("applyOps error: %v", err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("expected file deleted, stat err = %v", err)
		}
	})

	t.Run("rejects dotdot path", func(t *testing.T) {
		root := t.TempDir()
		ops := []geminiFileOp{{Path: "../escape.txt", Action: "write", Content: "x"}}
		if err := applyOps(root, ops, conventions.NewRuleset(root)); err == nil {
			t.Fatal("expected error for .. path")
		}
	})

	t.Run("rejects absolute path", func(t *testing.T) {
		root := t.TempDir()
		abs := filepath.Join(t.TempDir(), "abs.txt")
		ops := []geminiFileOp{{Path: abs, Action: "write", Content: "x"}}
		if err := applyOps(root, ops, conventions.NewRuleset(root)); err == nil {
			t.Fatal("expected error for absolute path")
		}
	})

	t.Run("rejects forbidden file", func(t *testing.T) {
		root := t.TempDir()
		rs := conventions.NewRuleset(root)
		rs.ForbiddenFiles = []string{"secret.go"}
		ops := []geminiFileOp{{Path: "pkg/secret.go", Action: "write", Content: "x"}}
		if err := applyOps(root, ops, rs); err == nil {
			t.Fatal("expected error for forbidden file")
		}
	})

	t.Run("rejects unknown action", func(t *testing.T) {
		root := t.TempDir()
		ops := []geminiFileOp{{Path: "a.txt", Action: "frobnicate"}}
		if err := applyOps(root, ops, conventions.NewRuleset(root)); err == nil {
			t.Fatal("expected error for unknown action")
		}
	})
}

func TestSlugAndBranchName(t *testing.T) {
	if got := slug("Add New Feature!"); got != "add-new-feature" {
		t.Errorf("slug = %q, want %q", got, "add-new-feature")
	}
	if got := slug("   --weird__name--  "); got != "weird-name" {
		t.Errorf("slug = %q, want %q", got, "weird-name")
	}
	long := slug("this is an extremely long title that should be truncated to forty characters max here")
	if len(long) > 40 {
		t.Errorf("slug length = %d, want <= 40", len(long))
	}

	task := &taskqueue.Task{IssueNumber: 42, Title: "Add Feature"}

	// Fallback (no pattern).
	rs := conventions.NewRuleset("/x")
	if got := branchName(task, rs); got != "feature/issue-42-add-feature" {
		t.Errorf("branchName fallback = %q", got)
	}

	// Pattern substitution.
	rs.BranchPattern = "feature/KAI-{ticket}-{summary}"
	if got := branchName(task, rs); got != "feature/KAI-42-add-feature" {
		t.Errorf("branchName pattern = %q", got)
	}
}

func TestCommitMessage(t *testing.T) {
	task := &taskqueue.Task{IssueNumber: 7, Title: "Fix bug"}

	// Fallback uses summary then title.
	rs := conventions.NewRuleset("/x")
	if got := commitMessage(task, rs, "did the thing"); got != "did the thing (#7)" {
		t.Errorf("commitMessage fallback (summary) = %q", got)
	}
	if got := commitMessage(task, rs, ""); got != "Fix bug (#7)" {
		t.Errorf("commitMessage fallback (title) = %q", got)
	}

	// Pattern substitution.
	rs.CommitPattern = "{ticket}_{description}"
	if got := commitMessage(task, rs, "summary text"); got != "7_summary text" {
		t.Errorf("commitMessage pattern = %q", got)
	}
}

func TestGeminiExecute_Success(t *testing.T) {
	workspace := t.TempDir()

	queue := newMockQueue()
	backend := newMockBackend("gemini-test", []string{"gemini-flash-3.5"})
	backend.executeFunc = func(ctx context.Context, prompt, model string) (string, error) {
		return "```json\n{\"summary\":\"add readme\",\"operations\":[{\"path\":\"README.md\",\"action\":\"write\",\"content\":\"# Hello\\n\"}],\"pr_title\":\"Add README\",\"pr_body\":\"Adds a readme.\"}\n```", nil
	}

	pr := &fakePRCreator{prNum: 321}

	w := NewGeminiWorker("gemini-flash-1", taskqueue.TierGeminiFlash, queue, backend, "/tmp/projects")
	// Inject hermetic fakes for workspace, quality gate, PR creation, and git.
	w.workspaceMgr = &fakeWorkspaceManager{dir: workspace}
	w.newQualityGates = func(string) qualityGate { return stubQualityGate{} }
	w.prCreator = pr

	var checkedOut string
	var committed bool
	var pushed string
	w.checkoutBranch = func(ctx context.Context, dir, branch string) error {
		checkedOut = branch
		return nil
	}
	w.commitAll = func(ctx context.Context, dir, message string) error {
		committed = true
		return nil
	}
	w.push = func(ctx context.Context, dir, branch string) error {
		pushed = branch
		return nil
	}
	w.detectBase = func(ctx context.Context, dir string) string { return "main" }

	task := &taskqueue.Task{
		ID:          "task-g1",
		IssueNumber: 99,
		RepoOwner:   "Mawar2",
		RepoName:    "Kaimi",
		Title:       "Add readme",
		Description: "Please add a README.",
		Status:      taskqueue.StatusClaimed,
		WorkerID:    "gemini-flash-1",
	}

	result, err := w.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got failure: %s", result.ErrorMsg)
	}

	// Ops applied to the temp dir.
	got, readErr := os.ReadFile(filepath.Join(workspace, "README.md"))
	if readErr != nil {
		t.Fatalf("expected README.md written: %v", readErr)
	}
	if string(got) != "# Hello\n" {
		t.Errorf("README content = %q", string(got))
	}

	// Git seams invoked.
	wantBranch := "feature/issue-99-add-readme"
	if checkedOut != wantBranch {
		t.Errorf("checkoutBranch = %q, want %q", checkedOut, wantBranch)
	}
	if !committed {
		t.Error("commitAll was not invoked")
	}
	if pushed != wantBranch {
		t.Errorf("push branch = %q, want %q", pushed, wantBranch)
	}

	// PR creator received the right head/base/title.
	if !pr.called {
		t.Fatal("prCreator.CreatePR was not called")
	}
	if pr.head != wantBranch || pr.base != "main" {
		t.Errorf("PR head=%q base=%q, want head=%q base=main", pr.head, pr.base, wantBranch)
	}
	if pr.title != "Add README" {
		t.Errorf("PR title = %q, want %q", pr.title, "Add README")
	}
	if pr.owner != "Mawar2" || pr.repo != "Kaimi" {
		t.Errorf("PR repo = %s/%s, want Mawar2/Kaimi", pr.owner, pr.repo)
	}

	// Task ended in Review with expected branch/PR.
	if task.Status != taskqueue.StatusReview {
		t.Errorf("task status = %v, want StatusReview", task.Status)
	}
	if task.BranchName != wantBranch {
		t.Errorf("task branch = %q, want %q", task.BranchName, wantBranch)
	}
	if task.PRNumber != 321 {
		t.Errorf("task PR = %d, want 321", task.PRNumber)
	}
	if result.PRNumber != 321 {
		t.Errorf("result PR = %d, want 321", result.PRNumber)
	}
	if w.tasksCompleted != 1 {
		t.Errorf("tasksCompleted = %d, want 1", w.tasksCompleted)
	}
}

func TestGeminiExecute_PRFeedbackReusesBranch(t *testing.T) {
	workspace := t.TempDir()

	queue := newMockQueue()
	backend := newMockBackend("gemini-test", []string{"gemini-flash-3.5"})
	backend.executeFunc = func(ctx context.Context, prompt, model string) (string, error) {
		return "```json\n{\"summary\":\"fix\",\"operations\":[{\"path\":\"fix.txt\",\"action\":\"write\",\"content\":\"fixed\"}]}\n```", nil
	}

	pr := &fakePRCreator{prNum: 999}

	w := NewGeminiWorker("gemini-flash-1", taskqueue.TierGeminiFlash, queue, backend, "/tmp/projects")
	w.workspaceMgr = &fakeWorkspaceManager{dir: workspace}
	w.newQualityGates = func(string) qualityGate { return stubQualityGate{} }
	w.prCreator = pr

	checkoutCalled := false
	w.checkoutBranch = func(ctx context.Context, dir, branch string) error { checkoutCalled = true; return nil }
	w.commitAll = func(ctx context.Context, dir, message string) error { return nil }
	w.push = func(ctx context.Context, dir, branch string) error { return nil }
	w.detectBase = func(ctx context.Context, dir string) string { return "main" }

	task := &taskqueue.Task{
		ID:              "task-fix",
		IssueNumber:     50,
		RepoOwner:       "Mawar2",
		RepoName:        "Kaimi",
		Title:           "Original",
		BranchName:      "feature/existing-branch",
		PRNumber:        77,
		ReviewIteration: 1,
		ReviewFeedback:  "Please rename the variable.",
		Status:          taskqueue.StatusClaimed,
		Metadata:        map[string]string{"task_type": "pr_feedback"},
	}

	result, err := w.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.Success {
		t.Fatalf("expected success, got: %s", result.ErrorMsg)
	}

	// For pr_feedback: no new branch, no new PR.
	if checkoutCalled {
		t.Error("checkoutBranch should NOT be called for pr_feedback tasks")
	}
	if pr.called {
		t.Error("prCreator should NOT be called for pr_feedback tasks (push updates existing PR)")
	}
	if task.PRNumber != 77 {
		t.Errorf("task PR = %d, want 77 (unchanged)", task.PRNumber)
	}
	if task.BranchName != "feature/existing-branch" {
		t.Errorf("task branch = %q, want existing branch", task.BranchName)
	}
	if task.Status != taskqueue.StatusReview {
		t.Errorf("task status = %v, want StatusReview", task.Status)
	}
}

func TestGeminiExecute_EmptyPlanFails(t *testing.T) {
	workspace := t.TempDir()
	queue := newMockQueue()
	backend := newMockBackend("gemini-test", []string{"gemini-flash-3.5"})
	backend.executeFunc = func(ctx context.Context, prompt, model string) (string, error) {
		return "```json\n{\"summary\":\"nothing\",\"operations\":[]}\n```", nil
	}

	w := NewGeminiWorker("gemini-flash-1", taskqueue.TierGeminiFlash, queue, backend, "/tmp/projects")
	w.workspaceMgr = &fakeWorkspaceManager{dir: workspace}
	w.newQualityGates = func(string) qualityGate { return stubQualityGate{} }
	w.prCreator = &fakePRCreator{}
	w.checkoutBranch = func(ctx context.Context, dir, branch string) error { return nil }
	w.commitAll = func(ctx context.Context, dir, message string) error { return nil }
	w.push = func(ctx context.Context, dir, branch string) error { return nil }
	w.detectBase = func(ctx context.Context, dir string) string { return "main" }

	task := &taskqueue.Task{
		ID:          "task-empty",
		IssueNumber: 1,
		RepoOwner:   "Mawar2",
		RepoName:    "Kaimi",
		Title:       "Empty",
		Status:      taskqueue.StatusClaimed,
	}

	result, err := w.Execute(context.Background(), task)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Success {
		t.Fatal("expected failure for empty plan")
	}
	if task.Status != taskqueue.StatusFailed {
		t.Errorf("task status = %v, want StatusFailed", task.Status)
	}
	if w.tasksFailed != 1 {
		t.Errorf("tasksFailed = %d, want 1", w.tasksFailed)
	}
}
