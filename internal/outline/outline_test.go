package outline

import (
	"context"
	"testing"

	"github.com/Mawar2/Kaimi/internal/agent"
	"github.com/Mawar2/Kaimi/internal/opportunity"
)

// makeTestOpportunity returns a minimal valid Opportunity for use in tests.
func makeTestOpportunity() *opportunity.Opportunity {
	return &opportunity.Opportunity{
		ID:    "TEST-2026-001",
		Title: "Cloud Infrastructure Modernization — USAF",
	}
}

// TestOutline_NilOpportunity verifies that Outline returns an error for nil input.
func TestOutline_NilOpportunity(t *testing.T) {
	_, err := Outline(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil opportunity, got nil")
	}
}

// TestOutline_MissingTitle verifies that an opportunity without a title produces
// StatusFailed in the AgentResult (not a Go error).
func TestOutline_MissingTitle(t *testing.T) {
	opp := makeTestOpportunity()
	opp.Title = ""

	result, err := Outline(context.Background(), opp)
	if err != nil {
		t.Fatalf("Outline returned unexpected error: %v", err)
	}
	if result.Status != agent.StatusFailed {
		t.Errorf("expected status %q for missing title, got %q", agent.StatusFailed, result.Status)
	}
	if result.Error == "" {
		t.Error("Error field must be non-empty on StatusFailed")
	}
}

// TestOutline_FormattingRulesPresent verifies that a solicitation stating all four
// formatting fields (font, margins, page limit, artifacts) produces a "complete"
// formatting flag and a successful result.
func TestOutline_FormattingRulesPresent(t *testing.T) {
	opp := makeTestOpportunity()
	opp.Description = `
Proposals shall be typed in Times New Roman, 12pt.
Margins: 1 inch on all sides.
Page limit: 25 pages.
Required documents: Past Performance Volume, Technical Approach.
`
	result, err := Outline(context.Background(), opp)
	if err != nil {
		t.Fatalf("Outline returned unexpected error: %v", err)
	}
	if result.Status != agent.StatusSuccess {
		t.Errorf("expected status %q, got %q (error: %s)", agent.StatusSuccess, result.Status, result.Error)
	}
	if result.Flags["formatting"] != "complete" {
		t.Errorf("expected formatting flag %q, got %q", "complete", result.Flags["formatting"])
	}
	if result.Flags["font"] == "" {
		t.Error("font flag should be populated when solicitation states font")
	}
	if result.Flags["margins"] == "" {
		t.Error("margins flag should be populated when solicitation states margins")
	}
	if result.Flags["page_limit"] == "" {
		t.Error("page_limit flag should be populated when solicitation states page limit")
	}
	if result.Flags["artifacts"] == "" {
		t.Error("artifacts flag should be populated when solicitation states required documents")
	}
	if _, ok := result.Flags["not_specified"]; ok {
		t.Errorf("not_specified flag should be absent when all rules are present, got %q", result.Flags["not_specified"])
	}
}

// TestOutline_FormattingRulesAbsent verifies that a solicitation with no formatting
// guidance produces an "absent" flag and still returns StatusSuccess — missing
// rules are reported, not treated as a failure.
func TestOutline_FormattingRulesAbsent(t *testing.T) {
	opp := makeTestOpportunity()
	opp.Description = "This solicitation provides no formatting guidance whatsoever."

	result, err := Outline(context.Background(), opp)
	if err != nil {
		t.Fatalf("Outline returned unexpected error: %v", err)
	}
	if result.Status != agent.StatusSuccess {
		t.Errorf("expected status %q even when formatting rules absent, got %q", agent.StatusSuccess, result.Status)
	}
	if result.Flags["formatting"] != "absent" {
		t.Errorf("expected formatting flag %q, got %q", "absent", result.Flags["formatting"])
	}
	if result.Flags["not_specified"] == "" {
		t.Error("not_specified flag should list all missing rules when solicitation has none")
	}
}

// TestOutline_FormattingRulesPartial verifies that a solicitation stating only some
// formatting fields produces a "partial" flag and correctly separates found rules
// from missing ones.
func TestOutline_FormattingRulesPartial(t *testing.T) {
	opp := makeTestOpportunity()
	opp.Description = `
Font: Arial, 11pt.
Not to exceed 30 pages.
No margins or attachments stated here.
`
	result, err := Outline(context.Background(), opp)
	if err != nil {
		t.Fatalf("Outline returned unexpected error: %v", err)
	}
	if result.Status != agent.StatusSuccess {
		t.Errorf("expected status %q for partial rules, got %q", agent.StatusSuccess, result.Status)
	}
	if result.Flags["formatting"] != "partial" {
		t.Errorf("expected formatting flag %q, got %q", "partial", result.Flags["formatting"])
	}
	if result.Flags["font"] == "" {
		t.Error("font flag should be populated when solicitation states font")
	}
	if result.Flags["page_limit"] == "" {
		t.Error("page_limit flag should be populated when solicitation states page limit")
	}
	if result.Flags["not_specified"] == "" {
		t.Error("not_specified flag should list missing rules for partial solicitation")
	}
}

// TestOutline_ResultShape verifies the AgentResult shape for a successful run:
// correct agent name, notice ID, non-empty summary, and populated OutputRef.
func TestOutline_ResultShape(t *testing.T) {
	opp := makeTestOpportunity()
	opp.Description = "No formatting rules stated."

	result, err := Outline(context.Background(), opp)
	if err != nil {
		t.Fatalf("Outline returned unexpected error: %v", err)
	}
	if result.AgentName != agentName {
		t.Errorf("AgentName = %q, want %q", result.AgentName, agentName)
	}
	if result.NoticeID != opp.ID {
		t.Errorf("NoticeID = %q, want %q", result.NoticeID, opp.ID)
	}
	if result.Summary == "" {
		t.Error("Summary must not be empty on success")
	}
	if result.OutputRef == "" {
		t.Error("OutputRef must not be empty on success — it carries the formatting rules JSON")
	}
	if result.CompletedAt.IsZero() {
		t.Error("CompletedAt must be set")
	}
}
