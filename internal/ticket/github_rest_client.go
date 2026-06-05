package ticket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// GitHubRESTClient implements MCPClient using GitHub REST API via HTTP.
// This is a simpler and more reliable alternative to the MCP HTTP client.
type GitHubRESTClient struct {
	token  string
	client *http.Client
}

// NewGitHubRESTClient creates a new GitHub REST client using the GitHub API.
// Reads GITHUB_TOKEN from environment.
func NewGitHubRESTClient() *GitHubRESTClient {
	token := os.Getenv("GITHUB_TOKEN")
	return &GitHubRESTClient{
		token: token,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Call invokes a GitHub operation by translating MCP tool names to gh CLI commands.
func (c *GitHubRESTClient) Call(ctx context.Context, tool string, params map[string]interface{}) (interface{}, error) {
	switch tool {
	case "mcp__github__list_issues":
		return c.listIssues(ctx, params)
	case "mcp__github__issue_read":
		return c.getIssue(ctx, params)
	case "mcp__github__search_pull_requests":
		return c.searchPullRequests(ctx, params)
	default:
		return nil, fmt.Errorf("unsupported tool: %s", tool)
	}
}

// listIssues lists issues using GitHub REST API.
func (c *GitHubRESTClient) listIssues(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	state, _ := params["state"].(string)

	if owner == "" || repo == "" {
		return nil, fmt.Errorf("owner and repo are required")
	}

	// Build GitHub API URL
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues", owner, repo)

	// Add query parameters
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query params
	q := req.URL.Query()
	if state == "OPEN" {
		q.Add("state", "open")
	} else if state == "CLOSED" {
		q.Add("state", "closed")
	} else {
		q.Add("state", "all")
	}
	q.Add("per_page", "100")
	req.URL.RawQuery = q.Encode()

	// Set headers
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var issues []map[string]interface{}
	if err := json.Unmarshal(body, &issues); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Wrap in items structure expected by client
	return map[string]interface{}{
		"items": issues,
	}, nil
}

// getIssue gets a specific issue using GitHub REST API.
func (c *GitHubRESTClient) getIssue(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	issueNumber, _ := params["issue_number"].(float64) // JSON numbers are float64

	if owner == "" || repo == "" || issueNumber == 0 {
		return nil, fmt.Errorf("owner, repo, and issue_number are required")
	}

	// Build GitHub API URL
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d", owner, repo, int(issueNumber))

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var issue map[string]interface{}
	if err := json.Unmarshal(body, &issue); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return issue, nil
}

// searchPullRequests searches for pull requests using GitHub REST API.
func (c *GitHubRESTClient) searchPullRequests(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)

	if owner == "" || repo == "" {
		return nil, fmt.Errorf("owner and repo are required")
	}

	// Build GitHub API URL for listing PRs
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", owner, repo)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query params
	q := req.URL.Query()
	q.Add("state", "all") // Include open, closed, and merged PRs
	q.Add("per_page", "100")
	req.URL.RawQuery = q.Encode()

	// Set headers
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response
	var prs []map[string]interface{}
	if err := json.Unmarshal(body, &prs); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Simple filtering: if query is provided, filter PRs
	// For now, just return all PRs (client handles filtering)
	// Wrap in items structure expected by client
	return map[string]interface{}{
		"items": prs,
	}, nil
}
