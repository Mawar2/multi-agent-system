package outline_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/Mawar2/multi-agent-system/internal/agent"
	"github.com/Mawar2/multi-agent-system/internal/outline"
)

// Compile-time assertion: OutlineAgent must satisfy agent.Agent.
var _ agent.Agent = (*outline.OutlineAgent)(nil)

// loadFixture reads testdata/opportunity.json and unmarshals it into an Opportunity.
// Tests share this fixture so no live calls are needed.
func loadFixture(t *testing.T) *outline.Opportunity {
	t.Helper()
	data, err := os.ReadFile("testdata/opportunity.json")
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}

	var raw struct {
		ID          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	return &outline.Opportunity{
		ID:          raw.ID,
		Title:       raw.Title,
		Description: raw.Description,
	}
}

func TestNewOutlineAgent_NilOpportunity(t *testing.T) {
	_, err := outline.NewOutlineAgent(nil)
	if err == nil {
		t.Fatal("expected error for nil opportunity, got nil")
	}
}

func TestOutlineAgent_Name(t *testing.T) {
	opp := loadFixture(t)
	a, err := outline.NewOutlineAgent(opp)
	if err != nil {
		t.Fatalf("NewOutlineAgent: %v", err)
	}
	if got := a.Name(); got != "outline" {
		t.Errorf("Name() = %q, want %q", got, "outline")
	}
}

func TestOutlineAgent_Run_HappyPath(t *testing.T) {
	opp := loadFixture(t)
	a, err := outline.NewOutlineAgent(opp)
	if err != nil {
		t.Fatalf("NewOutlineAgent: %v", err)
	}

	result, err := a.Run(context.Background(), "issue-2")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Run returned nil result")
	}
	if result.Status != agent.AgentStatusSuccess {
		t.Errorf("Status = %q, want %q", result.Status, agent.AgentStatusSuccess)
	}
	if result.AgentName != "outline" {
		t.Errorf("AgentName = %q, want %q", result.AgentName, "outline")
	}
	if result.NoticeID != "issue-2" {
		t.Errorf("NoticeID = %q, want %q", result.NoticeID, "issue-2")
	}
	if result.Summary == "" {
		t.Error("Summary must not be empty on success")
	}
	if result.Error != "" {
		t.Errorf("Error must be empty on success, got %q", result.Error)
	}
	if !result.Status.IsTerminal() {
		t.Error("success status must be terminal")
	}
}

func TestOutlineAgent_Run_EmptyTitle(t *testing.T) {
	opp := &outline.Opportunity{ID: "opp-x", Title: "", Description: "no title here"}
	a, err := outline.NewOutlineAgent(opp)
	if err != nil {
		t.Fatalf("NewOutlineAgent: %v", err)
	}

	result, err := a.Run(context.Background(), "issue-2")
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Run returned nil result")
	}
	if result.Status != agent.AgentStatusFailed {
		t.Errorf("Status = %q, want %q", result.Status, agent.AgentStatusFailed)
	}
	if result.Error == "" {
		t.Error("Error must be non-empty on failure")
	}
	if !result.Status.IsTerminal() {
		t.Error("failed status must be terminal")
	}
}

func TestOutlineAgent_Run_OutputRef_IsValidJSON(t *testing.T) {
	opp := loadFixture(t)
	a, err := outline.NewOutlineAgent(opp)
	if err != nil {
		t.Fatalf("NewOutlineAgent: %v", err)
	}

	result, err := a.Run(context.Background(), "issue-3")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.OutputRef == "" {
		t.Fatal("OutputRef must not be empty on success")
	}

	var got outline.OutlineResult
	if err := json.Unmarshal([]byte(result.OutputRef), &got); err != nil {
		t.Fatalf("OutputRef is not valid JSON: %v", err)
	}
	if got.OpportunityID != opp.ID {
		t.Errorf("OutlineResult.OpportunityID = %q, want %q", got.OpportunityID, opp.ID)
	}
	if got.Title != opp.Title {
		t.Errorf("OutlineResult.Title = %q, want %q", got.Title, opp.Title)
	}
	if len(got.Sections) == 0 {
		t.Error("OutlineResult.Sections must not be empty")
	}
}

func TestOutlineAgent_RunOutline_HappyPath(t *testing.T) {
	opp := loadFixture(t)
	a, err := outline.NewOutlineAgent(opp)
	if err != nil {
		t.Fatalf("NewOutlineAgent: %v", err)
	}

	result, err := a.RunOutline()
	if err != nil {
		t.Fatalf("RunOutline: %v", err)
	}
	if result == nil {
		t.Fatal("RunOutline returned nil")
	}
	if result.OpportunityID != opp.ID {
		t.Errorf("OpportunityID = %q, want %q", result.OpportunityID, opp.ID)
	}
	if result.Title != opp.Title {
		t.Errorf("Title = %q, want %q", result.Title, opp.Title)
	}
	if len(result.Sections) == 0 {
		t.Error("Sections must not be empty")
	}
}

func TestOutlineAgent_RunOutline_EmptyTitle(t *testing.T) {
	opp := &outline.Opportunity{ID: "opp-empty", Title: ""}
	a, err := outline.NewOutlineAgent(opp)
	if err != nil {
		t.Fatalf("NewOutlineAgent: %v", err)
	}

	_, err = a.RunOutline()
	if err == nil {
		t.Fatal("expected error for empty title, got nil")
	}
}

func TestOutlineAgent_RunOutline_SparseOpportunity(t *testing.T) {
	// An opportunity with only a title (no ID, no description) must still succeed.
	opp := &outline.Opportunity{Title: "Minimal Deal"}
	a, err := outline.NewOutlineAgent(opp)
	if err != nil {
		t.Fatalf("NewOutlineAgent: %v", err)
	}

	result, err := a.RunOutline()
	if err != nil {
		t.Fatalf("RunOutline on sparse opportunity: %v", err)
	}
	if len(result.Sections) == 0 {
		t.Error("sparse opportunity must still produce sections")
	}

	// All standard sections must appear.
	standardKeys := []string{
		"executive_summary", "problem_statement", "proposed_solution",
		"timeline", "pricing", "next_steps",
	}
	index := map[string]outline.Section{}
	for _, s := range result.Sections {
		index[s.Key] = s
	}
	for _, key := range standardKeys {
		s, ok := index[key]
		if !ok {
			t.Errorf("standard section %q missing from sparse outline", key)
			continue
		}
		if s.Title == "" {
			t.Errorf("section %q has empty title", key)
		}
		if !s.Required {
			t.Errorf("standard section %q must be marked required", key)
		}
		if s.Source != "standard" {
			t.Errorf("standard section %q has source %q, want %q", key, s.Source, "standard")
		}
	}
}

func TestOutlineAgent_RunOutline_OpportunityDerivedSections(t *testing.T) {
	// An opportunity mentioning enterprise and security should produce extra sections.
	opp := &outline.Opportunity{
		ID:          "opp-ent",
		Title:       "Enterprise Platform Rollout",
		Description: "Requires security review and compliance certification before go-live.",
	}
	a, err := outline.NewOutlineAgent(opp)
	if err != nil {
		t.Fatalf("NewOutlineAgent: %v", err)
	}

	result, err := a.RunOutline()
	if err != nil {
		t.Fatalf("RunOutline: %v", err)
	}

	index := map[string]outline.Section{}
	for _, s := range result.Sections {
		index[s.Key] = s
	}

	for _, key := range []string{"enterprise_requirements", "security_compliance"} {
		s, ok := index[key]
		if !ok {
			t.Errorf("expected opportunity-derived section %q, not found", key)
			continue
		}
		if s.Source != "opportunity" {
			t.Errorf("section %q source = %q, want %q", key, s.Source, "opportunity")
		}
	}
}

func TestOutlineAgent_RunOutline_NoDuplicateSections(t *testing.T) {
	// An opportunity that triggers the same section via multiple keywords
	// must not produce duplicate entries.
	opp := &outline.Opportunity{
		ID:          "opp-dup",
		Title:       "Security and Compliance Platform",
		Description: "Full security hardening plus compliance reporting.",
	}
	a, err := outline.NewOutlineAgent(opp)
	if err != nil {
		t.Fatalf("NewOutlineAgent: %v", err)
	}

	result, err := a.RunOutline()
	if err != nil {
		t.Fatalf("RunOutline: %v", err)
	}

	seen := map[string]int{}
	for _, s := range result.Sections {
		seen[s.Key]++
	}
	for key, count := range seen {
		if count > 1 {
			t.Errorf("section %q appears %d times, want 1", key, count)
		}
	}
}
