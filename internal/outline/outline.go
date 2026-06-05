package outline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Mawar2/multi-agent-system/internal/agent"
)

const agentName = "outline"

// OutlineAgent generates a document outline for a bound Opportunity.
// It derives the required sections from the opportunity's title and description,
// producing structured data the next agent or the UI can consume directly.
type OutlineAgent struct {
	opp *Opportunity
}

// NewOutlineAgent returns an OutlineAgent bound to opp.
// Returns an error if opp is nil, because the agent has no meaningful work to do.
func NewOutlineAgent(opp *Opportunity) (*OutlineAgent, error) {
	if opp == nil {
		return nil, errors.New("outline: opportunity must not be nil")
	}
	return &OutlineAgent{opp: opp}, nil
}

// Name returns the agent's identifier, satisfying agent.Agent.
func (a *OutlineAgent) Name() string { return agentName }

// RunOutline returns the structured outline for the bound Opportunity.
// It derives required sections from the opportunity content on a best-effort
// basis: every opportunity with a title produces at least the standard sections.
// Returns an error only when the opportunity is missing a required title.
func (a *OutlineAgent) RunOutline() (*OutlineResult, error) {
	if a.opp.Title == "" {
		return nil, errors.New("outline: Opportunity.Title is required")
	}
	return &OutlineResult{
		OpportunityID: a.opp.ID,
		Title:         a.opp.Title,
		Sections:      buildSections(a.opp),
	}, nil
}

// Run satisfies agent.Agent. It calls RunOutline and stores the JSON-encoded
// OutlineResult in AgentResult.OutputRef so downstream agents and the UI can
// unmarshal it without re-running the agent.
func (a *OutlineAgent) Run(_ context.Context, noticeID string) (*agent.AgentResult, error) {
	if a.opp.Title == "" {
		return &agent.AgentResult{
			AgentName: agentName,
			Status:    agent.AgentStatusFailed,
			NoticeID:  noticeID,
			Summary:   "opportunity has no title",
			Error:     "outline: Opportunity.Title is required",
		}, nil
	}

	result, err := a.RunOutline()
	if err != nil {
		return &agent.AgentResult{
			AgentName: agentName,
			Status:    agent.AgentStatusFailed,
			NoticeID:  noticeID,
			Summary:   "failed to build outline",
			Error:     err.Error(),
		}, nil
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return &agent.AgentResult{
			AgentName: agentName,
			Status:    agent.AgentStatusFailed,
			NoticeID:  noticeID,
			Summary:   "failed to encode outline",
			Error:     fmt.Sprintf("outline: json.Marshal: %v", err),
		}, nil
	}

	return &agent.AgentResult{
		AgentName: agentName,
		Status:    agent.AgentStatusSuccess,
		NoticeID:  noticeID,
		Summary:   fmt.Sprintf("outline produced %d sections for: %s", len(result.Sections), a.opp.Title),
		OutputRef: string(encoded),
	}, nil
}

// buildSections derives the required sections from the opportunity.
// Standard sections are always included. Additional sections are appended based
// on keywords found in the opportunity's title and description, so sparse
// opportunities still produce a usable skeleton.
func buildSections(opp *Opportunity) []Section {
	sections := []Section{
		{Key: "executive_summary", Title: "Executive Summary", Required: true, Source: "standard"},
		{Key: "problem_statement", Title: "Problem Statement", Required: true, Source: "standard"},
		{Key: "proposed_solution", Title: "Proposed Solution", Required: true, Source: "standard"},
		{Key: "timeline", Title: "Timeline & Milestones", Required: true, Source: "standard"},
		{Key: "pricing", Title: "Pricing & Investment", Required: true, Source: "standard"},
		{Key: "next_steps", Title: "Next Steps", Required: true, Source: "standard"},
	}

	combined := strings.ToLower(opp.Title + " " + opp.Description)

	type signal struct {
		keyword string
		section Section
	}

	signals := []signal{
		{"enterprise", Section{Key: "enterprise_requirements", Title: "Enterprise Requirements", Source: "opportunity"}},
		{"integrat", Section{Key: "integration_overview", Title: "Integration Overview", Source: "opportunity"}},
		{"security", Section{Key: "security_compliance", Title: "Security & Compliance", Source: "opportunity"}},
		{"compliance", Section{Key: "security_compliance", Title: "Security & Compliance", Source: "opportunity"}},
		{"migrat", Section{Key: "migration_plan", Title: "Migration Plan", Source: "opportunity"}},
		{"training", Section{Key: "training_onboarding", Title: "Training & Onboarding", Source: "opportunity"}},
		{"onboard", Section{Key: "training_onboarding", Title: "Training & Onboarding", Source: "opportunity"}},
		{"support", Section{Key: "support_sla", Title: "Support & SLA", Source: "opportunity"}},
		{"sla", Section{Key: "support_sla", Title: "Support & SLA", Source: "opportunity"}},
	}

	seen := map[string]bool{}
	for _, s := range signals {
		if strings.Contains(combined, s.keyword) && !seen[s.section.Key] {
			seen[s.section.Key] = true
			sections = append(sections, s.section)
		}
	}

	return sections
}
