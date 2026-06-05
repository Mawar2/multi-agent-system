package ticket

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Issue represents a GitHub Issue to be processed.
type Issue struct {
	Number    int
	Title     string
	Body      string
	Labels    []string
	RepoOwner string
	RepoName  string
	Assignees []string
	Milestone string
	CreatedAt string
	UpdatedAt string
}

// Client fetches GitHub Issues and parses them for task creation.
// Uses GitHub MCP tools for API access.
type Client interface {
	// FetchIssues retrieves open issues from a repository.
	// Filters by labels if specified.
	FetchIssues(ctx context.Context, owner, repo string, labels []string) ([]*Issue, error)

	// GetIssue retrieves a specific issue by number.
	GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error)

	// ParseAcceptanceCriteria extracts acceptance criteria checklist from issue body.
	ParseAcceptanceCriteria(body string) ([]string, error)

	// CheckPRStatus checks if an issue has an associated PR and its status.
	// Returns nil if no PR found, otherwise returns the PR status.
	CheckPRStatus(ctx context.Context, owner, repo string, issueNumber int) (*PRStatus, error)
}

// PRStatus represents the status of a pull request associated with an issue.
type PRStatus struct {
	Number  int    // PR number
	State   string // "open", "closed", "merged"
	HTMLURL string // PR URL
	Merged  bool   // Whether PR was merged
}

// GitHubClient implements Client using GitHub MCP tools.
type GitHubClient struct {
	// mcpClient provides access to GitHub MCP tools
	mcpClient MCPClient
}

// MCPClient defines the interface for calling MCP tools.
// This abstraction allows for mocking in tests.
type MCPClient interface {
	// Call invokes an MCP tool with the given parameters
	Call(ctx context.Context, tool string, params map[string]interface{}) (interface{}, error)
}

// NewGitHubClient creates a new GitHub client with the provided MCP client.
func NewGitHubClient(mcpClient MCPClient) *GitHubClient {
	return &GitHubClient{
		mcpClient: mcpClient,
	}
}

// FetchIssues retrieves open issues from a repository.
// Uses mcp__github__list_issues tool to fetch issues, optionally filtering by labels.
func (c *GitHubClient) FetchIssues(ctx context.Context, owner, repo string, labels []string) ([]*Issue, error) {
	params := map[string]interface{}{
		"owner":   owner,
		"repo":    repo,
		"state":   "OPEN",
		"perPage": 100, // Fetch up to 100 issues per call
	}

	// If labels specified, add to params
	if len(labels) > 0 {
		params["labels"] = labels
	}

	// Call GitHub MCP tool
	result, err := c.mcpClient.Call(ctx, "mcp__github__list_issues", params)
	if err != nil {
		return nil, fmt.Errorf("failed to list issues: %w", err)
	}

	// Parse response
	issues, err := c.parseIssuesResponse(result)
	if err != nil {
		return nil, fmt.Errorf("failed to parse issues response: %w", err)
	}

	// Ensure repo info is set for all issues
	for _, issue := range issues {
		if issue.RepoOwner == "" {
			issue.RepoOwner = owner
		}
		if issue.RepoName == "" {
			issue.RepoName = repo
		}
	}

	return issues, nil
}

// GetIssue retrieves a specific issue by number.
// Uses mcp__github__issue_read tool with method="get".
func (c *GitHubClient) GetIssue(ctx context.Context, owner, repo string, number int) (*Issue, error) {
	params := map[string]interface{}{
		"method":       "get",
		"owner":        owner,
		"repo":         repo,
		"issue_number": number,
	}

	// Call GitHub MCP tool
	result, err := c.mcpClient.Call(ctx, "mcp__github__issue_read", params)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue #%d: %w", number, err)
	}

	// Parse single issue response
	issue, err := c.parseIssueResponse(result, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to parse issue response: %w", err)
	}

	return issue, nil
}

// ParseAcceptanceCriteria extracts checklist items from issue body.
// Looks for patterns like:
//   - [ ] Acceptance criterion 1
//   - [ ] Acceptance criterion 2
//   - [x] Completed criterion
func (c *GitHubClient) ParseAcceptanceCriteria(body string) ([]string, error) {
	if body == "" {
		return []string{}, nil
	}

	// Regex to match markdown checkbox items: - [ ] or - [x] or - [X]
	// Captures the text after the checkbox
	checkboxPattern := regexp.MustCompile(`(?m)^\s*-\s*\[([ xX])\]\s*(.+)$`)
	matches := checkboxPattern.FindAllStringSubmatch(body, -1)

	if len(matches) == 0 {
		return []string{}, nil
	}

	criteria := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) >= 3 {
			criterionText := strings.TrimSpace(match[2])
			if criterionText != "" {
				criteria = append(criteria, criterionText)
			}
		}
	}

	return criteria, nil
}

// CheckPRStatus checks if an issue has an associated PR.
// Searches for PRs that reference the issue number in their body or title.
// Returns nil if no PR found, otherwise returns the PR status.
func (c *GitHubClient) CheckPRStatus(ctx context.Context, owner, repo string, issueNumber int) (*PRStatus, error) {
	// Build search query to find PRs referencing this issue
	// GitHub search syntax: repo:owner/repo is:pr #<issue_number>
	query := fmt.Sprintf("repo:%s/%s is:pr #%d", owner, repo, issueNumber)

	params := map[string]interface{}{
		"query":   query,
		"owner":   owner,
		"repo":    repo,
		"perPage": 5, // Only need first few results
	}

	// Call GitHub search tool
	result, err := c.mcpClient.Call(ctx, "mcp__github__search_pull_requests", params)
	if err != nil {
		return nil, fmt.Errorf("failed to search for PRs: %w", err)
	}

	// Parse PR search response
	prStatus, err := c.parsePRSearchResponse(result)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PR search response: %w", err)
	}

	return prStatus, nil
}

// parseIssuesResponse converts MCP response to Issue slice.
func (c *GitHubClient) parseIssuesResponse(result interface{}) ([]*Issue, error) {
	// Convert result to JSON for uniform parsing
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	// Expected structure from GitHub API
	var response struct {
		Items []map[string]interface{} `json:"items"`
		Data  []map[string]interface{} `json:"data"` // Alternative field name
	}

	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Use items if present, otherwise use data
	items := response.Items
	if len(items) == 0 {
		items = response.Data
	}

	if len(items) == 0 {
		return []*Issue{}, nil
	}

	issues := make([]*Issue, 0, len(items))
	for _, item := range items {
		issue, err := c.mapToIssue(item)
		if err != nil {
			// Log warning but continue with other issues
			continue
		}
		issues = append(issues, issue)
	}

	return issues, nil
}

// parseIssueResponse converts single issue MCP response to Issue.
func (c *GitHubClient) parseIssueResponse(result interface{}, owner, repo string) (*Issue, error) {
	// Convert result to JSON for uniform parsing
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	var issueData map[string]interface{}
	if err := json.Unmarshal(jsonData, &issueData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	issue, err := c.mapToIssue(issueData)
	if err != nil {
		return nil, fmt.Errorf("failed to map issue: %w", err)
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
func (c *GitHubClient) mapToIssue(data map[string]interface{}) (*Issue, error) {
	// Extract number (required)
	number, ok := data["number"]
	if !ok {
		return nil, fmt.Errorf("issue missing 'number' field")
	}
	issueNumber, err := c.toInt(number)
	if err != nil {
		return nil, fmt.Errorf("invalid issue number: %w", err)
	}

	// Extract title (required)
	title, _ := data["title"].(string)
	if title == "" {
		return nil, fmt.Errorf("issue missing 'title' field")
	}

	// Extract body (optional)
	body, _ := data["body"].(string)

	// Extract labels (optional)
	labels := c.extractLabels(data)

	// Extract repository info (optional, may be in nested structure)
	repoOwner, repoName := c.extractRepoInfo(data)

	// Extract assignees (optional)
	assignees := c.extractAssignees(data)

	// Extract milestone (optional)
	milestone := c.extractMilestone(data)

	// Extract timestamps (optional)
	createdAt, _ := data["created_at"].(string)
	updatedAt, _ := data["updated_at"].(string)

	return &Issue{
		Number:    issueNumber,
		Title:     title,
		Body:      body,
		Labels:    labels,
		RepoOwner: repoOwner,
		RepoName:  repoName,
		Assignees: assignees,
		Milestone: milestone,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

// extractLabels extracts label names from issue data.
func (c *GitHubClient) extractLabels(data map[string]interface{}) []string {
	labelsRaw, ok := data["labels"]
	if !ok {
		return []string{}
	}

	// Labels can be array of objects with "name" field
	labelsArray, ok := labelsRaw.([]interface{})
	if !ok {
		return []string{}
	}

	labels := make([]string, 0, len(labelsArray))
	for _, labelItem := range labelsArray {
		if labelMap, ok := labelItem.(map[string]interface{}); ok {
			if name, ok := labelMap["name"].(string); ok && name != "" {
				labels = append(labels, name)
			}
		}
	}

	return labels
}

// extractRepoInfo extracts repository owner and name from issue data.
func (c *GitHubClient) extractRepoInfo(data map[string]interface{}) (owner, repo string) {
	// Check for repository object
	repoData, ok := data["repository"].(map[string]interface{})
	if !ok {
		return "", ""
	}

	// Extract owner
	if ownerData, ok := repoData["owner"].(map[string]interface{}); ok {
		owner, _ = ownerData["login"].(string)
	}

	// Extract repo name
	repo, _ = repoData["name"].(string)

	return owner, repo
}

// extractAssignees extracts assignee usernames from issue data.
func (c *GitHubClient) extractAssignees(data map[string]interface{}) []string {
	assigneesRaw, ok := data["assignees"]
	if !ok {
		return []string{}
	}

	assigneesArray, ok := assigneesRaw.([]interface{})
	if !ok {
		return []string{}
	}

	assignees := make([]string, 0, len(assigneesArray))
	for _, assigneeItem := range assigneesArray {
		if assigneeMap, ok := assigneeItem.(map[string]interface{}); ok {
			if login, ok := assigneeMap["login"].(string); ok && login != "" {
				assignees = append(assignees, login)
			}
		}
	}

	return assignees
}

// extractMilestone extracts milestone title from issue data.
func (c *GitHubClient) extractMilestone(data map[string]interface{}) string {
	milestoneData, ok := data["milestone"].(map[string]interface{})
	if !ok {
		return ""
	}

	title, _ := milestoneData["title"].(string)
	return title
}

// parsePRSearchResponse extracts PR status from search results.
// Returns nil if no PRs found (not an error).
func (c *GitHubClient) parsePRSearchResponse(result interface{}) (*PRStatus, error) {
	// Convert result to JSON for uniform parsing
	jsonData, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	var response struct {
		Items []map[string]interface{} `json:"items"`
		Data  []map[string]interface{} `json:"data"`
	}

	if err := json.Unmarshal(jsonData, &response); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	// Use items if present, otherwise use data
	items := response.Items
	if len(items) == 0 {
		items = response.Data
	}

	// If no PRs found, return nil (not an error)
	if len(items) == 0 {
		return nil, nil
	}

	// Return first PR found (most relevant)
	pr := items[0]

	number, err := c.toInt(pr["number"])
	if err != nil {
		return nil, fmt.Errorf("invalid PR number: %w", err)
	}

	state, _ := pr["state"].(string)
	htmlURL, _ := pr["html_url"].(string)

	// Check if PR is merged
	merged := false
	if mergedVal, ok := pr["merged"]; ok {
		merged, _ = mergedVal.(bool)
	}
	// Also check merged_at field as fallback
	if mergedAt, ok := pr["merged_at"]; ok && mergedAt != nil {
		merged = true
	}

	// Normalize state to lowercase
	state = strings.ToLower(state)

	return &PRStatus{
		Number:  number,
		State:   state,
		HTMLURL: htmlURL,
		Merged:  merged,
	}, nil
}

// toInt converts various numeric types to int.
func (c *GitHubClient) toInt(val interface{}) (int, error) {
	switch v := val.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		return strconv.Atoi(v)
	default:
		return 0, fmt.Errorf("cannot convert %T to int", val)
	}
}
