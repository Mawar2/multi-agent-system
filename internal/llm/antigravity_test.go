package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestAntigravityServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *LocalAntigravityBackend) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	backend := NewLocalAntigravityBackend()
	backend.baseURL = srv.URL
	return srv, backend
}

// TestLocalAntigravityBackend_Name verifies the backend identifier.
func TestLocalAntigravityBackend_Name(t *testing.T) {
	backend := NewLocalAntigravityBackend()
	if got := backend.Name(); got != "antigravity" {
		t.Errorf("Name() = %q, want %q", got, "antigravity")
	}
}

// TestLocalAntigravityBackend_Models verifies available models.
func TestLocalAntigravityBackend_Models(t *testing.T) {
	backend := NewLocalAntigravityBackend()
	models := backend.Models()
	if len(models) == 0 {
		t.Fatal("Models() returned empty list")
	}

	modelSet := make(map[string]bool)
	for _, m := range models {
		modelSet[m] = true
	}
	for _, required := range []string{"gemini-flash-3.5", "gemini-pro-3.5"} {
		if !modelSet[required] {
			t.Errorf("Models() missing %q", required)
		}
	}
}

// TestLocalAntigravityBackend_Execute_EmptyPrompt verifies error on empty prompt.
func TestLocalAntigravityBackend_Execute_EmptyPrompt(t *testing.T) {
	backend := NewLocalAntigravityBackend()
	_, err := backend.Execute(context.Background(), "", "gemini-flash-3.5")
	if err == nil || err.Error() != "execute: prompt cannot be empty" {
		t.Errorf("got err=%v, want 'execute: prompt cannot be empty'", err)
	}
}

// TestLocalAntigravityBackend_Execute_UnsupportedModel verifies error on unsupported model.
func TestLocalAntigravityBackend_Execute_UnsupportedModel(t *testing.T) {
	backend := NewLocalAntigravityBackend()
	_, err := backend.Execute(context.Background(), "hello", "gpt-4")
	if err == nil {
		t.Fatal("expected error for unsupported model")
	}
	if !strings.Contains(err.Error(), "unsupported model") {
		t.Errorf("error %q does not contain 'unsupported model'", err.Error())
	}
}

// TestLocalAntigravityBackend_Execute_Success verifies a successful round-trip.
func TestLocalAntigravityBackend_Execute_Success(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		resp := chatResponse{
			Choices: []chatChoice{
				{Message: chatMessage{Role: "assistant", Content: "Hello, world!"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	_, backend := newTestAntigravityServer(t, handler)

	got, err := backend.Execute(context.Background(), "say hello", "gemini-flash-3.5")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got != "Hello, world!" {
		t.Errorf("Execute returned %q, want %q", got, "Hello, world!")
	}
}

// TestLocalAntigravityBackend_Execute_DefaultModel verifies the default model is used when empty.
func TestLocalAntigravityBackend_Execute_DefaultModel(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.Model != "gemini-flash-3.5" {
			http.Error(w, "unexpected model: "+req.Model, http.StatusBadRequest)
			return
		}
		resp := chatResponse{
			Choices: []chatChoice{
				{Message: chatMessage{Role: "assistant", Content: "ok"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	_, backend := newTestAntigravityServer(t, handler)

	got, err := backend.Execute(context.Background(), "hello", "")
	if err != nil {
		t.Fatalf("Execute with empty model returned error: %v", err)
	}
	if got != "ok" {
		t.Errorf("Execute returned %q, want %q", got, "ok")
	}
}

// TestLocalAntigravityBackend_Execute_ServerError verifies error on non-200 status.
func TestLocalAntigravityBackend_Execute_ServerError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})

	_, backend := newTestAntigravityServer(t, handler)

	_, err := backend.Execute(context.Background(), "hello", "gemini-flash-3.5")
	if err == nil {
		t.Fatal("expected error for server 500")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q does not contain status code", err.Error())
	}
}

// TestLocalAntigravityBackend_Execute_NoChoices verifies error when response has no choices.
func TestLocalAntigravityBackend_Execute_NoChoices(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{Choices: []chatChoice{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	_, backend := newTestAntigravityServer(t, handler)

	_, err := backend.Execute(context.Background(), "hello", "gemini-flash-3.5")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("error %q does not mention 'no choices'", err.Error())
	}
}

// TestLocalAntigravityBackend_ImplementsLLMBackend verifies interface compliance.
func TestLocalAntigravityBackend_ImplementsLLMBackend(t *testing.T) {
	var _ LLMBackend = (*LocalAntigravityBackend)(nil)
}
