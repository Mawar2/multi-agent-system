package llm

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

const (
	defaultAntigravityBaseURL = "http://localhost:8080"
	antigravityHealthPath     = "/health"
	antigravityCompletionPath = "/v1/chat/completions"
	antigravityHealthTimeout  = 5 * time.Second
	antigravityRequestTimeout = 10 * time.Minute // bridge can be slow
)

// LocalAntigravityBackend implements LLMBackend by forwarding prompts to a local
// Antigravity bridge process.  The bridge proxies Gemini API calls via the user's
// Antigravity subscription, enabling zero-cost Gemini access in development.
//
// The bridge exposes an OpenAI-compatible chat-completions endpoint.  It acts as
// an agentic assistant in its own project context (not the per-worker clone), so
// callers should use GeminiWorker's plan-execute pattern rather than expecting the
// bridge to perform git operations directly.
type LocalAntigravityBackend struct {
	model      string
	baseURL    string
	httpClient *http.Client
	name       string
}

// antigravityChatRequest is the OpenAI-compatible payload for the bridge.
type antigravityChatRequest struct {
	Model       string                   `json:"model"`
	Messages    []antigravityChatMessage `json:"messages"`
	Temperature float64                  `json:"temperature"`
}

type antigravityChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// antigravityChatResponse is the OpenAI-compatible response from the bridge.
type antigravityChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// NewLocalAntigravityBackend creates a LocalAntigravityBackend and performs a
// health check against the local bridge.  Returns an error if the bridge is
// unreachable so callers can fall back to another backend.
//
// The base URL defaults to http://localhost:8080 but can be overridden with
// the ANTIGRAVITY_BASE_URL environment variable (useful in tests).
func NewLocalAntigravityBackend(model string) (*LocalAntigravityBackend, error) {
	baseURL := defaultAntigravityBaseURL
	if v := os.Getenv("ANTIGRAVITY_BASE_URL"); v != "" {
		baseURL = v
	}
	b := &LocalAntigravityBackend{
		model:   model,
		baseURL: baseURL,
		name:    fmt.Sprintf("local-antigravity-%s", model),
		httpClient: &http.Client{
			Timeout: antigravityRequestTimeout,
		},
	}

	healthCtx, cancel := context.WithTimeout(context.Background(), antigravityHealthTimeout)
	defer cancel()
	if err := b.healthCheck(healthCtx); err != nil {
		return nil, fmt.Errorf("antigravity bridge unavailable at %s: %w", b.baseURL, err)
	}

	return b, nil
}

func (b *LocalAntigravityBackend) healthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+antigravityHealthPath, nil)
	if err != nil {
		return err
	}
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("health check returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// Execute sends a prompt to the bridge and returns the text response.
func (b *LocalAntigravityBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	return b.ExecuteInDir(ctx, prompt, model, "")
}

// ExecuteInDir sends a prompt to the bridge.  The workDir parameter is included
// in the system message as metadata for the bridge, but the bridge performs no
// file operations itself — that is the caller's responsibility.
func (b *LocalAntigravityBackend) ExecuteInDir(ctx context.Context, prompt string, model string, workDir string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("execute: prompt cannot be empty")
	}
	if model == "" {
		model = b.model
	}

	messages := []antigravityChatMessage{
		{Role: "user", Content: prompt},
	}
	if workDir != "" {
		// Prepend a system message so the bridge has repo context.
		messages = []antigravityChatMessage{
			{Role: "system", Content: fmt.Sprintf("Working directory: %s", workDir)},
			{Role: "user", Content: prompt},
		}
	}

	payload := antigravityChatRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0.1, // low temperature for structured output
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.baseURL+antigravityCompletionPath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read bridge response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("bridge returned HTTP %d: %s", resp.StatusCode, respBody)
	}

	var chatResp antigravityChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		// Bridge returned non-JSON — treat the raw body as the content.
		return string(respBody), nil
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("bridge error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 || chatResp.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("bridge returned empty response")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// Name returns the backend identifier.
func (b *LocalAntigravityBackend) Name() string { return b.name }

// Models returns the Gemini model this backend is configured for.
func (b *LocalAntigravityBackend) Models() []string { return []string{b.model} }
