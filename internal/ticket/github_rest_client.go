package ticket

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// GitHubRESTClient implements MCPClient using GitHub CLI (gh) commands.
// This is a simpler and more reliable alternative to the MCP HTTP client.
type GitHubRESTClient struct{}

// NewGitHubRESTClient creates a new GitHub REST client using gh CLI.
func NewGitHubRESTClient() *GitHubRESTClient {
	return &GitHubRESTClient{}
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

// listIssues lists issues using gh CLI.
func (c *GitHubRESTClient) listIssues(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	state, _ := params["state"].(string)

	if owner == "" || repo == "" {
		return nil, fmt.Errorf("owner and repo are required")
	}

	// Build gh issue list command
	args := []string{"issue", "list", "--repo", fmt.Sprintf("%s/%s", owner, repo), "--json", "number,title,body,labels,assignees,milestone,createdAt,updatedAt"}

	// Add state filter
	if state == "OPEN" {
		args = append(args, "--state", "open")
	} else if state == "CLOSED" {
		args = append(args, "--state", "closed")
	}

	// Execute gh command
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh issue list failed: %w\nStderr: %s", err, stderr.String())
	}

	// Parse JSON response
	var issues []map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &issues); err != nil {
		return nil, fmt.Errorf("failed to parse gh response: %w", err)
	}

	// Wrap in items structure expected by client
	return map[string]interface{}{
		"items": issues,
	}, nil
}

// getIssue gets a specific issue using gh CLI.
func (c *GitHubRESTClient) getIssue(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	issueNumber, _ := params["issue_number"].(float64) // JSON numbers are float64

	if owner == "" || repo == "" || issueNumber == 0 {
		return nil, fmt.Errorf("owner, repo, and issue_number are required")
	}

	// Execute gh issue view command
	args := []string{
		"issue", "view",
		fmt.Sprintf("%d", int(issueNumber)),
		"--repo", fmt.Sprintf("%s/%s", owner, repo),
		"--json", "number,title,body,labels,assignees,milestone,createdAt,updatedAt",
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh issue view failed: %w\nStderr: %s", err, stderr.String())
	}

	// Parse JSON response
	var issue map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &issue); err != nil {
		return nil, fmt.Errorf("failed to parse gh response: %w", err)
	}

	return issue, nil
}

// searchPullRequests searches for pull requests using gh CLI.
func (c *GitHubRESTClient) searchPullRequests(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	query, _ := params["query"].(string)

	if owner == "" || repo == "" {
		return nil, fmt.Errorf("owner and repo are required")
	}

	// Build gh pr list command
	// For issue reference search, list all PRs and filter in memory
	args := []string{
		"pr", "list",
		"--repo", fmt.Sprintf("%s/%s", owner, repo),
		"--json", "number,title,body,state,merged,mergedAt,url",
		"--state", "all", // Include open, closed, and merged PRs
		"--limit", "100",
	}

	cmd := exec.CommandContext(ctx, "gh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh pr list failed: %w\nStderr: %s", err, stderr.String())
	}

	// Parse JSON response
	var prs []map[string]interface{}
	if err := json.Unmarshal(stdout.Bytes(), &prs); err != nil {
		return nil, fmt.Errorf("failed to parse gh response: %w", err)
	}

	// If query contains issue number, filter PRs that reference it
	// Simple filtering: check if query appears in title or body
	filteredPRs := make([]map[string]interface{}, 0)
	if query != "" {
		for _, pr := range prs {
			title, _ := pr["title"].(string)
			body, _ := pr["body"].(string)
			// Simple substring match - could be improved
			if contains(title, query) || contains(body, query) {
				// Normalize field names to match MCP response
				if url, ok := pr["url"].(string); ok {
					pr["html_url"] = url
				}
				filteredPRs = append(filteredPRs, pr)
			}
		}
	} else {
		filteredPRs = prs
	}

	// Wrap in items structure expected by client
	return map[string]interface{}{
		"items": filteredPRs,
	}, nil
}

// contains checks if s contains substr (case-insensitive helper).
func contains(s, substr string) bool {
	// Simple case-sensitive check for now
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && bytesContains([]byte(s), []byte(substr)))
}

// bytesContains is a simple byte slice substring check.
func bytesContains(b, subslice []byte) bool {
	if len(subslice) == 0 {
		return true
	}
	if len(subslice) > len(b) {
		return false
	}
	for i := 0; i <= len(b)-len(subslice); i++ {
		if bytes.Equal(b[i:i+len(subslice)], subslice) {
			return true
		}
	}
	return false
}
