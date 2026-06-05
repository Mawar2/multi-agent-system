package llm

import "context"

// LLMBackend abstracts the underlying LLM service.
// This allows swapping between Claude Code CLI (Phase 1) and
// Antigravity/Vertex API (Phase 2+) without changing worker logic.
type LLMBackend interface {
	// Execute sends a prompt to the LLM and returns the response.
	// The model parameter specifies which model to use (e.g., "claude-sonnet-4.5", "gemini-flash-3.5").
	Execute(ctx context.Context, prompt string, model string) (string, error)

	// Name returns the backend name (e.g., "claude-code-cli", "antigravity", "vertex-ai").
	Name() string

	// Models returns the list of models available from this backend.
	Models() []string
}
