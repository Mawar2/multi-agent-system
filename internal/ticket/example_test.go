package ticket_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Mawar2/multi-agent-system/internal/ticket"
)

// ExampleGitHubClient demonstrates how to use the GitHub client to fetch and process issues.
func ExampleGitHubClient() {
	// In production, you would use a real MCP client implementation
	// For this example, we'll show the usage pattern

	// Create the client with your MCP client implementation
	// mcpClient := NewRealMCPClient()
	// client := ticket.NewGitHubClient(mcpClient)

	ctx := context.Background()

	// Example: Fetch all open issues
	// issues, err := client.FetchIssues(ctx, "Mawar2", "Pulse", nil)
	// if err != nil {
	//     log.Fatalf("Failed to fetch issues: %v", err)
	// }
	// fmt.Printf("Found %d open issues\n", len(issues))

	// Example: Fetch issues with specific labels
	// bugIssues, err := client.FetchIssues(ctx, "Mawar2", "Pulse", []string{"bug", "priority:high"})
	// if err != nil {
	//     log.Fatalf("Failed to fetch bug issues: %v", err)
	// }
	// fmt.Printf("Found %d high-priority bug issues\n", len(bugIssues))

	// Example: Get a specific issue by number
	// issue, err := client.GetIssue(ctx, "Mawar2", "Pulse", 42)
	// if err != nil {
	//     log.Fatalf("Failed to get issue #42: %v", err)
	// }
	// fmt.Printf("Issue #%d: %s\n", issue.Number, issue.Title)

	// Example: Parse acceptance criteria from issue body
	// criteria, err := client.ParseAcceptanceCriteria(issue.Body)
	// if err != nil {
	//     log.Fatalf("Failed to parse acceptance criteria: %v", err)
	// }
	// fmt.Printf("Found %d acceptance criteria:\n", len(criteria))
	// for i, criterion := range criteria {
	//     fmt.Printf("  %d. %s\n", i+1, criterion)
	// }

	// Example: Check if issue has an associated PR
	// prStatus, err := client.CheckPRStatus(ctx, "Mawar2", "Pulse", 42)
	// if err != nil {
	//     log.Fatalf("Failed to check PR status: %v", err)
	// }
	// if prStatus != nil {
	//     fmt.Printf("Issue has PR #%d (state: %s, merged: %t)\n",
	//         prStatus.Number, prStatus.State, prStatus.Merged)
	// } else {
	//     fmt.Println("No PR found for this issue")
	// }

	// Suppress unused variable warnings in example
	_ = ctx
}

// This example shows how to implement a basic MCP client adapter.
// In production, this would integrate with the actual MCP protocol implementation.
type exampleMCPClient struct{}

func (c *exampleMCPClient) Call(ctx context.Context, tool string, params map[string]interface{}) (interface{}, error) {
	// In a real implementation, this would:
	// 1. Serialize params to JSON
	// 2. Send MCP request to the tool
	// 3. Receive and deserialize response
	// 4. Return the result

	switch tool {
	case "mcp__github__list_issues":
		// Example: return mock data for demonstration
		return map[string]interface{}{
			"items": []map[string]interface{}{
				{
					"number": 1,
					"title":  "Example issue",
					"body":   "- [ ] Acceptance criterion 1\n- [ ] Acceptance criterion 2",
					"labels": []interface{}{
						map[string]interface{}{"name": "enhancement"},
					},
				},
			},
		}, nil
	case "mcp__github__issue_read":
		issueNum := params["issue_number"].(int)
		return map[string]interface{}{
			"number": issueNum,
			"title":  fmt.Sprintf("Issue #%d", issueNum),
			"body":   "Issue body",
		}, nil
	case "mcp__github__search_pull_requests":
		// Return no PRs for this example
		return map[string]interface{}{
			"items": []map[string]interface{}{},
		}, nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", tool)
	}
}

// Example_workflow demonstrates a complete workflow using the GitHub client.
func Example_workflow() {
	ctx := context.Background()

	// Create MCP client (use real implementation in production)
	mcpClient := &exampleMCPClient{}

	// Create GitHub client
	client := ticket.NewGitHubClient(mcpClient)

	// Step 1: Fetch all open issues
	issues, err := client.FetchIssues(ctx, "Mawar2", "Pulse", nil)
	if err != nil {
		log.Fatalf("Failed to fetch issues: %v", err)
	}
	fmt.Printf("Step 1: Found %d open issues\n", len(issues))

	// Step 2: Process each issue
	for _, issue := range issues {
		fmt.Printf("\nProcessing issue #%d: %s\n", issue.Number, issue.Title)

		// Step 3: Parse acceptance criteria
		criteria, err := client.ParseAcceptanceCriteria(issue.Body)
		if err != nil {
			log.Printf("Warning: Failed to parse criteria for issue #%d: %v", issue.Number, err)
			continue
		}

		if len(criteria) > 0 {
			fmt.Printf("  Acceptance criteria (%d):\n", len(criteria))
			for i, criterion := range criteria {
				fmt.Printf("    %d. %s\n", i+1, criterion)
			}
		}

		// Step 4: Check for associated PR
		prStatus, err := client.CheckPRStatus(ctx, issue.RepoOwner, issue.RepoName, issue.Number)
		if err != nil {
			log.Printf("Warning: Failed to check PR status for issue #%d: %v", issue.Number, err)
			continue
		}

		if prStatus != nil {
			fmt.Printf("  PR Status: #%d (%s) - Merged: %t\n",
				prStatus.Number, prStatus.State, prStatus.Merged)
		} else {
			fmt.Println("  No associated PR")
		}
	}

	// Output:
	// Step 1: Found 1 open issues
	//
	// Processing issue #1: Example issue
	//   Acceptance criteria (2):
	//     1. Acceptance criterion 1
	//     2. Acceptance criterion 2
	//   No associated PR
}
