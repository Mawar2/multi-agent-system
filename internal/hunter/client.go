package hunter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "https://api.sam.gov"
	defaultTimeout  = 30 * time.Second
	defaultLimit    = 100
	defaultDaysBack = 7
)

// Opportunity represents a SAM.gov contract opportunity.
type Opportunity struct {
	NoticeID         string           `json:"noticeId"`
	Title            string           `json:"title"`
	Agency           string           `json:"fullParentPathName"`
	Type             string           `json:"type"`
	BaseType         string           `json:"baseType"`
	PostedDate       string           `json:"postedDate"`
	ResponseDeadLine string           `json:"responseDeadLine"`
	ArchiveDate      string           `json:"archiveDate"`
	NAICSCode        string           `json:"naicsCode"`
	SetAside         string           `json:"setAside"`
	Active           string           `json:"active"`
	UILink           string           `json:"uiLink"`
	PointOfContact   []PointOfContact `json:"pointOfContact"`
}

// PointOfContact holds contact information for an opportunity.
type PointOfContact struct {
	FullName string `json:"fullName"`
	Email    string `json:"email"`
	Type     string `json:"type"`
}

// SearchOptions configures a SAM.gov opportunity search.
type SearchOptions struct {
	// Keywords are search terms applied to title and description.
	Keywords []string
	// DaysBack controls how many days back to search (default 7).
	DaysBack int
	// Limit is the maximum number of results to return (default 100, max 1000).
	Limit int
	// NAICSCodes filters by NAICS industry codes (e.g. "541511" for custom software).
	NAICSCodes []string
}

// SearchResult holds the results of a SAM.gov opportunity search.
type SearchResult struct {
	Opportunities []*Opportunity
	Total         int
}

// samResponse is the raw API response envelope.
type samResponse struct {
	TotalRecords      int            `json:"totalRecords"`
	OpportunitiesData []*Opportunity `json:"opportunitiesData"`
}

// Client fetches contract opportunities from the SAM.gov Opportunities API v2.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a SAM.gov opportunities client using the given API key.
// Requests time out after 30 seconds.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
}

// newClientWithHTTP creates a client with a custom HTTP client and base URL.
// Used in tests to point at a local httptest server.
func newClientWithHTTP(apiKey, baseURL string, hc *http.Client) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: hc,
	}
}

// Search fetches contract opportunities matching the given options.
//
// It returns all matching opportunities up to opts.Limit, filtered by keywords
// and NAICS codes when specified.
func (c *Client) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("SAM.gov API key is required")
	}

	opts = applyDefaults(opts)

	params := c.buildParams(opts)
	endpoint := c.baseURL + "/opportunities/v2/search?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("SAM.gov API returned 401: invalid or missing API key")
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SAM.gov API returned %d", resp.StatusCode)
	}

	var raw samResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	opportunities := filterByNAICS(raw.OpportunitiesData, opts.NAICSCodes)

	return &SearchResult{
		Opportunities: opportunities,
		Total:         raw.TotalRecords,
	}, nil
}

// buildParams constructs the query parameters for a search request.
func (c *Client) buildParams(opts SearchOptions) url.Values {
	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("limit", fmt.Sprintf("%d", opts.Limit))
	params.Set("status", "active")

	// Date range
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -opts.DaysBack)
	params.Set("postedFrom", from.Format("01/02/2006"))
	params.Set("postedTo", now.Format("01/02/2006"))

	if len(opts.Keywords) > 0 {
		params.Set("q", strings.Join(opts.Keywords, " "))
	}

	return params
}

// applyDefaults fills in zero values for SearchOptions with sensible defaults.
func applyDefaults(opts SearchOptions) SearchOptions {
	if opts.Limit <= 0 {
		opts.Limit = defaultLimit
	}
	if opts.DaysBack <= 0 {
		opts.DaysBack = defaultDaysBack
	}
	return opts
}

// filterByNAICS returns only opportunities matching any of the given NAICS codes.
// If naicsCodes is empty, all opportunities are returned.
func filterByNAICS(opportunities []*Opportunity, naicsCodes []string) []*Opportunity {
	if len(naicsCodes) == 0 {
		return opportunities
	}

	codeSet := make(map[string]bool, len(naicsCodes))
	for _, c := range naicsCodes {
		codeSet[c] = true
	}

	filtered := make([]*Opportunity, 0)
	for _, opp := range opportunities {
		if codeSet[opp.NAICSCode] {
			filtered = append(filtered, opp)
		}
	}
	return filtered
}
