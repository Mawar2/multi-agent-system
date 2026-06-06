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

const defaultAntigravityBaseURL = "http://localhost:8080"

// LocalAntigravityBackend implements LLMBackend using an OpenAI-compatible
// /v1/chat/completions endpoint served by the local Antigravity bridge.
// Set ANTIGRAVITY_BASE_URL to override the default endpoint for testing.
type LocalAntigravityBackend struct {
	name    string
	models  []string
	baseURL string
	client  *http.Client
}

// NewLocalAntigravityBackend creates a new Antigravity backend.
// The base URL defaults to http://localhost:8080 and can be overridden
// via the ANTIGRAVITY_BASE_URL environment variable.
func NewLocalAntigravityBackend() *LocalAntigravityBackend {
	baseURL := os.Getenv("ANTIGRAVITY_BASE_URL")
	if baseURL == "" {
		baseURL = defaultAntigravityBaseURL
	}
	return &LocalAntigravityBackend{
		name: "antigravity",
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

// Execute sends a prompt to the Antigravity bridge and returns the response.
func (b *LocalAntigravityBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	return b.ExecuteInDir(ctx, prompt, model, "")
}

// ExecuteInDir sends a prompt to the Antigravity bridge.
// workDir is ignored for this backend (remote HTTP call, no local context).
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

	reqBody := chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("execute: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("execute: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("execute: http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("execute: antigravity returned status %d: %s", resp.StatusCode, string(body))
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("execute: decode response: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("execute: antigravity returned no choices")
	}

	content := chatResp.Choices[0].Message.Content
	if content == "" {
		return "", fmt.Errorf("execute: antigravity returned empty content")
	}

	return content, nil
}

// Name returns the backend identifier.
func (b *LocalAntigravityBackend) Name() string {
	return b.name
}

// Models returns the list of models available from this backend.
func (b *LocalAntigravityBackend) Models() []string {
	return b.models
}

func (b *LocalAntigravityBackend) supportsModel(model string) bool {
	for _, m := range b.models {
		if m == model {
			return true
		}
	}
	return false
}
