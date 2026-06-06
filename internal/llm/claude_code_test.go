package llm

import (
	"context"
	"strings"
	"testing"
)

// TestNewClaudeCodeBackend verifies backend initialization.
func TestNewClaudeCodeBackend(t *testing.T) {
	backend := NewClaudeCodeBackend()

	if backend == nil {
		t.Fatal("NewClaudeCodeBackend returned nil")
	}

	if backend.name != "claude-code-cli" {
		t.Errorf("expected name 'claude-code-cli', got %q", backend.name)
	}

	if len(backend.models) == 0 {
		t.Fatal("expected models to be non-empty")
	}
}

// TestClaudeCodeBackend_Name verifies the backend identifier.
func TestClaudeCodeBackend_Name(t *testing.T) {
	backend := NewClaudeCodeBackend()
	expected := "claude-code-cli"

	if got := backend.Name(); got != expected {
		t.Errorf("Name() = %q, want %q", got, expected)
	}
}

// TestClaudeCodeBackend_Models verifies the available models include current and legacy IDs.
func TestClaudeCodeBackend_Models(t *testing.T) {
	backend := NewClaudeCodeBackend()
	models := backend.Models()

	// Verify required model names are present (by name, not by index)
	required := []string{
		"claude-sonnet-4-6",
		"claude-opus-4-8",
		"claude-haiku-4-5",
		"claude-sonnet-4.5",
		"claude-opus-4.6",
	}

	modelSet := make(map[string]bool, len(models))
	for _, m := range models {
		modelSet[m] = true
	}

	for _, name := range required {
		if !modelSet[name] {
			t.Errorf("Models() missing expected model %q", name)
		}
	}
}

// TestClaudeCodeBackend_Execute_EmptyPrompt verifies error on empty prompt.
func TestClaudeCodeBackend_Execute_EmptyPrompt(t *testing.T) {
	backend := NewClaudeCodeBackend()
	ctx := context.Background()

	_, err := backend.Execute(ctx, "", "claude-sonnet-4-6")

	if err == nil {
		t.Error("Execute with empty prompt expected error, got nil")
	}

	if err.Error() != "execute: prompt cannot be empty" {
		t.Errorf("Execute error = %q, want 'execute: prompt cannot be empty'", err.Error())
	}
}

// TestClaudeCodeBackend_Execute_UnsupportedModel verifies error on unsupported model.
func TestClaudeCodeBackend_Execute_UnsupportedModel(t *testing.T) {
	backend := NewClaudeCodeBackend()
	ctx := context.Background()
	prompt := "test prompt"

	_, err := backend.Execute(ctx, prompt, "gpt-4")

	if err == nil {
		t.Error("Execute with unsupported model expected error, got nil")
	}

	if !strings.Contains(err.Error(), `unsupported model "gpt-4"`) {
		t.Errorf("Execute error = %q, want it to contain unsupported model message", err.Error())
	}
}

// TestClaudeCodeBackend_Execute_DefaultModel verifies default model selection.
func TestClaudeCodeBackend_Execute_DefaultModel(t *testing.T) {
	t.Skip("Skipping integration test that calls real Claude CLI")
}

// TestClaudeCodeBackend_Execute_ValidModel_Phase1Placeholder verifies Phase 1 placeholder behavior.
func TestClaudeCodeBackend_Execute_ValidModel_Phase1Placeholder(t *testing.T) {
	t.Skip("Skipping test that calls real Claude CLI - use integration tests for this")

	backend := NewClaudeCodeBackend()
	ctx := context.Background()
	prompt := "Implement the Hunter agent with SAM.gov integration"

	result, err := backend.Execute(ctx, prompt, "claude-sonnet-4-6")

	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}

	if result == "" {
		t.Error("Execute result should not be empty in Phase 1")
	}
}

// TestClaudeCodeBackend_Execute_ComplexModel verifies handling of complex model.
func TestClaudeCodeBackend_Execute_ComplexModel(t *testing.T) {
	t.Skip("Skipping test that calls real Claude CLI - use integration tests for this")

	backend := NewClaudeCodeBackend()
	ctx := context.Background()
	prompt := "Design the Zone 2 orchestration architecture"

	result, err := backend.Execute(ctx, prompt, "claude-opus-4-8")

	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}

	if result == "" {
		t.Error("Execute result is empty")
	}
}

// TestClaudeCodeBackend_supportsModel_ValidModels verifies model support checks.
func TestClaudeCodeBackend_supportsModel_ValidModels(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		wanted bool
	}{
		{"Sonnet 4-6 (current)", "claude-sonnet-4-6", true},
		{"Opus 4-8 (current)", "claude-opus-4-8", true},
		{"Haiku 4-5 (current)", "claude-haiku-4-5", true},
		{"Sonnet 4.5 (legacy)", "claude-sonnet-4.5", true},
		{"Opus 4.6 (legacy)", "claude-opus-4.6", true},
		{"GPT-4", "gpt-4", false},
		{"Gemini", "gemini-pro", false},
		{"Empty", "", false},
	}

	backend := NewClaudeCodeBackend()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backend.supportsModel(tt.model); got != tt.wanted {
				t.Errorf("supportsModel(%q) = %v, want %v", tt.model, got, tt.wanted)
			}
		})
	}
}

// TestClaudeCodeBackend_ImplementsLLMBackend verifies interface compliance.
func TestClaudeCodeBackend_ImplementsLLMBackend(t *testing.T) {
	var _ LLMBackend = (*ClaudeCodeBackend)(nil)
}
