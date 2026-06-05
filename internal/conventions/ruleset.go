// Package conventions provides project-specific convention parsing and enforcement.
//
// This package reads convention files (CLAUDE.md, CONVENTIONS.md) from project
// directories and extracts rules about branch naming, commit formats, testing
// requirements, and other project-specific standards.
package conventions

// Ruleset represents project-specific conventions and rules extracted from
// convention files (CLAUDE.md, CONVENTIONS.md) in a project directory.
type Ruleset struct {
	// ProjectPath is the absolute path to the project root directory.
	ProjectPath string

	// BranchPattern defines the required branch naming pattern.
	// Example: "feature/KAI-{ticket}-{summary}"
	// Empty string means no specific pattern required.
	BranchPattern string

	// CommitPattern defines the required commit message format.
	// Example: "{ticket}_{description}"
	// Empty string means no specific pattern required.
	CommitPattern string

	// ForbiddenFiles lists file names that should never be created.
	// Example: ["utils.go", "helpers.go", "common.go"]
	ForbiddenFiles []string

	// TestCommand is the command to run tests.
	// Example: "make test" or "go test ./..."
	// Empty string means no test command specified.
	TestCommand string

	// LintCommand is the command to run the linter.
	// Example: "make lint" or "golangci-lint run"
	// Empty string means no lint command specified.
	LintCommand string

	// FormatCommand is the command to run the code formatter.
	// Example: "gofmt -w ." or "make fmt"
	// Empty string means no format command specified.
	FormatCommand string

	// BuildCommand is the command to build the project (optional).
	// Example: "make build" or "go build ./..."
	// Empty string means no build command (build check will be skipped).
	BuildCommand string

	// TDDRequired indicates whether test-driven development is required.
	// When true, tests must be written before implementation code.
	TDDRequired bool

	// CustomRules holds additional project-specific rules as key-value pairs.
	// Keys are rule names, values are rule descriptions or values.
	// Example: {"max_line_length": "120", "require_godoc": "true"}
	CustomRules map[string]string
}

// NewRuleset creates a new Ruleset with default values.
// The returned ruleset has empty patterns and commands, which means
// no specific conventions are enforced until populated by a parser.
func NewRuleset(projectPath string) *Ruleset {
	return &Ruleset{
		ProjectPath:    projectPath,
		BranchPattern:  "",
		CommitPattern:  "",
		ForbiddenFiles: []string{},
		TestCommand:    "go test ./...",
		LintCommand:    "golangci-lint run",
		FormatCommand:  "gofmt -w .",
		BuildCommand:   "", // Optional - not all projects need explicit build
		TDDRequired:    false,
		CustomRules:    make(map[string]string),
	}
}

// HasBranchPattern returns true if a branch naming pattern is defined.
func (r *Ruleset) HasBranchPattern() bool {
	return r.BranchPattern != ""
}

// HasCommitPattern returns true if a commit message pattern is defined.
func (r *Ruleset) HasCommitPattern() bool {
	return r.CommitPattern != ""
}

// IsForbidden checks if a given filename is in the forbidden list.
func (r *Ruleset) IsForbidden(filename string) bool {
	for _, forbidden := range r.ForbiddenFiles {
		if filename == forbidden {
			return true
		}
	}
	return false
}
