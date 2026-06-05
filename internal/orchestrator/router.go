package orchestrator

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Mawar2/multi-agent-system/internal/taskqueue"
	"github.com/Mawar2/multi-agent-system/internal/ticket"
)

// Router classifies GitHub Issues by complexity and routes to appropriate worker tier.
// Phase 1: Rule-based classification (fast, deterministic)
// Phase 2: LLM-based classification (Gemini Flash, more accurate)
type Router interface {
	// Route analyzes an issue and determines complexity and tier assignment.
	Route(ctx context.Context, issue *ticket.Issue) (taskqueue.Complexity, taskqueue.Tier, error)
}

// RuleBasedRouter classifies tasks using heuristic rules.
// Fast, deterministic, no API calls.
type RuleBasedRouter struct {
	// Configuration can be added later if needed
}

// NewRuleBasedRouter creates a new rule-based router.
func NewRuleBasedRouter() *RuleBasedRouter {
	return &RuleBasedRouter{}
}

// Route classifies the issue using rules.
func (r *RuleBasedRouter) Route(ctx context.Context, issue *ticket.Issue) (taskqueue.Complexity, taskqueue.Tier, error) {
	complexity := r.classifyComplexity(issue)
	tier := r.assignTier(complexity)
	return complexity, tier, nil
}

// classifyComplexity determines task complexity based on heuristics.
func (r *RuleBasedRouter) classifyComplexity(issue *ticket.Issue) taskqueue.Complexity {
	title := strings.ToLower(issue.Title)
	body := strings.ToLower(issue.Body)
	combined := title + " " + body

	// Simple task indicators
	simplePatterns := []string{
		"add comment", "add godoc", "add documentation",
		"fix typo", "update readme", "format code",
		"add logging", "update version",
		"docs:", "[docs]", "documentation",
	}
	for _, pattern := range simplePatterns {
		if strings.Contains(combined, pattern) {
			return taskqueue.ComplexitySimple
		}
	}

	// Complex task indicators
	complexPatterns := []string{
		"architecture", "design", "refactor.*system",
		"implement.*agent", "new feature.*complex",
		"database", "migration", "schema change",
		"security", "authentication", "authorization",
		"breaking change", "api redesign",
	}
	for _, pattern := range complexPatterns {
		matched, _ := regexp.MatchString(pattern, combined)
		if matched {
			return taskqueue.ComplexityComplex
		}
	}

	// File count estimation from body
	// Look for patterns like "Files to modify: X" or lists of files
	if strings.Contains(body, "files:") || strings.Contains(body, "affected files") {
		fileCount := r.estimateFileCount(body)
		if fileCount <= 3 {
			return taskqueue.ComplexitySimple
		} else if fileCount > 10 {
			return taskqueue.ComplexityComplex
		}
	}

	// Check labels
	for _, label := range issue.Labels {
		label = strings.ToLower(label)
		if strings.Contains(label, "simple") || strings.Contains(label, "easy") {
			return taskqueue.ComplexitySimple
		}
		if strings.Contains(label, "complex") || strings.Contains(label, "hard") {
			return taskqueue.ComplexityComplex
		}
	}

	// Default to medium if no clear signals
	return taskqueue.ComplexityMedium
}

// estimateFileCount tries to extract file count from issue body.
func (r *RuleBasedRouter) estimateFileCount(body string) int {
	// Look for "X files" pattern
	fileCountPattern := regexp.MustCompile(`(\d+)\s+files?`)
	matches := fileCountPattern.FindStringSubmatch(body)
	if len(matches) > 1 {
		var count int
		if _, err := fmt.Sscanf(matches[1], "%d", &count); err == nil {
			return count
		}
	}

	// Count bullet points in "Files" sections
	filesSection := regexp.MustCompile(`(?i)(files?|affected|modified):\s*\n((?:\s*[-*]\s*.+\n?)+)`)
	if matches := filesSection.FindStringSubmatch(body); len(matches) > 2 {
		lines := strings.Split(matches[2], "\n")
		count := 0
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "-") || strings.HasPrefix(strings.TrimSpace(line), "*") {
				count++
			}
		}
		return count
	}

	return 0 // Unknown
}

// assignTier maps complexity to worker tier.
func (r *RuleBasedRouter) assignTier(complexity taskqueue.Complexity) taskqueue.Tier {
	switch complexity {
	case taskqueue.ComplexitySimple:
		return taskqueue.TierGeminiFlash // Fast, free, handles simple tasks well
	case taskqueue.ComplexityMedium:
		return taskqueue.TierGeminiPro // More capable, still free
	case taskqueue.ComplexityComplex:
		return taskqueue.TierClaude // Best reasoning, use for hard problems
	default:
		return taskqueue.TierGeminiPro // Default to middle tier
	}
}
