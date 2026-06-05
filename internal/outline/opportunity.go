// Package outline provides the OutlineAgent, which generates a document outline
// for a given Opportunity.  This skeleton proves the agent fits the Agent interface
// before any real content-generation logic is added.
package outline

// Opportunity is the input the Outline agent processes.
// It carries the context needed to produce an outline: a unique identifier,
// a human-readable title, and an optional free-text description.
type Opportunity struct {
	// ID is an external identifier (e.g. CRM record ID, issue number).
	ID string

	// Title is the required headline for this opportunity.
	Title string

	// Description provides additional context (may be empty).
	Description string
}
