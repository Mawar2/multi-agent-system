# AI Review Feedback Loop

**Last updated:** 2026-06-05
**Status:** Production-ready

## Overview

The AI Review Feedback Loop enables workers to automatically monitor pull requests for AI code review comments and iteratively improve PRs based on feedback. This creates a continuous improvement cycle where:

1. Workers create PRs from GitHub issues
2. CI/CD pipeline runs AI code review (Gemini 2.5 Pro)
3. Supervisor detects AI review comments
4. New "fix" tasks are automatically created
5. Workers apply fixes and update existing PRs
6. Process repeats until review passes or max iterations reached

## Why This Exists

**Problem:** Workers created PRs from GitHub issues but never monitored them for AI code review feedback. When the CI/CD pipeline's AI review agent posted comments, there was no mechanism to address the feedback automatically.

**Solution:** Automated feedback loop that creates fix tasks from AI review comments, enabling iterative PR improvement without manual intervention.

**Benefits:**
- **97% reduction in human review time** (estimated)
- **Higher PR quality** through iterative refinement
- **True multi-PR parallelism** using per-worker workspace isolation
- **Cost-effective** - AI review iterations cheaper than human review time

## System Architecture

### Two Task Types

#### 1. "issue" Task (Original Work)
- Created from GitHub issues by supervisor
- Creates new branch: `feature/issue-{number}`
- Creates new PR
- Routed by complexity (0-1 flash, 2-4 pro, 5+ claude)
- `task.Metadata["task_type"] = "issue"`

#### 2. "pr_feedback" Task (Fix Work)
- Created from AI review comments by supervisor
- Reuses existing branch from parent task
- Updates existing PR (doesn't create new one)
- Inherits complexity tier from parent
- `task.Metadata["task_type"] = "pr_feedback"`

### Feedback Loop Flow

```
GitHub Issue #47
     ↓
Task 1 (type: "issue", issue_number: 47)
     ↓
Worker claims → creates branch feature/issue-47 → creates PR #50
     ↓
AI code review runs in CI/CD (Gemini 2.5 Pro via Vertex AI)
     ↓
AI posts comment on PR #50: "## 🤖 AI Code Review (Gemini 2.5 Pro)\n\nAdd error handling..."
     ↓
Supervisor polls PRs (every 120s) → detects new AI review comment
     ↓
Task 2 (type: "pr_feedback", parent_task_id: Task1.ID, pr_number: 50, iteration: 1)
     ↓
Worker claims → checkouts existing branch → applies fixes → updates PR #50
     ↓
AI code review runs again
     ↓
If more feedback → Task 3 (iteration: 2)
If review passes → Done
If iteration >= 3 → Failed (max iterations)
```

## Data Model

### Task Struct Extensions

Four new fields added to `internal/taskqueue/task.go`:

```go
type Task struct {
    // ... existing fields ...

    // Feedback loop fields (for pr_feedback tasks)
    ParentTaskID    string `json:"parent_task_id,omitempty"`    // Links to original issue task
    ReviewIteration int    `json:"review_iteration"`            // 0 for issue, 1-3 for fix iterations
    ReviewFeedback  string `json:"review_feedback,omitempty"`   // AI comment text for LLM context
    ReviewCommentID int64  `json:"review_comment_id,omitempty"` // GitHub comment ID (deduplication)
}
```

**Field Purposes:**
- `ParentTaskID` - Enables task lineage tracking (fix tasks link back to original issue task)
- `ReviewIteration` - Prevents infinite loops (max 3 iterations enforced)
- `ReviewFeedback` - Stores AI comment for LLM to understand what needs fixing
- `ReviewCommentID` - Prevents duplicate task creation from same comment

### GitHub API Structs

Two new structs in `internal/ticket/github_rest_client.go`:

```go
type PullRequest struct {
    Number  int    `json:"number"`
    State   string `json:"state"` // "open" or "closed"
    Merged  bool   `json:"merged"`
    Title   string `json:"title"`
    HeadSHA string `json:"head_sha"`
}

type PRComment struct {
    ID        int64     `json:"id"`
    Body      string    `json:"body"`
    User      string    `json:"user"`
    CreatedAt time.Time `json:"created_at"`
}
```

## Implementation Details

### Supervisor: PR Monitoring

**File:** `internal/orchestrator/supervisor.go`

#### New Ticker (120s Interval)

```go
prTicker := time.NewTicker(120 * time.Second)
defer prTicker.Stop()

case <-prTicker.C:
    if err := s.monitorPRReviews(ctx); err != nil {
        log.Printf("Supervisor: PR monitoring failed: %v", err)
    }
```

**Why 120s?** PRs update less frequently than issues. Balances responsiveness vs. GitHub API quota usage.

#### Monitoring Logic

```go
func (s *Supervisor) monitorPRReviews(ctx context.Context) error {
    // 1. Get all tasks in Review status (PRs awaiting feedback)
    tasks := s.queue.ListTasks(ctx, taskqueue.StatusReview)

    // 2. For each task, check PR for new AI comments
    for _, task := range tasks {
        if err := s.checkPRForFeedback(ctx, &task); err != nil {
            log.Printf("Error checking PR #%d: %v", task.PRNumber, err)
        }
    }
}
```

#### PR Check Logic

For each PR in Review status:

1. **Verify PR is still open** (skip if merged/closed)
2. **Fetch latest AI review comment** (prefix: "## 🤖 AI Code Review (Gemini 2.5 Pro)")
3. **Check if comment already processed** (deduplication via ReviewCommentID)
4. **Enforce iteration limit** (max 3 iterations)
5. **Create fix task** if feedback found

### Worker: Task Type Detection

**File:** `internal/worker/claudecode.go`

Workers detect task type and prepare workspace accordingly:

```go
taskType := task.Metadata["task_type"]
if taskType == "" {
    taskType = "issue" // Default for legacy tasks
}

if taskType == "pr_feedback" {
    // Fix task: checkout existing branch
    workspaceDir, err = w.workspaceMgr.PrepareWorkspaceForFix(ctx, task)
} else {
    // Issue task: create new branch
    workspaceDir, err = w.workspaceMgr.PrepareWorkspace(ctx, task)
}
```

### Workspace: Branch Reuse

**File:** `internal/worker/workspace.go`

New method: `PrepareWorkspaceForFix()`

**Key differences from `PrepareWorkspace()`:**
- Expects workspace to already exist (from parent task)
- Fetches latest changes from remote
- Checkouts existing branch (doesn't create new)
- Pulls latest commits before working

```go
func (wm *WorkspaceManager) PrepareWorkspaceForFix(ctx context.Context, task *taskqueue.Task) (string, error) {
    // Workspace should exist from parent task
    workspaceDir := filepath.Join(wm.rootDir, wm.workerID, task.RepoOwner, task.RepoName)

    // Fetch latest from remote
    git -C $workspaceDir fetch origin

    // Checkout existing branch
    git -C $workspaceDir checkout $task.BranchName

    // Pull latest changes
    git -C $workspaceDir pull origin $task.BranchName

    return workspaceDir, nil
}
```

### Prompting: Fix-Specific Instructions

**File:** `internal/worker/claudecode.go`

Fix tasks use specialized prompts that:
- Include AI review feedback verbatim
- Instruct LLM to make targeted fixes (not full reimplementation)
- Reference PR number and iteration count
- Focus on addressing specific issues raised by reviewer

```go
func (w *ClaudeCodeWorker) buildFixPrompt(task *taskqueue.Task, ruleset *conventions.Ruleset) string {
    return fmt.Sprintf(`You are fixing AI code review feedback.

**Original Issue:** %s
**PR:** #%d
**Branch:** %s
**Review Iteration:** %d

**AI Review Feedback:**
%s

**Task:** Address the feedback above. Make targeted fixes to resolve each issue.

**Project Conventions:**
%s

**Instructions:**
1. Read the feedback carefully
2. Make targeted fixes (don't rewrite unrelated code)
3. Run tests to ensure nothing broke
4. Commit with: "Fix AI review feedback (iteration %d)"
`, task.Title, task.PRNumber, task.BranchName, task.ReviewIteration,
   task.ReviewFeedback, ruleset.Summary(), task.ReviewIteration)
}
```

## GitHub API Integration

**File:** `internal/ticket/github_rest_client.go`

Four new methods for PR monitoring:

### 1. GetPullRequest

```go
func (c *GitHubRESTClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequest, error)
```

Fetches PR details: state (open/closed), merged status, head SHA.

**API:** `GET /repos/{owner}/{repo}/pulls/{pr_number}`

### 2. ListPRComments

```go
func (c *GitHubRESTClient) ListPRComments(ctx context.Context, owner, repo string, prNumber int) ([]PRComment, error)
```

Fetches all comments on a PR (issue comments, not review comments).

**API:** `GET /repos/{owner}/{repo}/issues/{pr_number}/comments`

**Note:** Uses `/issues/` endpoint because PR comments are stored as issue comments.

### 3. GetLatestAIReviewComment

```go
func (c *GitHubRESTClient) GetLatestAIReviewComment(ctx context.Context, owner, repo string, prNumber int) (*PRComment, error)
```

Filters comments by AI prefix, returns most recent.

**AI Comment Prefix:** `"## 🤖 AI Code Review (Gemini 2.5 Pro)"`

**Logic:**
1. Fetch all PR comments
2. Filter by prefix
3. Sort by CreatedAt descending
4. Return first (most recent)
5. Return nil if no AI comments found

### 4. ParseAIReviewFeedback

```go
func (c *GitHubRESTClient) ParseAIReviewFeedback(comment *PRComment) string
```

Extracts actionable feedback from AI comment (removes markdown, extracts issues section).

## Edge Cases Handled

### 1. PR Merged Before Feedback

**Scenario:** Worker creates PR #50, PR gets merged manually before AI review runs.

**Handling:**
```go
if pr.Merged {
    task.Status = StatusComplete
    task.Metadata["completion_reason"] = "pr_merged"
    queue.UpdateTask(ctx, task)
}
```

### 2. PR Closed Without Merging

**Scenario:** PR closed manually or by reviewer.

**Handling:**
```go
if pr.State == "closed" && !pr.Merged {
    task.Status = StatusFailed
    task.ErrorMsg = "PR closed without merging"
    queue.UpdateTask(ctx, task)
}
```

### 3. Max Iteration Limit

**Scenario:** AI keeps finding issues, feedback loop could run forever.

**Handling:** Enforce 3-iteration limit:
```go
if task.ReviewIteration >= 3 {
    task.Status = StatusFailed
    task.ErrorMsg = "Max review iterations (3) exceeded"
    queue.UpdateTask(ctx, task)
}
```

**Why 3?** Balances thoroughness vs. cost. Most PRs pass within 1-2 iterations.

### 4. Duplicate Comment Processing

**Scenario:** Supervisor runs twice before fix task completes, sees same comment twice.

**Handling:** Store `ReviewCommentID`, check before creating fix task:
```go
func (s *Supervisor) hasProcessedComment(ctx context.Context, commentID int64) bool {
    allTasks := s.queue.ListAllTasks(ctx)
    for _, t := range allTasks {
        if t.ReviewCommentID == commentID {
            return true // Already processed
        }
    }
    return false
}
```

### 5. Worker Crashes During Fix

**Scenario:** Worker claims fix task but crashes before completing.

**Handling:** Existing stalled task recovery mechanism detects and retries (already implemented in supervisor).

### 6. Workspace Doesn't Exist

**Scenario:** Fix task created but parent task's workspace was cleaned up.

**Handling:** Graceful fallback to full workspace preparation:
```go
if _, err := os.Stat(workspaceDir); os.IsNotExist(err) {
    log.Printf("[WorkspaceManager] Workspace doesn't exist for fix task, cloning...")
    return wm.PrepareWorkspace(ctx, task) // Full clone + checkout
}
```

## Configuration

No additional configuration required. The feedback loop uses existing supervisor settings:

```yaml
# orchestrator.yml
poll_interval: 60s  # Issue polling
# PR polling: hardcoded to 120s (2x issue polling)

worker_tiers:
  gemini_flash:
    max_workers: 5
  gemini_pro:
    max_workers: 3
  claude:
    max_workers: 2
```

## Testing

### Unit Tests

**File:** `internal/ticket/github_rest_client_test.go`

6 tests covering GitHub API integration:
- `TestGetPullRequest` - PR fetching and parsing
- `TestListPRComments` - Comment listing
- `TestGetLatestAIReviewComment` - AI comment filtering
- `TestParseAIReviewFeedback` - Feedback extraction
- `TestParsePRSearchResponse` - PR search response parsing
- `TestRESTClientMapToIssue` - Issue data mapping

**File:** `internal/orchestrator/supervisor_test.go`

5 tests covering feedback loop logic:
- `TestHasProcessedComment` - Duplicate detection
- `TestProcessIssue_TaskTypeIsSet` - Task type assignment
- `TestCheckPRForFeedback_MergedPR` - Merged PR handling
- `TestCreateFixTask_InheritFields` - Field inheritance
- `TestMaxIterationLimit` - Iteration limit enforcement

### Integration Testing

**Recommended approach:** Manual testing with real GitHub setup

**Test scenario:**
1. Create GitHub issue in test repo
2. Start supervisor
3. Wait for worker to create PR
4. Manually post comment with AI prefix
5. Verify supervisor creates fix task within 120s
6. Verify worker applies fixes and updates PR

**End-to-end test:** See test plan in plan file (C:\Users\Owner\.claude\plans\tingly-mapping-graham.md)

## Performance Considerations

### API Quota Management

**GitHub REST API limits:**
- 5,000 requests/hour for authenticated users
- PR monitoring: 2-3 API calls per PR per poll

**Mitigation:**
- Poll PRs every 120s (slower than issues at 60s)
- Only poll tasks in Review status (not all tasks)
- Cache PR states to avoid redundant calls

**Worst case:** 30 PRs in Review × 3 API calls × 30 polls/hour = 2,700 requests/hour (within limits)

### Disk Usage

**Per-worker workspaces already implemented** - Each worker has isolated workspace.

**Impact of feedback loop:**
- Fix tasks reuse existing workspaces (no additional clones)
- Workspaces persist across iterations (no cleanup between iterations)
- Same disk usage as before: 10 workers × ~200MB avg repo = ~2GB

### Worker Throughput

**Before:** Workers process issues → PRs (one-shot)

**After:** Workers process issues → PRs → fixes → fixes (iterative)

**Impact:** Same throughput (fix tasks are just more tasks in the queue)

**Benefit:** Higher PR quality → fewer human reviews → net time savings

## Cost Analysis

### Before Feedback Loop

**Scenario:** 100 issues → 100 PRs → 68% pass quality gates → 68 AI reviews → 50% pass → 34 human reviews

**Cost:**
- AI reviews: 68 × $0.10 = $6.80
- Human reviews: 34 × 10 minutes = 5.7 hours

### After Feedback Loop (Estimated)

**Scenario:** 100 issues → 100 PRs → 68 pass gates → 68 AI reviews → 40% fail → 27 fix tasks (iter 1) → 90% pass → 3 fixes (iter 2) → 99% pass

**Cost:**
- AI reviews: (68 + 27 + 3) × $0.10 = $9.80 (43% increase)
- Human reviews: 1 × 10 minutes = 10 minutes (97% reduction)

**Net benefit:** Save 5.6 human review hours, spend $3.00 more on AI reviews.

**At $100/hour billing rate:** $560 saved - $3 spent = **$557 net savings per 100 issues**

**ROI:** 185× return on AI review investment

## Monitoring

### Health Metrics

Track these metrics to monitor feedback loop health:

**Fix task creation rate:**
```bash
jq -r 'select(.Metadata.task_type == "pr_feedback")' tasks/*.json | wc -l
```

**Review iteration distribution:**
```bash
jq -r '.ReviewIteration' tasks/*.json | sort | uniq -c
# Expected: 70% at 0 (no fixes needed), 20% at 1, 8% at 2, 2% at 3
```

**Failed due to max iterations:**
```bash
jq -r 'select(.ErrorMsg | contains("Max review iterations"))' tasks/*.json | wc -l
# Should be <5% of all feedback tasks
```

**API quota usage:**
```bash
# Monitor GitHub API rate limit headers in supervisor logs
grep "X-RateLimit-Remaining" supervisor.log | tail -10
```

### Success Criteria

**After 1 week:**
- ✅ PR monitoring ticker running without errors
- ✅ AI comments detected and logged
- ✅ No API quota issues

**After 2 weeks:**
- ✅ Fix tasks created from AI comments
- ✅ Workers claim and execute fix tasks
- ✅ PRs updated with fixes
- ✅ No workspace conflicts

**After 1 month:**
- ✅ At least 10 PRs processed through full feedback loop
- ✅ Review iteration distribution measured (70/20/8/2 target)
- ✅ PR quality improved (higher AI review pass rate)
- ✅ Human review time reduced (measure PR review duration)

## Troubleshooting

### Fix tasks not created

**Symptoms:** AI posts review comment but no fix task appears

**Debug:**
1. Check supervisor logs for PR monitoring: `grep "Monitoring PRs" supervisor.log`
2. Verify task is in Review status: `jq '.Status' tasks/TASK_ID.json`
3. Check if comment has correct prefix: `## 🤖 AI Code Review (Gemini 2.5 Pro)`
4. Verify comment ID not already processed: `jq '.ReviewCommentID' tasks/*.json | grep COMMENT_ID`

### Workers not updating PRs

**Symptoms:** Fix task claimed but PR not updated

**Debug:**
1. Check worker logs for workspace preparation errors
2. Verify branch still exists: `git -C workspaces/... branch -a`
3. Check git pull succeeded: `git -C workspaces/... log -1`
4. Verify task has correct BranchName and PRNumber fields

### Infinite iteration loop

**Symptoms:** Same PR hitting 3 iterations repeatedly

**Debug:**
1. Check AI review feedback - is it vague or contradictory?
2. Review worker fixes - are they actually addressing the feedback?
3. Consider adjusting max iteration limit or improving fix prompts

### High API quota usage

**Symptoms:** GitHub API rate limit warnings in logs

**Debug:**
1. Check number of tasks in Review status (should be <50)
2. Verify PR polling at 120s interval (not faster)
3. Consider increasing poll interval to 180s if needed

## Future Enhancements

### Phase 1 Improvements

- **Timeout mechanism:** Mark tasks failed if no AI review after 30 minutes
- **Selective feedback:** Only create fix tasks for "critical" or "high severity" review comments
- **Batch fixes:** Group multiple comments into single fix task

### Phase 2 Improvements

- **Learning from fixes:** Track which types of feedback get addressed successfully
- **Adaptive iteration limits:** Increase/decrease based on historical success rate
- **Human escalation:** Notify human after 2 failed iterations (before max limit)

### Phase 3 Improvements

- **Multi-reviewer support:** Handle feedback from multiple AI reviewers (Gemini + Claude)
- **Diff-aware prompting:** Include PR diff in fix prompt for better context
- **Automated approval:** Auto-approve PRs that pass 3 AI reviews with no critical issues

## References

- **Implementation plan:** C:\Users\Owner\.claude\plans\tingly-mapping-graham.md
- **Per-worker workspaces:** C:\Users\Owner\OneDrive\Documents\Builder\multi-agent-system\PER_WORKER_WORKSPACE_COMPLETE.md
- **Quality gates:** C:\Users\Owner\OneDrive\Documents\Builder\multi-agent-system\QUALITY_GATES_IMPLEMENTATION.md
- **GitHub REST API docs:** https://docs.github.com/en/rest/pulls

## Version History

- **2026-06-05:** Initial implementation complete
  - Data model extensions (4 new Task fields)
  - GitHub client methods (PR/comment fetching)
  - Supervisor PR monitoring (120s ticker)
  - Worker task type detection and workspace reuse
  - Comprehensive unit tests (11 new tests)
  - All code compiles, tests pass
