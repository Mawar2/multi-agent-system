package hunter

import "time"

// Opportunity represents a SAM.gov federal contracting opportunity.
type Opportunity struct {
	NoticeID     string    `json:"noticeId"`
	Title        string    `json:"title"`
	Agency       string    `json:"fullParentPathName"`
	Type         string    `json:"type"`
	NAICS        string    `json:"naicsCode"`
	Description  string    `json:"description"`
	ResponseDate string    `json:"responseDeadLine"`
	PostedDate   time.Time `json:"postedDate"`
	Active       bool      `json:"active"`
	SetAside     string    `json:"typeOfSetAside"`
	UILink       string    `json:"uiLink"`
}

// SearchParams configures a SAM.gov opportunity search.
type SearchParams struct {
	// Keyword is a free-text search term (e.g. "software development").
	Keyword string
	// NAICS filters results to a specific industry code (e.g. "541511").
	NAICS string
	// Limit caps the number of returned results; defaults to 25 if zero.
	Limit int
	// PostedFrom is the earliest post date in MM/DD/YYYY format.
	PostedFrom string
	// PostedTo is the latest post date in MM/DD/YYYY format.
	PostedTo string
}

// SearchResult is the response from a SAM.gov opportunity search.
type SearchResult struct {
	TotalRecords  int            `json:"totalRecords"`
	Opportunities []*Opportunity `json:"opportunitiesData"`
}
