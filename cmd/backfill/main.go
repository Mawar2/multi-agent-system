package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/ticket"
	"github.com/google/uuid"
)

func main() {
	ctx := context.Background()

	// 1. Initialize GitHub client
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN environment variable not set")
	}

	githubClient := ticket.NewGitHubRESTClient()

	// 2. Initialize task queue
	queue, err := taskqueue.NewJSONQueue("./tasks")
	if err != nil {
		log.Fatalf("Failed to initialize task queue: %v", err)
	}

	// 3. Fetch open PRs from Kaimi
	fmt.Println("Fetching open PRs from Mawar2/Kaimi...")
	prs, err := githubClient.ListOpenPRs(ctx, "Mawar2", "Kaimi")
	if err != nil {
		log.Fatalf("Failed to fetch PRs: %v", err)
	}

	fmt.Printf("Found %d open PRs in Kaimi\n\n", len(prs))

	// 4. For each PR, create a task
	created := 0
	skipped := 0

	for _, pr := range prs {
		// Skip drafts
		if pr.Draft {
			fmt.Printf("⏭️  Skipping draft PR #%d: %s\n", pr.Number, pr.Title)
			skipped++
			continue
		}

		// Infer complexity from PR size (simple heuristic)
		complexity := inferComplexity(pr)
		tier := tierFromComplexity(complexity)

		// Infer issue number from branch name or PR body
		issueNum := inferIssueNumber(pr)

		task := &taskqueue.Task{
			ID:          uuid.New().String(),
			IssueNumber: issueNum,
			RepoOwner:   "Mawar2",
			RepoName:    "Kaimi",
			Title:       pr.Title,
			Description: pr.Body,
			Complexity:  complexity,
			Tier:        tier,
			Status:      taskqueue.StatusReview, // KEY: Mark as Review so supervisor monitors
			BranchName:  pr.HeadBranch,
			PRNumber:    pr.Number,
			ClaimedAt:   time.Now(),
			StartedAt:   time.Now(),
			Metadata: map[string]string{
				"task_type":  "issue",
				"backfilled": "true",
			},
			ReviewIteration: 0,
		}

		if err := queue.Enqueue(ctx, task); err != nil {
			log.Printf("❌ Failed to enqueue task for PR #%d: %v", pr.Number, err)
			continue
		}

		fmt.Printf("✅ Created task %s for PR #%d: %s (complexity: %d, tier: %s)\n",
			task.ID[:8], pr.Number, pr.Title, complexity, tier)
		created++
	}

	fmt.Println()
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("Backfill complete!")
	fmt.Printf("  Created: %d tasks\n", created)
	fmt.Printf("  Skipped: %d tasks (drafts)\n", skipped)
	fmt.Printf("  Total:   %d PRs\n", len(prs))
	fmt.Println(strings.Repeat("=", 60))
}

// inferComplexity estimates task complexity based on PR size.
// Returns a complexity score from 0-10.
func inferComplexity(pr *ticket.PullRequest) taskqueue.Complexity {
	totalChanges := pr.Additions + pr.Deletions

	// Heuristic based on lines changed
	switch {
	case totalChanges < 50:
		return 1 // Simple (small fix, doc change)
	case totalChanges < 200:
		return 2 // Medium-low (feature skeleton, small feature)
	case totalChanges < 500:
		return 3 // Medium (feature implementation)
	case totalChanges < 1000:
		return 4 // Medium-high (large feature, refactor)
	default:
		return 5 // Complex (major refactor, architecture change)
	}
}

// tierFromComplexity maps complexity score to worker tier.
func tierFromComplexity(c taskqueue.Complexity) taskqueue.Tier {
	switch {
	case c <= 1:
		return taskqueue.TierGeminiFlash // Simple tasks → Gemini Flash
	case c <= 4:
		return taskqueue.TierGeminiPro // Medium tasks → Gemini Pro
	default:
		return taskqueue.TierClaude // Complex tasks → Claude
	}
}

// inferIssueNumber extracts issue number from PR branch name or body.
// Examples:
//   - "feature/KAI-6-final-review" → 6
//   - "40_quota_failover" → 40
//   - PR body: "Closes #42" → 42
func inferIssueNumber(pr *ticket.PullRequest) int {
	// Try to extract from branch name first
	// Pattern 1: "feature/KAI-{num}-..." or "KAI-{num}"
	re1 := regexp.MustCompile(`KAI-(\d+)`)
	if matches := re1.FindStringSubmatch(pr.HeadBranch); len(matches) > 1 {
		if num, err := strconv.Atoi(matches[1]); err == nil {
			return num
		}
	}

	// Pattern 2: "{num}_..." (e.g., "40_quota_failover")
	re2 := regexp.MustCompile(`^(\d+)_`)
	if matches := re2.FindStringSubmatch(pr.HeadBranch); len(matches) > 1 {
		if num, err := strconv.Atoi(matches[1]); err == nil {
			return num
		}
	}

	// Try to extract from PR body
	// Pattern: "Closes #num" or "Fixes #num"
	re3 := regexp.MustCompile(`(?i)(closes|fixes|resolves)\s+#(\d+)`)
	if matches := re3.FindStringSubmatch(pr.Body); len(matches) > 2 {
		if num, err := strconv.Atoi(matches[2]); err == nil {
			return num
		}
	}

	// No issue number found, return 0 (will be handled as unknown)
	return 0
}
