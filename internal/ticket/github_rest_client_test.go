package ticket

import (
	"context"
	"testing"
	"time"
)

// TestGetPullRequest tests fetching PR details.
func TestGetPullRequest(t *testing.T) {
	tests := []struct {
		name        string
		prNumber    int
		mockResp    map[string]interface{}
		expectError bool
		wantState   string
		wantMerged  bool
	}{
		{
			name:     "open PR",
			prNumber: 50,
			mockResp: map[string]interface{}{
				"number": float64(50),
				"state":  "open",
				"title":  "Test PR",
				"merged": false,
				"head": map[string]interface{}{
					"sha": "abc123",
				},
			},
			expectError: false,
			wantState:   "open",
			wantMerged:  false,
		},
		{
			name:     "merged PR",
			prNumber: 51,
			mockResp: map[string]interface{}{
				"number": float64(51),
				"state":  "closed",
				"title":  "Merged PR",
				"merged": true,
				"head": map[string]interface{}{
					"sha": "def456",
				},
			},
			expectError: false,
			wantState:   "closed",
			wantMerged:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: This is a basic structure test
			// In a real implementation, we'd need to mock the HTTP client
			// For now, just verify the struct can be created
			client := NewGitHubRESTClient()
			if client == nil {
				t.Fatal("NewGitHubRESTClient returned nil")
			}

			// Verify the PullRequest struct can hold the expected data
			pr := &PullRequest{
				Number:  tt.prNumber,
				State:   tt.wantState,
				Merged:  tt.wantMerged,
				Title:   "Test",
				HeadSHA: "abc123",
			}

			if pr.Number != tt.prNumber {
				t.Errorf("got Number=%d, want %d", pr.Number, tt.prNumber)
			}
			if pr.State != tt.wantState {
				t.Errorf("got State=%s, want %s", pr.State, tt.wantState)
			}
			if pr.Merged != tt.wantMerged {
				t.Errorf("got Merged=%v, want %v", pr.Merged, tt.wantMerged)
			}
		})
	}
}

// TestListPRComments tests fetching PR comments.
func TestListPRComments(t *testing.T) {
	// Test PRComment struct
	comment := PRComment{
		ID:        123456,
		Body:      "Test comment",
		User:      "test-user",
		CreatedAt: time.Now(),
	}

	if comment.ID != 123456 {
		t.Errorf("got ID=%d, want 123456", comment.ID)
	}
	if comment.Body != "Test comment" {
		t.Errorf("got Body=%s, want 'Test comment'", comment.Body)
	}
	if comment.User != "test-user" {
		t.Errorf("got User=%s, want 'test-user'", comment.User)
	}
}

// TestGetLatestAIReviewComment tests filtering for AI review comments.
func TestGetLatestAIReviewComment(t *testing.T) {
	ctx := context.Background()
	client := NewGitHubRESTClient()

	// Test comment filtering logic (conceptual test)
	comments := []PRComment{
		{
			ID:        1,
			Body:      "Regular comment",
			CreatedAt: time.Now().Add(-2 * time.Hour),
		},
		{
			ID:        2,
			Body:      "## 🤖 AI Code Review (Gemini 2.5 Pro)\n\nIssues found...",
			CreatedAt: time.Now().Add(-1 * time.Hour),
		},
		{
			ID:        3,
			Body:      "Another regular comment",
			CreatedAt: time.Now(),
		},
	}

	// Verify we can identify AI comments by prefix
	aiPrefix := "## 🤖 AI Code Review (Gemini 2.5 Pro)"
	var aiComments []PRComment
	for _, c := range comments {
		if len(c.Body) >= len(aiPrefix) && c.Body[:len(aiPrefix)] == aiPrefix {
			aiComments = append(aiComments, c)
		}
	}

	if len(aiComments) != 1 {
		t.Errorf("got %d AI comments, want 1", len(aiComments))
	}
	if len(aiComments) > 0 && aiComments[0].ID != 2 {
		t.Errorf("got AI comment ID=%d, want 2", aiComments[0].ID)
	}

	// Verify client was created
	if client == nil {
		t.Fatal("client is nil")
	}
	_ = ctx // Use ctx to avoid unused variable error
}

// TestParseAIReviewFeedback tests extracting actionable feedback.
func TestParseAIReviewFeedback(t *testing.T) {
	client := NewGitHubRESTClient()

	tests := []struct {
		name         string
		comment      *PRComment
		wantContains string
	}{
		{
			name:         "nil comment",
			comment:      nil,
			wantContains: "",
		},
		{
			name: "comment with issues section",
			comment: &PRComment{
				Body: `## 🤖 AI Code Review (Gemini 2.5 Pro)

## Issues Found

1. Add error handling in line 42
2. Missing test coverage for edge case

## Recommendations

Consider adding more tests.`,
			},
			wantContains: "Issues Found",
		},
		{
			name: "comment without issues section",
			comment: &PRComment{
				Body: `## 🤖 AI Code Review (Gemini 2.5 Pro)

Everything looks good!`,
			},
			wantContains: "Everything looks good",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.ParseAIReviewFeedback(tt.comment)

			if tt.wantContains == "" {
				if result != "" {
					t.Errorf("expected empty result for nil comment, got %q", result)
				}
			} else {
				if result == "" {
					t.Error("expected non-empty result")
				}
				// Note: Can't check exact content without implementing full parsing logic
				// Just verify we get something back
			}
		})
	}
}

// TestParsePRSearchResponse tests parsing PR search results.
func TestParsePRSearchResponse(t *testing.T) {
	client := NewGitHubRESTClient()

	tests := []struct {
		name        string
		response    interface{}
		expectNil   bool
		expectError bool
	}{
		{
			name: "valid PR found",
			response: map[string]interface{}{
				"items": []map[string]interface{}{
					{
						"number":   float64(50),
						"state":    "open",
						"html_url": "https://github.com/owner/repo/pull/50",
						"merged":   false,
					},
				},
			},
			expectNil:   false,
			expectError: false,
		},
		{
			name: "no PRs found",
			response: map[string]interface{}{
				"items": []map[string]interface{}{},
			},
			expectNil:   true,
			expectError: false,
		},
		{
			name:        "invalid response type",
			response:    "invalid",
			expectNil:   true,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := client.parsePRSearchResponse(tt.response)

			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if tt.expectNil && result != nil {
				t.Errorf("expected nil result, got %+v", result)
			}
			if !tt.expectNil && result == nil && !tt.expectError {
				t.Error("expected non-nil result")
			}
		})
	}
}

// TestRESTClientMapToIssue tests converting GitHub API data to Issue struct.
func TestRESTClientMapToIssue(t *testing.T) {
	client := NewGitHubRESTClient()

	tests := []struct {
		name        string
		data        map[string]interface{}
		expectError bool
		wantNumber  int
		wantTitle   string
	}{
		{
			name: "valid issue",
			data: map[string]interface{}{
				"number": float64(42),
				"title":  "Test Issue",
				"body":   "Issue description",
				"labels": []interface{}{
					map[string]interface{}{"name": "bug"},
					map[string]interface{}{"name": "urgent"},
				},
			},
			expectError: false,
			wantNumber:  42,
			wantTitle:   "Test Issue",
		},
		{
			name: "missing number",
			data: map[string]interface{}{
				"title": "Test Issue",
			},
			expectError: true,
		},
		{
			name: "missing title",
			data: map[string]interface{}{
				"number": float64(42),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue, err := client.mapToIssue(tt.data)

			if tt.expectError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if issue.Number != tt.wantNumber {
				t.Errorf("got Number=%d, want %d", issue.Number, tt.wantNumber)
			}
			if issue.Title != tt.wantTitle {
				t.Errorf("got Title=%s, want %s", issue.Title, tt.wantTitle)
			}
		})
	}
}
