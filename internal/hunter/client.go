// Package hunter discovers contracting opportunities from SAM.gov.
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

// Opportunity represents a single contracting opportunity from SAM.gov.
type Opportunity struct {
	NoticeID     string `json:"noticeId"`
	Title        string `json:"title"`
	NAICS        string `json:"naicsCode"`
	PostedDate   string `json:"postedDate"`
	ResponseDate string `json:"responseDeadLine"`
	Description  string `json:"description"`
	Department   string `json:"fullParentPathName"`
	Type         string `json:"type"`
	Active       string `json:"active"`
}

// SearchResult holds the paged response from SAM.gov.
type SearchResult struct {
	Opportunities []Opportunity `json:"opportunitiesData"`
	TotalRecords  int           `json:"totalRecords"`
}

// SearchOptions parameterises a SAM.gov opportunity search.
type SearchOptions struct {
	// Keywords is the free-text search query (maps to ?q=).
	Keywords string
	// NAICSCode filters results to a specific NAICS code (e.g. "541511").
	NAICSCode string
	// Limit is the maximum number of results to return (1–100; default 10).
	Limit int
	// Offset is the zero-based pagination offset.
	Offset int
}

// Client is a thin HTTP wrapper around the SAM.gov Opportunities v2 API.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient returns a Client that authenticates with the provided API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Search queries SAM.gov for contracting opportunities matching opts.
//
// The context is forwarded to the underlying HTTP request, so callers can
// cancel or time-out long searches via the context.
func (c *Client) Search(ctx context.Context, opts SearchOptions) (*SearchResult, error) {
	params := url.Values{}
	params.Set("api_key", c.apiKey)

	if opts.Keywords != "" {
		params.Set("q", opts.Keywords)
	}
	if opts.NAICSCode != "" {
		params.Set("naicsCode", opts.NAICSCode)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 10
	}
	params.Set("limit", strconv.Itoa(limit))
	params.Set("offset", strconv.Itoa(opts.Offset))

	reqURL := c.baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("hunter: building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hunter: executing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hunter: SAM.gov returned HTTP %d", resp.StatusCode)
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("hunter: decoding response: %w", err)
	}

	return &result, nil
}
