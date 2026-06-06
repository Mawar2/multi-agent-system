package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Mawar2/multi-agent-system/internal/conventions"
	"github.com/Mawar2/multi-agent-system/internal/llm"
	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
)

// GeminiWorker is a worker implementation that drives a Gemini model (via the
// Antigravity bridge LLM backend) which CANNOT run shell commands. Instead of
// asking the model to create branches, run tests, and open PRs itself (as
// ClaudeCodeWorker does), GeminiWorker uses a plan-execute flow:
//
//  1. Build a prompt that asks the model to return a JSON "plan" of file
//     operations (write/delete) plus PR title/body.
//  2. Apply those operations to the workspace ourselves.
//  3. Run quality gates, commit, push, and open the PR via the gh CLI.
//
// This mirrors ClaudeCodeWorker's testability seams: workspaceManager and
// qualityGate are injectable, and the git/PR steps go through function-field
// seams (checkoutBranch/commitAll/push and prCreator) so tests can inject
// no-op git and a fake PR creator, keeping them fully hermetic.
type GeminiWorker struct {
	id              string
	tier            taskqueue.Tier
	queue           taskqueue.TaskQueue
	backend         llm.LLMBackend
	workspaceRoot   string
	workspaceMgr    workspaceManager
	newQualityGates func(workspaceDir string) qualityGate
	prCreator       prCreator

	// Git seams. Default to real git via gitCmd; tests inject no-ops so Execute
	// never shells out to git (which would otherwise try to push over a network).
	checkoutBranch func(ctx context.Context, dir, branch string) error
	commitAll      func(ctx context.Context, dir, message string) error
	push           func(ctx context.Context, dir, branch string) error
	detectBase     func(ctx context.Context, dir string) string

	tasksCompleted int
	tasksFailed    int
	mu             sync.Mutex
}

var _ Worker = (*GeminiWorker)(nil)

// prCreator abstracts pull-request creation so Execute is testable without a
// real gh CLI or network. ghPRCreator is the production implementation.
type prCreator interface {
	CreatePR(ctx context.Context, owner, repo, head, base, title, body string) (int, error)
}

// ghPRCreator creates pull requests by shelling out to the gh CLI.
type ghPRCreator struct{}

var prURLNumberRe = regexp.MustCompile(`/pull/(\d+)`)

// CreatePR runs `gh pr create ...` and parses the PR number out of the printed URL.
func (ghPRCreator) CreatePR(ctx context.Context, owner, repo, head, base, title, body string) (int, error) {
	repoSlug := fmt.Sprintf("%s/%s", owner, repo)
	cmd := exec.CommandContext(ctx, "gh", "pr", "create",
		"-R", repoSlug,
		"--head", head,
		"--base", base,
		"--title", title,
		"--body", body,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("gh pr create failed: %w\nOutput:\n%s", err, string(out))
	}

	m := prURLNumberRe.FindStringSubmatch(string(out))
	if len(m) != 2 {
		return 0, fmt.Errorf("could not parse PR number from gh output:\n%s", string(out))
	}
	var num int
	if _, scanErr := fmt.Sscanf(m[1], "%d", &num); scanErr != nil {
		return 0, fmt.Errorf("could not parse PR number %q: %w", m[1], scanErr)
	}
	return num, nil
}

// geminiPlan is the JSON schema the model is asked to return.
type geminiPlan struct {
	Summary    string         `json:"summary"`
	Operations []geminiFileOp `json:"operations"`
	PRTitle    string         `json:"pr_title"`
	PRBody     string         `json:"pr_body"`
}

// geminiFileOp describes a single file operation in a plan.
type geminiFileOp struct {
	Path    string `json:"path"`
	Action  string `json:"action"` // "write" or "delete"
	Content string `json:"content"`
}

// NewGeminiWorker creates a new GeminiWorker. The git and PR steps default to
// real implementations (gitCmd / gh CLI); tests override the unexported seams.
func NewGeminiWorker(
	id string,
	tier taskqueue.Tier,
	queue taskqueue.TaskQueue,
	backend llm.LLMBackend,
	workspaceRoot string,
) *GeminiWorker {
	return &GeminiWorker{
		id:            id,
		tier:          tier,
		queue:         queue,
		backend:       backend,
		workspaceRoot: workspaceRoot,
		workspaceMgr:  NewWorkspaceManager(workspaceRoot, id),
		newQualityGates: func(workspaceDir string) qualityGate {
			return NewQualityGates(workspaceDir)
		},
		prCreator:      ghPRCreator{},
		checkoutBranch: gitCheckoutNewBranch,
		commitAll:      gitCommitAll,
		push:           gitPush,
		detectBase:     detectDefaultBranch,
	}
}

// Claim attempts to atomically claim a task for this worker's tier.
func (w *GeminiWorker) Claim(ctx context.Context) (*taskqueue.Task, error) {
	task, err := w.queue.Dequeue(ctx, w.tier, w.id)
	if err != nil {
		return nil, fmt.Errorf("failed to claim task: %w", err)
	}
	if task == nil {
		return nil, nil
	}
	return task, nil
}

// Release returns a task back to the queue for another worker to try.
func (w *GeminiWorker) Release(ctx context.Context, taskID string) error {
	if err := w.queue.Release(ctx, taskID); err != nil {
		return fmt.Errorf("failed to release task %s: %w", taskID, err)
	}
	return nil
}

// Health returns the current health status of this worker.
func (w *GeminiWorker) Health(ctx context.Context) (*HealthStatus, error) {
	w.mu.Lock()
	completed := w.tasksCompleted
	failed := w.tasksFailed
	w.mu.Unlock()

	return &HealthStatus{
		WorkerID:       w.id,
		Tier:           w.tier,
		Healthy:        true,
		TasksCompleted: completed,
		TasksFailed:    failed,
		LastHeartbeat:  time.Now().Format(time.RFC3339),
		ErrorMsg:       "",
	}, nil
}

// ID returns this worker's unique identifier.
func (w *GeminiWorker) ID() string { return w.id }

// Tier returns which tier this worker operates in.
func (w *GeminiWorker) Tier() taskqueue.Tier { return w.tier }

// Execute performs the plan-execute flow for a claimed task.
func (w *GeminiWorker) Execute(ctx context.Context, task *taskqueue.Task) (*Result, error) {
	// Step a: mark InProgress.
	task.Status = taskqueue.StatusInProgress
	task.StartedAt = time.Now()
	if err := w.queue.Update(ctx, task); err != nil {
		return w.failResult(task, fmt.Errorf("failed to update task status: %w", err)), nil
	}

	// Step b: detect task type.
	taskType := task.Metadata["task_type"]
	if taskType == "" {
		taskType = "issue"
	}

	// Step c: prepare workspace.
	var workspaceDir string
	var err error
	if taskType == "pr_feedback" {
		workspaceDir, err = w.workspaceMgr.PrepareWorkspaceForFix(ctx, task)
		if err != nil {
			return w.failResult(task, fmt.Errorf("failed to prepare workspace for fix: %w", err)), nil
		}
	} else {
		workspaceDir, err = w.workspaceMgr.PrepareWorkspace(ctx, task)
		if err != nil {
			return w.failResult(task, fmt.Errorf("failed to prepare workspace: %w", err)), nil
		}
	}

	// Step d: parse conventions.
	ruleset, err := conventions.ParseConventions(workspaceDir)
	if err != nil {
		return w.failResult(task, fmt.Errorf("failed to parse conventions: %w", err)), nil
	}

	// Step e: detect base (default) branch.
	base := w.detectBase(ctx, workspaceDir)

	// Step f: determine branch.
	var branch string
	if taskType == "pr_feedback" {
		// Branch already checked out by PrepareWorkspaceForFix.
		branch = task.BranchName
	} else {
		branch = branchName(task, ruleset)
		if err := w.checkoutBranch(ctx, workspaceDir, branch); err != nil {
			return w.failResult(task, fmt.Errorf("failed to create branch %s: %w", branch, err)), nil
		}
	}

	// Step g: build prompt and call the model.
	prompt := w.buildPlanPrompt(task, ruleset, workspaceDir)
	models := w.backend.Models()
	if len(models) == 0 {
		return w.failResult(task, fmt.Errorf("backend has no available models")), nil
	}
	model := models[0]

	resp, err := w.backend.ExecuteInDir(ctx, prompt, model, workspaceDir)
	if err != nil {
		return w.failResult(task, fmt.Errorf("LLM execution failed: %w", err)), nil
	}

	// Step h: parse plan.
	plan, err := parseGeminiPlan(resp)
	if err != nil {
		return w.failResult(task, fmt.Errorf("failed to parse plan from LLM response: %w", err)), nil
	}
	if len(plan.Operations) == 0 {
		return w.failResult(task, fmt.Errorf("plan contained no file operations")), nil
	}

	// Step i: apply operations.
	if err := applyOps(workspaceDir, plan.Operations, ruleset); err != nil {
		return w.failResult(task, fmt.Errorf("failed to apply file operations: %w", err)), nil
	}

	// Step j: quality gates.
	gates := w.newQualityGates(workspaceDir)
	if err := gates.Validate(ctx, ruleset); err != nil {
		return w.failResult(task, fmt.Errorf("quality gates failed: %w", err)), nil
	}

	// Step k: commit.
	msg := commitMessage(task, ruleset, plan.Summary)
	if err := w.commitAll(ctx, workspaceDir, msg); err != nil {
		return w.failResult(task, fmt.Errorf("commit failed (agent produced no effective change?): %w", err)), nil
	}

	// Step l: push.
	if err := w.push(ctx, workspaceDir, branch); err != nil {
		return w.failResult(task, fmt.Errorf("push failed: %w", err)), nil
	}

	// Step m: PR.
	prNum := task.PRNumber
	if taskType == "issue" {
		title := plan.PRTitle
		if title == "" {
			title = task.Title
		}
		body := plan.PRBody
		if body == "" {
			body = fmt.Sprintf("Implements #%d.\n\n%s", task.IssueNumber, firstNonEmpty(plan.Summary, task.Title))
		}
		prNum, err = w.prCreator.CreatePR(ctx, task.RepoOwner, task.RepoName, branch, base, title, body)
		if err != nil {
			return w.failResult(task, fmt.Errorf("failed to create PR: %w", err)), nil
		}
	}

	// Step n: mark Review.
	task.Status = taskqueue.StatusReview
	task.BranchName = branch
	task.PRNumber = prNum
	task.CompletedAt = time.Now()
	if err := w.queue.Update(ctx, task); err != nil {
		return w.failResult(task, fmt.Errorf("failed to update task to review: %w", err)), nil
	}

	// Step o: record success.
	w.mu.Lock()
	w.tasksCompleted++
	w.mu.Unlock()

	return &Result{
		TaskID:     task.ID,
		Success:    true,
		BranchName: branch,
		PRNumber:   prNum,
	}, nil
}

// failResult marks a task failed, updates stats, and returns a failure Result.
func (w *GeminiWorker) failResult(task *taskqueue.Task, err error) *Result {
	task.Status = taskqueue.StatusFailed
	task.ErrorMsg = err.Error()
	task.CompletedAt = time.Now()

	ctx := context.Background()
	if updateErr := w.queue.Update(ctx, task); updateErr != nil {
		fmt.Printf("Warning: failed to update task status to failed: %v\n", updateErr)
	}

	w.mu.Lock()
	w.tasksFailed++
	w.mu.Unlock()

	return &Result{
		TaskID:   task.ID,
		Success:  false,
		ErrorMsg: err.Error(),
	}
}

// ---- Git seam default implementations (real git via gitCmd) ----

// gitCheckoutNewBranch creates and checks out a new branch (or resets it if it already exists).
func gitCheckoutNewBranch(ctx context.Context, dir, branch string) error {
	cmd := gitCmd(ctx, "-C", dir, "checkout", "-B", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout -B %s: %w\nOutput:\n%s", branch, err, string(out))
	}
	return nil
}

// gitCommitAll stages all changes and commits them. It returns an error if
// there is nothing to commit (the agent produced no effective change).
func gitCommitAll(ctx context.Context, dir, message string) error {
	if out, err := gitCmd(ctx, "-C", dir, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("git add -A: %w\nOutput:\n%s", err, string(out))
	}
	out, err := gitCmd(ctx, "-C", dir, "commit", "-m", message).CombinedOutput()
	if err != nil {
		o := strings.ToLower(string(out))
		if strings.Contains(o, "nothing to commit") || strings.Contains(o, "no changes added") {
			return fmt.Errorf("nothing to commit: %s", strings.TrimSpace(string(out)))
		}
		return fmt.Errorf("git commit: %w\nOutput:\n%s", err, string(out))
	}
	return nil
}

// gitPush pushes the branch to origin, setting upstream.
func gitPush(ctx context.Context, dir, branch string) error {
	out, err := gitCmd(ctx, "-C", dir, "push", "-u", "origin", branch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git push: %w\nOutput:\n%s", err, string(out))
	}
	return nil
}

// detectDefaultBranch returns the repository's default branch, falling back to "main".
func detectDefaultBranch(ctx context.Context, dir string) string {
	out, err := gitCmd(ctx, "-C", dir, "symbolic-ref", "refs/remotes/origin/HEAD", "--short").Output()
	if err != nil {
		return "main"
	}
	ref := strings.TrimSpace(string(out))
	if ref == "" {
		return "main"
	}
	return filepath.Base(ref)
}

// ---- Helpers ----

// branchName derives a branch name from the task, honoring a project pattern if any.
func branchName(task *taskqueue.Task, ruleset *conventions.Ruleset) string {
	if ruleset.HasBranchPattern() {
		b := ruleset.BranchPattern
		b = strings.ReplaceAll(b, "{ticket}", fmt.Sprintf("%d", task.IssueNumber))
		b = strings.ReplaceAll(b, "{summary}", slug(task.Title))
		return b
	}
	return fmt.Sprintf("feature/issue-%d-%s", task.IssueNumber, slug(task.Title))
}

// commitMessage derives a commit message, honoring a project pattern if any.
func commitMessage(task *taskqueue.Task, ruleset *conventions.Ruleset, summary string) string {
	desc := firstNonEmpty(summary, task.Title)
	if ruleset.HasCommitPattern() {
		c := ruleset.CommitPattern
		c = strings.ReplaceAll(c, "{ticket}", fmt.Sprintf("%d", task.IssueNumber))
		c = strings.ReplaceAll(c, "{description}", desc)
		return c
	}
	return fmt.Sprintf("%s (#%d)", desc, task.IssueNumber)
}

// slug converts a string to a lowercase, hyphen-separated slug capped at ~40 chars.
func slug(s string) string {
	var b strings.Builder
	prevHyphen := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	const maxLen = 40
	if len(out) > maxLen {
		out = strings.Trim(out[:maxLen], "-")
	}
	return out
}

// firstNonEmpty returns the first non-empty string, or "" if all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

var jsonFenceRe = regexp.MustCompile("(?s)```json\\s*(.*?)```")
var anyFenceRe = regexp.MustCompile("(?s)```[a-zA-Z0-9]*\\s*(.*?)```")

// parseGeminiPlan extracts a JSON object from an LLM response and unmarshals it.
// It prefers a ```json fenced block, then any fenced block whose content starts
// with "{", then the substring from the first "{" to the last "}".
func parseGeminiPlan(resp string) (*geminiPlan, error) {
	candidate := ""

	if m := jsonFenceRe.FindStringSubmatch(resp); len(m) == 2 {
		candidate = strings.TrimSpace(m[1])
	}

	if candidate == "" {
		for _, m := range anyFenceRe.FindAllStringSubmatch(resp, -1) {
			inner := strings.TrimSpace(m[1])
			if strings.HasPrefix(inner, "{") {
				candidate = inner
				break
			}
		}
	}

	if candidate == "" {
		start := strings.Index(resp, "{")
		end := strings.LastIndex(resp, "}")
		if start >= 0 && end > start {
			candidate = resp[start : end+1]
		}
	}

	if candidate == "" {
		return nil, fmt.Errorf("no JSON object found in response")
	}

	var plan geminiPlan
	if err := json.Unmarshal([]byte(candidate), &plan); err != nil {
		return nil, fmt.Errorf("invalid plan JSON: %w", err)
	}
	return &plan, nil
}

// applyOps applies file operations to the workspace, rejecting any path that
// escapes the workspace root or names a forbidden file.
func applyOps(root string, ops []geminiFileOp, ruleset *conventions.Ruleset) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to resolve workspace root: %w", err)
	}

	for _, op := range ops {
		clean := filepath.Clean(op.Path)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return fmt.Errorf("rejected unsafe path %q", op.Path)
		}

		full := filepath.Join(absRoot, clean)
		rel, err := filepath.Rel(absRoot, full)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("rejected path %q outside workspace", op.Path)
		}

		if ruleset.IsForbidden(filepath.Base(clean)) {
			return fmt.Errorf("rejected forbidden file %q", filepath.Base(clean))
		}

		switch op.Action {
		case "write", "create", "":
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				return fmt.Errorf("failed to create dir for %q: %w", op.Path, err)
			}
			if err := os.WriteFile(full, []byte(op.Content), 0o644); err != nil {
				return fmt.Errorf("failed to write %q: %w", op.Path, err)
			}
		case "delete", "remove":
			if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to delete %q: %w", op.Path, err)
			}
		default:
			return fmt.Errorf("unknown action %q for %q", op.Action, op.Path)
		}
	}
	return nil
}

// textExtensions is the allowlist of file extensions whose contents are included
// in the repository context sent to the model.
var textExtensions = map[string]bool{
	".md": true, ".go": true, ".txt": true, ".json": true, ".yml": true,
	".yaml": true, ".toml": true, ".js": true, ".ts": true, ".tsx": true,
	".py": true, ".sh": true, ".bat": true, ".cfg": true, ".ini": true,
}

// textNames is the allowlist of extensionless filenames whose contents are included.
var textNames = map[string]bool{
	"Makefile": true, "Dockerfile": true, "LICENSE": true, "README": true,
}

const (
	maxFileBytes    = 8192
	maxContentBytes = 48 * 1024
)

// isTextFile reports whether a file should have its contents inlined into context.
func isTextFile(name string) bool {
	if textNames[name] {
		return true
	}
	return textExtensions[strings.ToLower(filepath.Ext(name))]
}

// buildRepoContext walks the workspace (skipping .git) and builds a string with a
// file tree section and an (inlined) file contents section, within a byte budget.
func buildRepoContext(root string) string {
	var tree strings.Builder
	var contents strings.Builder
	budget := maxContentBytes

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		fmt.Fprintf(&tree, "%s (%d bytes)\n", rel, info.Size())

		if budget <= 0 || info.Size() > maxFileBytes || !isTextFile(d.Name()) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			return nil
		}
		if len(data) > budget {
			return nil
		}
		budget -= len(data)
		fmt.Fprintf(&contents, "--- %s ---\n%s\n\n", rel, string(data))
		return nil
	})

	var sb strings.Builder
	sb.WriteString("### FILE TREE\n")
	sb.WriteString(tree.String())
	sb.WriteString("\n### FILE CONTENTS\n")
	sb.WriteString(contents.String())
	return sb.String()
}

// buildPlanPrompt constructs the plan-request prompt for the model.
func (w *GeminiWorker) buildPlanPrompt(task *taskqueue.Task, ruleset *conventions.Ruleset, workspaceDir string) string {
	var sb strings.Builder

	// First line: ticket identifier so the Antigravity bridge labels the
	// conversation by repo+issue.
	fmt.Fprintf(&sb, "%s/%s#%d: %s\n\n", task.RepoOwner, task.RepoName, task.IssueNumber, task.Title)

	taskType := task.Metadata["task_type"]
	if taskType == "" {
		taskType = "issue"
	}

	sb.WriteString("You are an autonomous coding agent. You CANNOT run shell commands, ")
	sb.WriteString("create branches, run tests, or open pull requests. Instead, you MUST ")
	sb.WriteString("return a plan describing the exact file operations to perform, as JSON.\n\n")

	if taskType == "pr_feedback" {
		sb.WriteString("## AI CODE REVIEW FEEDBACK TO ADDRESS\n\n")
		fmt.Fprintf(&sb, "**Repository:** %s/%s\n", task.RepoOwner, task.RepoName)
		fmt.Fprintf(&sb, "**Original Issue:** #%d - %s\n", task.IssueNumber, task.Title)
		fmt.Fprintf(&sb, "**Pull Request:** #%d\n", task.PRNumber)
		fmt.Fprintf(&sb, "**Branch:** %s (already checked out)\n", task.BranchName)
		fmt.Fprintf(&sb, "**Review Iteration:** %d of 3\n\n", task.ReviewIteration)
		sb.WriteString("Address ALL of the following review feedback on the existing branch. ")
		sb.WriteString("Make minimal, targeted changes — do not rewrite unrelated code:\n\n")
		feedback := firstNonEmpty(task.ReviewFeedback, task.Description)
		sb.WriteString(feedback)
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("## ISSUE TO IMPLEMENT\n\n")
		fmt.Fprintf(&sb, "**Repository:** %s/%s\n", task.RepoOwner, task.RepoName)
		fmt.Fprintf(&sb, "**Issue #%d:** %s\n\n", task.IssueNumber, task.Title)
		sb.WriteString("**Description:**\n")
		sb.WriteString(task.Description)
		sb.WriteString("\n\n")
	}

	// Conventions.
	sb.WriteString("## CONVENTIONS\n\n")
	if ruleset.HasBranchPattern() {
		fmt.Fprintf(&sb, "- Branch pattern: %s\n", ruleset.BranchPattern)
	}
	if ruleset.HasCommitPattern() {
		fmt.Fprintf(&sb, "- Commit pattern: %s\n", ruleset.CommitPattern)
	}
	if len(ruleset.ForbiddenFiles) > 0 {
		fmt.Fprintf(&sb, "- Forbidden files (do NOT create/modify): %s\n", strings.Join(ruleset.ForbiddenFiles, ", "))
	}
	fmt.Fprintf(&sb, "- Test command: %s\n", ruleset.TestCommand)
	fmt.Fprintf(&sb, "- Lint command: %s\n", ruleset.LintCommand)
	fmt.Fprintf(&sb, "- Format command: %s\n", ruleset.FormatCommand)
	if ruleset.TDDRequired {
		sb.WriteString("- TDD REQUIRED: include tests alongside implementation.\n")
	}
	sb.WriteString("\n")

	// Repository context.
	sb.WriteString("## REPOSITORY CONTEXT\n\n")
	sb.WriteString(buildRepoContext(workspaceDir))
	sb.WriteString("\n")

	// Strict response format.
	sb.WriteString("## STRICT RESPONSE FORMAT\n\n")
	sb.WriteString("Output ONLY a single fenced ```json block (no prose before or after) ")
	sb.WriteString("matching this schema exactly:\n\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"summary\": \"one-line summary of the change\",\n")
	sb.WriteString("  \"operations\": [\n")
	sb.WriteString("    {\"path\": \"relative/path.ext\", \"action\": \"write\", \"content\": \"FULL file contents\"},\n")
	sb.WriteString("    {\"path\": \"old/file.ext\", \"action\": \"delete\", \"content\": \"\"}\n")
	sb.WriteString("  ],\n")
	sb.WriteString("  \"pr_title\": \"concise PR title\",\n")
	sb.WriteString("  \"pr_body\": \"PR description referencing the issue\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	sb.WriteString("Rules:\n")
	sb.WriteString("- For \"write\" operations, provide the COMPLETE file contents, not a diff.\n")
	sb.WriteString("- Make minimal, focused changes that satisfy the task.\n")
	sb.WriteString("- Use relative paths only; never absolute paths or paths containing \"..\".\n")
	sb.WriteString("- Do NOT touch any forbidden files listed above.\n")

	return sb.String()
}
