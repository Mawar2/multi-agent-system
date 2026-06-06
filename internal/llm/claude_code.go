package llm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// ClaudeCodeBackend implements the LLMBackend interface using Claude Code CLI
// to spawn sub-agents for complex task execution.
//
// Phase 1 Implementation: This is a placeholder that wraps potential Claude Code
// CLI Task tool invocations. The actual process spawning and Task tool integration
// will be refined based on how the supervisor spawns worker processes.
//
// Architecture:
// - Intended for TierClaude in the multi-agent task queue system
// - Spawns local Claude Code agents to read project conventions and implement tasks
// - Each agent operates within CLAUDE.md, CONVENTIONS.md, and WORKFLOW.md constraints
// - Returns agent output (branch name, PR number, logs path) as structured results
type ClaudeCodeBackend struct {
	// name is the backend identifier ("claude-code-cli")
	name string

	// models is the list of Claude models available via Claude Code
	models []string

	// maxTokens sets the context window limit for CLI invocations
	maxTokens int
}

// NewClaudeCodeBackend creates a new Claude Code backend instance.
// In Phase 1, this is a placeholder that will be extended when actual CLI
// process spawning is implemented.
//
// Returns an initialized ClaudeCodeBackend configured for the available models.
func NewClaudeCodeBackend() *ClaudeCodeBackend {
	return &ClaudeCodeBackend{
		name: "claude-code-cli",
		models: []string{
			"claude-sonnet-4.5", // Fast, primary model for most tasks
			"claude-opus-4.6",   // Reasoning, complex architecture decisions
		},
		maxTokens: 200000, // Matches Claude's token limit
	}
}

// Execute sends a prompt to Claude Code and returns the agent's response.
//
// The prompt should contain:
// - Clear task description and acceptance criteria
// - References to project conventions (CLAUDE.md, CONVENTIONS.md, WORKFLOW.md)
// - Context about the GitHub Issue (ticket number, description)
// - Expected outputs (branch name, PR number, test results)
//
// The model parameter specifies which Claude model to use for this execution
// (e.g., "claude-sonnet-4.5" for speed, "claude-opus-4.6" for reasoning).
//
// Phase 1 Note: Currently returns a TODO error indicating that actual Task tool
// integration is pending. When implemented, this will:
// 1. Authenticate with Claude Code CLI
// 2. Create a new agent context with project repositories
// 3. Spawn a sub-agent via the Task tool
// 4. Poll for completion or stream results
// 5. Return structured output (agent logs, PR details, etc.)
//
// Returns an error if the model is unsupported or the CLI is unavailable.
func (b *ClaudeCodeBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("execute: prompt cannot be empty")
	}

	if model == "" {
		model = "claude-sonnet-4.5" // Default to Sonnet for speed
	}

	// Validate that the requested model is supported
	if !b.supportsModel(model) {
		return "", fmt.Errorf("execute: unsupported model %q (supported: %v)", model, b.models)
	}

	// TODO(phase-1): Implement actual Claude Code CLI Task tool integration
	// Steps to implement:
	// 1. Validate CLI availability (check if claude-code is in PATH or use cliPath)
	// 2. Create a context file with:
	//    - GitHub repository details (owner, repo, branch)
	//    - Project conventions (CLAUDE.md, CONVENTIONS.md, WORKFLOW.md)
	//    - Task metadata (GitHub Issue number, acceptance criteria)
	// 3. Invoke the Task tool via Claude Code CLI:
	//    - `claude task "prompt with context" --model=<model>`
	//    - Or use MCP if available for tighter integration
	// 4. Capture stdout/stderr and parse results:
	//    - Extract branch name (feature/KAI-XXX-summary format)
	//    - Extract PR number (if PR was created)
	//    - Collect execution logs
	// 5. Return structured response:
	//    - Agent output (logs, decisions made)
	//    - Success indicator (task completed vs. failed)
	//    - Any errors encountered
	//
	// Key constraints for Phase 1:
	// - No parallelization yet (sequential agent spawning)
	// - No retry logic (single attempt per Execute call)
	// - No fallback to other backends (if CLI fails, task fails)
	// - No resource limits (no CPU/memory constraints on agent processes)
	//
	// These will be added in Phase 2 as part of the full supervisor implementation.

	// Call the directory-aware version with no working directory
	return b.ExecuteInDir(ctx, prompt, model, "")
}

// ExecuteInDir executes Claude Code CLI in a specific working directory.
// This ensures git operations happen in the correct repository context.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - prompt: The task prompt to send to Claude
//   - model: The Claude model to use ("claude-sonnet-4.5" or "claude-opus-4.6")
//   - workDir: Absolute path to working directory (where target repo is cloned)
//
// If workDir is empty, uses current directory.
//
// Returns the Claude CLI output or an error if execution fails.
func (b *ClaudeCodeBackend) ExecuteInDir(ctx context.Context, prompt string, model string, workDir string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("execute: prompt cannot be empty")
	}

	if model == "" {
		model = "claude-sonnet-4.5" // Default to Sonnet for speed
	}

	// Validate that the requested model is supported
	if !b.supportsModel(model) {
		return "", fmt.Errorf("execute: unsupported model %q (supported: %v)", model, b.models)
	}

	// Convert model alias for CLI
	modelAlias := model
	switch model {
	case "claude-sonnet-4.5", "claude-sonnet-4-5":
		modelAlias = "sonnet"
	case "claude-opus-4.6", "claude-opus-4-6":
		modelAlias = "opus"
	}

	// Spawn Claude Code subprocess with --print for non-interactive output.
	//
	// Workers run headless in isolated per-worker clones (./projects/<worker>/...),
	// so the agent must be able to edit files and run git/gh/tests without an
	// interactive permission prompt (which would hang in --print mode). The
	// permission mode is configurable via CLAUDE_PERMISSION_MODE (e.g. "acceptEdits"
	// or "plan"); it defaults to bypassing prompts for full autonomy.
	permFlag := "--dangerously-skip-permissions"
	if mode := os.Getenv("CLAUDE_PERMISSION_MODE"); mode != "" {
		permFlag = "--permission-mode=" + mode
	}
	cmd := exec.CommandContext(ctx, "claude", "--print", permFlag, "--model", modelAlias)

	// Set working directory if specified
	if workDir != "" {
		cmd.Dir = workDir
		fmt.Printf("[ClaudeCodeBackend] Executing in directory: %s\n", workDir)
	}

	// Pass prompt via stdin
	cmd.Stdin = bytes.NewBufferString(prompt)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("execute: claude CLI failed: %w\nStderr: %s", err, stderr.String())
	}

	response := stdout.String()
	if response == "" {
		return "", fmt.Errorf("execute: claude returned empty response")
	}

	return response, nil
}

// Name returns the backend identifier.
func (b *ClaudeCodeBackend) Name() string {
	return b.name
}

// Models returns the list of Claude models available via this backend.
func (b *ClaudeCodeBackend) Models() []string {
	return b.models
}

// supportsModel checks if the backend supports the given model name.
func (b *ClaudeCodeBackend) supportsModel(model string) bool {
	for _, m := range b.models {
		if m == model {
			return true
		}
	}
	return false
}
