# Standalone Multi-Agent Orchestrator Plan

**Last updated:** 2026-06-05
**Status:** Design Phase

## Vision

Transform the multi-agent orchestration system from Kaimi-specific code into a **standalone, reusable repository** that can orchestrate ANY GitHub project with configurable conventions.

**Use cases after this change:**
- Run orchestrator for Kaimi (current)
- Run orchestrator for other BlueMeta projects
- Run orchestrator for client projects
- Open-source orchestrator for community use

## Current State

**What exists:**
- Orchestration code built inside `Pulse` directory (Kaimi repo)
- All orchestration files untracked (not committed to Kaimi)
- Hardcoded assumptions about repo structure
- Single-project configuration

**What's broken:**
- PRs created in wrong repo (see WRONG_REPO_FIX_PLAN.md)
- Can't be used for multiple projects
- No clear separation between orchestrator and target projects
- Can't be versioned/deployed independently

## Goals

1. **Separate repository** - Multi-agent-system lives in its own GitHub repo
2. **Multi-project support** - Configure multiple target repos in one config file
3. **Per-project working directories** - Each project has isolated workspace
4. **Convention-agnostic** - Load project conventions from target repo's CLAUDE.md
5. **Zero coupling** - Orchestrator has no knowledge of Kaimi specifics
6. **Easy deployment** - Docker container or single binary
7. **Open-sourceable** - Clean enough to share publicly

## Architecture Design

### Repository Structure

```
Mawar2/multi-agent-orchestrator/
├── cmd/
│   └── supervisor/
│       └── main.go              # Supervisor entry point
├── internal/
│   ├── orchestrator/
│   │   ├── supervisor.go        # Main supervisor loop
│   │   ├── project.go           # Project configuration
│   │   └── workspace.go         # Working directory manager
│   ├── worker/
│   │   ├── claudecode.go        # Claude Code CLI worker
│   │   ├── gemini.go            # Gemini API worker (Phase 2)
│   │   └── executor.go          # Git operations executor
│   ├── taskqueue/
│   │   ├── queue.go             # Task queue interface
│   │   └── json.go              # JSON file implementation
│   ├── llm/
│   │   ├── backend.go           # LLM backend interface
│   │   ├── claude_code.go       # Claude Code backend
│   │   └── gemini.go            # Gemini API backend
│   └── ticket/
│       ├── client.go            # GitHub ticket client
│       └── github_rest_client.go
├── config/
│   └── orchestrator.example.yml # Example configuration
├── docs/
│   ├── GETTING_STARTED.md
│   ├── CONFIGURATION.md
│   └── ARCHITECTURE.md
├── .github/
│   └── workflows/
│       └── ci.yml               # CI for orchestrator itself
├── Dockerfile
├── docker-compose.yml
├── go.mod
├── go.sum
└── README.md
```

### Configuration Format

**orchestrator.yml** - User's configuration file (not in repo):

```yaml
# Multi-Agent Orchestrator Configuration
version: "1.0"

# Global settings
poll_interval_seconds: 60
task_timeout_minutes: 120
max_retry_attempts: 3
workspace_root: ./workspaces  # Where to clone target repos

# Worker tiers
worker_tiers:
  gemini_flash:
    max_workers: 5
    model: gemini-flash-3.5
  gemini_pro:
    max_workers: 3
    model: gemini-pro-3.5
  claude:
    max_workers: 2
    model: claude-sonnet-4.5

# Fallback chains for quota failover
fallback_chains:
  gemini-flash:
    - gemini-pro
    - claude
  gemini-pro:
    - claude
    - gemini-flash
  claude:
    - claude-opus
    - gemini-pro

# Quota reset windows
quota_reset:
  gemini-flash: 1h
  gemini-pro: 1h
  claude: 5m
  claude-opus: 5m

# Projects to monitor
projects:
  # Project 1: Kaimi
  - name: kaimi
    repo_owner: Mawar2
    repo_name: Kaimi
    conventions_path: ./CLAUDE.md       # Path inside target repo
    branch_pattern: "feature/KAI-{ticket}-{summary}"
    commit_pattern: "{ticket}_{description}"
    labels: []                           # Optional: filter by labels
    workspace_dir: ./workspaces/kaimi   # Where this project is cloned

  # Project 2: Example other project
  - name: other-project
    repo_owner: YourOrg
    repo_name: YourRepo
    conventions_path: ./PROJECT_RULES.md
    branch_pattern: "{ticket}-{summary}"
    commit_pattern: "[{ticket}] {description}"
    labels: ["orchestrator:pending"]
    workspace_dir: ./workspaces/other-project

# GitHub authentication
github:
  token_env: GITHUB_TOKEN  # Environment variable name for token

# Observability (optional)
observability:
  dashboard_enabled: true
  dashboard_port: 8080
  log_level: info
  log_format: json
```

### Working Directory Management

**Key change:** Workers operate in isolated project workspaces, not the orchestrator's directory.

```go
// internal/orchestrator/workspace.go
type WorkspaceManager struct {
    rootDir string  // e.g., ./workspaces
}

// Ensure project workspace exists (clone if needed, pull if exists)
func (wm *WorkspaceManager) PrepareWorkspace(ctx context.Context, project *Project) (string, error) {
    workspaceDir := filepath.Join(wm.rootDir, project.Name)

    // Check if workspace already exists
    if _, err := os.Stat(filepath.Join(workspaceDir, ".git")); err == nil {
        // Workspace exists, pull latest
        return workspaceDir, wm.pullLatest(workspaceDir)
    }

    // Clone fresh workspace
    return workspaceDir, wm.cloneRepo(ctx, project, workspaceDir)
}

func (wm *WorkspaceManager) cloneRepo(ctx context.Context, project *Project, dest string) error {
    repoURL := fmt.Sprintf("https://github.com/%s/%s.git", project.RepoOwner, project.RepoName)

    cmd := exec.CommandContext(ctx, "git", "clone", repoURL, dest)
    return cmd.Run()
}

func (wm *WorkspaceManager) pullLatest(workspaceDir string) error {
    cmd := exec.Command("git", "-C", workspaceDir, "pull", "origin", "main")
    return cmd.Run()
}
```

### Worker Execution Flow

**New flow with workspaces:**

```go
// internal/worker/claudecode.go
func (w *ClaudeCodeWorker) Execute(ctx context.Context, task *taskqueue.Task) (*Result, error) {
    // 1. Get project workspace (already cloned by supervisor)
    workspaceDir := task.WorkspaceDir  // e.g., ./workspaces/kaimi

    // 2. Load conventions from target repo
    ruleset, err := conventions.LoadFromPath(filepath.Join(workspaceDir, task.ConventionsPath))
    if err != nil {
        return nil, fmt.Errorf("failed to load conventions: %w", err)
    }

    // 3. Build prompt with project-specific conventions
    prompt := w.buildPrompt(task, ruleset)

    // 4. Execute Claude Code CLI in project workspace
    response, err := w.backend.ExecuteInDir(ctx, prompt, w.model, workspaceDir)
    if err != nil {
        return nil, fmt.Errorf("claude execution failed: %w", err)
    }

    // 5. Extract results (branch, PR number)
    branchName := extractBranchName(response)
    prNumber := extractPRNumber(response)

    // 6. Update task status
    task.BranchName = branchName
    task.PRNumber = prNumber
    task.Status = taskqueue.StatusReview

    return &Result{
        Success:    true,
        BranchName: branchName,
        PRNumber:   prNumber,
    }, nil
}
```

### Supervisor Changes

**Multi-project polling:**

```go
// internal/orchestrator/supervisor.go
type Supervisor struct {
    projects     []*Project
    workspaces   *WorkspaceManager
    queue        taskqueue.TaskQueue
    workers      map[taskqueue.Tier][]*Worker
    ticketClient ticket.Client
}

func (s *Supervisor) Run(ctx context.Context) error {
    ticker := time.NewTicker(s.pollInterval)
    defer ticker.Stop()

    // Initialize workspaces for all projects
    for _, project := range s.projects {
        if _, err := s.workspaces.PrepareWorkspace(ctx, project); err != nil {
            log.Printf("Failed to prepare workspace for %s: %v", project.Name, err)
        }
    }

    for {
        select {
        case <-ticker.C:
            // Poll each project for new issues
            for _, project := range s.projects {
                s.pollProject(ctx, project)
            }
        case <-ctx.Done():
            return nil
        }
    }
}

func (s *Supervisor) pollProject(ctx context.Context, project *Project) {
    // Fetch issues from GitHub
    issues, err := s.ticketClient.ListIssues(ctx, project.RepoOwner, project.RepoName, ticket.IssueFilter{
        State:  "OPEN",
        Labels: project.Labels,
    })

    for _, issue := range issues {
        // Check if already in queue
        if s.queue.Exists(issue.Number, project.RepoOwner, project.RepoName) {
            continue
        }

        // Classify complexity
        complexity := classifyComplexity(issue)

        // Enqueue task
        task := &taskqueue.Task{
            IssueNumber:     issue.Number,
            RepoOwner:       project.RepoOwner,
            RepoName:        project.RepoName,
            ProjectName:     project.Name,
            WorkspaceDir:    filepath.Join(s.workspaces.rootDir, project.Name),
            ConventionsPath: project.ConventionsPath,
            Complexity:      complexity,
            Tier:            tierFromComplexity(complexity),
        }

        s.queue.Enqueue(task)
    }
}
```

## Migration Plan

### Phase 1: Extract and Separate (Week 1)

**Goal:** Create standalone repo with current code

**Steps:**
1. **Create new GitHub repo** - `Mawar2/multi-agent-orchestrator`

2. **Copy orchestration code** from Pulse to new repo:
   ```bash
   # Create new repo locally
   mkdir multi-agent-orchestrator
   cd multi-agent-orchestrator
   git init

   # Copy orchestration files from Pulse
   cp -r ../Pulse/cmd/supervisor ./cmd/
   cp -r ../Pulse/internal/worker ./internal/
   cp -r ../Pulse/internal/llm ./internal/
   cp -r ../Pulse/internal/taskqueue ./internal/
   cp -r ../Pulse/internal/ticket ./internal/
   cp -r ../Pulse/internal/orchestrator ./internal/

   # Copy config and docs
   cp ../Pulse/orchestrator.yml ./config/orchestrator.example.yml
   cp ../Pulse/ORCHESTRATION_RESULTS.md ./docs/
   cp ../Pulse/docs/design/QUOTA_FAILOVER_PLAN.md ./docs/

   # Create new README
   # Create go.mod
   go mod init github.com/Mawar2/multi-agent-orchestrator
   go mod tidy

   # First commit
   git add .
   git commit -m "Initial commit: Extract orchestrator from Kaimi"
   ```

3. **Push to GitHub**:
   ```bash
   gh repo create Mawar2/multi-agent-orchestrator --public --source=. --push
   ```

4. **Clean up Pulse directory**:
   - Remove untracked orchestration files from Pulse
   - Restore Pulse to be Kaimi-only repository

### Phase 2: Add Multi-Project Support (Week 1-2)

**Changes:**
1. **Update configuration format** - Add projects array
2. **Implement WorkspaceManager** - Clone/manage project workspaces
3. **Update supervisor** - Poll multiple projects
4. **Update workers** - Execute in project workspaces
5. **Update task schema** - Add workspace_dir and project_name fields

**Acceptance Criteria:**
- [ ] Orchestrator runs from separate directory
- [ ] Can configure multiple projects in orchestrator.yml
- [ ] Each project has isolated workspace
- [ ] Workers execute in correct project workspace
- [ ] PRs created in correct repositories
- [ ] Conventions loaded from target repo's CLAUDE.md

### Phase 3: Deployment and Tooling (Week 2)

**Deliverables:**
1. **Docker support**
   - Dockerfile with multi-stage build
   - docker-compose.yml for easy deployment
   - Volume mounts for config and workspaces

2. **Documentation**
   - GETTING_STARTED.md - Install, configure, run
   - CONFIGURATION.md - All config options explained
   - ARCHITECTURE.md - How orchestrator works

3. **CI/CD**
   - GitHub Actions for tests and linting
   - Release workflow for tagged versions
   - Docker image publishing

### Phase 4: Kaimi Migration (Week 2-3)

**Migrate Kaimi to use standalone orchestrator:**

1. **Install orchestrator** as separate tool:
   ```bash
   # Clone orchestrator
   git clone https://github.com/Mawar2/multi-agent-orchestrator.git
   cd multi-agent-orchestrator

   # Configure for Kaimi
   cp config/orchestrator.example.yml orchestrator.yml
   # Edit orchestrator.yml to add Kaimi project

   # Run
   GITHUB_TOKEN=xxx ./supervisor
   ```

2. **Configure orchestrator.yml**:
   ```yaml
   projects:
     - name: kaimi
       repo_owner: Mawar2
       repo_name: Kaimi
       conventions_path: ./CLAUDE.md
       branch_pattern: "feature/KAI-{ticket}-{summary}"
       commit_pattern: "{ticket}_{description}"
       workspace_dir: ./workspaces/kaimi
   ```

3. **Run orchestrator** - Verify PRs go to Kaimi correctly

## Success Criteria

**Phase 1 Complete:**
- [ ] Orchestrator exists in separate GitHub repo
- [ ] Builds and runs independently
- [ ] Can orchestrate Kaimi project
- [ ] All original features working

**Phase 2 Complete:**
- [ ] Multi-project configuration working
- [ ] Workspaces isolated per project
- [ ] Workers execute in correct project directories
- [ ] PRs created in correct repositories
- [ ] Tested with 2+ projects simultaneously

**Phase 3 Complete:**
- [ ] Docker deployment working
- [ ] Documentation complete and accurate
- [ ] CI/CD pipeline operational
- [ ] Ready for public release

**Phase 4 Complete:**
- [ ] Kaimi using standalone orchestrator
- [ ] No orchestration code in Kaimi repo
- [ ] Kaimi PRs created correctly
- [ ] Other BlueMeta projects can be added easily

## Open Questions

1. **Workspace persistence** - Keep workspaces between runs or clean up?
   - **Recommendation:** Keep but clean old branches periodically

2. **Workspace concurrency** - Multiple workers in same workspace?
   - **Recommendation:** Lock workspace during active work (use file lock)

3. **GitHub token management** - Per-project tokens or single token?
   - **Recommendation:** Single token with org-level access for simplicity

4. **Cloud deployment** - Run orchestrator where?
   - **Recommendation:** GCP Compute Engine or Cloud Run for Kaimi

5. **Cost tracking** - How to attribute costs per project?
   - **Recommendation:** Log project_name with every LLM call, aggregate in dashboard

## Timeline

**Week 1:**
- Day 1-2: Phase 1 (Extract and separate)
- Day 3-5: Phase 2 start (Workspace manager, multi-project config)

**Week 2:**
- Day 1-3: Phase 2 complete (Multi-project support fully working)
- Day 4-5: Phase 3 (Docker, docs, CI/CD)

**Week 3:**
- Day 1-2: Phase 4 (Kaimi migration)
- Day 3-5: Testing, polish, open-source prep

**Total: 3 weeks to production-ready standalone orchestrator**

## Next Steps

1. Review this plan with team
2. Decide on timeline (all at once vs. phased)
3. Create GitHub repo `Mawar2/multi-agent-orchestrator`
4. Start Phase 1 extraction
5. Test with Kaimi before expanding

---

**Note:** This plan assumes immediate fix (WRONG_REPO_FIX_PLAN.md) is applied first to stop PRs going to wrong repo while this longer-term work is underway.
