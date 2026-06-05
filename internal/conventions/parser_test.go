package conventions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewRuleset(t *testing.T) {
	projectPath := "/test/project"
	ruleset := NewRuleset(projectPath)

	if ruleset.ProjectPath != projectPath {
		t.Errorf("expected ProjectPath %s, got %s", projectPath, ruleset.ProjectPath)
	}

	if ruleset.BranchPattern != "" {
		t.Errorf("expected empty BranchPattern, got %s", ruleset.BranchPattern)
	}

	if ruleset.CommitPattern != "" {
		t.Errorf("expected empty CommitPattern, got %s", ruleset.CommitPattern)
	}

	if ruleset.TestCommand != "go test ./..." {
		t.Errorf("expected default TestCommand, got %s", ruleset.TestCommand)
	}

	if ruleset.TDDRequired {
		t.Error("expected TDDRequired to be false by default")
	}

	if ruleset.CustomRules == nil {
		t.Error("expected CustomRules to be initialized")
	}
}

func TestRulesetHelperMethods(t *testing.T) {
	ruleset := NewRuleset("/test")

	// Test HasBranchPattern
	if ruleset.HasBranchPattern() {
		t.Error("expected HasBranchPattern to return false for empty pattern")
	}
	ruleset.BranchPattern = "feature/{ticket}"
	if !ruleset.HasBranchPattern() {
		t.Error("expected HasBranchPattern to return true")
	}

	// Test HasCommitPattern
	ruleset.CommitPattern = ""
	if ruleset.HasCommitPattern() {
		t.Error("expected HasCommitPattern to return false for empty pattern")
	}
	ruleset.CommitPattern = "{ticket}: {message}"
	if !ruleset.HasCommitPattern() {
		t.Error("expected HasCommitPattern to return true")
	}

	// Test IsForbidden
	ruleset.ForbiddenFiles = []string{"utils.go", "helpers.go"}
	if !ruleset.IsForbidden("utils.go") {
		t.Error("expected utils.go to be forbidden")
	}
	if !ruleset.IsForbidden("helpers.go") {
		t.Error("expected helpers.go to be forbidden")
	}
	if ruleset.IsForbidden("main.go") {
		t.Error("expected main.go to not be forbidden")
	}
}

func TestParseConventionsWithFixtures(t *testing.T) {
	// Get the test fixtures directory
	fixturesDir := getFixturesDir(t)

	ruleset, err := ParseConventions(fixturesDir)
	if err != nil {
		t.Fatalf("ParseConventions failed: %v", err)
	}

	// Verify project path
	if ruleset.ProjectPath != fixturesDir {
		t.Errorf("expected ProjectPath %s, got %s", fixturesDir, ruleset.ProjectPath)
	}

	// Verify branch pattern (CLAUDE.md takes priority)
	expected := "feature/KAI-{ticket}-{summary}"
	if ruleset.BranchPattern != expected {
		t.Errorf("expected BranchPattern %s, got %s", expected, ruleset.BranchPattern)
	}

	// Verify commit pattern (CLAUDE.md takes priority)
	expected = "{ticket}_{description}"
	if ruleset.CommitPattern != expected {
		t.Errorf("expected CommitPattern %s, got %s", expected, ruleset.CommitPattern)
	}

	// Verify TDD required
	if !ruleset.TDDRequired {
		t.Error("expected TDDRequired to be true")
	}

	// Verify commands (CLAUDE.md sets these)
	if ruleset.TestCommand != "make test" {
		t.Errorf("expected TestCommand 'make test', got %s", ruleset.TestCommand)
	}

	if ruleset.LintCommand != "make lint" {
		t.Errorf("expected LintCommand 'make lint', got %s", ruleset.LintCommand)
	}

	if ruleset.FormatCommand != "gofmt -w ." {
		t.Errorf("expected FormatCommand 'gofmt -w .', got %s", ruleset.FormatCommand)
	}

	// Verify forbidden files
	expectedFiles := []string{"utils.go", "helpers.go", "common.go", "misc.go"}
	if len(ruleset.ForbiddenFiles) != len(expectedFiles) {
		t.Errorf("expected %d forbidden files, got %d", len(expectedFiles), len(ruleset.ForbiddenFiles))
	}
	for _, file := range expectedFiles {
		if !ruleset.IsForbidden(file) {
			t.Errorf("expected %s to be forbidden", file)
		}
	}
}

func TestParseConventionsNoFiles(t *testing.T) {
	// Create a temporary empty directory
	tempDir := t.TempDir()

	ruleset, err := ParseConventions(tempDir)
	if err != nil {
		t.Fatalf("ParseConventions should not fail on missing files: %v", err)
	}

	// Should return defaults
	if ruleset.ProjectPath != tempDir {
		t.Errorf("expected ProjectPath %s, got %s", tempDir, ruleset.ProjectPath)
	}

	if ruleset.BranchPattern != "" {
		t.Errorf("expected empty BranchPattern, got %s", ruleset.BranchPattern)
	}

	if ruleset.CommitPattern != "" {
		t.Errorf("expected empty CommitPattern, got %s", ruleset.CommitPattern)
	}

	if ruleset.TestCommand != "go test ./..." {
		t.Errorf("expected default TestCommand, got %s", ruleset.TestCommand)
	}

	if len(ruleset.ForbiddenFiles) != 0 {
		t.Errorf("expected no forbidden files, got %d", len(ruleset.ForbiddenFiles))
	}
}

func TestParseConventionsEmptyPath(t *testing.T) {
	_, err := ParseConventions("")
	if err == nil {
		t.Error("expected error for empty project path")
	}
}

func TestParseConventionsOnlyConventionsMd(t *testing.T) {
	tempDir := t.TempDir()

	// Create only CONVENTIONS.md
	conventionsContent := `# Conventions

Branch pattern: feat/{ticket}
Commit pattern: [{ticket}] {message}
Test command: go test -v ./...
`
	conventionsPath := filepath.Join(tempDir, "CONVENTIONS.md")
	if err := os.WriteFile(conventionsPath, []byte(conventionsContent), 0644); err != nil {
		t.Fatalf("failed to write CONVENTIONS.md: %v", err)
	}

	ruleset, err := ParseConventions(tempDir)
	if err != nil {
		t.Fatalf("ParseConventions failed: %v", err)
	}

	if ruleset.BranchPattern != "feat/{ticket}" {
		t.Errorf("expected BranchPattern 'feat/{ticket}', got %s", ruleset.BranchPattern)
	}

	if ruleset.CommitPattern != "[{ticket}] {message}" {
		t.Errorf("expected CommitPattern '[{ticket}] {message}', got %s", ruleset.CommitPattern)
	}

	if ruleset.TestCommand != "go test -v ./..." {
		t.Errorf("expected TestCommand 'go test -v ./...', got %s", ruleset.TestCommand)
	}
}

func TestParseMakefileDetection(t *testing.T) {
	tempDir := t.TempDir()

	// Create a Makefile with test and lint targets
	makefileContent := `.PHONY: test lint

test:
	go test ./...

lint:
	golangci-lint run
`
	makefilePath := filepath.Join(tempDir, "Makefile")
	if err := os.WriteFile(makefilePath, []byte(makefileContent), 0644); err != nil {
		t.Fatalf("failed to write Makefile: %v", err)
	}

	ruleset, err := ParseConventions(tempDir)
	if err != nil {
		t.Fatalf("ParseConventions failed: %v", err)
	}

	// Should detect make commands from Makefile
	if ruleset.TestCommand != "make test" {
		t.Errorf("expected TestCommand 'make test', got %s", ruleset.TestCommand)
	}

	if ruleset.LintCommand != "make lint" {
		t.Errorf("expected LintCommand 'make lint', got %s", ruleset.LintCommand)
	}
}

func TestExtractPattern(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		pattern  string
		expected string
	}{
		{
			name:     "simple branch pattern",
			text:     "Branch pattern: feature/{ticket}",
			pattern:  `branch.*pattern[:\s]+([^\n]+)`,
			expected: "feature/{ticket}",
		},
		{
			name:     "commit format",
			text:     "Commit format: {ticket}_{desc}",
			pattern:  `commit.*(?:format|pattern)[:\s]+([^\n]+)`,
			expected: "{ticket}_{desc}",
		},
		{
			name:     "with backticks",
			text:     "Branch pattern: `feat/{ticket}`",
			pattern:  `branch.*pattern[:\s]+([^\n]+)`,
			expected: "`feat/{ticket}`",
		},
		{
			name:     "no match",
			text:     "Some other text",
			pattern:  `branch.*pattern[:\s]+([^\n]+)`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPattern(tt.text, tt.pattern)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestExtractForbiddenFiles(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected []string
	}{
		{
			name:     "inline list",
			text:     "FORBIDDEN filenames: utils.go, helpers.go, common.go",
			expected: []string{"utils.go", "helpers.go", "common.go"},
		},
		{
			name:     "with extra whitespace",
			text:     "Forbidden files:   utils.go   helpers.go",
			expected: []string{"utils.go", "helpers.go"},
		},
		{
			name:     "no forbidden files",
			text:     "Some text without forbidden files",
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractForbiddenFiles(tt.text)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d files, got %d", len(tt.expected), len(result))
			}
			for i, file := range tt.expected {
				if i >= len(result) || result[i] != file {
					t.Errorf("expected file %s at index %d, got %v", file, i, result)
				}
			}
		})
	}
}

func TestCleanPattern(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"`feature/{ticket}`", "feature/{ticket}"},
		{"\"branch/name\"", "branch/name"},
		{"  spaced  ", "spaced"},
		{"`backticks and quotes\"`", "backticks and quotes"},
		{"normal", "normal"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := cleanPattern(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	tempDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tempDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Test existing file
	if !fileExists(testFile) {
		t.Error("expected fileExists to return true for existing file")
	}

	// Test non-existing file
	if fileExists(filepath.Join(tempDir, "nonexistent.txt")) {
		t.Error("expected fileExists to return false for non-existing file")
	}

	// Test directory
	if fileExists(tempDir) {
		t.Error("expected fileExists to return false for directory")
	}
}

// getFixturesDir returns the path to the test fixtures directory.
func getFixturesDir(t *testing.T) string {
	t.Helper()

	// Get current working directory
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	// Navigate up to find the project root, then to test/fixtures/conventions
	// This works whether we're running from the package dir or project root
	for {
		fixturesPath := filepath.Join(wd, "test", "fixtures", "conventions")
		if info, err := os.Stat(fixturesPath); err == nil && info.IsDir() {
			return fixturesPath
		}

		// Move up one directory
		parent := filepath.Dir(wd)
		if parent == wd {
			// Reached filesystem root
			break
		}
		wd = parent
	}

	t.Fatal("could not find test fixtures directory")
	return ""
}
