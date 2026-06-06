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

// LocalAntigravityBackend implements LLMBackend using a local Antigravity
// server exposing an OpenAI-compatible /v1/chat/completions endpoint.
// Set ANTIGRAVITY_BASE_URL to override the default base URL (for testing).
type LocalAntigravityBackend struct {
	name    string
	models  []string
	baseURL string
	client  *http.Client
}

// NewLocalAntigravityBackend creates a new Antigravity backend.
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

type chatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
}

// Execute sends a prompt to the Antigravity server and returns the response.
func (b *LocalAntigravityBackend) Execute(ctx context.Context, prompt string, model string) (string, error) {
	return b.ExecuteInDir(ctx, prompt, model, "")
}

// ExecuteInDir sends a prompt to the Antigravity server.
// workDir is accepted for interface compatibility but not used by this backend.
func (b *LocalAntigravityBackend) ExecuteInDir(ctx context.Context, prompt string, model string, workDir string) (string, error) {
	if prompt == "" {
		return "", fmt.Errorf("execute: prompt cannot be empty")
	}

	if model == "" {
		model = "gemini-flash-3.5"
	}

	if !b.supportsModel(model) {
		return "", fmt.Errorf("execute: unsupported model %q (supported: %v)", model, b.models)
	}

	reqBody := chatCompletionRequest{
		Model: model,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("execute: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
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
		return "", fmt.Errorf("execute: antigravity returned status %d", resp.StatusCode)
	}

	var result chatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("execute: decode response: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("execute: antigravity returned no choices")
	}

	return result.Choices[0].Message.Content, nil
}

// Name returns the backend identifier.
func (b *LocalAntigravityBackend) Name() string {
	return b.name
}

// Models returns the list of models available via this backend.
func (b *LocalAntigravityBackend) Models() []string {
	return b.models
}

// supportsModel checks if the backend supports the given model name.
func (b *LocalAntigravityBackend) supportsModel(model string) bool {
	for _, m := range b.models {
		if m == model {
			return true
		}
	}
	return false
}
