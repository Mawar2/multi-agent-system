package finalreview

import (
	"context"
	"testing"
	"time"

	"github.com/Mawar2/Kaimi/internal/agent"
	"github.com/Mawar2/Kaimi/internal/opportunity"
)

// makeTestOpportunity returns a minimal valid Opportunity for use in tests.
func makeTestOpportunity() *opportunity.Opportunity {
	deadline := time.Now().Add(30 * 24 * time.Hour)
	return &opportunity.Opportunity{
		ID:               "TEST-001-2026",
		Title:            "Cloud Infrastructure Modernization",
		Agency:           "Department of Testing",
		ResponseDeadline: deadline,
		NAICSCode:        "541512",
	}
}

// TestReview_ReadyToSubmit verifies the happy path: all checks pass and the
// result carries StatusReadyToSubmit.
func TestReview_ReadyToSubmit(t *testing.T) {
	ctx := context.Background()
	opp := makeTestOpportunity()
	draft := "This is the approved proposal draft content. It contains enough text to pass the draft check."

	result, err := Review(ctx, draft, opp)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Review returned nil result")
	}

	if result.Status != agent.StatusReadyToSubmit {
		t.Errorf("expected status %q, got %q (summary: %s)", agent.StatusReadyToSubmit, result.Status, result.Summary)
	}
	if result.AgentName != "final-review" {
		t.Errorf("expected agent name %q, got %q", "final-review", result.AgentName)
	}
	if result.NoticeID != opp.ID {
		t.Errorf("expected notice ID %q, got %q", opp.ID, result.NoticeID)
	}
	if result.Summary == "" {
		t.Error("expected non-empty summary")
	}
	if result.CompletedAt.IsZero() {
		t.Error("expected CompletedAt to be set")
	}
}

// TestReview_EmptyDraft verifies that an empty draft produces StatusNeedsHuman.
func TestReview_EmptyDraft(t *testing.T) {
	ctx := context.Background()
	opp := makeTestOpportunity()

	result, err := Review(ctx, "", opp)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}

	if result.Status == agent.StatusReadyToSubmit {
		t.Error("expected non-ready status for empty draft")
	}
	if result.Status != agent.StatusNeedsHuman {
		t.Errorf("expected status %q, got %q", agent.StatusNeedsHuman, result.Status)
	}
}

// TestReview_MissingTitle verifies that an opportunity without a title produces StatusNeedsHuman.
func TestReview_MissingTitle(t *testing.T) {
	ctx := context.Background()
	opp := makeTestOpportunity()
	opp.Title = "" // remove title — check should fail
	draft := "Approved proposal draft content."

	result, err := Review(ctx, draft, opp)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}
	if result.Status == agent.StatusReadyToSubmit {
		t.Error("expected non-ready status when opportunity title is missing")
	}
}

// TestReview_MissingDeadline verifies that an opportunity without a response deadline
// produces StatusNeedsHuman.
func TestReview_MissingDeadline(t *testing.T) {
	ctx := context.Background()
	opp := makeTestOpportunity()
	opp.ResponseDeadline = time.Time{} // zero value = no deadline
	draft := "Approved proposal draft content."

	result, err := Review(ctx, draft, opp)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}
	if result.Status == agent.StatusReadyToSubmit {
		t.Error("expected non-ready status when response deadline is missing")
	}
}

// TestReview_NilOpportunity verifies that passing a nil Opportunity returns an error.
func TestReview_NilOpportunity(t *testing.T) {
	ctx := context.Background()

	_, err := Review(ctx, "some draft", nil)
	if err == nil {
		t.Error("expected error for nil opportunity, got none")
	}
}

// TestReview_FlagsPopulated verifies that AgentResult.Flags contains check outcomes
// and the aggregate checks_passed summary.
func TestReview_FlagsPopulated(t *testing.T) {
	ctx := context.Background()
	opp := makeTestOpportunity()
	draft := "Approved proposal draft content."

	result, err := Review(ctx, draft, opp)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}
	if len(result.Flags) == 0 {
		t.Error("expected non-empty Flags map")
	}

	val, ok := result.Flags["checks_passed"]
	if !ok {
		t.Error("expected 'checks_passed' key in Flags")
	}
	if val == "" {
		t.Error("expected non-empty 'checks_passed' value")
	}
}

// TestReview_StatusReadyToSubmitIsSuccess verifies that the AgentResult helper
// methods treat StatusReadyToSubmit as a success (per the contract in
// internal/agent/result.go).
func TestReview_StatusReadyToSubmitIsSuccess(t *testing.T) {
	ctx := context.Background()
	opp := makeTestOpportunity()
	draft := "Approved proposal draft content."

	result, err := Review(ctx, draft, opp)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}

	if !result.IsSuccess() {
		t.Errorf("expected IsSuccess()=true for StatusReadyToSubmit, got false (status: %q)", result.Status)
	}
	if result.IsFailed() {
		t.Error("expected IsFailed()=false for StatusReadyToSubmit")
	}
}

// TestReview_PartialFailureNotReadyToSubmit verifies that a single failing check
// prevents the ready_to_submit outcome.
func TestReview_PartialFailureNotReadyToSubmit(t *testing.T) {
	ctx := context.Background()
	opp := makeTestOpportunity()
	opp.ResponseDeadline = time.Time{} // fail exactly one check

	result, err := Review(ctx, "Good draft content here.", opp)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}
	if result.Status == agent.StatusReadyToSubmit {
		t.Error("expected non-ready status when at least one check fails")
	}
}

// TestReview_NeverSubmits verifies that Review does not mutate the Opportunity
// and that no submission side-effects occur — the agent is read-only.
func TestReview_NeverSubmits(t *testing.T) {
	ctx := context.Background()
	opp := makeTestOpportunity()
	originalStatus := opp.ProposalStatus
	draft := "Approved proposal draft content."

	_, err := Review(ctx, draft, opp)
	if err != nil {
		t.Fatalf("Review returned unexpected error: %v", err)
	}

	// The agent must not modify the Opportunity — submission is a human action.
	if opp.ProposalStatus != originalStatus {
		t.Errorf("Review must not modify Opportunity.ProposalStatus: before=%q after=%q",
			originalStatus, opp.ProposalStatus)
	}
}
