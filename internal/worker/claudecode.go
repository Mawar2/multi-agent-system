package worker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/conventions"
	"github.com/Mawar2/multi-agent-system/internal/llm"
	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
)

// ClaudeCodeWorker is a worker implementation that uses Claude Code CLI to complete tasks.
// It claims tasks from the queue, parses project conventions, and uses the LLM backend
// to implement solutions following project rules.
//
// Phase 1: Uses Claude Code CLI as the backend (local, free)
// Phase 2+: Can use other backends (Antigravity, Vertex AI) via LLMBackend abstraction
type ClaudeCodeWorker struct {
	id              string               // Unique worker identifier
	tier            taskqueue.Tier       // Worker tier (determines which tasks to claim)
	queue           taskqueue.TaskQueue  // Queue to claim tasks from
	backend         llm.LLMBackend       // LLM backend for code generation
	workspaceRoot   string               // Root directory for isolated workspaces
	workspaceMgr    *WorkspaceManager    // Manages cloning and updating target repos
	tasksCompleted  int                  // Number of tasks successfully completed
	tasksFailed     int                  // Number of tasks failed
	mu              sync.Mutex           // Protects stats (tasksCompleted, tasksFailed)
}

// NewClaudeCodeWorker creates a new ClaudeCodeWorker instance.
//
// Parameters:
//   - id: Unique identifier for this worker (e.g., "claude-worker-1")
//   - tier: Which tier this worker operates in (TierClaude, TierGeminiPro, TierGeminiFlash)
//   - queue: TaskQueue to claim tasks from
//   - backend: LLMBackend for executing prompts
//   - workspaceRoot: Absolute path to root directory for isolated workspaces (e.g., "./workspaces")
//
// Returns a configured worker ready to claim and execute tasks.
func NewClaudeCodeWorker(
	id string,
	tier taskqueue.Tier,
	queue taskqueue.TaskQueue,
	backend llm.LLMBackend,
	workspaceRoot string,
) *ClaudeCodeWorker {
	return &ClaudeCodeWorker{
		id:            id,
		tier:          tier,
		queue:         queue,
		backend:       backend,
		workspaceRoot: workspaceRoot,
		workspaceMgr:  NewWorkspaceManager(workspaceRoot),
		tasksCompleted: 0,
		tasksFailed:    0,
	}
}

// Claim attempts to atomically claim a task from the queue for this worker's tier.
// Returns the claimed task, or nil if no tasks are available for this tier.
//
// The queue's Dequeue method handles atomicity - only one worker can claim a given task.
func (w *ClaudeCodeWorker) Claim(ctx context.Context) (*taskqueue.Task, error) {
	task, err := w.queue.Dequeue(ctx, w.tier, w.id)
	if err != nil {
		return nil, fmt.Errorf("failed to claim task: %w", err)
	}

	// nil task means no work available (not an error)
	if task == nil {
		return nil, nil
	}

	return task, nil
}

// Execute performs the work for a claimed task.
//
// Steps:
//  1. Prepare workspace (clone or pull target repository)
//  2. Parse project conventions (CLAUDE.md, CONVENTIONS.md)
//  3. Build detailed LLM prompt with task details and conventions
//  4. Call backend.Execute to get implementation from LLM (runs in workspace)
//  5. Parse response for branch name and PR number
//  6. Update task status to Review
//  7. Return Result with success status
//
// If any step fails, returns a Result with success=false and error details.
func (w *ClaudeCodeWorker) Execute(ctx context.Context, task *taskqueue.Task) (*Result, error) {
	// Update task to InProgress
	task.Status = taskqueue.StatusInProgress
	task.StartedAt = time.Now()
	if err := w.queue.Update(ctx, task); err != nil {
		return w.failResult(task, fmt.Errorf("failed to update task status: %w", err)), nil
	}

	// Step 1: Prepare workspace (clone or update target repo)
	workspaceDir, err := w.workspaceMgr.PrepareWorkspace(ctx, task)
	if err != nil {
		return w.failResult(task, fmt.Errorf("failed to prepare workspace: %w", err)), nil
	}
	fmt.Printf("[Worker %s] Using workspace: %s\n", w.id, workspaceDir)

	// Step 2: Parse project conventions from workspace
	ruleset, err := conventions.ParseConventions(workspaceDir)
	if err != nil {
		return w.failResult(task, fmt.Errorf("failed to parse conventions: %w", err)), nil
	}

	// Step 3: Build LLM prompt
	prompt := w.buildPrompt(task, ruleset)

	// Step 4: Execute LLM call in workspace directory
	// For Phase 1, we use the first available model from the backend
	models := w.backend.Models()
	if len(models) == 0 {
		return w.failResult(task, fmt.Errorf("backend has no available models")), nil
	}
	model := models[0]

	// Execute in workspace directory so git operations use correct repo context
	response, err := w.backend.ExecuteInDir(ctx, prompt, model, workspaceDir)
	if err != nil {
		return w.failResult(task, fmt.Errorf("LLM execution failed: %w", err)), nil
	}

	// Step 4: Parse response for branch name and PR number
	// Expected format in response:
	// - "Branch: feature/KAI-123-summary"
	// - "PR: #456" or "Pull Request: #456"
	branchName := extractBranchName(response)
	prNumber := extractPRNumber(response)

	if branchName == "" {
		return w.failResult(task, fmt.Errorf("could not extract branch name from LLM response")), nil
	}

	// Step 5: Update task to Review status
	task.Status = taskqueue.StatusReview
	task.BranchName = branchName
	task.PRNumber = prNumber
	task.CompletedAt = time.Now()
	if err := w.queue.Update(ctx, task); err != nil {
		return w.failResult(task, fmt.Errorf("failed to update task to review: %w", err)), nil
	}

	// Step 6: Record success and return result
	w.mu.Lock()
	w.tasksCompleted++
	w.mu.Unlock()

	return &Result{
		TaskID:     task.ID,
		Success:    true,
		BranchName: branchName,
		PRNumber:   prNumber,
		ErrorMsg:   "",
		LogsPath:   "", // Could be added in future for debugging
	}, nil
}

// Release returns a task back to the queue for another worker to try.
// This is called when Execute fails or when a worker crashes while working on a task.
//
// The queue's Release method handles incrementing Attempts and resetting WorkerID.
func (w *ClaudeCodeWorker) Release(ctx context.Context, taskID string) error {
	if err := w.queue.Release(ctx, taskID); err != nil {
		return fmt.Errorf("failed to release task %s: %w", taskID, err)
	}
	return nil
}

// Health returns the current health status of this worker.
// Reports worker ID, tier, operational status, and task completion stats.
func (w *ClaudeCodeWorker) Health(ctx context.Context) (*HealthStatus, error) {
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
func (w *ClaudeCodeWorker) ID() string {
	return w.id
}

// Tier returns which tier this worker operates in.
func (w *ClaudeCodeWorker) Tier() taskqueue.Tier {
	return w.tier
}

// buildPrompt constructs a detailed prompt for the LLM that includes:
// - Task details (title, description, acceptance criteria)
// - Project conventions (branch pattern, commit format, test commands)
// - Clear step-by-step instructions for implementation
//
// The prompt is designed to guide the LLM to:
// 1. Create a feature branch following the project's naming pattern
// 2. Implement the solution using TDD if required
// 3. Run tests and linter
// 4. Create a pull request
// 5. Report the branch name and PR number
func (w *ClaudeCodeWorker) buildPrompt(task *taskqueue.Task, ruleset *conventions.Ruleset) string {
	var sb strings.Builder

	// Header
	sb.WriteString("You are an autonomous code agent implementing a GitHub Issue.\n\n")

	// Task details
	sb.WriteString("## TASK DETAILS\n\n")
	fmt.Fprintf(&sb, "**Repository:** %s/%s\n", task.RepoOwner, task.RepoName)
	fmt.Fprintf(&sb, "**Issue #%d:** %s\n\n", task.IssueNumber, task.Title)
	sb.WriteString("**Description:**\n")
	sb.WriteString(task.Description)
	sb.WriteString("\n\n")

	// Project conventions
	sb.WriteString("## PROJECT CONVENTIONS\n\n")
	fmt.Fprintf(&sb, "**Project Path:** %s\n\n", ruleset.ProjectPath)

	if ruleset.HasBranchPattern() {
		fmt.Fprintf(&sb, "**Branch Naming Pattern:** %s\n", ruleset.BranchPattern)
		sb.WriteString("Example: Replace {ticket} with issue number, {summary} with brief description\n\n")
	}

	if ruleset.HasCommitPattern() {
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

	// Implementation instructions
	sb.WriteString("## IMPLEMENTATION INSTRUCTIONS\n\n")
	sb.WriteString("Follow these steps in order:\n\n")

	sb.WriteString("1. **Create Feature Branch**\n")
	if ruleset.HasBranchPattern() {
		fmt.Fprintf(&sb, "   - Use the branch pattern: %s\n", ruleset.BranchPattern)
		fmt.Fprintf(&sb, "   - For this task: Replace {ticket} with %d\n", task.IssueNumber)
	} else {
		fmt.Fprintf(&sb, "   - Create branch: feature/issue-%d-<brief-summary>\n", task.IssueNumber)
	}
	sb.WriteString("\n")

	sb.WriteString("2. **Implement Solution**\n")
	if ruleset.TDDRequired {
		sb.WriteString("   - Write tests FIRST (TDD required)\n")
		sb.WriteString("   - Watch tests fail\n")
		sb.WriteString("   - Implement code to make tests pass\n")
	} else {
		sb.WriteString("   - Implement the solution described in the task\n")
		sb.WriteString("   - Follow project conventions and patterns\n")
	}
	sb.WriteString("   - Ensure all acceptance criteria are met\n")
	sb.WriteString("\n")

	sb.WriteString("3. **Run Quality Checks**\n")
	fmt.Fprintf(&sb, "   - Run tests: %s\n", ruleset.TestCommand)
	fmt.Fprintf(&sb, "   - Run linter: %s\n", ruleset.LintCommand)
	fmt.Fprintf(&sb, "   - Run formatter: %s\n", ruleset.FormatCommand)
	sb.WriteString("   - Fix any failures before proceeding\n")
	sb.WriteString("\n")

	sb.WriteString("4. **Create Pull Request**\n")
	sb.WriteString("   - Write clear PR title and description\n")
	sb.WriteString("   - Reference this issue in the PR description\n")
	sb.WriteString("   - Include testing evidence (test names, coverage)\n")
	sb.WriteString("\n")

	sb.WriteString("5. **Report Results**\n")
	sb.WriteString("   - In your response, include:\n")
	sb.WriteString("     Branch: <branch-name>\n")
	sb.WriteString("     PR: #<pr-number>\n")
	sb.WriteString("\n")

	// Final notes
	sb.WriteString("## IMPORTANT NOTES\n\n")
	sb.WriteString("- Follow ALL project conventions strictly\n")
	sb.WriteString("- Do NOT create any forbidden files\n")
	sb.WriteString("- Ensure tests pass before creating PR\n")
	sb.WriteString("- Be thorough but concise in implementation\n")
	sb.WriteString("- Focus on meeting acceptance criteria exactly\n")

	return sb.String()
}

// failResult is a helper that marks a task as failed, updates stats, and returns a failure Result.
func (w *ClaudeCodeWorker) failResult(task *taskqueue.Task, err error) *Result {
	// Update task to Failed status
	task.Status = taskqueue.StatusFailed
	task.ErrorMsg = err.Error()
	task.CompletedAt = time.Now()

	// Try to update task in queue (best effort)
	ctx := context.Background()
	if updateErr := w.queue.Update(ctx, task); updateErr != nil {
		// Log but don't fail on update error
		fmt.Printf("Warning: failed to update task status to failed: %v\n", updateErr)
	}

	// Update stats
	w.mu.Lock()
	w.tasksFailed++
	w.mu.Unlock()

	return &Result{
		TaskID:     task.ID,
		Success:    false,
		BranchName: "",
		PRNumber:   0,
		ErrorMsg:   err.Error(),
		LogsPath:   "",
	}
}

// extractBranchName extracts the branch name from an LLM response.
// Looks for patterns like "Branch: feature/KAI-123-summary" or "Created branch: ..."
func extractBranchName(response string) string {
	// Try pattern: "Branch: <name>" or "branch: <name>"
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)

		if strings.HasPrefix(lower, "branch:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}

		if strings.Contains(lower, "created branch") {
			// Extract branch name after "created branch"
			idx := strings.Index(lower, "created branch")
			if idx >= 0 {
				rest := line[idx+len("created branch"):]
				rest = strings.TrimSpace(rest)
				// Take first word (branch name)
				fields := strings.Fields(rest)
				if len(fields) > 0 {
					return fields[0]
				}
			}
		}
	}

	// Try pattern: Lines starting with feature/, fix/, etc.
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "feature/") || strings.HasPrefix(line, "fix/") ||
			strings.HasPrefix(line, "chore/") || strings.HasPrefix(line, "docs/") {
			return line
		}
	}

	return ""
}

// extractPRNumber extracts the PR number from an LLM response.
// Looks for patterns like "PR: #123" or "Pull Request: #456"
func extractPRNumber(response string) int {
	lines := strings.Split(response, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)

		// Pattern: "PR: #123" or "pr: #123"
		if strings.HasPrefix(lower, "pr:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				numStr := strings.TrimSpace(parts[1])
				numStr = strings.TrimPrefix(numStr, "#")
				var num int
				if _, err := fmt.Sscanf(numStr, "%d", &num); err == nil {
					return num
				}
			}
		}

		// Pattern: "Pull Request: #123" or "pull request #123"
		if strings.Contains(lower, "pull request") {
			// Find the # symbol and extract number after it
			idx := strings.Index(line, "#")
			if idx >= 0 {
				numStr := line[idx+1:]
				// Take digits only
				var num int
				if _, err := fmt.Sscanf(numStr, "%d", &num); err == nil {
					return num
				}
			}
		}
	}

	return 0
}
