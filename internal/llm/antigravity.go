package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

const defaultAntigravityBaseURL = "http://localhost:8080"

// LocalAntigravityBackend calls a locally-running Antigravity server via the
// OpenAI-compatible /v1/chat/completions endpoint. Set ANTIGRAVITY_BASE_URL to
// override the default base URL (useful in tests).
type LocalAntigravityBackend struct {
	name    string
	models  []string
	baseURL string
	client  *http.Client
}

// NewLocalAntigravityBackend creates a backend that routes to the local
// Antigravity bridge. The base URL is read from ANTIGRAVITY_BASE_URL; if unset,
// it defaults to http://localhost:8080.
func NewLocalAntigravityBackend() *LocalAntigravityBackend {
	baseURL := os.Getenv("ANTIGRAVITY_BASE_URL")
	if baseURL == "" {
		baseURL = defaultAntigravityBaseURL
	}
	return &LocalAntigravityBackend{
		name: "local-antigravity",
		models: []string{
			"gemini-flash-3.5",
			"gemini-pro-3.5",
		},
		baseURL: baseURL,
		client:  &http.Client{},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
}

type chatChoice struct {
	Message chatMessage `json:"message"`
}

type chatResponse struct {
	Choices []chatChoice `json:"choices"`
}

// Execute sends a prompt to the Antigravity server and returns the response.
func (b *LocalAntigravityBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	return b.ExecuteInDir(ctx, prompt, model, "")
}

// ExecuteInDir sends a prompt to the Antigravity server. The workDir parameter
// is accepted for interface compatibility but is not used by this backend.
func (b *LocalAntigravityBackend) ExecuteInDir(ctx context.Context, prompt string, model string, _ string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("execute: prompt cannot be empty")
	}

	if model == "" {
		model = "gemini-flash-3.5"
	}

	if !b.supportsModel(model) {
		return "", fmt.Errorf("execute: unsupported model %q (supported: %v)", model, b.models)
	}

	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("execute: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("execute: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute: http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("execute: server returned status %d", resp.StatusCode)
	}

	var chat chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chat); err != nil {
		return "", fmt.Errorf("execute: decode response: %w", err)
	}

	if len(chat.Choices) == 0 {
		return "", fmt.Errorf("execute: empty choices in response")
	}

	return chat.Choices[0].Message.Content, nil
}

// Name returns the backend identifier.
func (b *LocalAntigravityBackend) Name() string { return b.name }

// Models returns the list of models available from this backend.
func (b *LocalAntigravityBackend) Models() []string { return b.models }

func (b *LocalAntigravityBackend) supportsModel(model string) bool {
	for _, m := range b.models {
		if m == model {
			return true
		}
	}
	return false
}
