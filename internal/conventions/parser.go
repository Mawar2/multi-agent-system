package conventions

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ParseConventions reads convention files from the specified project directory
// and extracts project-specific rules into a Ruleset.
//
// It looks for CLAUDE.md and CONVENTIONS.md in the project root and parses
// patterns, commands, and rules from them. If the files don't exist or parsing
// fails, it returns a Ruleset with sensible defaults.
//
// The projectPath should be an absolute path to the project root directory.
func ParseConventions(projectPath string) (*Ruleset, error) {
	// Validate project path
	if projectPath == "" {
		return nil, fmt.Errorf("project path cannot be empty")
	}

	// Create base ruleset with defaults
	ruleset := NewRuleset(projectPath)

	// Try to parse CLAUDE.md first (higher priority)
	claudePath := filepath.Join(projectPath, "CLAUDE.md")
	if fileExists(claudePath) {
		if err := parseClaudeMd(claudePath, ruleset); err != nil {
			// Non-fatal: log but continue with defaults
			fmt.Fprintf(os.Stderr, "Warning: error parsing CLAUDE.md: %v\n", err)
		}
	}

	// Try to parse CONVENTIONS.md (can override or augment CLAUDE.md)
	conventionsPath := filepath.Join(projectPath, "CONVENTIONS.md")
	if fileExists(conventionsPath) {
		if err := parseConventionsMd(conventionsPath, ruleset); err != nil {
			// Non-fatal: log but continue with defaults
			fmt.Fprintf(os.Stderr, "Warning: error parsing CONVENTIONS.md: %v\n", err)
		}
	}

	// Try to detect commands from Makefile if not already set
	if ruleset.TestCommand == "go test ./..." || ruleset.LintCommand == "golangci-lint run" {
		makefilePath := filepath.Join(projectPath, "Makefile")
		if fileExists(makefilePath) {
			parseMakefile(makefilePath, ruleset)
		}
	}

	return ruleset, nil
}

// parseClaudeMd extracts conventions from CLAUDE.md.
func parseClaudeMd(path string, ruleset *Ruleset) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read CLAUDE.md: %w", err)
	}

	text := string(content)

	// Extract branch pattern
	if pattern := extractPattern(text, `(?m)^.*branch.*pattern:\s*(.+)$`); pattern != "" {
		ruleset.BranchPattern = cleanPattern(pattern)
	}

	// Extract commit pattern
	if pattern := extractPattern(text, `(?m)^.*commit.*(?:format|pattern):\s*(.+)$`); pattern != "" {
		ruleset.CommitPattern = cleanPattern(pattern)
	}

	// Extract forbidden files
	if files := extractForbiddenFiles(text); len(files) > 0 {
		ruleset.ForbiddenFiles = files
	}

	// Check for TDD requirement
	if containsIgnoreCase(text, "TDD required: true") || containsIgnoreCase(text, "TDDRequired: true") ||
		containsIgnoreCase(text, "TDD: true") || containsIgnoreCase(text, "Write the test first") {
		ruleset.TDDRequired = true
	}

	// Extract test command
	if cmd := extractCommand(text, `(?:test.*command|run.*tests?)[:\s]+(.+?)(?:\n|$)`); cmd != "" {
		ruleset.TestCommand = cmd
	}

	// Extract lint command
	if cmd := extractCommand(text, `(?:lint.*command|run.*linter?)[:\s]+(.+?)(?:\n|$)`); cmd != "" {
		ruleset.LintCommand = cmd
	}

	// Extract format command
	if cmd := extractCommand(text, `(?:format.*command|run.*formatter?)[:\s]+(.+?)(?:\n|$)`); cmd != "" {
		ruleset.FormatCommand = cmd
	}

	return nil
}

// parseConventionsMd extracts conventions from CONVENTIONS.md.
func parseConventionsMd(path string, ruleset *Ruleset) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read CONVENTIONS.md: %w", err)
	}

	text := string(content)

	// Same extraction logic as CLAUDE.md, but only override if CLAUDE.md didn't set it
	if ruleset.BranchPattern == "" {
		if pattern := extractPattern(text, `(?m)^.*branch.*pattern:\s*(.+)$`); pattern != "" {
			ruleset.BranchPattern = cleanPattern(pattern)
		}
	}

	if ruleset.CommitPattern == "" {
		if pattern := extractPattern(text, `(?m)^.*commit.*(?:format|pattern):\s*(.+)$`); pattern != "" {
			ruleset.CommitPattern = cleanPattern(pattern)
		}
	}

	if len(ruleset.ForbiddenFiles) == 0 {
		if files := extractForbiddenFiles(text); len(files) > 0 {
			ruleset.ForbiddenFiles = files
		}
	}

	// Extract commands if not already set by CLAUDE.md
	if ruleset.TestCommand == "go test ./..." {
		if cmd := extractCommand(text, `(?:test.*command|run.*tests?)[:\s]+(.+?)(?:\n|$)`); cmd != "" {
			ruleset.TestCommand = cmd
		}
	}

	if ruleset.LintCommand == "golangci-lint run" {
		if cmd := extractCommand(text, `(?:lint.*command|run.*linter?)[:\s]+(.+?)(?:\n|$)`); cmd != "" {
			ruleset.LintCommand = cmd
		}
	}

	if ruleset.FormatCommand == "gofmt -w ." {
		if cmd := extractCommand(text, `(?:format.*command|run.*formatter?)[:\s]+(.+?)(?:\n|$)`); cmd != "" {
			ruleset.FormatCommand = cmd
		}
	}

	return nil
}

// parseMakefile extracts test and lint commands from a Makefile.
func parseMakefile(path string, ruleset *Ruleset) {
	content, err := os.ReadFile(path)
	if err != nil {
		return // Non-fatal
	}

	text := string(content)

	// Look for test target
	if ruleset.TestCommand == "go test ./..." {
		if strings.Contains(text, "test:") || strings.Contains(text, ".PHONY: test") {
			ruleset.TestCommand = "make test"
		}
	}

	// Look for lint target
	if ruleset.LintCommand == "golangci-lint run" {
		if strings.Contains(text, "lint:") || strings.Contains(text, ".PHONY: lint") {
			ruleset.LintCommand = "make lint"
		}
	}

	// Look for format/fmt target
	if ruleset.FormatCommand == "gofmt -w ." {
		if strings.Contains(text, "fmt:") || strings.Contains(text, "format:") ||
			strings.Contains(text, ".PHONY: fmt") || strings.Contains(text, ".PHONY: format") {
			ruleset.FormatCommand = "make fmt"
		}
	}
}

// extractPattern extracts a pattern using the given regex.
// Returns empty string if no match found.
func extractPattern(text, pattern string) string {
	re := regexp.MustCompile(`(?i)` + pattern)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractCommand extracts a command using the given regex.
// Returns empty string if no match found.
func extractCommand(text, pattern string) string {
	re := regexp.MustCompile(`(?i)` + pattern)
	matches := re.FindStringSubmatch(text)
	if len(matches) > 1 {
		cmd := strings.TrimSpace(matches[1])
		// Remove trailing backticks if present
		cmd = strings.Trim(cmd, "`")
		return cmd
	}
	return ""
}

// extractForbiddenFiles looks for lists of forbidden filenames.
// Searches for patterns like "FORBIDDEN filenames: utils.go, helpers.go"
// or bullet lists under a "Forbidden" heading.
func extractForbiddenFiles(text string) []string {
	var files []string

	// Pattern 1: Inline list (e.g., "FORBIDDEN filenames: utils.go, helpers.go")
	re1 := regexp.MustCompile(`(?im)^.*forbidden.*(?:filenames?|files?):\s*(.+)$`)
	if matches := re1.FindStringSubmatch(text); len(matches) > 1 {
		list := matches[1]

		// Check if comma-separated or space-separated
		var parts []string
		if strings.Contains(list, ",") {
			// Comma-separated
			parts = strings.Split(list, ",")
		} else {
			// Space-separated
			parts = strings.Fields(list)
		}

		// Process each part
		for _, part := range parts {
			part = strings.TrimSpace(part)
			part = strings.Trim(part, "`\"'[]")
			if part != "" && (strings.HasSuffix(part, ".go") || strings.Contains(part, ".")) {
				files = append(files, part)
			}
		}
	}

	// Pattern 2: Array notation (e.g., ["utils.go", "helpers.go"])
	re2 := regexp.MustCompile(`\[\s*"([^"]+)"(?:\s*,\s*"([^"]+)")*\s*]`)
	arrayMatches := re2.FindAllStringSubmatch(text, -1)
	for _, match := range arrayMatches {
		for i := 1; i < len(match); i++ {
			if match[i] != "" && strings.HasSuffix(match[i], ".go") {
				// Avoid duplicates
				if !contains(files, match[i]) {
					files = append(files, match[i])
				}
			}
		}
	}

	return files
}

// cleanPattern removes markdown formatting and extra quotes from a pattern string.
func cleanPattern(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"'")
	return s
}

// containsIgnoreCase checks if text contains substr (case-insensitive).
func containsIgnoreCase(text, substr string) bool {
	return strings.Contains(strings.ToLower(text), strings.ToLower(substr))
}

// contains checks if a slice contains a string.
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// fileExists checks if a file exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
