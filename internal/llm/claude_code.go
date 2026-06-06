package llm

import (
	"bytes"
	"context"
	"fmt"
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
			"claude-sonnet-4-6", // Current Sonnet — primary model for most tasks
			"claude-opus-4-8",   // Current Opus — complex architecture decisions
			"claude-haiku-4-5",  // Current Haiku — fast, low-cost tasks
			"claude-sonnet-4.5", // Legacy alias kept for backward compatibility
			"claude-opus-4.6",   // Legacy alias kept for backward compatibility
		},
		maxTokens: 200000, // Matches Claude's token limit
	}
}

// Execute sends a prompt to Claude Code and returns the agent's response.
// Delegates to ExecuteInDir with no working directory override.
func (b *ClaudeCodeBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	return b.ExecuteInDir(ctx, prompt, model, "")
}

// ExecuteInDir executes Claude Code CLI in a specific working directory.
// This ensures git operations happen in the correct repository context.
//
// Parameters:
//   - ctx: Context for cancellation and timeout
//   - prompt: The task prompt to send to Claude
//   - model: The Claude model to use (e.g., "claude-sonnet-4-6", "claude-opus-4-8")
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
		model = "claude-sonnet-4-6" // Default to current Sonnet
	}

	// Validate that the requested model is supported
	if !b.supportsModel(model) {
		return "", fmt.Errorf("execute: unsupported model %q (supported: %v)", model, b.models)
	}

	// Convert full model ID to CLI short alias
	modelAlias := model
	switch model {
	case "claude-sonnet-4-6", "claude-sonnet-4.5", "claude-sonnet-4-5":
		modelAlias = "sonnet"
	case "claude-opus-4-8", "claude-opus-4.6", "claude-opus-4-6":
		modelAlias = "opus"
	case "claude-haiku-4-5", "claude-haiku-4-5-20251001":
		modelAlias = "haiku"
	}

	// --dangerously-skip-permissions allows autonomous worker execution
	cmd := exec.CommandContext(ctx, "claude", "--print", "--dangerously-skip-permissions", "--model", modelAlias)

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
