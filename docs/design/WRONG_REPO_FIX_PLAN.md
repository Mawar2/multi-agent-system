# Wrong Repository PR Fix Plan

**Last updated:** 2026-06-05
**Status:** Investigation Complete

## Problem Statement

Kaimi GitHub issues (e.g., #4, #6) are creating pull requests in the **multi-agent-system** repository instead of the **Kaimi** repository.

**Evidence:**
- Feature branches: `feature/issue-6-kai-6-final-review-skeleton`, `feature/issue-4-formatting-rules`
- These were Kaimi tickets but PRs appeared in multi-agent-system repo
- Task JSON files correctly show `"repo_owner": "Mawar2"` and `"repo_name": "Kaimi"`

## Root Cause Analysis

### Investigation Findings

1. **Multi-agent orchestration code location**
   - Orchestration system (supervisor, workers) built inside `Pulse` directory (Kaimi repo)
   - All orchestration code is **untracked** (not committed to Kaimi repo)
   - See `git status`: `tasks/`, `internal/outline/`, etc. are untracked

2. **Git remote configuration in Pulse directory**
   ```
   origin → https://github.com/Mawar2/multi-agent-system.git (fetch/push)
   kaimi  → https://github.com/Mawar2/Kaimi.git (fetch/push)
   ```

3. **Workflow that causes the issue**
   - Supervisor runs in Pulse directory
   - Worker spawns `claude --print --model sonnet` subprocess
   - Claude Code CLI runs in Pulse directory (inherits working directory)
   - Task JSON says "work on Kaimi issue #6"
   - BUT Claude runs in directory where `git remote origin` = multi-agent-system
   - Claude creates PR using `origin` → PR goes to wrong repo

4. **Why this happens**
   - Claude Code CLI doesn't know about the target repo from task metadata
   - It uses the git remote of the current working directory
   - The prompt includes conventions from `CLAUDE.md` but doesn't override git context
   - Claude executes: `git checkout -b feature/...`, `git push origin`, `gh pr create`
   - All these use `origin` which is multi-agent-system

### Architectural Issue

**The fundamental problem:** Workers run in the orchestrator's directory, not the target project's directory.

```
Current (broken):
  Supervisor (Pulse dir)
    └── Worker spawns Claude CLI (inherits Pulse dir)
        └── Claude works on Kaimi issue (but git context is Pulse/multi-agent-system)
            └── PR created in multi-agent-system ❌

Desired (fixed):
  Supervisor (anywhere)
    └── Worker clones/uses target repo (Kaimi dir)
        └── Claude CLI runs in Kaimi directory
            └── PR created in Kaimi ✅
```

## Immediate Fix (Short-term)

**Goal:** Stop creating PRs in wrong repo TODAY

### Option A: Change Pulse Directory Remote (Fastest - 5 minutes)

**Pros:**
- Immediate fix
- No code changes needed
- Works for current run

**Cons:**
- Only works if all issues are for same repo (Kaimi)
- Breaks multi-project support
- Temporary hack

**Steps:**
```bash
cd C:\Users\Owner\OneDrive\Documents\Builder\Pulse
git remote set-url origin https://github.com/Mawar2/Kaimi.git
git remote set-url kaimi https://github.com/Mawar2/multi-agent-system.git
```

Now `origin` points to Kaimi. All PRs will go there.

### Option B: Pass Repo Context to Claude CLI (Better - 2 hours)

**Modify worker to:**
1. Clone target repo into temp directory
2. Pass `--work-dir` or run Claude CLI from that directory
3. Clean up after completion

**Code changes needed:**
- `internal/worker/claudecode.go` - Add repo cloning before Execute()
- `internal/llm/claude_code.go` - Add working directory parameter

**Implementation:**
```go
// In claudecode.go Execute()
func (w *ClaudeCodeWorker) Execute(ctx context.Context, task *taskqueue.Task) (*Result, error) {
    // 1. Clone target repo to temp directory
    workDir, err := cloneTargetRepo(task.RepoOwner, task.RepoName)
    if err != nil {
        return nil, fmt.Errorf("failed to clone target repo: %w", err)
    }
    defer os.RemoveAll(workDir)

    // 2. Pass workDir to Claude CLI backend
    prompt := w.buildPrompt(task, ruleset)
    response, err := w.backend.ExecuteInDir(ctx, prompt, model, workDir)

    // ... rest unchanged
}

// In claude_code.go
func (b *ClaudeCodeBackend) ExecuteInDir(ctx context.Context, prompt string, model string, workDir string) (string, error) {
    cmd := exec.CommandContext(ctx, "claude", "--print", "--model", modelAlias)
    cmd.Dir = workDir  // SET WORKING DIRECTORY
    cmd.Stdin = bytes.NewBufferString(prompt)
    // ... rest unchanged
}

func cloneTargetRepo(owner, repo string) (string, error) {
    tmpDir, _ := os.MkdirTemp("", fmt.Sprintf("agent-work-%s-%s-*", owner, repo))

    cmd := exec.Command("git", "clone",
        fmt.Sprintf("https://github.com/%s/%s.git", owner, repo),
        tmpDir)

    if err := cmd.Run(); err != nil {
        return "", err
    }

    return tmpDir, nil
}
```

## Recommended Immediate Action

**Use Option A NOW** to stop the bleeding:
1. Stop supervisor (already done)
2. Change git remote in Pulse directory to point to Kaimi
3. Restart supervisor
4. Verify next PR goes to Kaimi

Then implement Option B (proper fix) before expanding to multi-project.

## Verification Steps

After applying fix:

1. **Create test issue in Kaimi** - Simple ticket (e.g., "Add comment to README")
2. **Run supervisor** with single worker
3. **Monitor worker output** - Check what repo it's working in
4. **Verify PR created in Kaimi** - Check GitHub for new PR
5. **Verify branch exists in Kaimi** - `gh pr view` should show Kaimi PR

## Long-term Solution

This is addressed in **STANDALONE_ORCHESTRATOR_PLAN.md** which separates the orchestration system into its own repository and properly handles multi-project routing.

---

## Implementation Timeline

**Immediate (today):**
- Apply Option A (5 min)
- Verify with test issue (15 min)
- Resume orchestration run

**Short-term (this week):**
- Implement Option B (2 hours)
- Test with multiple Kaimi issues
- Verify all PRs go to correct repo

**Long-term (next 2 weeks):**
- Separate orchestrator into standalone repo
- Implement per-project working directories
- Add multi-repo support
