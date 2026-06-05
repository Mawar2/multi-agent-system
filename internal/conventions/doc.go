// Package conventions provides project-specific convention parsing and validation.
//
// This package reads convention files (CLAUDE.md, CONVENTIONS.md) from project
// directories and extracts rules about:
//   - Branch naming patterns
//   - Commit message formats
//   - Forbidden file names
//   - Test/lint/format commands
//   - TDD requirements
//   - Custom project-specific rules
//
// # Usage
//
// To parse conventions from a project directory:
//
//	ruleset, err := conventions.ParseConventions("/path/to/project")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Check if a branch name follows the pattern
//	if ruleset.HasBranchPattern() {
//	    // Validate branch name against ruleset.BranchPattern
//	}
//
//	// Check if a filename is forbidden
//	if ruleset.IsForbidden("utils.go") {
//	    fmt.Println("This filename should not be created")
//	}
//
// # Convention File Formats
//
// The parser looks for CLAUDE.md and CONVENTIONS.md in the project root.
// CLAUDE.md takes priority if both exist.
//
// Example CLAUDE.md format:
//
//	## Branch Naming
//	Branch pattern: `feature/{ticket}-{summary}`
//
//	## Commit Format
//	Commit format: `{ticket}_{description}`
//
//	## Testing
//	TDD required: true
//	Test command: `make test`
//
//	## Linting
//	Lint command: `make lint`
//
//	## Forbidden Files
//	FORBIDDEN filenames: utils.go, helpers.go
//
// The parser is lenient - if files don't exist or parsing fails, it returns
// a Ruleset with sensible defaults rather than failing.
//
// # Makefile Detection
//
// If test/lint commands are not specified in convention files, the parser
// will check for a Makefile and detect common targets (test, lint, fmt).
package conventions
