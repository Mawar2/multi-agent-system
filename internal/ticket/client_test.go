package ticket

import (
	"context"
	"errors"
	"testing"
)

// mockMCPClient is a test mock for MCPClient.
type mockMCPClient struct {
	// responses maps tool names to mock responses
	responses map[string]interface{}
	// errors maps tool names to mock errors
	errors map[string]error
	// calls tracks which tools were called
	calls []string
}

// Call implements MCPClient.Call for testing.
func (m *mockMCPClient) Call(ctx context.Context, tool string, params map[string]interface{}) (interface{}, error) {
	m.calls = append(m.calls, tool)

	if err, ok := m.errors[tool]; ok {
		return nil, err
	}

	if resp, ok := m.responses[tool]; ok {
		return resp, nil
	}

	return nil, errors.New("mock not configured for tool: " + tool)
}

// TestFetchIssues tests fetching issues from GitHub.
func TestFetchIssues(t *testing.T) {
	tests := []struct {
		name           string
		mockResponse   interface{}
		mockError      error
		expectedIssues int
		expectError    bool
	}{
		{
			name: "successful fetch with items",
			mockResponse: map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"number": 1,
						"title":  "Test issue 1",
						"body":   "Issue body",
						"labels": []interface{}{
							map[string]interface{}{"name": "bug"},
						},
						"created_at": "2024-01-01T00:00:00Z",
						"updated_at": "2024-01-02T00:00:00Z",
					},
					{
						"number": 2,
						"title":  "Test issue 2",
						"body":   "Another issue",
						"labels": []interface{}{},
					},
				},
			},
			expectedIssues: 2,
			expectError:    false,
		},
		{
			name: "successful fetch with data field",
			mockResponse: map[string]interface{}{
				"data": []map[string]interface{}{
					{
						"number": 3,
						"title":  "Test issue 3",
						"body":   "Issue with data field",
					},
				},
			},
			expectedIssues: 1,
			expectError:    false,
		},
		{
			name:           "empty response",
			mockResponse:   map[string]interface{}{"items": []map[string]interface{}{}},
			expectedIssues: 0,
			expectError:    false,
		},
		{
			name:        "api error",
			mockError:   errors.New("API rate limit exceeded"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMCPClient{
				responses: map[string]interface{}{
					"mcp__github__list_issues": tt.mockResponse,
				},
				errors: map[string]error{},
			}
			if tt.mockError != nil {
				mock.errors["mcp__github__list_issues"] = tt.mockError
			}

			client := NewGitHubClient(mock)
			issues, err := client.FetchIssues(context.Background(), "owner", "repo", nil)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(issues) != tt.expectedIssues {
				t.Errorf("expected %d issues, got %d", tt.expectedIssues, len(issues))
			}

			// Verify repo info is set
			for _, issue := range issues {
				if issue.RepoOwner != "owner" {
					t.Errorf("expected RepoOwner='owner', got '%s'", issue.RepoOwner)
				}
				if issue.RepoName != "repo" {
					t.Errorf("expected RepoName='repo', got '%s'", issue.RepoName)
				}
			}
		})
	}
}

// TestGetIssue tests fetching a single issue by number.
func TestGetIssue(t *testing.T) {
	tests := []struct {
		name         string
		mockResponse interface{}
		mockError    error
		expectError  bool
	}{
		{
			name: "successful get",
			mockResponse: map[string]interface{}{
				"number": 42,
				"title":  "Single issue",
				"body":   "Issue body",
				"labels": []interface{}{
					map[string]interface{}{"name": "enhancement"},
				},
				"assignees": []interface{}{
					map[string]interface{}{"login": "user1"},
				},
				"milestone": map[string]interface{}{
					"title": "v1.0",
				},
			},
			expectError: false,
		},
		{
			name:        "api error",
			mockError:   errors.New("issue not found"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMCPClient{
				responses: map[string]interface{}{
					"mcp__github__issue_read": tt.mockResponse,
				},
				errors: map[string]error{},
			}
			if tt.mockError != nil {
				mock.errors["mcp__github__issue_read"] = tt.mockError
			}

			client := NewGitHubClient(mock)
			issue, err := client.GetIssue(context.Background(), "owner", "repo", 42)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if issue.Number != 42 {
				t.Errorf("expected issue number 42, got %d", issue.Number)
			}

			if issue.Title != "Single issue" {
				t.Errorf("expected title 'Single issue', got '%s'", issue.Title)
			}

			if issue.RepoOwner != "owner" || issue.RepoName != "repo" {
				t.Errorf("repo info not set correctly: owner=%s, repo=%s", issue.RepoOwner, issue.RepoName)
			}
		})
	}
}

// TestParseAcceptanceCriteria tests parsing checklist items from issue body.
func TestParseAcceptanceCriteria(t *testing.T) {
	tests := []struct {
		name             string
		body             string
		expectedCriteria []string
	}{
		{
			name: "basic checklist",
			body: `
## Acceptance Criteria
- [ ] Implement feature X
- [ ] Write tests
- [ ] Update documentation
`,
			expectedCriteria: []string{
				"Implement feature X",
				"Write tests",
				"Update documentation",
			},
		},
		{
			name: "mixed checked and unchecked",
			body: `
- [x] Already done
- [ ] Still todo
- [X] Another done (capital X)
`,
			expectedCriteria: []string{
				"Already done",
				"Still todo",
				"Another done (capital X)",
			},
		},
		{
			name: "with extra spaces",
			body: `
  -  [ ]   Item with spaces
  -   [x]  Another item
`,
			expectedCriteria: []string{
				"Item with spaces",
				"Another item",
			},
		},
		{
			name:             "empty body",
			body:             "",
			expectedCriteria: []string{},
		},
		{
			name: "no checkboxes",
			body: `
This is just a description with no checklist items.
- Regular bullet point
* Another bullet
`,
			expectedCriteria: []string{},
		},
		{
			name: "nested content",
			body: `
- [ ] Top level criterion
  - Sub-bullet (not a checkbox)
- [ ] Another top level
`,
			expectedCriteria: []string{
				"Top level criterion",
				"Another top level",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewGitHubClient(&mockMCPClient{})
			criteria, err := client.ParseAcceptanceCriteria(tt.body)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(criteria) != len(tt.expectedCriteria) {
				t.Errorf("expected %d criteria, got %d", len(tt.expectedCriteria), len(criteria))
				t.Logf("Got criteria: %v", criteria)
				return
			}

			for i, expected := range tt.expectedCriteria {
				if criteria[i] != expected {
					t.Errorf("criterion[%d]: expected '%s', got '%s'", i, expected, criteria[i])
				}
			}
		})
	}
}

// TestCheckPRStatus tests checking for associated PRs.
func TestCheckPRStatus(t *testing.T) {
	tests := []struct {
		name         string
		mockResponse interface{}
		mockError    error
		expectPR     bool
		expectError  bool
	}{
		{
			name: "pr found",
			mockResponse: map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"number":   123,
						"state":    "OPEN",
						"html_url": "https://github.com/owner/repo/pull/123",
						"merged":   false,
					},
				},
			},
			expectPR:    true,
			expectError: false,
		},
		{
			name: "merged pr",
			mockResponse: map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"number":    456,
						"state":     "CLOSED",
						"html_url":  "https://github.com/owner/repo/pull/456",
						"merged_at": "2024-01-01T00:00:00Z",
					},
				},
			},
			expectPR:    true,
			expectError: false,
		},
		{
			name:         "no pr found",
			mockResponse: map[string]interface{}{"items": []map[string]interface{}{}},
			expectPR:     false,
			expectError:  false,
		},
		{
			name:        "api error",
			mockError:   errors.New("search failed"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMCPClient{
				responses: map[string]interface{}{
					"mcp__github__search_pull_requests": tt.mockResponse,
				},
				errors: map[string]error{},
			}
			if tt.mockError != nil {
				mock.errors["mcp__github__search_pull_requests"] = tt.mockError
			}

			client := NewGitHubClient(mock)
			prStatus, err := client.CheckPRStatus(context.Background(), "owner", "repo", 42)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectPR {
				if prStatus == nil {
					t.Errorf("expected PR status but got nil")
					return
				}
				if prStatus.Number == 0 {
					t.Errorf("expected PR number to be set")
				}
			} else {
				if prStatus != nil {
					t.Errorf("expected no PR but got: %+v", prStatus)
				}
			}
		})
	}
}

// TestMapToIssue tests mapping GitHub API data to Issue struct.
func TestMapToIssue(t *testing.T) {
	tests := []struct {
		name        string
		data        map[string]interface{}
		expectError bool
		validate    func(*testing.T, *Issue)
	}{
		{
			name: "complete issue data",
			data: map[string]interface{}{
				"number": float64(100),
				"title":  "Complete issue",
				"body":   "Full body",
				"labels": []interface{}{
					map[string]interface{}{"name": "bug"},
					map[string]interface{}{"name": "priority:high"},
				},
				"repository": map[string]interface{}{
					"name": "test-repo",
					"owner": map[string]interface{}{
						"login": "test-owner",
					},
				},
				"assignees": []interface{}{
					map[string]interface{}{"login": "user1"},
					map[string]interface{}{"login": "user2"},
				},
				"milestone": map[string]interface{}{
					"title": "Sprint 1",
				},
				"created_at": "2024-01-01T00:00:00Z",
				"updated_at": "2024-01-02T00:00:00Z",
			},
			expectError: false,
			validate: func(t *testing.T, issue *Issue) {
				if issue.Number != 100 {
					t.Errorf("expected number 100, got %d", issue.Number)
				}
				if len(issue.Labels) != 2 {
					t.Errorf("expected 2 labels, got %d", len(issue.Labels))
				}
				if len(issue.Assignees) != 2 {
					t.Errorf("expected 2 assignees, got %d", len(issue.Assignees))
				}
				if issue.Milestone != "Sprint 1" {
					t.Errorf("expected milestone 'Sprint 1', got '%s'", issue.Milestone)
				}
			},
		},
		{
			name: "minimal issue data",
			data: map[string]interface{}{
				"number": 1,
				"title":  "Minimal",
			},
			expectError: false,
			validate: func(t *testing.T, issue *Issue) {
				if issue.Number != 1 {
					t.Errorf("expected number 1, got %d", issue.Number)
				}
				if len(issue.Labels) != 0 {
					t.Errorf("expected 0 labels, got %d", len(issue.Labels))
				}
			},
		},
		{
			name:        "missing number",
			data:        map[string]interface{}{"title": "No number"},
			expectError: true,
		},
		{
			name:        "missing title",
			data:        map[string]interface{}{"number": 1},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewGitHubClient(&mockMCPClient{})
			issue, err := client.mapToIssue(tt.data)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, issue)
			}
		})
	}
}

// TestToInt tests type conversion to int.
func TestToInt(t *testing.T) {
	client := NewGitHubClient(&mockMCPClient{})

	tests := []struct {
		name        string
		value       interface{}
		expected    int
		expectError bool
	}{
		{"int", 42, 42, false},
		{"int64", int64(42), 42, false},
		{"float64", float64(42.0), 42, false},
		{"string", "42", 42, false},
		{"invalid string", "abc", 0, true},
		{"unsupported type", true, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.toInt(tt.value)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}
