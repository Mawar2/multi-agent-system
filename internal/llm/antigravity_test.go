package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalAntigravityBackend_Name(t *testing.T) {
	b := NewLocalAntigravityBackend()
	if b.Name() != "antigravity" {
		t.Errorf("Name() = %q, want %q", b.Name(), "antigravity")
	}
}

func TestLocalAntigravityBackend_Models(t *testing.T) {
	b := NewLocalAntigravityBackend()
	models := b.Models()
	if len(models) == 0 {
		t.Fatal("Models() returned empty list")
	}
	modelSet := make(map[string]bool)
	for _, m := range models {
		modelSet[m] = true
	}
	if !modelSet["gemini-flash-3.5"] {
		t.Error("Models() missing gemini-flash-3.5")
	}
	if !modelSet["gemini-pro-3.5"] {
		t.Error("Models() missing gemini-pro-3.5")
	}
}

func TestLocalAntigravityBackend_ImplementsLLMBackend(t *testing.T) {
	var _ LLMBackend = (*LocalAntigravityBackend)(nil)
}

func TestLocalAntigravityBackend_Execute_EmptyPrompt(t *testing.T) {
	b := NewLocalAntigravityBackend()
	_, err := b.Execute(context.Background(), "", "gemini-flash-3.5")
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
	if err.Error() != "execute: prompt cannot be empty" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLocalAntigravityBackend_Execute_UnsupportedModel(t *testing.T) {
	b := NewLocalAntigravityBackend()
	_, err := b.Execute(context.Background(), "hello", "gpt-4")
	if err == nil {
		t.Fatal("expected error for unsupported model")
	}
	if !strings.Contains(err.Error(), `unsupported model "gpt-4"`) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLocalAntigravityBackend_Execute_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		resp := chatResponse{
			Choices: []chatChoice{
				{Message: chatMessage{Role: "assistant", Content: "pong"}},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)
	b := NewLocalAntigravityBackend()

	result, err := b.Execute(context.Background(), "ping", "gemini-flash-3.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "pong" {
		t.Errorf("result = %q, want %q", result, "pong")
	}
}

func TestLocalAntigravityBackend_Execute_DefaultModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Model != "gemini-flash-3.5" {
			t.Errorf("expected default model gemini-flash-3.5, got %q", req.Model)
		}
		resp := chatResponse{
			Choices: []chatChoice{
				{Message: chatMessage{Role: "assistant", Content: "ok"}},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)
	b := NewLocalAntigravityBackend()

	_, err := b.Execute(context.Background(), "test", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocalAntigravityBackend_Execute_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)
	b := NewLocalAntigravityBackend()

	_, err := b.Execute(context.Background(), "hello", "gemini-flash-3.5")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "status 500") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLocalAntigravityBackend_Execute_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := chatResponse{Choices: []chatChoice{}}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)
	b := NewLocalAntigravityBackend()

	_, err := b.Execute(context.Background(), "hello", "gemini-flash-3.5")
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("unexpected error: %v", err)
	}
}
