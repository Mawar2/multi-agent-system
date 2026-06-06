package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// PromptRequest is the JSON body for /prompt endpoint
type PromptRequest struct {
	Prompt string `json:"prompt"`
	Model  string `json:"model"` // "flash", "flash_lite", or "pro"
}

// PromptResponse is the JSON response from /prompt endpoint
type PromptResponse struct {
	Response string `json:"response"`
	Error    string `json:"error,omitempty"`
}

// AntigravityBridge runs an HTTP server that bridges to agentapi.bat
// This server MUST be run from within Antigravity's integrated terminal
// so it inherits the CSRF token from the authenticated parent process.
type AntigravityBridge struct {
	port         string
	agentAPIPath string
}

func main() {
	// Load Antigravity session environment variables if not already set
	loadAntigravityEnv()

	port := flag.String("port", "8765", "Port to listen on")
	flag.Parse()

	// Find agentapi.bat in the standard location
	homeDir := os.Getenv("USERPROFILE") // Windows
	if homeDir == "" {
		homeDir = os.Getenv("HOME") // Unix/Linux
	}

	agentAPIPath := filepath.Join(homeDir, ".gemini", "antigravity", "bin", "agentapi.bat")

	// Check if agentapi.bat exists
	if _, err := os.Stat(agentAPIPath); err != nil {
		log.Fatalf("agentapi.bat not found at %s. Is Antigravity installed?", agentAPIPath)
	}

	// Check if ANTIGRAVITY_LS_ADDRESS is set
	if os.Getenv("ANTIGRAVITY_LS_ADDRESS") == "" {
		log.Println("WARNING: ANTIGRAVITY_LS_ADDRESS not set. Attempting to continue anyway...")
		log.Println("If requests fail, you may need to set this environment variable.")
	}

	bridge := &AntigravityBridge{
		port:         *port,
		agentAPIPath: agentAPIPath,
	}

	http.HandleFunc("/", bridge.handleRoot)
	http.HandleFunc("/prompt", bridge.handlePrompt)
	http.HandleFunc("/health", bridge.handleHealth)

	addr := fmt.Sprintf("localhost:%s", bridge.port)
	log.Printf("Antigravity Bridge Server starting on http://%s", addr)
	log.Printf("Using agentapi at: %s", agentAPIPath)
	log.Println()
	log.Println("IMPORTANT: This server MUST be run from within Antigravity's integrated terminal")
	log.Println("           to inherit the authenticated session and CSRF token.")
	log.Println()
	log.Printf("Test with: curl -X POST http://%s/prompt -H 'Content-Type: application/json' -d '{\"prompt\":\"What is 2+2?\",\"model\":\"flash\"}'", addr)
	log.Println()
	log.Println("Press Ctrl+C to stop")

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func (b *AntigravityBridge) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>Antigravity Bridge Server</title>
    <style>
        body { font-family: monospace; max-width: 800px; margin: 50px auto; padding: 20px; }
        h1 { color: #1a73e8; }
        .endpoint { background: #f5f5f5; padding: 10px; margin: 10px 0; border-radius: 4px; }
        .important { color: #d93025; font-weight: bold; }
    </style>
</head>
<body>
    <h1>Antigravity Bridge Server</h1>
    <p><strong>Status:</strong> Running on port %s</p>
    <p class="important">⚠️ This server MUST run in Antigravity's terminal to inherit auth</p>

    <h2>Available Endpoints</h2>

    <div class="endpoint">
        <strong>POST /prompt</strong><br>
        Send a prompt to Gemini via Antigravity<br>
        Body: {"prompt": "your prompt", "model": "flash|flash_lite|pro"}
    </div>

    <div class="endpoint">
        <strong>GET /health</strong><br>
        Check if the bridge is running
    </div>

    <h2>Example Usage</h2>
    <pre>curl -X POST http://localhost:%s/prompt \
  -H 'Content-Type: application/json' \
  -d '{"prompt":"What is 2+2?","model":"flash"}'</pre>

    <h2>Architecture</h2>
    <pre>
Your Go Backend
  └─> HTTP POST → Antigravity Bridge (this server)
                    └─> agentapi.bat (inherits CSRF token)
                          └─> language_server (authenticated)
    </pre>
</body>
</html>`, b.port, b.port)
}

func (b *AntigravityBridge) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"server": "antigravity-bridge",
	})
}

func (b *AntigravityBridge) handlePrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req PromptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if req.Prompt == "" {
		http.Error(w, "Prompt cannot be empty", http.StatusBadRequest)
		return
	}

	// Default to flash if no model specified
	if req.Model == "" {
		req.Model = "flash"
	}

	// Validate model
	validModels := map[string]bool{"flash": true, "flash_lite": true, "pro": true}
	if !validModels[req.Model] {
		http.Error(w, fmt.Sprintf("Invalid model: %s (must be flash, flash_lite, or pro)", req.Model), http.StatusBadRequest)
		return
	}

	log.Printf("Received prompt request: model=%s, prompt_length=%d", req.Model, len(req.Prompt))

	// Call agentapi.bat
	response, err := b.callAgentAPI(req.Prompt, req.Model)

	// Build response
	resp := PromptResponse{
		Response: response,
	}
	if err != nil {
		resp.Error = err.Error()
		log.Printf("Error from agentapi: %v", err)
	} else {
		log.Printf("Success: response_length=%d", len(response))
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (b *AntigravityBridge) callAgentAPI(prompt, model string) (string, error) {
	// Build command: agentapi.bat new-conversation --model=<model> "<prompt>"
	cmd := exec.Command(b.agentAPIPath, "new-conversation", fmt.Sprintf("--model=%s", model), prompt)

	// Capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("agentapi failed: %w\nOutput: %s", err, string(output))
	}

	outputStr := strings.TrimSpace(string(output))
	if outputStr == "" {
		return "", fmt.Errorf("agentapi returned empty response")
	}

	// Extract the JSON part of the output (in case there are warning logs printed before it)
	firstBrace := strings.Index(outputStr, "{")
	lastBrace := strings.LastIndex(outputStr, "}")
	if firstBrace == -1 || lastBrace == -1 || firstBrace > lastBrace {
		return "", fmt.Errorf("agentapi output is not valid JSON:\n%s", outputStr)
	}
	jsonStr := outputStr[firstBrace : lastBrace+1]

	// Parse conversationId from response
	type AgentAPIResponse struct {
		Response struct {
			NewConversation struct {
				ConversationID string `json:"conversationId"`
			} `json:"newConversation"`
		} `json:"response"`
		Error string `json:"error,omitempty"`
	}

	var apiResp AgentAPIResponse
	if err := json.Unmarshal([]byte(jsonStr), &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse agentapi JSON output: %w\nJSON: %s", err, jsonStr)
	}

	if apiResp.Error != "" {
		return "", fmt.Errorf("agentapi returned error: %s", apiResp.Error)
	}

	convID := apiResp.Response.NewConversation.ConversationID
	if convID == "" {
		return "", fmt.Errorf("no conversation ID returned by agentapi\nJSON: %s", jsonStr)
	}

	log.Printf("Created conversation %s. Waiting for model response...", convID)

	// Construct path to transcript.jsonl
	homeDir := os.Getenv("USERPROFILE")
	if homeDir == "" {
		homeDir = os.Getenv("HOME")
	}
	transcriptPath := filepath.Join(homeDir, ".gemini", "antigravity", "brain", convID, ".system_generated", "logs", "transcript.jsonl")

	type TranscriptLine struct {
		StepIndex int    `json:"step_index"`
		Source    string `json:"source"`
		Type      string `json:"type"`
		Status    string `json:"status"`
		Content   string `json:"content"`
	}

	// Poll the transcript.jsonl file
	maxAttempts := 600 // 10 minutes (600 * 1s)
	var finalResponse string
	var errPoll error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		time.Sleep(1 * time.Second)

		// Check if file exists
		if _, err := os.Stat(transcriptPath); os.IsNotExist(err) {
			continue
		}

		// Read file contents
		data, err := os.ReadFile(transcriptPath)
		if err != nil {
			log.Printf("[Attempt %d] Failed to read transcript: %v", attempt, err)
			continue
		}

		// Parse line by line
		lines := strings.Split(string(data), "\n")
		var modelFinished bool
		var modelError string

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			var step TranscriptLine
			if err := json.Unmarshal([]byte(line), &step); err != nil {
				continue
			}

			// We look for the model's planner response
			if step.Source == "MODEL" && step.Type == "PLANNER_RESPONSE" {
				if step.Status == "DONE" {
					finalResponse = step.Content
					modelFinished = true
					break
				} else if step.Status == "ERROR" {
					modelError = step.Content
					if modelError == "" {
						modelError = "Model execution failed"
					}
					modelFinished = true
					break
				}
			}
		}

		if modelFinished {
			if modelError != "" {
				errPoll = fmt.Errorf("model error: %s", modelError)
			}
			break
		}
	}

	if finalResponse == "" && errPoll == nil {
		errPoll = fmt.Errorf("timeout waiting for model response after 10 minutes")
	}

	if errPoll != nil {
		return "", errPoll
	}

	return finalResponse, nil
}

// loadAntigravityEnv checks the current and parent directories for .env.antigravity
// and loads the environment variables into the process.
func loadAntigravityEnv() {
	dir, err := os.Getwd()
	if err != nil {
		return
	}

	for {
		envPath := filepath.Join(dir, ".env.antigravity")
		if _, err := os.Stat(envPath); err == nil {
			// Found it, read and load
			content, err := os.ReadFile(envPath)
			if err != nil {
				break
			}
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					val := strings.TrimSpace(parts[1])
					// Only set if not already set in environment
					if os.Getenv(key) == "" {
						os.Setenv(key, val)
						log.Printf("Loaded environment variable from %s: %s=%s", envPath, key, val)
					}
				}
			}
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break // Reached root
		}
		dir = parent
	}
}
