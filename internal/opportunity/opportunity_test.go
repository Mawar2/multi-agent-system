package opportunity

import (
	"encoding/json"
	"testing"
	"time"
)

// TestOpportunity_JSONRoundTrip verifies that the Opportunity struct can be
// marshaled to JSON and back without data loss.
//
// This test ensures the schema is JSON-serializable, which is required for
// storing opportunities in the queue (currently JSON files, later Firestore).
func TestOpportunity_JSONRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second) // Truncate to avoid subsecond precision issues

	original := Opportunity{
		ID:                 "TEST-123",
		Title:              "Test Opportunity",
		SolicitationNum:    "SOL-2026-001",
		Agency:             "Department of Test",
		Office:             "Office of Testing",
		PostedDate:         now,
		ResponseDeadline:   now.Add(30 * 24 * time.Hour),
		NAICSCode:          "541512",
		NAICSDescription:   "Computer Systems Design Services",
		SetAsideCode:       "SBA",
		PlaceOfPerformance: "Washington, DC",
		Description:        "Test opportunity description",
		Type:               "Solicitation",
		ContractType:       "Firm Fixed Price",
		URL:                "https://sam.gov/test/123",
		Attachments:        []string{"https://sam.gov/test/123/rfp.pdf"},
		Score:              0.85,
		ScoreReasoning:     "Strong technical fit",
		Selected:           false,
		ProposalStatus:     "",
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	// Marshal to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal Opportunity: %v", err)
	}

	// Unmarshal back to struct
	var decoded Opportunity
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Opportunity: %v", err)
	}

	// Verify critical fields match
	if decoded.ID != original.ID {
		t.Errorf("ID mismatch: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Title != original.Title {
		t.Errorf("Title mismatch: got %q, want %q", decoded.Title, original.Title)
	}
	if !decoded.PostedDate.Equal(original.PostedDate) {
		t.Errorf("PostedDate mismatch: got %v, want %v", decoded.PostedDate, original.PostedDate)
	}
	if !decoded.ResponseDeadline.Equal(original.ResponseDeadline) {
		t.Errorf("ResponseDeadline mismatch: got %v, want %v", decoded.ResponseDeadline, original.ResponseDeadline)
	}
	if decoded.Score != original.Score {
		t.Errorf("Score mismatch: got %f, want %f", decoded.Score, original.Score)
	}
}

// TestOpportunity_EmptyInitialization verifies that an Opportunity can be
// created with zero values and that optional fields handle nil properly.
func TestOpportunity_EmptyInitialization(t *testing.T) {
	var opp Opportunity

	// Verify zero values are safe
	if opp.ID != "" {
		t.Errorf("Expected empty ID, got %q", opp.ID)
	}
	if opp.Score != 0.0 {
		t.Errorf("Expected zero Score, got %f", opp.Score)
	}
	if opp.Selected {
		t.Error("Expected Selected to be false")
	}

	// Verify pointer fields are nil
	if opp.ScoredAt != nil {
		t.Error("Expected ScoredAt to be nil")
	}
	if opp.SelectedAt != nil {
		t.Error("Expected SelectedAt to be nil")
	}
}
