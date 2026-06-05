package ticket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNewHTTPMCPClient(t *testing.T) {
	client := NewHTTPMCPClient("https://example.com/mcp", "test-token")
	if client == nil {
		t.Fatal("expected client to be created")
	}
	if client.serverURL != "https://example.com/mcp" {
		t.Errorf("expected serverURL to be 'https://example.com/mcp', got %s", client.serverURL)
	}
	if client.authToken != "test-token" {
		t.Errorf("expected authToken to be 'test-token', got %s", client.authToken)
	}
}

func TestNewHTTPMCPClientFromEnv(t *testing.T) {
	// Save original env vars
	originalURL := os.Getenv("MCP_SERVER_URL")
	originalToken := os.Getenv("GITHUB_TOKEN")
	defer func() {
		os.Setenv("MCP_SERVER_URL", originalURL)
		os.Setenv("GITHUB_TOKEN", originalToken)
	}()

	t.Run("with both env vars set", func(t *testing.T) {
		os.Setenv("MCP_SERVER_URL", "https://custom.mcp.server/")
		os.Setenv("GITHUB_TOKEN", "test-token-123")

		client, err := NewHTTPMCPClientFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client.serverURL != "https://custom.mcp.server/" {
			t.Errorf("expected custom server URL, got %s", client.serverURL)
		}
		if client.authToken != "test-token-123" {
			t.Errorf("expected custom token, got %s", client.authToken)
		}
	})

	t.Run("with default server URL", func(t *testing.T) {
		os.Unsetenv("MCP_SERVER_URL")
		os.Setenv("GITHUB_TOKEN", "test-token-456")

		client, err := NewHTTPMCPClientFromEnv()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if client.serverURL != "https://api.githubcopilot.com/mcp/" {
			t.Errorf("expected default GitHub Copilot URL, got %s", client.serverURL)
		}
	})

	t.Run("missing GITHUB_TOKEN", func(t *testing.T) {
		os.Unsetenv("GITHUB_TOKEN")

		_, err := NewHTTPMCPClientFromEnv()
		if err == nil {
			t.Error("expected error when GITHUB_TOKEN is missing")
		}
	})
}

func TestHTTPMCPClient_Call_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and headers
		if r.Method != "POST" {
			t.Errorf("expected POST request, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type: application/json")
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Authorization header with token")
		}

		// Parse request body
		var req mcpRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		// Verify request content
		if req.Method != "mcp__github__list_issues" {
			t.Errorf("expected tool 'mcp__github__list_issues', got %s", req.Method)
		}

		// Send success response
		resp := mcpResponse{
			Result: map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{
						"number": float64(123),
						"title":  "Test Issue",
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create client pointing to mock server
	client := NewHTTPMCPClient(server.URL, "test-token")

	// Call MCP tool
	result, err := client.Call(context.Background(), "mcp__github__list_issues", map[string]interface{}{
		"owner": "testowner",
		"repo":  "testrepo",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify result
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatal("expected result to be a map")
	}

	items, ok := resultMap["items"].([]interface{})
	if !ok {
		t.Fatal("expected items to be an array")
	}

	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}
}

func TestHTTPMCPClient_Call_MCPError(t *testing.T) {
	// Create mock server that returns MCP error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := mcpResponse{
			Error: &mcpError{
				Code:    404,
				Message: "Repository not found",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewHTTPMCPClient(server.URL, "test-token")

	_, err := client.Call(context.Background(), "mcp__github__list_issues", map[string]interface{}{
		"owner": "nonexistent",
		"repo":  "repo",
	})

	if err == nil {
		t.Fatal("expected error")
	}

	expectedMsg := "MCP error 404: Repository not found"
	if err.Error() != expectedMsg {
		t.Errorf("expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}

func TestHTTPMCPClient_Call_HTTPError(t *testing.T) {
	// Create mock server that returns HTTP error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Internal Server Error"))
	}))
	defer server.Close()

	client := NewHTTPMCPClient(server.URL, "test-token")

	_, err := client.Call(context.Background(), "mcp__github__list_issues", nil)

	if err == nil {
		t.Fatal("expected error")
	}

	// Should contain status code in error
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestHTTPMCPClient_Call_InvalidJSON(t *testing.T) {
	// Create mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{invalid json"))
	}))
	defer server.Close()

	client := NewHTTPMCPClient(server.URL, "test-token")

	_, err := client.Call(context.Background(), "mcp__github__list_issues", nil)

	if err == nil {
		t.Fatal("expected error")
	}
}

func TestHTTPMCPClient_Call_ContextCancellation(t *testing.T) {
	// Create mock server with slow response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This will never finish
		select {}
	}))
	defer server.Close()

	client := NewHTTPMCPClient(server.URL, "test-token")

	// Create context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Call(ctx, "mcp__github__list_issues", nil)

	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
}
