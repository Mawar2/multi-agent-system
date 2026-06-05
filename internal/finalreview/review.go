// Package finalreview implements the Final Review agent for Zone 2 of the Kaimi pipeline.
//
// Final Review is the last automated step before a human submits a proposal to SAM.gov.
// It evaluates an approved draft against the Opportunity's requirements and signals
// readiness for human review. The agent NEVER submits anything — submission is always
// a human action.
//
// This package contains the core Review function and the stubbed check implementations.
// Real verification logic (RFP alignment, compliance checklist, deadline proximity) will
// be added in the next ticket.
//
// Usage:
//
//	result, err := finalreview.Review(ctx, draftContent, opp)
//	if err != nil {
//	    // handle unexpected errors (e.g. nil opportunity)
//	}
//	if result.Status == agent.StatusReadyToSubmit {
//	    // signal human to review and submit
//	}
package finalreview

import (
	"context"
	"fmt"
	"time"

	"github.com/Mawar2/Kaimi/internal/agent"
	"github.com/Mawar2/Kaimi/internal/opportunity"
)

// CheckResult holds the outcome of a single review check.
type CheckResult struct {
	// Name is a machine-readable identifier for the check, used in AgentResult.Flags.
	Name string

	// Passed indicates whether the check succeeded.
	Passed bool

	// Message describes the check expectation (displayed when the check fails).
	Message string
}

// Review evaluates an approved draft against the Opportunity's requirements and
// returns an AgentResult indicating whether the proposal is ready for human
// submission.
//
// All checks are stubbed in this skeleton — real verification logic will be added
// in the next ticket. The function is deterministic and makes no network calls.
//
// Returns an error only for invalid inputs (e.g. nil opportunity). A failing check
// is not an error — it produces StatusNeedsHuman in the AgentResult instead.
func Review(ctx context.Context, draft string, opp *opportunity.Opportunity) (*agent.AgentResult, error) {
	if opp == nil {
		return nil, fmt.Errorf("opportunity must not be nil")
	}

	checks := runChecks(draft, opp)

	// Collect any failed check names for the error field.
	var failed []string
	for _, c := range checks {
		if !c.Passed {
			failed = append(failed, c.Name)
		}
	}

	result := &agent.AgentResult{
		AgentName:   "final-review",
		NoticeID:    opp.ID,
		Flags:       buildFlags(checks),
		CompletedAt: time.Now(),
	}

	if len(failed) == 0 {
		result.Status = agent.StatusReadyToSubmit
		result.Summary = fmt.Sprintf("all checks passed for %q — ready for human review and submission", opp.Title)
	} else {
		result.Status = agent.StatusNeedsHuman
		result.Summary = fmt.Sprintf("review checks failed for %q — human intervention required", opp.Title)
		result.Error = fmt.Sprintf("failed checks: %v", failed)
	}

	return result, nil
}

// runChecks executes all review checks on the draft and opportunity.
//
// TODO(issue-7): Replace stub implementations with real verification logic
// (RFP alignment, compliance checklist, deadline proximity, required sections).
func runChecks(draft string, opp *opportunity.Opportunity) []CheckResult {
	return []CheckResult{
		checkDraftNotEmpty(draft),
		checkOpportunityHasTitle(opp),
		checkOpportunityHasDeadline(opp),
	}
}

// checkDraftNotEmpty verifies the draft contains content.
//
// TODO(issue-7): Replace with full completeness check (required sections present,
// page limits, mandatory certifications).
func checkDraftNotEmpty(draft string) CheckResult {
	return CheckResult{
		Name:    "draft_not_empty",
		Passed:  draft != "",
		Message: "draft must contain content",
	}
}

// checkOpportunityHasTitle verifies the opportunity has a title.
//
// TODO(issue-7): Replace with proposal–requirements alignment check.
func checkOpportunityHasTitle(opp *opportunity.Opportunity) CheckResult {
	return CheckResult{
		Name:    "opportunity_has_title",
		Passed:  opp.Title != "",
		Message: "opportunity must have a title",
	}
}

// checkOpportunityHasDeadline verifies the opportunity has a response deadline.
//
// TODO(issue-7): Replace with deadline proximity check (warn if <72 hours remain).
func checkOpportunityHasDeadline(opp *opportunity.Opportunity) CheckResult {
	return CheckResult{
		Name:    "opportunity_has_deadline",
		Passed:  !opp.ResponseDeadline.IsZero(),
		Message: "opportunity must have a response deadline",
	}
}

// buildFlags converts check results into the AgentResult.Flags map.
//
// Each check name maps to "pass" or "fail". An additional "checks_passed"
// key summarises the aggregate outcome (e.g. "3/3").
func buildFlags(checks []CheckResult) map[string]string {
	flags := make(map[string]string, len(checks)+1)
	passed := 0

	for _, c := range checks {
		if c.Passed {
			passed++
			flags[c.Name] = "pass"
		} else {
			flags[c.Name] = "fail"
		}
	}

	flags["checks_passed"] = fmt.Sprintf("%d/%d", passed, len(checks))
	return flags
}
