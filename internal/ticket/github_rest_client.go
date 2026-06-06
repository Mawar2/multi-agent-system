package ticket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// GitHubRESTClient implements MCPClient using GitHub REST API via HTTP.
// This is a simpler and more reliable alternative to the MCP HTTP client.
type GitHubRESTClient struct {
	token  string
	client *http.Client
}

// PullRequest represents a GitHub pull request.
type PullRequest struct {
	Number     int    `json:"number"`
	State      string `json:"state"` // "open", "closed"
	Merged     bool   `json:"merged"`
	Title      string `json:"title"`
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_ref"`   // Branch name (e.g., "feature/KAI-6-final-review")
	Body       string `json:"body"`       // PR description
	Draft      bool   `json:"draft"`      // Whether PR is draft
	Additions  int    `json:"additions"`  // Lines added
	Deletions  int    `json:"deletions"`  // Lines deleted
}

// PRComment represents a comment on a pull request.
type PRComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      string    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
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

// FetchIssues retrieves open issues from a repository (implements ticket.Client).
func (c *GitHubRESTClient) FetchIssues(ctx context.Context, owner, repo string, labels []string) ([]*Issue, error) {
	params := map[string]interface{}{
		"owner": owner,
		"repo":  repo,
		"state": "OPEN",
	}

	if len(labels) > 0 {
		params["labels"] = labels
	}

	result, err := c.listIssues(ctx, params)
	if err != nil {
		return nil, err
	}

	return c.parseIssuesResponse(result)
}

// GetIssue retrieves a specific issue by number (implements ticket.Client).
func (c *GitHubRESTClient) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	params := map[string]interface{}{
		"owner":        owner,
		"repo":         repo,
		"issue_number": float64(number),
	}

	result, err := c.getIssue(ctx, params)
	if err != nil {
		return nil, err
	}

	return c.parseIssueResponse(result, owner, repo)
}

// ParseAcceptanceCriteria extracts checklist items from issue body (implements ticket.Client).
func (c *GitHubRESTClient) ParseAcceptanceCriteria(body string) ([]string, error) {
	// Reuse the implementation from GitHubClient
	ghClient := &GitHubClient{}
	return ghClient.ParseAcceptanceCriteria(body)
}

// CheckPRStatus checks if an issue has an associated PR (implements ticket.Client).
func (c *GitHubRESTClient) CheckPRStatus(ctx context.Context, owner, repo string, issueNumber int) (*PRStatus, error) {
	query := fmt.Sprintf("repo:%s/%s is:pr #%d", owner, repo, issueNumber)

	params := map[string]interface{}{
		"query": query,
		"owner": owner,
		"repo":  repo,
	}

	result, err := c.searchPullRequests(ctx, params)
	if err != nil {
		return nil, err
	}

	return c.parsePRSearchResponse(result)
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
	switch state {
	case "OPEN":
		q.Add("state", "open")
	case "CLOSED":
		q.Add("state", "closed")
	default:
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
	defer func() { _ = resp.Body.Close() }()

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
	defer func() { _ = resp.Body.Close() }()

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
// Filters PRs that reference a specific issue number in title or body.
func (c *GitHubRESTClient) searchPullRequests(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	owner, _ := params["owner"].(string)
	repo, _ := params["repo"].(string)
	query, _ := params["query"].(string)

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
	defer func() { _ = resp.Body.Close() }()

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

	// Filter PRs based on query (e.g., "repo:owner/repo is:pr #47")
	// Extract issue number from query (format: "#<number>")
	filteredPRs := make([]map[string]interface{}, 0)
	if query != "" {
		// Simple extraction: find "#<number>" in query
		issueRef := ""
		for i := 0; i < len(query); i++ {
			if query[i] == '#' {
				// Extract the number after #
				j := i + 1
				for j < len(query) && query[j] >= '0' && query[j] <= '9' {
					j++
				}
				if j > i+1 {
					issueRef = query[i:j] // e.g., "#47"
					break
				}
			}
		}

		// Filter PRs that mention this issue reference in title or body
		if issueRef != "" {
			for _, pr := range prs {
				title, _ := pr["title"].(string)
				body, _ := pr["body"].(string)

				// Check if title or body contains the issue reference
				if containsSubstring(title, issueRef) || containsSubstring(body, issueRef) {
					filteredPRs = append(filteredPRs, pr)
				}
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

// containsSubstring checks if s contains substr (case-sensitive).
func containsSubstring(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// GetPullRequest fetches PR details (state, merged status, head SHA).
func (c *GitHubRESTClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequest, error) {
	// Build GitHub API URL
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNumber)

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
	defer func() { _ = resp.Body.Close() }()

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
	var rawPR map[string]interface{}
	if err := json.Unmarshal(body, &rawPR); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract fields we care about
	pr := &PullRequest{
		Number: int(rawPR["number"].(float64)),
		State:  rawPR["state"].(string),
		Title:  rawPR["title"].(string),
	}

	// Check merged status
	if merged, ok := rawPR["merged"].(bool); ok {
		pr.Merged = merged
	}

	// Extract head SHA
	if head, ok := rawPR["head"].(map[string]interface{}); ok {
		if sha, ok := head["sha"].(string); ok {
			pr.HeadSHA = sha
		}
	}

	return pr, nil
}

// ListOpenPRs fetches all open pull requests for a repository.
// Used by backfill script to create tasks for existing PRs.
func (c *GitHubRESTClient) ListOpenPRs(ctx context.Context, owner, repo string) ([]*PullRequest, error) {
	// Build GitHub API URL
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?state=open&per_page=100", owner, repo)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	// Execute request
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse JSON response (array of PRs)
	var rawPRs []map[string]interface{}
	if err := json.Unmarshal(body, &rawPRs); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to PullRequest structs
	prs := make([]*PullRequest, 0, len(rawPRs))
	for _, rawPR := range rawPRs {
		pr := &PullRequest{
			Number: int(rawPR["number"].(float64)),
			State:  rawPR["state"].(string),
			Title:  rawPR["title"].(string),
		}

		// Extract optional fields
		if merged, ok := rawPR["merged"].(bool); ok {
			pr.Merged = merged
		}
		if draft, ok := rawPR["draft"].(bool); ok {
			pr.Draft = draft
		}
		if additions, ok := rawPR["additions"].(float64); ok {
			pr.Additions = int(additions)
		}
		if deletions, ok := rawPR["deletions"].(float64); ok {
			pr.Deletions = int(deletions)
		}
		if body, ok := rawPR["body"].(string); ok {
			pr.Body = body
		}

		// Extract head SHA and branch
		if head, ok := rawPR["head"].(map[string]interface{}); ok {
			if sha, ok := head["sha"].(string); ok {
				pr.HeadSHA = sha
			}
			if ref, ok := head["ref"].(string); ok {
				pr.HeadBranch = ref
			}
		}

		prs = append(prs, pr)
	}

	return prs, nil
}

// ListPRComments fetches all comments on a pull request.
func (c *GitHubRESTClient) ListPRComments(ctx context.Context, owner, repo string, prNumber int) ([]PRComment, error) {
	// Build GitHub API URL (use issues endpoint for comments)
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, prNumber)

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add query params
	q := req.URL.Query()
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
	defer func() { _ = resp.Body.Close() }()

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
	var rawComments []map[string]interface{}
	if err := json.Unmarshal(body, &rawComments); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Convert to PRComment structs
	comments := make([]PRComment, 0, len(rawComments))
	for _, raw := range rawComments {
		comment := PRComment{
			ID:   int64(raw["id"].(float64)),
			Body: raw["body"].(string),
		}

		// Extract user login
		if user, ok := raw["user"].(map[string]interface{}); ok {
			if login, ok := user["login"].(string); ok {
				comment.User = login
			}
		}

		// Parse created_at timestamp
		if createdStr, ok := raw["created_at"].(string); ok {
			if created, err := time.Parse(time.RFC3339, createdStr); err == nil {
				comment.CreatedAt = created
			}
		}

		comments = append(comments, comment)
	}

	return comments, nil
}

// GetLatestAIReviewComment finds the most recent AI review comment (filters by prefix).
// Returns nil if no AI review comment found.
func (c *GitHubRESTClient) GetLatestAIReviewComment(ctx context.Context, owner, repo string, prNumber int) (*PRComment, error) {
	// Fetch all comments
	comments, err := c.ListPRComments(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, err
	}

	// Filter for AI review comments (prefix: "## 🤖 AI Code Review (Gemini 2.5 Pro)")
	aiComments := make([]PRComment, 0)
	for _, comment := range comments {
		if strings.HasPrefix(comment.Body, "## 🤖 AI Code Review (Gemini 2.5 Pro)") {
			aiComments = append(aiComments, comment)
		}
	}

	// No AI comments found
	if len(aiComments) == 0 {
		return nil, nil
	}

	// Sort by CreatedAt descending (most recent first)
	sort.Slice(aiComments, func(i, j int) bool {
		return aiComments[i].CreatedAt.After(aiComments[j].CreatedAt)
	})

	// Return most recent
	return &aiComments[0], nil
}

// ParseAIReviewFeedback extracts actionable feedback from AI review comment.
// Strips markdown formatting and extracts the "Issues Found" section.
func (c *GitHubRESTClient) ParseAIReviewFeedback(comment *PRComment) string {
	if comment == nil {
		return ""
	}

	body := comment.Body

	// Extract content after "## Issues Found" or "## 🔍 Issues Found"
	issuesIdx := strings.Index(body, "## Issues Found")
	if issuesIdx == -1 {
		issuesIdx = strings.Index(body, "## 🔍 Issues Found")
	}

	// If "Issues Found" section exists, extract it
	if issuesIdx != -1 {
		// Find the end of the section (next ## heading or end of comment)
		sectionStart := issuesIdx
		sectionEnd := len(body)

		// Look for next ## heading after Issues Found
		nextHeadingIdx := strings.Index(body[issuesIdx+10:], "\n##")
		if nextHeadingIdx != -1 {
			sectionEnd = issuesIdx + 10 + nextHeadingIdx
		}

		// Extract the section
		section := body[sectionStart:sectionEnd]

		// Clean up: remove markdown formatting
		section = strings.ReplaceAll(section, "**", "")
		section = strings.ReplaceAll(section, "`", "")

		return strings.TrimSpace(section)
	}

	// Fallback: return full comment body with markdown cleaned
	cleaned := strings.ReplaceAll(body, "**", "")
	cleaned = strings.ReplaceAll(cleaned, "`", "")
	return strings.TrimSpace(cleaned)
}

// Helper methods to parse GitHub API responses into ticket types

// parseIssuesResponse converts raw GitHub API response to Issue slice.
func (c *GitHubRESTClient) parseIssuesResponse(result interface{}) ([]*Issue, error) {
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response type")
	}

	items, ok := resultMap["items"].([]map[string]interface{})
	if !ok {
		return []*Issue{}, nil
	}

	issues := make([]*Issue, 0, len(items))
	for _, item := range items {
		issue, err := c.mapToIssue(item)
		if err != nil {
			continue // Skip malformed issues
		}
		issues = append(issues, issue)
	}

	return issues, nil
}

// parseIssueResponse converts single issue response to Issue.
func (c *GitHubRESTClient) parseIssueResponse(result interface{}, owner, repo string) (*Issue, error) {
	issueData, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected response type")
	}

	issue, err := c.mapToIssue(issueData)
	if err != nil {
		return nil, err
	}

	// Ensure owner and repo are set
	if issue.RepoOwner == "" {
		issue.RepoOwner = owner
	}
	if issue.RepoName == "" {
		issue.RepoName = repo
	}

	return issue, nil
}

// mapToIssue converts GitHub API issue data to Issue.
func (c *GitHubRESTClient) mapToIssue(data map[string]interface{}) (*Issue, error) {
	number, ok := data["number"].(float64)
	if !ok {
		return nil, fmt.Errorf("issue missing number")
	}

	title, ok := data["title"].(string)
	if !ok {
		return nil, fmt.Errorf("issue missing title")
	}

	body, _ := data["body"].(string)

	// Extract labels
	labels := []string{}
	if labelsRaw, ok := data["labels"].([]interface{}); ok {
		for _, labelItem := range labelsRaw {
			if labelMap, ok := labelItem.(map[string]interface{}); ok {
				if name, ok := labelMap["name"].(string); ok {
					labels = append(labels, name)
				}
			}
		}
	}

	return &Issue{
		Number:    int(number),
		Title:     title,
		Body:      body,
		Labels:    labels,
		RepoOwner: "",
		RepoName:  "",
	}, nil
}

// parsePRSearchResponse extracts PR status from search results.
func (c *GitHubRESTClient) parsePRSearchResponse(result interface{}) (*PRStatus, error) {
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return nil, nil
	}

	items, ok := resultMap["items"].([]map[string]interface{})
	if !ok || len(items) == 0 {
		return nil, nil // No PRs found
	}

	// Return first PR
	pr := items[0]

	number, ok := pr["number"].(float64)
	if !ok {
		return nil, fmt.Errorf("PR missing number")
	}

	state, _ := pr["state"].(string)
	htmlURL, _ := pr["html_url"].(string)
	merged, _ := pr["merged"].(bool)

	return &PRStatus{
		Number:  int(number),
		State:   state,
		HTMLURL: htmlURL,
		Merged:  merged,
	}, nil
}
