package hunter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const defaultBaseURL = "https://api.sam.gov/opportunities/v2/search"

// Client queries the SAM.gov Opportunities API for federal contracting leads.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient returns a Client configured with the given SAM.gov API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Opportunity represents a federal contracting opportunity from SAM.gov.
type Opportunity struct {
	NoticeID         string        `json:"noticeId"`
	Title            string        `json:"title"`
	SolicitationNum  string        `json:"solicitationNumber"`
	PostedDate       string        `json:"postedDate"`
	ResponseDeadLine string        `json:"responseDeadLine"`
	NaicsCode        string        `json:"naicsCode"`
	Type             string        `json:"type"`
	Active           string        `json:"active"`
	Description      string        `json:"description"`
	OrganizationName string        `json:"organizationName"`
	PointOfContact   []ContactInfo `json:"pointOfContact"`
}

// ContactInfo holds contact details attached to an opportunity.
type ContactInfo struct {
	Type  string `json:"type"`
	Email string `json:"email"`
	Phone string `json:"phone"`
	Name  string `json:"fullName"`
}

// SearchParams defines the criteria for an opportunity search.
type SearchParams struct {
	Keyword    string
	PostedFrom string // YYYY-MM-DD
	PostedTo   string // YYYY-MM-DD
	Limit      int    // defaults to 25 when zero
	Offset     int
}

// SearchResult is the decoded response from the SAM.gov search endpoint.
type SearchResult struct {
	TotalRecords  int           `json:"totalRecords"`
	Opportunities []Opportunity `json:"opportunitiesData"`
}

// Search queries SAM.gov for opportunities matching the given parameters.
func (c *Client) Search(ctx context.Context, params SearchParams) (*SearchResult, error) {
	if params.Limit <= 0 {
		params.Limit = 25
	}

	q := url.Values{}
	q.Set("api_key", c.apiKey)
	q.Set("limit", strconv.Itoa(params.Limit))
	q.Set("offset", strconv.Itoa(params.Offset))
	if params.Keyword != "" {
		q.Set("keyword", params.Keyword)
	}
	if params.PostedFrom != "" {
		q.Set("postedFrom", params.PostedFrom)
	}
	if params.PostedTo != "" {
		q.Set("postedTo", params.PostedTo)
	}

	reqURL := c.baseURL + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SAM.gov API returned status %d", resp.StatusCode)
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &result, nil
}
