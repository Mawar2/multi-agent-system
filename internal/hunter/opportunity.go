package hunter

import "time"

// Opportunity represents a federal contract opportunity from SAM.gov.
type Opportunity struct {
	NoticeID           string `json:"noticeId"`
	Title              string `json:"title"`
	SolicitationNumber string `json:"solicitationNumber"`
	Department         string `json:"department"`
	SubTier            string `json:"subTier"`
	Office             string `json:"office"`
	PostedDate         string `json:"postedDate"`
	Type               string `json:"type"`
	ResponseDeadline   string `json:"responseDeadLine"`
	NAICSCode          string `json:"naicsCode"`
	NAICSDescription   string `json:"naicsDescription"`
	ClassificationCode string `json:"classificationCode"`
	Active             string `json:"active"`
	Description        string `json:"description"`
	UILink             string `json:"uiLink"`
	TypeOfSetAside     string `json:"typeOfSetAside"`
	ArchiveDate        string `json:"archiveDate"`

	// Score is computed locally and not from SAM.gov.
	Score float64 `json:"-"`
}

// DeadlineTime parses ResponseDeadline into a time.Time.
// Returns zero time and false if the field is empty or unparseable.
func (o *Opportunity) DeadlineTime() (time.Time, bool) {
	if o.ResponseDeadline == "" {
		return time.Time{}, false
	}
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, o.ResponseDeadline); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// DaysUntilDeadline returns the number of days until the response deadline.
// Returns -1 if the deadline cannot be parsed.
func (o *Opportunity) DaysUntilDeadline() int {
	t, ok := o.DeadlineTime()
	if !ok {
		return -1
	}
	return int(time.Until(t).Hours() / 24)
}

// IsSmallBusinessSetAside reports whether this opportunity is reserved for small businesses.
func (o *Opportunity) IsSmallBusinessSetAside() bool {
	return o.TypeOfSetAside != "" && o.TypeOfSetAside != "N/A"
}

// samResponse is the top-level JSON envelope returned by SAM.gov.
type samResponse struct {
	TotalRecords      int            `json:"totalRecords"`
	Limit             int            `json:"limit"`
	Offset            int            `json:"offset"`
	OpportunitiesData []*Opportunity `json:"opportunitiesData"`
}
