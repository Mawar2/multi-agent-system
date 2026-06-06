package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

// Compile-time assertion that LocalAntigravityBackend satisfies LLMBackend.
var _ LLMBackend = (*LocalAntigravityBackend)(nil)

// LocalAntigravityBackend implements LLMBackend by connecting to the Antigravity Bridge Server.
// This uses the user's paid Antigravity subscription without additional API costs.
//
// Architecture:
// - Connects to a local HTTP bridge server (cmd/antigravity-bridge)
// - Bridge server runs in Antigravity's terminal (inherits CSRF token)
// - Bridge calls agentapi.bat which has authenticated access
// - Zero API charges - all usage goes through the paid subscription plan
//
// The bridge server MUST be running in Antigravity's integrated terminal for this to work.
type LocalAntigravityBackend struct {
	// bridgeURL is the URL of the Antigravity bridge server
	bridgeURL string

	// httpClient for making requests to the bridge
	httpClient *http.Client

	// primaryModel is the preferred model for this backend instance.
	// It controls the ordering returned by Models() (Models()[0] is the
	// model a worker tier will actually use).
	primaryModel string
}

// promptRequest is sent to the bridge server
type promptRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"`
}

// promptResponse is received from the bridge server
type promptResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// NewLocalAntigravityBackend creates a new local Antigravity backend that connects
// to the Antigravity Bridge Server.
//
// The primaryModel parameter selects the preferred model for this instance and
// controls the ordering of Models(). If empty, it defaults to "gemini-3.5-flash".
//
// The bridge server must be running in Antigravity's integrated terminal:
//
//	cd multi-agent-system
//	go run cmd/antigravity-bridge/main.go
//
// Or use the compiled binary:
//
//	./bin/antigravity-bridge.exe
//
// The bridge server inherits Antigravity's authentication context, including the CSRF token.
//
// Returns an error if the bridge server is not reachable.
func NewLocalAntigravityBackend(primaryModel string) (*LocalAntigravityBackend, error) {
	// Default primary model
	if primaryModel == "" {
		primaryModel = "gemini-3.5-flash"
	}

	// Default bridge URL
	bridgeURL := "http://localhost:8765"

	// Allow override via environment variable
	if envURL := os.Getenv("ANTIGRAVITY_BRIDGE_URL"); envURL != "" {
		bridgeURL = envURL
	}

	backend := &LocalAntigravityBackend{
		bridgeURL:    bridgeURL,
		httpClient:   &http.Client{},
		primaryModel: primaryModel,
	}

	// Test connectivity
	resp, err := backend.httpClient.Get(bridgeURL + "/health")
	if err != nil {
		return nil, fmt.Errorf("bridge server not reachable at %s: %w\n"+
			"Make sure the bridge server is running in Antigravity's terminal:\n"+
			"  go run cmd/antigravity-bridge/main.go", bridgeURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bridge server returned status %d (expected 200)", resp.StatusCode)
	}

	return backend, nil
}

// Execute sends a prompt to the Antigravity Bridge Server and returns the response.
//
// The bridge server (running in Antigravity's terminal) forwards the request to agentapi.bat
// which has authenticated access to the language server.
//
// The model parameter maps to Antigravity's model selection:
// - "gemini-3.5-flash" / "gemini-flash" → flash
// - "gemini-3.5-pro" / "gemini-pro" → pro
// - "gemini-flash-lite" → flash_lite
// - "" (empty) → defaults to flash
//
// Returns an error if the bridge server is unreachable or returns an error.
func (b *LocalAntigravityBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("execute: prompt cannot be empty")
	}

	// Map model names to Antigravity's model values
	modelValue := "flash" // Default to flash
	switch model {
	case "gemini-3.5-flash", "gemini-flash":
		modelValue = "flash"
	case "gemini-3.5-pro", "gemini-pro":
		modelValue = "pro"
	case "gemini-flash-lite":
		modelValue = "flash_lite"
	}

	// Build request
	reqBody := promptRequest{
		Prompt: prompt,
		Model:  modelValue,
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, "POST", b.bridgeURL+"/prompt", bytes.NewBuffer(reqJSON))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge server request failed: %w\n"+
			"Is the bridge server running in Antigravity's terminal?", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	// Parse response
	var promptResp promptResponse
	if err := json.Unmarshal(body, &promptResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w\nBody: %s", err, string(body))
	}

	// Check for error from bridge
	if promptResp.Error != "" {
		return "", fmt.Errorf("bridge server error: %s", promptResp.Error)
	}

	if promptResp.Response == "" {
		return "", fmt.Errorf("bridge server returned empty response")
	}

	return promptResp.Response, nil
}

// ExecuteInDir sends a prompt to the Antigravity Bridge Server, ignoring workDir.
//
// LIMITATION: The bridge has no concept of a working directory. It relays prompts
// to the Antigravity language server, which always operates in Antigravity's own
// open project context — NOT the orchestrator's per-worker clone. Therefore workDir
// is intentionally not forwarded; this method simply delegates to Execute.
func (b *LocalAntigravityBackend) ExecuteInDir(ctx context.Context, prompt string, model string, workDir string) (string, error) {
	// workDir is intentionally ignored: the bridge runs in Antigravity's own
	// project context, not the orchestrator's per-worker clone.
	return b.Execute(ctx, prompt, model)
}

// Name returns the backend identifier.
func (b *LocalAntigravityBackend) Name() string {
	return "local-antigravity-cli"
}

// Models returns the list of models available through Antigravity, ordered so that
// the configured primary model is first. Workers select Models()[0] as the model
// to use, so this controls which Gemini model a tier uses.
func (b *LocalAntigravityBackend) Models() []string {
	if b.primaryModel == "gemini-3.5-pro" {
		return []string{"gemini-3.5-pro", "gemini-3.5-flash"}
	}
	return []string{"gemini-3.5-flash", "gemini-3.5-pro"}
}
