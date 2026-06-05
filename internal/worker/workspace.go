package worker

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
)

// WorkspaceManager manages isolated working directories for target repositories.
// Each worker gets its own workspace to enable parallel execution without conflicts.
type WorkspaceManager struct {
	rootDir   string                 // Base root directory, e.g., "./projects"
	workerID  string                 // Worker ID for isolation, e.g., "gemini-flash-1"
	repoLocks map[string]*sync.Mutex // Per-worker-repo locks (key: "workerID/owner/repo")
	locksMu   sync.RWMutex           // Protects repoLocks map itself
}

// NewWorkspaceManager creates a new workspace manager for a specific worker.
// Each worker gets isolated workspaces to enable parallel execution.
func NewWorkspaceManager(rootDir, workerID string) *WorkspaceManager {
	return &WorkspaceManager{
		rootDir:   rootDir,
		workerID:  workerID,
		repoLocks: make(map[string]*sync.Mutex),
	}
}

// PrepareWorkspace ensures a workspace exists for the given task's repository.
// If the workspace doesn't exist, it clones the repo.
// If it exists, it pulls the latest changes from the default branch.
//
// Returns the absolute path to the workspace directory.
func (wm *WorkspaceManager) PrepareWorkspace(ctx context.Context, task *taskqueue.Task) (string, error) {
	// Acquire per-worker-repo lock to prevent concurrent clone/pull operations
	// Each worker has its own lock for each repo, enabling true parallelism
	lock := wm.getRepoLock(task.RepoOwner, task.RepoName)
	lock.Lock()
	defer lock.Unlock()

	// Workspace path: {rootDir}/{workerID}/{owner}/{repo}
	// Example: ./projects/gemini-flash-1/Mawar2/Kaimi
	workspaceDir := filepath.Join(wm.rootDir, wm.workerID, task.RepoOwner, task.RepoName)

	// Check if workspace already exists
	if _, err := os.Stat(filepath.Join(workspaceDir, ".git")); err == nil {
		// Workspace exists, pull latest
		fmt.Printf("[WorkspaceManager] Workspace exists for %s/%s, pulling latest...\n", task.RepoOwner, task.RepoName)
		if err := wm.pullLatest(ctx, workspaceDir); err != nil {
			return "", fmt.Errorf("failed to pull latest in workspace: %w", err)
		}
		return workspaceDir, nil
	}

	// Workspace doesn't exist, clone it
	fmt.Printf("[WorkspaceManager] Cloning %s/%s into workspace...\n", task.RepoOwner, task.RepoName)
	if err := wm.cloneRepo(ctx, task.RepoOwner, task.RepoName, workspaceDir); err != nil {
		return "", fmt.Errorf("failed to clone repo: %w", err)
	}

	return workspaceDir, nil
}

// cloneRepo clones a GitHub repository into the specified directory.
func (wm *WorkspaceManager) cloneRepo(ctx context.Context, owner, repo, dest string) error {
	// Create parent directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Clone the repository
	repoURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	cmd := exec.CommandContext(ctx, "git", "clone", repoURL, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	fmt.Printf("[WorkspaceManager] Successfully cloned %s/%s\n", owner, repo)
	return nil
}

// pullLatest pulls the latest changes from the default branch.
func (wm *WorkspaceManager) pullLatest(ctx context.Context, workspaceDir string) error {
	// First, fetch all remotes
	fetchCmd := exec.CommandContext(ctx, "git", "-C", workspaceDir, "fetch", "origin")
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("git fetch failed: %w", err)
	}

	// Get the default branch name
	defaultBranchCmd := exec.CommandContext(ctx, "git", "-C", workspaceDir, "symbolic-ref", "refs/remotes/origin/HEAD", "--short")
	output, err := defaultBranchCmd.Output()
	if err != nil {
		// Fall back to "main" if we can't determine default branch
		output = []byte("origin/main")
	}
	defaultBranch := string(output)
	defaultBranch = filepath.Base(defaultBranch) // Extract branch name from origin/main

	// Checkout default branch
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", workspaceDir, "checkout", defaultBranch)
	if err := checkoutCmd.Run(); err != nil {
		return fmt.Errorf("git checkout failed: %w", err)
	}

	// Pull latest changes
	pullCmd := exec.CommandContext(ctx, "git", "-C", workspaceDir, "pull", "origin", defaultBranch)
	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("git pull failed: %w", err)
	}

	fmt.Printf("[WorkspaceManager] Pulled latest changes for %s\n", workspaceDir)
	return nil
}

// getRepoLock returns the mutex for a specific worker-repo combination, creating it if needed.
// Thread-safe: uses locksMu to protect the repoLocks map.
// Lock key includes worker ID for true per-worker isolation and parallel execution.
func (wm *WorkspaceManager) getRepoLock(owner, repo string) *sync.Mutex {
	key := fmt.Sprintf("%s/%s/%s", wm.workerID, owner, repo)

	// Fast path: try to get existing lock with read lock
	wm.locksMu.RLock()
	lock, exists := wm.repoLocks[key]
	wm.locksMu.RUnlock()

	if exists {
		return lock
	}

	// Slow path: create new lock with write lock
	wm.locksMu.Lock()
	defer wm.locksMu.Unlock()

	// Double-check: another goroutine may have created it
	if lock, exists := wm.repoLocks[key]; exists {
		return lock
	}

	// Create new lock for this repo
	lock = &sync.Mutex{}
	wm.repoLocks[key] = lock
	return lock
}

// CleanWorkspace removes a worker's workspace directory (useful for cleanup).
func (wm *WorkspaceManager) CleanWorkspace(owner, repo string) error {
	workspaceDir := filepath.Join(wm.rootDir, wm.workerID, owner, repo)
	if err := os.RemoveAll(workspaceDir); err != nil {
		return fmt.Errorf("failed to remove workspace: %w", err)
	}
	fmt.Printf("[WorkspaceManager] Cleaned workspace for %s/%s\n", owner, repo)
	return nil
}
