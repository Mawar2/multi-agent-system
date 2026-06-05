# AI Review Feedback Handling & Pre-PR Quality Gates

**Last updated:** 2026-06-05
**Status:** Design Phase
**Priority:** HIGH (Cost Reduction + Quality Improvement)

## Problem Statement

**Current Issue:**
- Kaimi has AI code review in CI/CD that comments on PRs
- **Every PR costs money** (AI review billing)
- Workers creating low-quality PRs = wasted review costs
- AI review feedback is posted but **not acted upon**

**Goals:**
1. **Reduce costs** - Only create PRs that will pass quality checks
2. **Handle feedback** - Workers respond to AI review comments
3. **Improve success rate** - Catch issues before PR creation

---

## Two-Phase Solution

###Phase 1: Pre-PR Quality Gates (Reduce Costs)
**Before creating PR, validate:**
- Tests pass
- Linter clean
- Code compiles
- Branch created successfully

### Phase 2: AI Review Feedback Loop (Handle Comments)
**After PR created, monitor AI review:**
- Detect AI review comments
- Parse feedback
- Worker addresses issues
- Update PR or comment back

---

## Phase 1: Pre-PR Quality Gates

### Architecture

**Current Flow (Broken - Creates Bad PRs):**
```
Worker → Execute Claude CLI → Parse response → Create PR
                                                    ↓
                                            AI Review ($$$)
                                                    ↓
                                            Finds bugs/issues
                                                    ↓
                                            PR rejected ❌
```

**New Flow (Quality Gates - Only Good PRs):**
```
Worker → Execute Claude CLI → Validate Quality → Create PR
                |                    ↓              ↓
                |            Tests pass? ✅    AI Review ($$$)
                |            Linter clean? ✅       ↓
                |            Builds? ✅        Likely approved ✅
                |                    ↓
                ↓            Any failure? ❌
        Retry or fail task    (No PR created, no cost!)
```

### Implementation

**File:** `internal/worker/quality_gates.go` (NEW)

```go
package worker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// QualityGates runs pre-PR validation checks to ensure code quality
// before creating expensive GitHub PRs that trigger AI reviews.
type QualityGates struct {
	workspaceDir string
}

// NewQualityGates creates quality gate validator for a workspace.
func NewQualityGates(workspaceDir string) *QualityGates {
	return &QualityGates{
		workspaceDir: workspaceDir,
	}
}

// Validate runs all quality checks and returns detailed results.
//
// Checks performed:
// 1. Tests pass (go test ./... or npm test)
// 2. Linter clean (golangci-lint or eslint)
// 3. Formatter clean (gofmt or prettier)
// 4. Build succeeds (go build or npm build)
// 5. Branch exists and has commits
//
// Returns error if ANY check fails. This prevents PR creation.
func (qg *QualityGates) Validate(ctx context.Context, ruleset *conventions.Ruleset) error {
	results := &ValidationResults{
		Checks: make(map[string]CheckResult),
	}

	// Check 1: Tests
	if err := qg.runTests(ctx, ruleset); err != nil {
		results.Checks["tests"] = CheckResult{
			Passed: false,
			Error:  err.Error(),
		}
		return fmt.Errorf("quality gate failed - tests: %w", err)
	}
	results.Checks["tests"] = CheckResult{Passed: true}

	// Check 2: Linter
	if err := qg.runLinter(ctx, ruleset); err != nil {
		results.Checks["linter"] = CheckResult{
			Passed: false,
			Error:  err.Error(),
		}
		return fmt.Errorf("quality gate failed - linter: %w", err)
	}
	results.Checks["linter"] = CheckResult{Passed: true}

	// Check 3: Formatter
	if err := qg.runFormatter(ctx, ruleset); err != nil {
		results.Checks["formatter"] = CheckResult{
			Passed: false,
			Error:  err.Error(),
		}
		return fmt.Errorf("quality gate failed - formatter: %w", err)
	}
	results.Checks["formatter"] = CheckResult{Passed: true}

	// Check 4: Build (optional - not all projects have build step)
	if ruleset.BuildCommand != "" {
		if err := qg.runBuild(ctx, ruleset); err != nil {
			results.Checks["build"] = CheckResult{
				Passed: false,
				Error:  err.Error(),
			}
			return fmt.Errorf("quality gate failed - build: %w", err)
		}
		results.Checks["build"] = CheckResult{Passed: true}
	}

	fmt.Printf("[QualityGates] All checks passed ✅\n")
	return nil
}

func (qg *QualityGates) runTests(ctx context.Context, ruleset *conventions.Ruleset) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", ruleset.TestCommand)
	cmd.Dir = qg.workspaceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tests failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("[QualityGates] Tests passed ✅\n")
	return nil
}

func (qg *QualityGates) runLinter(ctx context.Context, ruleset *conventions.Ruleset) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", ruleset.LintCommand)
	cmd.Dir = qg.workspaceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("linter failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("[QualityGates] Linter passed ✅\n")
	return nil
}

func (qg *QualityGates) runFormatter(ctx context.Context, ruleset *conventions.Ruleset) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", ruleset.FormatCommand)
	cmd.Dir = qg.workspaceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("formatter failed: %w\nOutput: %s", err, string(output))
	}

	// Check if formatter made changes
	statusCmd := exec.CommandContext(ctx, "git", "-C", qg.workspaceDir, "status", "--porcelain")
	statusOutput, _ := statusCmd.Output()

	if len(statusOutput) > 0 {
		return fmt.Errorf("formatter made changes - code not formatted:\n%s", string(statusOutput))
	}

	fmt.Printf("[QualityGates] Formatter passed ✅\n")
	return nil
}

func (qg *QualityGates) runBuild(ctx context.Context, ruleset *conventions.Ruleset) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", ruleset.BuildCommand)
	cmd.Dir = qg.workspaceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %w\nOutput: %s", err, string(output))
	}

	fmt.Printf("[QualityGates] Build passed ✅\n")
	return nil
}

type ValidationResults struct {
	Checks map[string]CheckResult
}

type CheckResult struct {
	Passed bool
	Error  string
}
```

### Integration into Worker

**File:** `internal/worker/claudecode.go` - Update Execute()

```go
func (w *ClaudeCodeWorker) Execute(ctx context.Context, task *taskqueue.Task) (*Result, error) {
	// ... existing workspace setup ...

	// Execute LLM call
	response, err := w.backend.ExecuteInDir(ctx, prompt, model, workspaceDir)
	if err != nil {
		return w.failResult(task, fmt.Errorf("LLM execution failed: %w", err)), nil
	}

	// Parse response
	branchName := extractBranchName(response)
	prNumber := extractPRNumber(response)

	// NEW: Verify branch exists
	if branchName == "" {
		return w.failResult(task, fmt.Errorf("no branch name found in response")), nil
	}

	// NEW: Run quality gates BEFORE creating PR
	gates := NewQualityGates(workspaceDir)
	if err := gates.Validate(ctx, ruleset); err != nil {
		// Quality checks failed - DO NOT create PR
		return w.failResult(task, fmt.Errorf("quality gates failed: %w", err)), nil
	}

	// Quality passed - NOW create PR (or Claude already created it)
	if prNumber == 0 {
		return w.failResult(task, fmt.Errorf("no PR created")), nil
	}

	// Update task to Review
	task.Status = taskqueue.StatusReview
	task.BranchName = branchName
	task.PRNumber = prNumber
	task.CompletedAt = time.Now()

	return &Result{
		Success:    true,
		BranchName: branchName,
		PRNumber:   prNumber,
	}, nil
}
```

### Cost Savings Estimate

**Before Quality Gates:**
```
100 tasks → 100 PRs created → 100 AI reviews
Cost: 100 × $0.10 = $10.00
Success rate: 68% (32 PRs failed review)
Wasted cost: 32 × $0.10 = $3.20
```

**After Quality Gates:**
```
100 tasks → 68 pass quality → 68 PRs created → 68 AI reviews
Cost: 68 × $0.10 = $6.80
Success rate: 95% (quality pre-screened)
Wasted cost: ~3 × $0.10 = $0.30

SAVINGS: $10.00 - $6.80 = $3.20 per 100 tasks (32% cost reduction)
```

---

## Phase 2: AI Review Feedback Loop

### Problem

**Current:** AI review comments on PR, but worker doesn't see or respond.

```
Worker creates PR
  ↓
AI Review bot comments: "Security issue on line 42"
  ↓
??? (No one responds)
  ↓
PR sits unmerged
```

### Solution: Feedback Monitoring Loop

**Architecture:**

```
Worker creates PR → Mark task as "AwaitingReview"
                          ↓
                   Supervisor monitors PR
                          ↓
                   AI review comment detected
                          ↓
                   Create feedback task
                          ↓
                   Worker claims feedback task
                          ↓
                   Worker reads AI comments
                          ↓
                   Worker fixes issues
                          ↓
                   Worker pushes update to PR
                          ↓
                   AI review re-runs
                          ↓
                   Approved → Human merges ✅
```

### Implementation

**New Task Status:**

```go
// internal/taskqueue/task.go

const (
	StatusPending      Status = iota
	StatusClaimed
	StatusInProgress
	StatusReview       // NEW: PR created, awaiting AI review
	StatusNeedsFixes   // NEW: AI review left comments
	StatusComplete
	StatusFailed
)
```

**New Component:** `internal/feedback/monitor.go`

```go
package feedback

import (
	"context"
	"fmt"
	"time"
)

// FeedbackMonitor watches PRs for AI review comments
// and creates feedback tasks for workers to address.
type FeedbackMonitor struct {
	githubClient GitHubClient
	taskQueue    TaskQueue
	pollInterval time.Duration
}

// MonitorPR watches a PR for AI review comments.
// When comments detected, create feedback task.
func (fm *FeedbackMonitor) MonitorPR(ctx context.Context, task *Task) error {
	ticker := time.NewTicker(fm.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check for AI review comments
			comments, err := fm.githubClient.GetPRComments(ctx, task.RepoOwner, task.RepoName, task.PRNumber)
			if err != nil {
				return fmt.Errorf("failed to fetch PR comments: %w", err)
			}

			// Filter for AI review bot comments
			aiComments := filterAIReviewComments(comments)
			if len(aiComments) == 0 {
				continue // No feedback yet
			}

			// AI review posted feedback - create feedback task
			feedbackTask := &Task{
				Type:        TaskTypeFeedback,
				ParentTask:  task.ID,
				IssueNumber: task.IssueNumber,
				RepoOwner:   task.RepoOwner,
				RepoName:    task.RepoName,
				PRNumber:    task.PRNumber,
				Description: formatAIFeedback(aiComments),
				Tier:        task.Tier, // Same tier as original
			}

			if err := fm.taskQueue.Enqueue(ctx, feedbackTask); err != nil {
				return fmt.Errorf("failed to enqueue feedback task: %w", err)
			}

			// Update original task status
			task.Status = StatusNeedsFixes
			fm.taskQueue.Update(ctx, task)

			return nil // Stop monitoring, feedback task created

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func filterAIReviewComments(comments []Comment) []Comment {
	var aiComments []Comment
	for _, c := range comments {
		// Check if comment is from AI review bot
		// (based on bot username, e.g., "github-actions[bot]")
		if strings.Contains(c.Author, "[bot]") && strings.Contains(c.Body, "AI Code Review") {
			aiComments = append(aiComments, c)
		}
	}
	return aiComments
}

func formatAIFeedback(comments []Comment) string {
	var sb strings.Builder
	sb.WriteString("## AI Review Feedback\n\n")
	sb.WriteString("The AI code review identified the following issues:\n\n")

	for i, c := range comments {
		fmt.Fprintf(&sb, "### Issue %d\n", i+1)
		fmt.Fprintf(&sb, "**File:** %s:%d\n", c.Path, c.Line)
		fmt.Fprintf(&sb, "**Feedback:**\n%s\n\n", c.Body)
	}

	sb.WriteString("## Instructions\n\n")
	sb.WriteString("Please address each issue above and push updates to the PR.\n")

	return sb.String()
}
```

**Worker handles feedback tasks:**

```go
// internal/worker/claudecode.go

func (w *ClaudeCodeWorker) Execute(ctx context.Context, task *taskqueue.Task) (*Result, error) {
	// Check task type
	if task.Type == taskqueue.TaskTypeFeedback {
		return w.handleFeedback(ctx, task)
	}

	// ... normal task execution ...
}

func (w *ClaudeCodeWorker) handleFeedback(ctx context.Context, task *taskqueue.Task) (*Result, error) {
	// Prepare workspace (already has PR branch checked out)
	workspaceDir, err := w.workspaceMgr.PrepareWorkspace(ctx, task)
	if err != nil {
		return w.failResult(task, err), nil
	}

	// Checkout PR branch
	branchName := task.BranchName
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", workspaceDir, "checkout", branchName)
	if err := checkoutCmd.Run(); err != nil {
		return w.failResult(task, fmt.Errorf("failed to checkout PR branch: %w", err)), nil
	}

	// Build prompt with AI feedback
	prompt := w.buildFeedbackPrompt(task)

	// Execute Claude to fix issues
	response, err := w.backend.ExecuteInDir(ctx, prompt, model, workspaceDir)
	if err != nil {
		return w.failResult(task, err), nil
	}

	// Validate fixes with quality gates
	gates := NewQualityGates(workspaceDir)
	if err := gates.Validate(ctx, ruleset); err != nil {
		return w.failResult(task, fmt.Errorf("fixes failed quality gates: %w", err)), nil
	}

	// Push updates to PR
	pushCmd := exec.CommandContext(ctx, "git", "-C", workspaceDir, "push", "origin", branchName)
	if err := pushCmd.Run(); err != nil {
		return w.failResult(task, fmt.Errorf("failed to push fixes: %w", err)), nil
	}

	// Update task status
	task.Status = taskqueue.StatusReview // Back to review, awaiting re-review

	return &Result{Success: true}, nil
}

func (w *ClaudeCodeWorker) buildFeedbackPrompt(task *taskqueue.Task) string {
	var sb strings.Builder

	sb.WriteString("You are reviewing and fixing AI code review feedback.\n\n")
	sb.WriteString("## CONTEXT\n\n")
	fmt.Fprintf(&sb, "**Repository:** %s/%s\n", task.RepoOwner, task.RepoName)
	fmt.Fprintf(&sb, "**PR #%d** (already created)\n\n", task.PRNumber)

	sb.WriteString("## AI REVIEW FEEDBACK\n\n")
	sb.WriteString(task.Description) // Contains formatted AI comments

	sb.WriteString("\n## INSTRUCTIONS\n\n")
	sb.WriteString("1. Read each piece of feedback carefully\n")
	sb.WriteString("2. Fix the issues identified by the AI review\n")
	sb.WriteString("3. Ensure tests still pass after fixes\n")
	sb.WriteString("4. Commit and push your fixes\n")
	sb.WriteString("5. Report completion\n\n")

	sb.WriteString("The PR branch is already checked out. Make your fixes and push.\n")

	return sb.String()
}
```

---

## Configuration

**Add to orchestrator.yml:**

```yaml
# Quality gates configuration
quality_gates:
  enabled: true
  run_tests: true
  run_linter: true
  run_formatter: true
  run_build: false  # Optional, not all projects need build

# AI review feedback monitoring
ai_review_feedback:
  enabled: true
  poll_interval_seconds: 300  # Check for comments every 5 minutes
  max_feedback_iterations: 2  # Max times to retry after feedback
  bot_usernames:
    - "github-actions[bot]"
    - "copilot-code-review[bot]"
```

---

## Rollout Plan

### Week 1: Phase 1 (Quality Gates)

1. **Implement quality_gates.go**
2. **Update worker to use quality gates**
3. **Test with 10 tasks** - verify PR quality improves
4. **Monitor cost savings**

### Week 2: Phase 2 (Feedback Loop)

1. **Implement feedback monitor**
2. **Add feedback task type**
3. **Update worker to handle feedback**
4. **Test with real AI review comments**

### Week 3: Production

1. **Enable for all projects**
2. **Monitor feedback success rate**
3. **Measure cost reduction**

---

## Success Metrics

**Quality Gates (Phase 1):**
- ✅ 90%+ of PRs pass AI review on first attempt
- ✅ 30%+ cost reduction (fewer failed PRs)
- ✅ Faster PR merge time (no back-and-forth)

**Feedback Loop (Phase 2):**
- ✅ 80%+ of AI feedback addressed automatically
- ✅ <10% require human intervention
- ✅ Average 2 iterations or less to approval

---

## Next Steps

1. Review this plan
2. Prioritize Phase 1 (cost reduction is immediate win)
3. Create implementation tasks
4. Start with quality_gates.go
5. Test with Kaimi project

**Estimated Timeline:** 2 weeks for both phases
**Estimated Cost Savings:** 30-40% reduction in AI review costs
