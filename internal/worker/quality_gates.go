package worker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Mawar2/multi-agent-system/internal/conventions"
)

// QualityGates runs pre-PR validation checks to ensure code quality
// before creating expensive GitHub PRs that trigger AI reviews.
//
// This reduces costs by preventing low-quality PRs from being created,
// which would waste AI review budget and require rework.
//
// QualityGates is stateless: the workspace directory is passed to each method
// so a single instance can serve multiple workers or tasks.
type QualityGates struct{}

// NewQualityGates creates a stateless quality gate validator.
func NewQualityGates() *QualityGates {
	return &QualityGates{}
}

// ValidationResult tracks the outcome of a single quality check.
type ValidationResult struct {
	CheckName string
	Passed    bool
	Output    string
	Error     error
}

// Validate runs all quality checks and returns detailed results.
//
// Checks performed (in order):
// 1. Tests pass (go test ./... or npm test)
// 2. Linter clean (golangci-lint or eslint)
// 3. Formatter clean (gofmt or prettier)
// 4. Build succeeds (optional - only if BuildCommand specified)
//
// Returns error on first failure. This prevents PR creation and saves costs.
func (qg *QualityGates) Validate(ctx context.Context, workspaceDir string, ruleset *conventions.Ruleset) error {
	fmt.Printf("[QualityGates] Running pre-PR quality checks in %s\n", workspaceDir)

	// Check 1: Tests
	if err := qg.runTests(ctx, workspaceDir, ruleset); err != nil {
		return fmt.Errorf("quality gate failed - tests: %w", err)
	}

	// Check 2: Linter
	if err := qg.runLinter(ctx, workspaceDir, ruleset); err != nil {
		return fmt.Errorf("quality gate failed - linter: %w", err)
	}

	// Check 3: Formatter
	if err := qg.runFormatter(ctx, workspaceDir, ruleset); err != nil {
		return fmt.Errorf("quality gate failed - formatter: %w", err)
	}

	// Check 4: Build (optional - only if project has build command)
	if ruleset.BuildCommand != "" {
		if err := qg.runBuild(ctx, workspaceDir, ruleset); err != nil {
			return fmt.Errorf("quality gate failed - build: %w", err)
		}
	}

	fmt.Printf("[QualityGates] ✅ All quality checks passed - safe to create PR\n")
	return nil
}

// runTests executes the project's test command and verifies all tests pass.
func (qg *QualityGates) runTests(ctx context.Context, workspaceDir string, ruleset *conventions.Ruleset) error {
	fmt.Printf("[QualityGates] Running tests: %s\n", ruleset.TestCommand)

	cmd := exec.CommandContext(ctx, "sh", "-c", ruleset.TestCommand)
	cmd.Dir = workspaceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tests failed: %w\nOutput:\n%s", err, string(output))
	}

	fmt.Printf("[QualityGates] ✅ Tests passed\n")
	return nil
}

// runLinter executes the project's linter and verifies no issues found.
func (qg *QualityGates) runLinter(ctx context.Context, workspaceDir string, ruleset *conventions.Ruleset) error {
	fmt.Printf("[QualityGates] Running linter: %s\n", ruleset.LintCommand)

	cmd := exec.CommandContext(ctx, "sh", "-c", ruleset.LintCommand)
	cmd.Dir = workspaceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("linter found issues: %w\nOutput:\n%s", err, string(output))
	}

	// Some linters return 0 but still output warnings
	// Check if output contains common error indicators
	outputStr := strings.ToLower(string(output))
	if strings.Contains(outputStr, "error") || strings.Contains(outputStr, "fail") {
		return fmt.Errorf("linter found issues:\n%s", string(output))
	}

	fmt.Printf("[QualityGates] ✅ Linter passed\n")
	return nil
}

// runFormatter executes the formatter and verifies code is properly formatted.
// This checks if the formatter would make any changes - if so, code is not formatted.
func (qg *QualityGates) runFormatter(ctx context.Context, workspaceDir string, ruleset *conventions.Ruleset) error {
	fmt.Printf("[QualityGates] Checking formatter: %s\n", ruleset.FormatCommand)

	// Run formatter (most formatters auto-fix)
	cmd := exec.CommandContext(ctx, "sh", "-c", ruleset.FormatCommand)
	cmd.Dir = workspaceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("formatter failed: %w\nOutput:\n%s", err, string(output))
	}

	// Check if formatter made any changes to files
	statusCmd := exec.CommandContext(ctx, "git", "-C", workspaceDir, "status", "--porcelain")
	statusOutput, err := statusCmd.Output()
	if err != nil {
		// If git status fails, that's OK - might not be a git repo yet
		fmt.Printf("[QualityGates] ✅ Formatter passed (no git status available)\n")
		return nil
	}

	if len(statusOutput) > 0 {
		return fmt.Errorf("code not properly formatted - formatter made changes:\n%s", string(statusOutput))
	}

	fmt.Printf("[QualityGates] ✅ Formatter passed\n")
	return nil
}

// runBuild executes the project's build command and verifies it succeeds.
// This is optional - only runs if the project specifies a BuildCommand.
func (qg *QualityGates) runBuild(ctx context.Context, workspaceDir string, ruleset *conventions.Ruleset) error {
	fmt.Printf("[QualityGates] Running build: %s\n", ruleset.BuildCommand)

	cmd := exec.CommandContext(ctx, "sh", "-c", ruleset.BuildCommand)
	cmd.Dir = workspaceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("build failed: %w\nOutput:\n%s", err, string(output))
	}

	fmt.Printf("[QualityGates] ✅ Build passed\n")
	return nil
}

// ValidateWithDetails runs all checks and returns detailed results for each.
// Use this when you need granular feedback about which checks passed/failed.
func (qg *QualityGates) ValidateWithDetails(ctx context.Context, workspaceDir string, ruleset *conventions.Ruleset) []ValidationResult {
	results := make([]ValidationResult, 0)

	// Test check
	testErr := qg.runTests(ctx, workspaceDir, ruleset)
	results = append(results, ValidationResult{
		CheckName: "tests",
		Passed:    testErr == nil,
		Error:     testErr,
	})

	// Linter check
	lintErr := qg.runLinter(ctx, workspaceDir, ruleset)
	results = append(results, ValidationResult{
		CheckName: "linter",
		Passed:    lintErr == nil,
		Error:     lintErr,
	})

	// Formatter check
	fmtErr := qg.runFormatter(ctx, workspaceDir, ruleset)
	results = append(results, ValidationResult{
		CheckName: "formatter",
		Passed:    fmtErr == nil,
		Error:     fmtErr,
	})

	// Build check (optional)
	if ruleset.BuildCommand != "" {
		buildErr := qg.runBuild(ctx, workspaceDir, ruleset)
		results = append(results, ValidationResult{
			CheckName: "build",
			Passed:    buildErr == nil,
			Error:     buildErr,
		})
	}

	return results
}
