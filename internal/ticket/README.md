# Ticket Client

**Package:** `github.com/Mawar2/multi-agent-system/internal/ticket`

The ticket client provides a Go interface for fetching and parsing GitHub Issues using GitHub MCP tools. It's designed for the multi-agent supervisor system to route issues to appropriate worker agents.

## Overview

The ticket client:
- Fetches open issues from GitHub repositories
- Retrieves specific issues by number
- Parses acceptance criteria checklists from issue bodies
- Checks for associated pull requests

## Architecture

```
┌─────────────────┐
│  GitHubClient   │
└────────┬────────┘
         │
         ▼
   ┌─────────────┐
   │  MCPClient  │ (interface)
   └─────────────┘
         │
         ▼
   GitHub MCP Tools
```

### Key Components

**`Client` interface:** Defines operations for fetching and parsing issues.

**`GitHubClient` struct:** Implements `Client` using GitHub MCP tools.

**`MCPClient` interface:** Abstraction for calling MCP tools, enabling mocking in tests.

**`Issue` struct:** Represents a GitHub issue with all relevant metadata.

**`PRStatus` struct:** Represents pull request status associated with an issue.

## Usage

### Basic Example

```go
import (
    "context"
    "github.com/Mawar2/multi-agent-system/internal/ticket"
)

// Create MCP client (implementation-specific)
mcpClient := NewYourMCPClient()

// Create GitHub client
client := ticket.NewGitHubClient(mcpClient)

// Fetch all open issues
issues, err := client.FetchIssues(ctx, "owner", "repo", nil)
if err != nil {
    log.Fatalf("Failed to fetch issues: %v", err)
}

for _, issue := range issues {
    fmt.Printf("Issue #%d: %s\n", issue.Number, issue.Title)
}
```

### Filtering by Labels

```go
// Fetch only issues with specific labels
bugIssues, err := client.FetchIssues(
    ctx,
    "owner",
    "repo",
    []string{"bug", "priority:high"},
)
```

### Getting a Specific Issue

```go
issue, err := client.GetIssue(ctx, "owner", "repo", 42)
if err != nil {
    log.Fatalf("Failed to get issue: %v", err)
}
fmt.Printf("Title: %s\n", issue.Title)
fmt.Printf("Labels: %v\n", issue.Labels)
fmt.Printf("Assignees: %v\n", issue.Assignees)
```

### Parsing Acceptance Criteria

The client automatically extracts checklist items from issue bodies:

```go
issueBody := `
## Acceptance Criteria
- [ ] Implement feature X
- [ ] Write unit tests
- [x] Update documentation
`

criteria, err := client.ParseAcceptanceCriteria(issueBody)
// Returns: ["Implement feature X", "Write unit tests", "Update documentation"]
```

### Checking PR Status

```go
prStatus, err := client.CheckPRStatus(ctx, "owner", "repo", 42)
if err != nil {
    log.Fatalf("Failed to check PR status: %v", err)
}

if prStatus != nil {
    fmt.Printf("PR #%d: %s (merged: %t)\n",
        prStatus.Number,
        prStatus.State,
        prStatus.Merged,
    )
} else {
    fmt.Println("No PR found")
}
```

## MCP Client Integration

The client requires an `MCPClient` implementation. This abstraction allows:

1. **Mocking in tests** - Use mock responses without real API calls
2. **Flexible integration** - Swap MCP implementations without changing client code
3. **Testability** - Unit tests run fast with no external dependencies

### MCP Client Interface

```go
type MCPClient interface {
    Call(ctx context.Context, tool string, params map[string]interface{}) (interface{}, error)
}
```

### MCP Tools Used

- `mcp__github__list_issues` - Fetches open issues from a repository
- `mcp__github__issue_read` - Retrieves a single issue by number
- `mcp__github__search_pull_requests` - Searches for PRs referencing an issue

## Data Structures

### Issue

```go
type Issue struct {
    Number      int       // Issue number
    Title       string    // Issue title
    Body        string    // Issue body (markdown)
    Labels      []string  // Label names
    RepoOwner   string    // Repository owner
    RepoName    string    // Repository name
    Assignees   []string  // Assignee usernames
    Milestone   string    // Milestone title
    CreatedAt   string    // ISO 8601 timestamp
    UpdatedAt   string    // ISO 8601 timestamp
}
```

### PRStatus

```go
type PRStatus struct {
    Number  int    // PR number
    State   string // "open", "closed", "merged"
    HTMLURL string // PR URL on GitHub
    Merged  bool   // Whether PR was merged
}
```

## Testing

The package includes comprehensive tests with mocked MCP responses:

```bash
# Run all tests
go test ./internal/ticket/...

# Run with verbose output
go test ./internal/ticket/... -v

# Run specific test
go test ./internal/ticket/... -run TestFetchIssues
```

### Test Coverage

- ✅ FetchIssues with various response formats
- ✅ GetIssue success and error cases
- ✅ ParseAcceptanceCriteria with multiple markdown formats
- ✅ CheckPRStatus with open/closed/merged states
- ✅ Data mapping and type conversion
- ✅ Error handling

## Implementation Details

### Response Parsing

The client handles multiple GitHub API response formats:
- Responses with `items` field (search results)
- Responses with `data` field (alternative format)
- Nested repository/owner/assignee objects
- Various numeric types (int, int64, float64)

### Error Handling

All methods return descriptive errors with context:
```go
return nil, fmt.Errorf("failed to list issues: %w", err)
```

Errors are wrapped using `%w` for error chain inspection.

### Acceptance Criteria Parsing

The parser uses regex to extract markdown checklist items:
```regex
^\s*-\s*\[([ xX])\]\s*(.+)$
```

This matches:
- `- [ ] Unchecked item`
- `- [x] Checked item`
- `- [X] Checked item (capital X)`
- Items with varying whitespace

## Integration with Supervisor

The ticket client is used by the supervisor to:

1. **Poll GitHub** for new issues at regular intervals
2. **Filter issues** by project-specific labels
3. **Extract acceptance criteria** for task definition
4. **Check PR status** to avoid duplicate work
5. **Route issues** to appropriate worker agents based on complexity

## Future Enhancements

Potential improvements (not currently implemented):

- Pagination support for large result sets
- Caching to reduce API calls
- Rate limit tracking and backoff
- Issue state transitions (open → closed)
- Comment retrieval for issue context
- Issue creation/update (if needed for automation)

## Dependencies

- `encoding/json` - Response parsing
- `regexp` - Acceptance criteria extraction
- `strings` - String manipulation

No external dependencies required (stdlib only).

---

**Last updated:** 2026-06-05
