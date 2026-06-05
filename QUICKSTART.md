# Quick Start Guide

**Get the multi-agent orchestration system running in 5 minutes.**

## Prerequisites

- Go 1.21+ installed
- Git installed
- GitHub account with repository access
- GitHub Personal Access Token (see [MCP_SETUP.md](MCP_SETUP.md) for details)
- Claude Code CLI (already running locally)

## Step 1: Setup GitHub Access

The supervisor uses GitHub MCP to access repositories. Set your GitHub token:

```bash
# Linux/Mac
export GITHUB_TOKEN="ghp_your_token_here"

# Windows PowerShell
$env:GITHUB_TOKEN = "ghp_your_token_here"
```

**Get a token:** GitHub Settings → Developer settings → Personal access tokens → Generate new token (classic)
- Required scopes: `repo`, `read:org`, `workflow`

See [MCP_SETUP.md](MCP_SETUP.md) for detailed instructions.

## Step 2: Configure

Copy the example config and customize for your project:

```bash
cd C:\Users\Owner\OneDrive\Documents\Builder\Pulse\multi-agent-system
cp orchestrator.example.yml orchestrator.yml
```

Edit `orchestrator.yml`:
```yaml
projects:
  - name: your-project
    repo_owner: YourGitHubUser
    repo_name: YourRepo
    conventions_path: ./CLAUDE.md
    branch_pattern: "feature/{ticket}-{summary}"
    commit_pattern: "{ticket}_{description}"
```

## Step 3: Build

```bash
make build
```

This creates `bin/supervisor` binary.

## Step 4: Run

```bash
./bin/supervisor --config orchestrator.yml
```

**What happens:**
1. Supervisor polls GitHub Issues from configured repos
2. Routes issues by complexity (simple → Gemini Flash, complex → Claude)
3. Workers claim tasks and implement solutions autonomously
4. PRs created following your project's conventions
5. You review and merge

## Step 5: Create a Test Issue

Create a simple GitHub Issue in your repo:

```markdown
Title: Add godoc comment to main function

Description:
Add a godoc comment to the main() function in cmd/app/main.go

**Acceptance Criteria:**
- [ ] Comment starts with "main is the entry point"
- [ ] Comment explains what the function does
- [ ] gofmt passes

**Complexity:** simple
```

**The system will:**
1. Poll and detect the issue
2. Route to Gemini Flash tier (simple complexity)
3. Worker claims task
4. Spawns Claude Code agent
5. Agent reads your CLAUDE.md conventions
6. Implements the change
7. Creates PR with correct branch/commit format
8. You review and merge!

## Step 6: Monitor

Watch the logs:
```bash
[supervisor] Polling issues from YourUser/YourRepo...
[supervisor] Found 1 new issue: #42
[supervisor] Routed #42 to gemini-flash tier (complexity: simple)
[gemini-flash-1] Claimed task task-abc123 (issue #42)
[gemini-flash-1] Reading conventions from ./CLAUDE.md...
[gemini-flash-1] Creating feature branch: feature/42-add-godoc-comment
[gemini-flash-1] Running tests...
[gemini-flash-1] Creating PR #15...
[gemini-flash-1] Completed task task-abc123 - PR #15 created
```

## Common Commands

```bash
# Build
make build

# Run tests
make test

# Run linter
make lint

# Clean build artifacts
make clean

# Build and run with example config
make run

# Check task queue
ls tasks/

# Check worker health
# (Future: dashboard at http://localhost:8080)
```

## Configuration Options

### Worker Pools

Adjust worker counts in `orchestrator.yml`:

```yaml
worker_tiers:
  gemini_flash:
    max_workers: 5    # Fast, free tier for simple tasks
  gemini_pro:
    max_workers: 3    # Moderate complexity
  claude:
    max_workers: 2    # Complex tasks, novel problems
```

### Supervisor Settings

```yaml
poll_interval_seconds: 60     # How often to check GitHub
task_timeout_minutes: 120     # Max time per task
max_retry_attempts: 3         # Retries before marking failed
task_queue_dir: ./tasks       # Where to store task queue
```

## Troubleshooting

### "No tasks claimed"
- Check that issues have correct labels (if filtering enabled)
- Verify issues are open (not closed)
- Check complexity routing in logs

### "Worker fails to create PR"
- Verify GitHub credentials are configured
- Check that branch patterns match your project's conventions
- Review worker logs in task queue directory

### "Tests fail in CI"
- Ensure your CLAUDE.md has correct test commands
- Verify workers are running tests before creating PRs
- Check that TDD requirement is set correctly

## Next Steps

1. **Add more projects** - Configure multiple repos in orchestrator.yml
2. **Tune routing** - Adjust complexity classification rules
3. **Monitor metrics** - Track completion rates, worker utilization
4. **Scale workers** - Add more workers as ticket volume increases

## Cost

**$0/day** - Uses your existing subscriptions:
- Gemini via Antigravity (your subscription)
- Claude via local Claude Code CLI
- No per-ticket API charges

## Support

- See `docs/PLAN.md` for full implementation details
- Check `STATUS.md` for build status
- Review `README.md` for architecture overview

---

**Ready to 10x your productivity!** 🚀

The system transforms you from developer to Product Owner - write detailed tickets, let agents implement, you review and approve.
