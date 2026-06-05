package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAgentResult_IsSuccess(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"success status", StatusSuccess, true},
		{"ready_to_submit status", StatusReadyToSubmit, true},
		{"failed status", StatusFailed, false},
		{"needs_human status", StatusNeedsHuman, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &AgentResult{Status: tt.status}
			if got := result.IsSuccess(); got != tt.want {
				t.Errorf("IsSuccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentResult_IsFailed(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"failed status", StatusFailed, true},
		{"success status", StatusSuccess, false},
		{"needs_human status", StatusNeedsHuman, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &AgentResult{Status: tt.status}
			if got := result.IsFailed(); got != tt.want {
				t.Errorf("IsFailed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentResult_NeedsHuman(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"needs_human status", StatusNeedsHuman, true},
		{"success status", StatusSuccess, false},
		{"failed status", StatusFailed, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &AgentResult{Status: tt.status}
			if got := result.NeedsHuman(); got != tt.want {
				t.Errorf("NeedsHuman() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentResult_SuccessfulResult(t *testing.T) {
	// Test creating a complete successful result (as Scorer would)
	result := &AgentResult{
		AgentName:   "scorer",
		Status:      StatusSuccess,
		NoticeID:    "ABC-123-2026",
		Summary:     "Scored 87/100 - Strong NAICS match",
		OutputRef:   "opportunities/ABC-123-2026.json",
		Flags:       map[string]string{"score": "87", "recommendation": "BID"},
		CompletedAt: time.Now(),
	}

	if result.AgentName != "scorer" {
		t.Errorf("AgentName = %v, want scorer", result.AgentName)
	}
	if result.Status != StatusSuccess {
		t.Errorf("Status = %v, want success", result.Status)
	}
	if !result.IsSuccess() {
		t.Error("IsSuccess() should be true for successful result")
	}
	if result.IsFailed() {
		t.Error("IsFailed() should be false for successful result")
	}
	if result.Flags["score"] != "87" {
		t.Errorf("Flags[score] = %v, want 87", result.Flags["score"])
	}
}

func TestAgentResult_FailedResult(t *testing.T) {
	// Test creating a failed result (as Hunter would on API error)
	result := &AgentResult{
		AgentName:   "hunter",
		Status:      StatusFailed,
		Error:       "SAM.gov API returned 429 (rate limit exceeded)",
		CompletedAt: time.Now(),
	}

	if result.Status != StatusFailed {
		t.Errorf("Status = %v, want failed", result.Status)
	}
	if !result.IsFailed() {
		t.Error("IsFailed() should be true for failed result")
	}
	if result.IsSuccess() {
		t.Error("IsSuccess() should be false for failed result")
	}
	if result.Error == "" {
		t.Error("Error should not be empty for failed result")
	}
}

func TestAgentResult_NeedsHumanResult(t *testing.T) {
	// Test creating a needs_human result (as Outline would when requirements are ambiguous)
	result := &AgentResult{
		AgentName:   "outline",
		Status:      StatusNeedsHuman,
		NoticeID:    "XYZ-456-2026",
		Summary:     "Solicitation has conflicting page limit requirements (15 pages in section A, 10 pages in appendix B)",
		Flags:       map[string]string{"ambiguity_type": "page_limits"},
		CompletedAt: time.Now(),
	}

	if result.Status != StatusNeedsHuman {
		t.Errorf("Status = %v, want needs_human", result.Status)
	}
	if !result.NeedsHuman() {
		t.Error("NeedsHuman() should be true")
	}
	if result.IsSuccess() {
		t.Error("IsSuccess() should be false for needs_human result")
	}
	if result.Summary == "" {
		t.Error("Summary should not be empty for needs_human result")
	}
}

func TestAgentResult_ReadyToSubmitResult(t *testing.T) {
	// Test creating a ready_to_submit result (as Final Review would)
	result := &AgentResult{
		AgentName:   "final-review",
		Status:      StatusReadyToSubmit,
		NoticeID:    "DEF-789-2026",
		Summary:     "All must-haves addressed, formatting correct, proposal ready for human approval and submission",
		OutputRef:   "proposals/DEF-789-2026/review-report.json",
		Flags:       map[string]string{"issues_found": "0", "must_haves_met": "12"},
		CompletedAt: time.Now(),
	}

	if result.Status != StatusReadyToSubmit {
		t.Errorf("Status = %v, want ready_to_submit", result.Status)
	}
	if !result.IsSuccess() {
		t.Error("IsSuccess() should be true for ready_to_submit result")
	}
	if result.IsFailed() {
		t.Error("IsFailed() should be false for ready_to_submit result")
	}
}

func TestAgentResult_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"success is terminal", StatusSuccess, true},
		{"failed is terminal", StatusFailed, true},
		{"ready_to_submit is terminal", StatusReadyToSubmit, true},
		{"needs_human is not terminal", StatusNeedsHuman, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &AgentResult{Status: tt.status}
			if got := result.IsTerminal(); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAgentResult_JSONRoundTrip(t *testing.T) {
	original := &AgentResult{
		AgentName:   "scorer",
		Status:      StatusSuccess,
		NoticeID:    "ABC-123-2026",
		Summary:     "Scored 87/100 - Strong NAICS match",
		OutputRef:   "opportunities/ABC-123-2026.json",
		Flags:       map[string]string{"score": "87", "recommendation": "BID"},
		CompletedAt: time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var decoded AgentResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if decoded.AgentName != original.AgentName {
		t.Errorf("AgentName = %v, want %v", decoded.AgentName, original.AgentName)
	}
	if decoded.Status != original.Status {
		t.Errorf("Status = %v, want %v", decoded.Status, original.Status)
	}
	if decoded.NoticeID != original.NoticeID {
		t.Errorf("NoticeID = %v, want %v", decoded.NoticeID, original.NoticeID)
	}
	if decoded.Summary != original.Summary {
		t.Errorf("Summary = %v, want %v", decoded.Summary, original.Summary)
	}
	if decoded.OutputRef != original.OutputRef {
		t.Errorf("OutputRef = %v, want %v", decoded.OutputRef, original.OutputRef)
	}
	if decoded.Flags["score"] != original.Flags["score"] {
		t.Errorf("Flags[score] = %v, want %v", decoded.Flags["score"], original.Flags["score"])
	}
	if !decoded.CompletedAt.Equal(original.CompletedAt) {
		t.Errorf("CompletedAt = %v, want %v", decoded.CompletedAt, original.CompletedAt)
	}
}

func TestAgentResult_OptionalFieldsOmitted(t *testing.T) {
	// Only required fields populated; optional fields should be absent from JSON.
	result := &AgentResult{
		AgentName:   "hunter",
		Status:      StatusFailed,
		Error:       "SAM.gov rate limit exceeded",
		CompletedAt: time.Date(2026, 6, 5, 8, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// These fields are omitempty and must not appear when empty.
	for _, absent := range []string{"notice_id", "summary", "output_ref", "flags"} {
		if _, ok := raw[absent]; ok {
			t.Errorf("field %q should be omitted from JSON when empty, but was present", absent)
		}
	}

	// Required fields must always be present.
	for _, present := range []string{"agent_name", "status", "error", "completed_at"} {
		if _, ok := raw[present]; !ok {
			t.Errorf("field %q should always be present in JSON, but was absent", present)
		}
	}
}

func TestStatusConstants(t *testing.T) {
	// Ensure status constants have expected string values
	// (these are part of the contract - changing them breaks storage/serialization)
	if StatusSuccess != "success" {
		t.Errorf("StatusSuccess = %v, want 'success'", StatusSuccess)
	}
	if StatusFailed != "failed" {
		t.Errorf("StatusFailed = %v, want 'failed'", StatusFailed)
	}
	if StatusNeedsHuman != "needs_human" {
		t.Errorf("StatusNeedsHuman = %v, want 'needs_human'", StatusNeedsHuman)
	}
	if StatusReadyToSubmit != "ready_to_submit" {
		t.Errorf("StatusReadyToSubmit = %v, want 'ready_to_submit'", StatusReadyToSubmit)
	}
}
