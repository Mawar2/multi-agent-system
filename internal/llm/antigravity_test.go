package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNewLocalAntigravityBackend verifies backend initialization.
func TestNewLocalAntigravityBackend(t *testing.T) {
	backend := NewLocalAntigravityBackend()

	if backend == nil {
		t.Fatal("NewLocalAntigravityBackend returned nil")
	}

	if backend.Name() != "local-antigravity" {
		t.Errorf("Name() = %q, want 'local-antigravity'", backend.Name())
	}

	models := backend.Models()
	if len(models) == 0 {
		t.Fatal("expected models to be non-empty")
	}
}

// TestLocalAntigravityBackend_ImplementsLLMBackend verifies interface compliance.
func TestLocalAntigravityBackend_ImplementsLLMBackend(t *testing.T) {
	var _ LLMBackend = (*LocalAntigravityBackend)(nil)
}

// TestLocalAntigravityBackend_Execute_EmptyPrompt verifies error on empty prompt.
func TestLocalAntigravityBackend_Execute_EmptyPrompt(t *testing.T) {
	backend := NewLocalAntigravityBackend()
	_, err := backend.Execute(context.Background(), "", "gemini-flash-3.5")
	if err == nil || err.Error() != "execute: prompt cannot be empty" {
		t.Errorf("got error %v, want 'execute: prompt cannot be empty'", err)
	}
}

// TestLocalAntigravityBackend_Execute_UnsupportedModel verifies error on unsupported model.
func TestLocalAntigravityBackend_Execute_UnsupportedModel(t *testing.T) {
	backend := NewLocalAntigravityBackend()
	_, err := backend.Execute(context.Background(), "prompt", "gpt-4")
	if err == nil {
		t.Fatal("expected error for unsupported model")
	}
	if !strings.Contains(err.Error(), "unsupported model") {
		t.Errorf("error %q does not contain 'unsupported model'", err.Error())
	}
}

// TestLocalAntigravityBackend_Execute_Success exercises the happy path via a test server.
func TestLocalAntigravityBackend_Execute_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		resp := chatCompletionResponse{
			Choices: []struct {
				Message message `json:"message"`
			}{
				{Message: message{Role: "assistant", Content: "hello from antigravity"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)
	backend := NewLocalAntigravityBackend()

	result, err := backend.Execute(context.Background(), "test prompt", "gemini-flash-3.5")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if result != "hello from antigravity" {
		t.Errorf("result = %q, want 'hello from antigravity'", result)
	}
}

// TestLocalAntigravityBackend_Execute_ServerError verifies non-200 response handling.
func TestLocalAntigravityBackend_Execute_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)
	backend := NewLocalAntigravityBackend()

	_, err := backend.Execute(context.Background(), "test prompt", "gemini-flash-3.5")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("error %q does not contain 'status 500'", err.Error())
	}
}
