package llm

import (
	"context"
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

// TestClaudeCodeBackend_Models verifies the available models.
func TestClaudeCodeBackend_Models(t *testing.T) {
	backend := NewClaudeCodeBackend()
	models := backend.Models()

	expectedModels := []string{
		"claude-sonnet-4.5",
		"claude-opus-4.6",
	}

	if len(models) != len(expectedModels) {
		t.Errorf("Models() returned %d models, want %d", len(models), len(expectedModels))
	}

	for i, expected := range expectedModels {
		if i >= len(models) {
			break
		}
		if models[i] != expected {
			t.Errorf("Models()[%d] = %q, want %q", i, models[i], expected)
		}
	}
}

// TestClaudeCodeBackend_Execute_EmptyPrompt verifies error on empty prompt.
func TestClaudeCodeBackend_Execute_EmptyPrompt(t *testing.T) {
	backend := NewClaudeCodeBackend()
	ctx := context.Background()

	_, err := backend.Execute(ctx, "", "claude-sonnet-4.5")

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

	if err.Error() != "execute: unsupported model \"gpt-4\" (supported: [claude-sonnet-4.5 claude-opus-4.6])" {
		t.Errorf("Execute error = %q", err.Error())
	}
}

// TestClaudeCodeBackend_Execute_UnsupportedModel_DefaultModel verifies default model selection.
func TestClaudeCodeBackend_Execute_UnsupportedModel_DefaultModel(t *testing.T) {
	backend := NewClaudeCodeBackend()
	ctx := context.Background()
	prompt := "test prompt"

	// Empty model should use default (claude-sonnet-4.5)
	// which should not cause an unsupported model error
	result, err := backend.Execute(ctx, prompt, "")

	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}

	if result == "" {
		t.Error("Execute result is empty")
	}
}

// TestClaudeCodeBackend_Execute_ValidModel_Phase1Placeholder verifies Phase 1 placeholder behavior.
func TestClaudeCodeBackend_Execute_ValidModel_Phase1Placeholder(t *testing.T) {
	t.Skip("Skipping test that calls real Claude CLI - use integration tests for this")

	backend := NewClaudeCodeBackend()
	ctx := context.Background()
	prompt := "Implement the Hunter agent with SAM.gov integration"

	result, err := backend.Execute(ctx, prompt, "claude-sonnet-4.5")

	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}

	// Result should not be empty
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

	// Using Opus (complex reasoning model) should still work and return placeholder response
	result, err := backend.Execute(ctx, prompt, "claude-opus-4.6")

	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}

	if result == "" {
		t.Error("Execute result is empty")
	}
}

// TestClaudeCodeBackend_SupportsModel_ValidModels verifies model support checks.
func TestClaudeCodeBackend_supportsModel_ValidModels(t *testing.T) {
	tests := []struct {
		name   string
		model  string
		wanted bool
	}{
		{"Sonnet 4.5", "claude-sonnet-4.5", true},
		{"Opus 4.6", "claude-opus-4.6", true},
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
