package llm

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// ClaudeCodeBackend implements the LLMBackend interface using Claude Code CLI.
// It spawns a `claude --print` subprocess in the target repo's workspace so
// the agent can read conventions, edit files, run tests, and create PRs.
type ClaudeCodeBackend struct {
	// name is the backend identifier ("claude-code-cli")
	name string

	// models is the list of Claude models available via Claude Code
	models []string

	// maxTokens sets the context window limit for CLI invocations
	maxTokens int
}

// NewClaudeCodeBackend creates a new Claude Code backend instance.
//
// Returns an initialized ClaudeCodeBackend configured for the available models.
func NewClaudeCodeBackend() *ClaudeCodeBackend {
	return &ClaudeCodeBackend{
		name: "claude-code-cli",
		models: []string{
			"claude-sonnet-4-6", // Current Sonnet - fast, primary model for most tasks
			"claude-opus-4-8",   // Current Opus - powerful, complex architecture decisions
			"claude-haiku-4-5",  // Current Haiku - fastest, simple tasks
			// Legacy aliases kept for backward compatibility
			"claude-sonnet-4.5",
			"claude-opus-4.6",
		},
		maxTokens: 200000,
	}
}

// Execute sends a prompt to Claude Code and returns the agent's response.
// Delegates to ExecuteInDir with the current working directory.
//
// The model parameter specifies which Claude model to use (e.g., "claude-sonnet-4-6"
// for speed, "claude-opus-4-8" for complex reasoning). Empty model defaults to Sonnet.
//
// Returns an error if the model is unsupported or the CLI is unavailable.
func (b *ClaudeCodeBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	return b.ExecuteInDir(ctx, prompt, model, "")
}

// ExecuteInDir executes Claude Code CLI in a specific working directory.
// workDir is the absolute path to the target repo clone; empty means the current directory.
//
// Returns the Claude CLI stdout or an error if the CLI is unavailable or exits non-zero.
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

	// Convert full model IDs to CLI short aliases accepted by --model.
	modelAlias := model
	switch model {
	case "claude-sonnet-4-6", "claude-sonnet-4.5", "claude-sonnet-4-5":
		modelAlias = "sonnet"
	case "claude-opus-4-8", "claude-opus-4.6", "claude-opus-4-6":
		modelAlias = "opus"
	case "claude-haiku-4-5", "claude-haiku-4.5":
		modelAlias = "haiku"
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
