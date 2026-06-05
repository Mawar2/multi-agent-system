package outline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Mawar2/Kaimi/internal/agent"
	"github.com/Mawar2/Kaimi/internal/opportunity"
)

const agentName = "outline"

// Outline generates a document outline for the given Opportunity.
// It extracts formatting requirements from the solicitation description and
// attaches them to the result so the writing team has them up front.
//
// Returns an error only for invalid inputs (e.g. nil opportunity). A missing
// title produces StatusFailed in the AgentResult rather than a Go error.
// Missing formatting rules are reported in FormattingRules.NotSpecified —
// rules are never invented.
func Outline(ctx context.Context, opp *opportunity.Opportunity) (*agent.AgentResult, error) {
	if opp == nil {
		return nil, fmt.Errorf("outline: opportunity must not be nil")
	}

	if opp.Title == "" {
		return &agent.AgentResult{
			AgentName:   agentName,
			Status:      agent.StatusFailed,
			NoticeID:    opp.ID,
			Error:       "outline: opportunity title is required",
			CompletedAt: time.Now(),
		}, nil
	}

	rules := ExtractFormattingRules(opp.Description)
	rulesJSON, err := json.Marshal(rules)
	if err != nil {
		return &agent.AgentResult{
			AgentName:   agentName,
			Status:      agent.StatusFailed,
			NoticeID:    opp.ID,
			Error:       fmt.Sprintf("outline: marshal formatting rules: %v", err),
			CompletedAt: time.Now(),
		}, nil
	}

	return &agent.AgentResult{
		AgentName:   agentName,
		Status:      agent.StatusSuccess,
		NoticeID:    opp.ID,
		Summary:     fmt.Sprintf("outline stub produced for %q; formatting rules attached", opp.Title),
		OutputRef:   string(rulesJSON),
		Flags:       buildFlags(rules),
		CompletedAt: time.Now(),
	}, nil
}

// buildFlags converts FormattingRules into the AgentResult.Flags map.
// Each found rule is stored under its field name. The "formatting" key
// summarises overall completeness: "complete", "partial", or "absent".
func buildFlags(r FormattingRules) map[string]string {
	flags := make(map[string]string)

	switch len(r.NotSpecified) {
	case 0:
		flags["formatting"] = "complete"
	case 4:
		flags["formatting"] = "absent"
	default:
		flags["formatting"] = "partial"
	}

	if r.Font != "" {
		flags["font"] = r.Font
	}
	if r.Margins != "" {
		flags["margins"] = r.Margins
	}
	if r.PageLimit != "" {
		flags["page_limit"] = r.PageLimit
	}
	if len(r.Artifacts) > 0 {
		flags["artifacts"] = strings.Join(r.Artifacts, "; ")
	}
	if len(r.NotSpecified) > 0 {
		flags["not_specified"] = strings.Join(r.NotSpecified, ",")
	}

	return flags
}
