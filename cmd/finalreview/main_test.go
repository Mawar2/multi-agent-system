package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Mawar2/Kaimi/internal/finalreview"
	"github.com/Mawar2/Kaimi/internal/opportunity"
	"github.com/Mawar2/Kaimi/internal/store"
)

// TestValidateConfig verifies all configuration validation rules.
func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		shouldError bool
	}{
		{
			name: "valid config",
			config: Config{
				DraftPath:     "./draft.txt",
				OpportunityID: "ABC-123",
				StoreType:     "json",
				StorePath:     "./queue",
			},
			shouldError: false,
		},
		{
			name: "missing draft path",
			config: Config{
				DraftPath:     "",
				OpportunityID: "ABC-123",
				StoreType:     "json",
				StorePath:     "./queue",
			},
			shouldError: true,
		},
		{
			name: "missing opportunity ID",
			config: Config{
				DraftPath:     "./draft.txt",
				OpportunityID: "",
				StoreType:     "json",
				StorePath:     "./queue",
			},
			shouldError: true,
		},
		{
			name: "unsupported store type",
			config: Config{
				DraftPath:     "./draft.txt",
				OpportunityID: "ABC-123",
				StoreType:     "firestore",
				StorePath:     "./queue",
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.config)
			if tt.shouldError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestGetEnv verifies environment variable reading with fallback defaults.
func TestGetEnv(t *testing.T) {
	const testKey = "TEST_FINALREVIEW_VAR"
	const testVal = "test-value"

	if err := os.Setenv(testKey, testVal); err != nil {
		t.Fatalf("failed to set env var: %v", err)
	}
	defer func() {
		if err := os.Unsetenv(testKey); err != nil {
			t.Errorf("failed to unset env var: %v", err)
		}
	}()

	tests := []struct {
		name     string
		key      string
		def      string
		expected string
	}{
		{
			name:     "existing variable",
			key:      testKey,
			def:      "default",
			expected: testVal,
		},
		{
			name:     "non-existent variable uses default",
			key:      "NONEXISTENT_FINALREVIEW_VAR",
			def:      "fallback",
			expected: "fallback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getEnv(tt.key, tt.def)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

// TestFinalReviewIntegration is an end-to-end integration test for the Final Review agent.
//
// It exercises the full flow without any live calls:
//  1. Save a test Opportunity to a temporary JSON store
//  2. Write a draft file to a temporary directory
//  3. Run finalreview.Review using those fixtures
//  4. Verify the AgentResult is StatusReadyToSubmit
func TestFinalReviewIntegration(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	// Prepare a test opportunity and save it to the temp store.
	deadline := time.Now().Add(30 * 24 * time.Hour)
	opp := &opportunity.Opportunity{
		ID:               "INT-001-2026",
		Title:            "Integration Test Opportunity",
		Agency:           "Department of Integration",
		ResponseDeadline: deadline,
		NAICSCode:        "541512",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	opportunityStore, err := store.NewJSONStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create JSON store: %v", err)
	}

	if err := opportunityStore.Save(ctx, opp); err != nil {
		t.Fatalf("failed to save opportunity: %v", err)
	}

	// Write a draft file.
	draftContent := "Executive Summary\n\nBlueMeta Technologies proposes a comprehensive cloud infrastructure solution.\n"
	draftPath := filepath.Join(tempDir, "draft.txt")
	if err := os.WriteFile(draftPath, []byte(draftContent), 0o600); err != nil {
		t.Fatalf("failed to write draft file: %v", err)
	}

	// Reload the opportunity from the store (mirrors what the CLI does).
	loaded, err := opportunityStore.Get(ctx, opp.ID)
	if err != nil {
		t.Fatalf("failed to retrieve opportunity: %v", err)
	}

	// Run the final review using the same function the CLI calls.
	draft, err := os.ReadFile(draftPath)
	if err != nil {
		t.Fatalf("failed to read draft: %v", err)
	}

	result, err := finalreview.Review(ctx, string(draft), loaded)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("Review returned nil result")
	}

	// Verify the happy path outcome.
	if result.AgentName != "final-review" {
		t.Errorf("expected agent name %q, got %q", "final-review", result.AgentName)
	}
	if result.NoticeID != opp.ID {
		t.Errorf("expected notice ID %q, got %q", opp.ID, result.NoticeID)
	}
	if result.CompletedAt.IsZero() {
		t.Error("expected CompletedAt to be set")
	}

	// All checks should pass → ready_to_submit.
	if !result.IsSuccess() {
		t.Errorf("expected successful result, got status %q (error: %s)", result.Status, result.Error)
	}

	t.Logf("Integration test complete: status=%s summary=%s", result.Status, result.Summary)
}

// TestFinalReviewIntegration_EmptyDraft verifies behavior when the draft file is empty.
func TestFinalReviewIntegration_EmptyDraft(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	deadline := time.Now().Add(30 * 24 * time.Hour)
	opp := &opportunity.Opportunity{
		ID:               "EMPTY-DRAFT-001",
		Title:            "Test Opportunity",
		Agency:           "Test Agency",
		ResponseDeadline: deadline,
		NAICSCode:        "541512",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	opportunityStore, err := store.NewJSONStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create JSON store: %v", err)
	}
	if err := opportunityStore.Save(ctx, opp); err != nil {
		t.Fatalf("failed to save opportunity: %v", err)
	}

	loaded, err := opportunityStore.Get(ctx, opp.ID)
	if err != nil {
		t.Fatalf("failed to retrieve opportunity: %v", err)
	}

	// Empty draft — review should not produce ready_to_submit.
	result, err := finalreview.Review(ctx, "", loaded)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}
	if result.IsSuccess() {
		t.Errorf("expected non-success for empty draft, got status %q", result.Status)
	}
}
