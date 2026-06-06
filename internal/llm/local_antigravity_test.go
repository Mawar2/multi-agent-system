package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newPromptServer returns an httptest server that serves /health (200) and
// /prompt. The /prompt handler decodes the request, records the model field
// into *gotModel (if non-nil), and writes the supplied promptResponse.
func newPromptServer(t *testing.T, resp promptResponse, gotModel *string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/prompt", func(w http.ResponseWriter, r *http.Request) {
		var req promptRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if gotModel != nil {
			*gotModel = req.Model
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})
	return httptest.NewServer(mux)
}

func TestNewLocalAntigravityBackend_Success(t *testing.T) {
	srv := newPromptServer(t, promptResponse{Response: "ok"}, nil)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BRIDGE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if b == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestNewLocalAntigravityBackend_HealthFailure(t *testing.T) {
	// Server that returns 500 on /health.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BRIDGE_URL", srv.URL)

	if _, err := NewLocalAntigravityBackend(""); err == nil {
		t.Fatal("expected error when /health returns 500")
	}
}

func TestNewLocalAntigravityBackend_Unreachable(t *testing.T) {
	// Start a server then close it to get an unused URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	t.Setenv("ANTIGRAVITY_BRIDGE_URL", url)

	if _, err := NewLocalAntigravityBackend(""); err == nil {
		t.Fatal("expected error when bridge is unreachable")
	}
}

func TestExecute_Success(t *testing.T) {
	srv := newPromptServer(t, promptResponse{Response: "hello"}, nil)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BRIDGE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("")
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

func TestExecute_BridgeError(t *testing.T) {
	srv := newPromptServer(t, promptResponse{Response: "", Error: "boom"}, nil)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BRIDGE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	_, err = b.Execute(context.Background(), "hi", "gemini-3.5-flash")
	if err == nil {
		t.Fatal("expected error from bridge")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error mentioning %q, got: %v", "boom", err)
	}
}

func TestExecute_EmptyResponse(t *testing.T) {
	srv := newPromptServer(t, promptResponse{Response: ""}, nil)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BRIDGE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	if _, err := b.Execute(context.Background(), "hi", "gemini-3.5-flash"); err == nil {
		t.Fatal("expected error on empty response")
	}
}

func TestExecute_EmptyPrompt(t *testing.T) {
	srv := newPromptServer(t, promptResponse{Response: "x"}, nil)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BRIDGE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	if _, err := b.Execute(context.Background(), "", "gemini-3.5-flash"); err == nil {
		t.Fatal("expected error on empty prompt")
	}
}

func TestExecute_ModelMapping(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"gemini-3.5-pro", "pro"},
		{"gemini-pro", "pro"},
		{"gemini-3.5-flash", "flash"},
		{"gemini-flash", "flash"},
		{"", "flash"},
		{"gemini-flash-lite", "flash_lite"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			var gotModel string
			srv := newPromptServer(t, promptResponse{Response: "ok"}, &gotModel)
			defer srv.Close()
			t.Setenv("ANTIGRAVITY_BRIDGE_URL", srv.URL)

			b, err := NewLocalAntigravityBackend("")
			if err != nil {
				t.Fatalf("constructor failed: %v", err)
			}

			if _, err := b.Execute(context.Background(), "hi", tc.input); err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			if gotModel != tc.want {
				t.Fatalf("input %q: expected model %q sent to bridge, got %q", tc.input, tc.want, gotModel)
			}
		})
	}
}

func TestExecuteInDir_Delegates(t *testing.T) {
	var gotModel string
	srv := newPromptServer(t, promptResponse{Response: "hello"}, &gotModel)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BRIDGE_URL", srv.URL)

	b, err := NewLocalAntigravityBackend("")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}

	out, err := b.ExecuteInDir(context.Background(), "hi", "gemini-3.5-pro", "/some/dir")
	if err != nil {
		t.Fatalf("ExecuteInDir returned error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("expected %q, got %q", "hello", out)
	}
	// workDir has no effect; model mapping still applies as in Execute.
	if gotModel != "pro" {
		t.Fatalf("expected model %q sent to bridge, got %q", "pro", gotModel)
	}
}

func TestModels_Ordering(t *testing.T) {
	srv := newPromptServer(t, promptResponse{Response: "ok"}, nil)
	defer srv.Close()
	t.Setenv("ANTIGRAVITY_BRIDGE_URL", srv.URL)

	proBackend, err := NewLocalAntigravityBackend("gemini-3.5-pro")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}
	if got := proBackend.Models()[0]; got != "gemini-3.5-pro" {
		t.Fatalf("expected pro primary, got %q", got)
	}

	defaultBackend, err := NewLocalAntigravityBackend("")
	if err != nil {
		t.Fatalf("constructor failed: %v", err)
	}
	if got := defaultBackend.Models()[0]; got != "gemini-3.5-flash" {
		t.Fatalf("expected flash primary, got %q", got)
	}
}
