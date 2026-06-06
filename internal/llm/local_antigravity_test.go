package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newChatServer creates an httptest server that serves:
//   - GET /health → 200
//   - POST /v1/chat/completions → the supplied antigravityChatResponse
func newChatServer(t *testing.T, resp antigravityChatResponse) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(mux)
}

func TestNewLocalAntigravityBackend_Success(t *testing.T) {
	resp := antigravityChatResponse{
		Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{{Message: struct {
			Content string `json:"content"`
		}{Content: "ok"}}},
	}
	srv := newChatServer(t, resp)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("gemini-3.5-flash")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestNewLocalAntigravityBackend_HealthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)

	if _, err := NewLocalAntigravityBackend(""); err == nil {
		t.Fatal("expected error when /health returns 500")
	}
}

func TestNewLocalAntigravityBackend_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	t.Setenv("ANTIGRAVITY_BASE_URL", url)

	if _, err := NewLocalAntigravityBackend(""); err == nil {
		t.Fatal("expected error when bridge is unreachable")
	}
}

func TestLocalAntigravityBackend_Execute_Success(t *testing.T) {
	resp := antigravityChatResponse{
		Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{{Message: struct {
			Content string `json:"content"`
		}{Content: "hello"}}},
	}
	srv := newChatServer(t, resp)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("gemini-3.5-flash")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	out, err := b.Execute(context.Background(), "hi", "gemini-3.5-flash")
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("expected %q, got %q", "hello", out)
	}
}

func TestLocalAntigravityBackend_Execute_EmptyPrompt(t *testing.T) {
	resp := antigravityChatResponse{}
	srv := newChatServer(t, resp)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	if _, err := b.Execute(context.Background(), "", "gemini-3.5-flash"); err == nil {
		t.Fatal("expected error on empty prompt")
	}
}

func TestLocalAntigravityBackend_Execute_BridgeError(t *testing.T) {
	errMsg := "boom"
	resp := antigravityChatResponse{
		Error: &struct {
			Message string `json:"message"`
		}{Message: errMsg},
	}
	srv := newChatServer(t, resp)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	_, err = b.Execute(context.Background(), "hi", "gemini-3.5-flash")
	if err == nil {
		t.Fatal("expected error from bridge")
	}
	if !strings.Contains(err.Error(), errMsg) {
		t.Fatalf("expected error mentioning %q, got: %v", errMsg, err)
	}
}

func TestLocalAntigravityBackend_Execute_EmptyResponse(t *testing.T) {
	resp := antigravityChatResponse{Choices: nil}
	srv := newChatServer(t, resp)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	if _, err := b.Execute(context.Background(), "hi", "gemini-3.5-flash"); err == nil {
		t.Fatal("expected error on empty choices")
	}
}

func TestLocalAntigravityBackend_ExecuteInDir_Delegates(t *testing.T) {
	resp := antigravityChatResponse{
		Choices: []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{{Message: struct {
			Content string `json:"content"`
		}{Content: "hello"}}},
	}
	srv := newChatServer(t, resp)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("gemini-3.5-flash")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	out, err := b.ExecuteInDir(context.Background(), "hi", "gemini-3.5-flash", "/some/dir")
	if err != nil {
		t.Fatalf("ExecuteInDir returned error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("expected %q, got %q", "hello", out)
	}
}

func TestLocalAntigravityBackend_NameAndModels(t *testing.T) {
	resp := antigravityChatResponse{}
	srv := newChatServer(t, resp)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BASE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("gemini-3.5-flash")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	if !strings.Contains(b.Name(), "local-antigravity") {
		t.Errorf("Name() = %q, want it to contain 'local-antigravity'", b.Name())
	}

	models := b.Models()
	if len(models) == 0 {
		t.Error("Models() returned empty list")
	}
}
