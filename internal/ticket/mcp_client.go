package ticket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// HTTPMCPClient implements MCPClient using HTTP transport to communicate with MCP server.
// This client sends JSON-RPC style requests to the MCP server and parses responses.
type HTTPMCPClient struct {
	serverURL string
	authToken string
	client    *http.Client
}

// NewHTTPMCPClient creates a new MCP client that communicates via HTTP.
// serverURL: Base URL of the MCP server (e.g., "https://api.githubcopilot.com/mcp/")
// authToken: GitHub Personal Access Token for authorization
func NewHTTPMCPClient(serverURL, authToken string) *HTTPMCPClient {
	return &HTTPMCPClient{
		serverURL: serverURL,
		authToken: authToken,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewHTTPMCPClientFromEnv creates a new MCP client using environment variables.
// Reads MCP_SERVER_URL (defaults to GitHub Copilot MCP endpoint) and GITHUB_TOKEN.
func NewHTTPMCPClientFromEnv() (*HTTPMCPClient, error) {
	serverURL := os.Getenv("MCP_SERVER_URL")
	if serverURL == "" {
		serverURL = "https://api.githubcopilot.com/mcp/"
	}

	authToken := os.Getenv("GITHUB_TOKEN")
	if authToken == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN environment variable is required")
	}

	return NewHTTPMCPClient(serverURL, authToken), nil
}

// mcpRequest represents the JSON-RPC 2.0 request sent to MCP server.
type mcpRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params"`
	ID      int                    `json:"id"`
}

// mcpResponse represents the response from MCP server.
type mcpResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  *mcpError   `json:"error,omitempty"`
}

// mcpError represents an error returned by the MCP server.
type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Call invokes an MCP tool via HTTP request.
// This method:
// 1. Marshals the tool name and params to JSON
// 2. Sends POST request to MCP server
// 3. Parses the response
// 4. Returns the result or error
func (c *HTTPMCPClient) Call(ctx context.Context, tool string, params map[string]interface{}) (interface{}, error) {
	// Build JSON-RPC 2.0 request payload
	reqPayload := mcpRequest{
		JSONRPC: "2.0",
		Method:  tool,
		Params:  params,
		ID:      1, // Simple incrementing ID
	}

	reqBody, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", c.serverURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.authToken)

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP server returned status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse MCP response
	var mcpResp mcpResponse
	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for MCP error
	if mcpResp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", mcpResp.Error.Code, mcpResp.Error.Message)
	}

	return mcpResp.Result, nil
}
