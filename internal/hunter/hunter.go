// Package hunter provides a client for discovering US federal government
// contract opportunities via the SAM.gov Opportunities API.
package hunter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	defaultBaseURL = "https://api.sam.gov"
	defaultLimit   = 10
	defaultTimeout = 30 * time.Second
)

// Opportunity represents a single contract opportunity returned by SAM.gov.
type Opportunity struct {
	NoticeID            string  `json:"noticeId"`
	Title               string  `json:"title"`
	SolicitationNumber  string  `json:"solicitationNumber"`
	FullParentPathName  string  `json:"fullParentPathName"`
	PostedDate          string  `json:"postedDate"`
	Type                string  `json:"type"`
	BaseType            string  `json:"baseType"`
	ArchiveType         string  `json:"archiveType"`
	ArchiveDate         *string `json:"archiveDate"`
	SetAside            *string `json:"typeOfSetAside"`
	SetAsideDescription *string `json:"typeOfSetAsideDescription"`
	ResponseDeadline    *string `json:"responseDeadline"`
	NaicsCode           string  `json:"naicsCode"`
	ClassificationCode  string  `json:"classificationCode"`
	Active              string  `json:"active"`
	Award               *Award  `json:"award"`
	Description         *string `json:"description"`
	OrganizationType    string  `json:"organizationType"`
	UILink              string  `json:"uiLink"`
}

// Award holds contract award details attached to an opportunity.
type Award struct {
	Date    string   `json:"date"`
	Amount  float64  `json:"amount"`
	Number  string   `json:"number"`
	Awardee *Awardee `json:"awardee"`
}

// Awardee holds information about the entity that received an award.
type Awardee struct {
	Name string `json:"name"`
	Duns string `json:"duns"`
	UEI  string `json:"uei"`
}

// SearchResult is the top-level envelope returned by the SAM.gov search endpoint.
type SearchResult struct {
	TotalRecords  int           `json:"totalRecords"`
	Opportunities []Opportunity `json:"opportunitiesData"`
}

// SearchParams holds parameters for an opportunity search request.
type SearchParams struct {
	// Keywords filters by free-text keywords in the title/description.
	Keywords string
	// NAICSCode filters by a NAICS industry code (e.g. "541511").
	NAICSCode string
	// Limit caps the number of results per page (default: 10, max: 1000).
	Limit int
	// Offset is the zero-based result index for pagination.
	Offset int
	// PostedFrom restricts results to opportunities posted on or after this date (MM/DD/YYYY).
	PostedFrom string
	// PostedTo restricts results to opportunities posted on or before this date (MM/DD/YYYY).
	PostedTo string
}

// Client is a SAM.gov Opportunities API client.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a Client using the given SAM.gov API key.
// A 30-second HTTP timeout is applied by default.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// Search returns contract opportunities matching params.
// It performs a single paginated request; call repeatedly with increasing
// Offset values to iterate through all results.
func (c *Client) Search(ctx context.Context, params SearchParams) (*SearchResult, error) {
	rawURL := c.baseURL + "/opportunities/v2/search"

	// Build the request first so http.NewRequestWithContext validates the URL.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("hunter: create request: %w", err)
	}

	// Append query parameters to the already-parsed URL.
	q := req.URL.Query()
	q.Set("api_key", c.apiKey)

	if params.Keywords != "" {
		q.Set("keyword", params.Keywords)
	}
	if params.NAICSCode != "" {
		q.Set("naicsCode", params.NAICSCode)
	}

	limit := params.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	q.Set("limit", strconv.Itoa(limit))

	if params.Offset > 0 {
		q.Set("offset", strconv.Itoa(params.Offset))
	}
	if params.PostedFrom != "" {
		q.Set("postedFrom", params.PostedFrom)
	}
	if params.PostedTo != "" {
		q.Set("postedTo", params.PostedTo)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hunter: execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hunter: SAM.gov API returned HTTP %d", resp.StatusCode)
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("hunter: decode response: %w", err)
	}

	return &result, nil
}
