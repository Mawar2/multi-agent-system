// Package hunter discovers federal contracting opportunities from SAM.gov.
// It queries the SAM.gov Opportunities API and returns structured opportunity data
// for further routing into the multi-agent task queue.
package hunter

import (
	"context"
	"time"
)

// Opportunity represents a federal contracting opportunity from SAM.gov.
type Opportunity struct {
	NoticeID         string
	Title            string
	Type             string
	PostedDate       string
	ResponseDeadline string
	NAICSCode        string
	Agency           string
	Description      string
	UIURL            string
	SetAside         string
}

// SearchQuery holds parameters for querying SAM.gov opportunities.
type SearchQuery struct {
	Keywords    []string
	NAICSCodes  []string
	SetAsides   []string
	PostedAfter time.Time
	Limit       int
}

// Client searches SAM.gov for federal contracting opportunities.
type Client interface {
	Search(ctx context.Context, q SearchQuery) ([]Opportunity, error)
}
