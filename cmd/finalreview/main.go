// Package main is the entry point for the Final Review agent.
//
// Final Review is the last automated step in Zone 2 before a human submits a
// proposal to SAM.gov. It reads an approved draft file and its associated
// Opportunity from the store, runs a set of readiness checks, and prints an
// AgentResult indicating whether the proposal is ready for human submission.
//
// The agent NEVER submits anything. Submission is always a human action
// performed after reviewing the output of this agent.
//
// Configuration (flags or environment variables):
//   - DRAFT_PATH / --draft-path: Path to the approved draft file (required)
//   - OPPORTUNITY_ID / --opportunity-id: SAM.gov notice ID (required)
//   - STORE_TYPE / --store-type: Store implementation type, "json" only in Phase 0 (default: "json")
//   - STORE_PATH / --store-path: Path to store directory (default: "./queue")
//
// Example usage:
//
//	go run cmd/finalreview/main.go \
//	  --draft-path=./proposals/ABC-123-draft.txt \
//	  --opportunity-id=ABC-123-2026
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Mawar2/Kaimi/internal/finalreview"
	"github.com/Mawar2/Kaimi/internal/store"
)

// Config holds the Final Review agent configuration.
type Config struct {
	DraftPath     string // Path to the approved draft file
	OpportunityID string // SAM.gov notice ID to review
	StoreType     string // Store implementation type ("json")
	StorePath     string // Path to store directory
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Final Review error: %v\n", err)
		os.Exit(1)
	}
}

// run contains the main logic for the Final Review agent.
func run() error {
	config := parseConfig()

	if err := validateConfig(&config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	fmt.Println("Final Review agent starting...")
	fmt.Printf("Draft path:     %s\n", config.DraftPath)
	fmt.Printf("Opportunity ID: %s\n", config.OpportunityID)
	fmt.Printf("Store path:     %s\n", config.StorePath)

	// Read the approved draft from disk.
	draftBytes, err := os.ReadFile(config.DraftPath)
	if err != nil {
		return fmt.Errorf("failed to read draft file %q: %w", config.DraftPath, err)
	}
	draft := string(draftBytes)

	// Initialise the opportunity store and retrieve the target opportunity.
	var opportunityStore store.Store
	switch config.StoreType {
	case "json":
		opportunityStore, err = store.NewJSONStore(config.StorePath)
		if err != nil {
			return fmt.Errorf("failed to create JSON store: %w", err)
		}
	default:
		return fmt.Errorf("unsupported store type: %s", config.StoreType)
	}

	ctx := context.Background()
	opp, err := opportunityStore.Get(ctx, config.OpportunityID)
	if err != nil {
		return fmt.Errorf("failed to retrieve opportunity %q: %w", config.OpportunityID, err)
	}

	// Run the final review.
	fmt.Println("Running final review checks...")
	result, err := finalreview.Review(ctx, draft, opp)
	if err != nil {
		return fmt.Errorf("final review failed: %w", err)
	}

	// Print human-readable summary.
	fmt.Println("\n--- Final Review Summary ---")
	fmt.Printf("Status:   %s\n", result.Status)
	fmt.Printf("Summary:  %s\n", result.Summary)
	if result.Error != "" {
		fmt.Printf("Issues:   %s\n", result.Error)
	}
	for key, val := range result.Flags {
		fmt.Printf("  %-28s %s\n", key+":", val)
	}

	// Emit the full AgentResult as JSON for programmatic consumption by the Manager.
	resultJSON, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialise result: %w", err)
	}
	fmt.Printf("\nAgent Result (JSON):\n%s\n", resultJSON)

	return nil
}

// parseConfig reads configuration from environment variables and command-line flags.
// Flags take precedence over environment variables via the flag default mechanism.
func parseConfig() Config {
	draftPath := flag.String("draft-path", getEnv("DRAFT_PATH", ""), "Path to the approved draft file")
	oppID := flag.String("opportunity-id", getEnv("OPPORTUNITY_ID", ""), "SAM.gov notice ID")
	storeType := flag.String("store-type", getEnv("STORE_TYPE", "json"), "Store type: json")
	storePath := flag.String("store-path", getEnv("STORE_PATH", "./queue"), "Store directory path")

	flag.Parse()

	return Config{
		DraftPath:     *draftPath,
		OpportunityID: *oppID,
		StoreType:     *storeType,
		StorePath:     *storePath,
	}
}

// validateConfig validates the Final Review agent configuration.
func validateConfig(config *Config) error {
	if config.DraftPath == "" {
		return fmt.Errorf("draft path is required (set DRAFT_PATH env var or --draft-path flag)")
	}
	if config.OpportunityID == "" {
		return fmt.Errorf("opportunity ID is required (set OPPORTUNITY_ID env var or --opportunity-id flag)")
	}
	if config.StoreType != "json" {
		return fmt.Errorf("unsupported store type: %s (only 'json' is supported in Phase 0)", config.StoreType)
	}
	return nil
}

// getEnv returns the value of an environment variable or a default value.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
