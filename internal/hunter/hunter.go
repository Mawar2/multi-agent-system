// Package hunter discovers SAM.gov contract opportunities relevant to
// the multi-agent system's target market (AI/ML, software, IT services).
package hunter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultBaseURL = "https://api.sam.gov/opportunities/v2/search"
	defaultTimeout = 30 * time.Second
)

// Opportunity represents a single SAM.gov contract opportunity.
type Opportunity struct {
	ID           string `json:"noticeId"`
	Title        string `json:"title"`
	Type         string `json:"type"`
	Agency       string `json:"fullParentPathName"`
	NAICS        string `json:"naicsCode"`
	SetAside     string `json:"typeOfSetAside"`
	PostedDate   string `json:"postedDate"`
	ResponseDate string `json:"responseDeadLine"`
	UILink       string `json:"uiLink"`
}

// SearchResult is the top-level response envelope from the SAM.gov API.
type SearchResult struct {
	TotalRecords  int            `json:"totalRecords"`
	Opportunities []*Opportunity `json:"opportunitiesData"`
}

// Client queries the SAM.gov Opportunities API.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a Client with the given SAM.gov API key.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// SearchOpportunities fetches active opportunities matching keyword, up to limit results.
// It constrains the search to opportunities posted in the last 90 days.
func (c *Client) SearchOpportunities(ctx context.Context, keyword string, limit int) ([]*Opportunity, error) {
	if keyword == "" {
		return nil, fmt.Errorf("keyword must not be empty")
	}
	if limit <= 0 {
		limit = 10
	}

	now := time.Now()
	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("keyword", keyword)
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("postedFrom", now.AddDate(0, -3, 0).Format("01/02/2006"))
	params.Set("postedTo", now.Format("01/02/2006"))
	params.Set("active", "Yes")

	reqURL := c.baseURL + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SAM.gov API returned status %d", resp.StatusCode)
	}

	var result SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return result.Opportunities, nil
}
