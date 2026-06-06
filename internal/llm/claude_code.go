package llm

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// ClaudeCodeBackend implements the LLMBackend interface using Claude Code CLI
// to spawn sub-agents for complex task execution.
type ClaudeCodeBackend struct {
	// name is the backend identifier ("claude-code-cli")
	name string

	// models is the list of Claude models available via Claude Code
	models []string

	// maxTokens sets the context window limit for CLI invocations
	maxTokens int
}

// NewClaudeCodeBackend creates a new Claude Code backend instance.
func NewClaudeCodeBackend() *ClaudeCodeBackend {
	return &ClaudeCodeBackend{
		name: "claude-code-cli",
		models: []string{
			"claude-sonnet-4-6", // Current Sonnet ID
			"claude-opus-4-8",   // Current Opus ID
			"claude-haiku-4-5",  // Current Haiku ID
			"claude-sonnet-4.5", // Legacy alias
			"claude-opus-4.6",   // Legacy alias
		},
		maxTokens: 200000,
	}
}

// Execute sends a prompt to Claude Code and returns the agent's response.
func (b *ClaudeCodeBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("execute: prompt cannot be empty")
	}

	if model == "" {
		model = "claude-sonnet-4-6"
	}

	if !b.supportsModel(model) {
		return "", fmt.Errorf("execute: unsupported model %q (supported: %v)", model, b.models)
	}

	return b.ExecuteInDir(ctx, prompt, model, "")
}

// ExecuteInDir executes Claude Code CLI in a specific working directory.
func (b *ClaudeCodeBackend) ExecuteInDir(ctx context.Context, prompt string, model string, workDir string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("execute: prompt cannot be empty")
	}

	if model == "" {
		model = "claude-sonnet-4-6"
	}

	if !b.supportsModel(model) {
		return "", fmt.Errorf("execute: unsupported model %q (supported: %v)", model, b.models)
	}

	// Convert model ID/alias to CLI short alias
	modelAlias := model
	switch model {
	case "claude-sonnet-4-6", "claude-sonnet-4.5", "claude-sonnet-4-5":
		modelAlias = "sonnet"
	case "claude-opus-4-8", "claude-opus-4.6", "claude-opus-4-6":
		modelAlias = "opus"
	case "claude-haiku-4-5", "claude-haiku-4-5-20251001":
		modelAlias = "haiku"
	}

	cmd := exec.CommandContext(ctx, "claude", "--print", "--dangerously-skip-permissions", "--model", modelAlias)

	if workDir != "" {
		cmd.Dir = workDir
		fmt.Printf("[ClaudeCodeBackend] Executing in directory: %s\n", workDir)
	}

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
