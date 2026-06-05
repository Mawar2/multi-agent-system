package outline

// Section is one required section in the proposal outline.
type Section struct {
	// Key is a machine-readable slug, e.g. "executive_summary".
	Key string `json:"key"`

	// Title is the human-readable heading for this section.
	Title string `json:"title"`

	// Required indicates whether this section must appear in every proposal.
	Required bool `json:"required"`

	// Source records what drove this section: "standard" for baseline sections
	// present in every proposal, "opportunity" for sections derived from the
	// opportunity's title or description.
	Source string `json:"source"`
}

// OutlineResult is the structured output produced by OutlineAgent.
// It contains an ordered list of sections derived from the Opportunity.
// The JSON representation is stored in AgentResult.OutputRef so downstream
// agents and the UI can consume it without re-running the agent.
type OutlineResult struct {
	// OpportunityID echoes the source opportunity's identifier.
	OpportunityID string `json:"opportunity_id"`

	// Title echoes the opportunity title for convenience.
	Title string `json:"title"`

	// Sections is the ordered list of required sections for this proposal.
	Sections []Section `json:"sections"`
}
