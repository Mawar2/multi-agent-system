// Package hunter discovers federal contracting opportunities from SAM.gov.
package hunter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	defaultBaseURL = "https://api.sam.gov/opportunities/v2/search"
	defaultLimit   = 25
)

// Client queries the SAM.gov Opportunities API.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// NewClient creates a SAM.gov client authenticated with apiKey.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		http: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Search queries SAM.gov for contract opportunities matching params.
func (c *Client) Search(ctx context.Context, params SearchParams) (*SearchResult, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = defaultLimit
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	q := req.URL.Query()
	q.Set("api_key", c.apiKey)
	q.Set("limit", fmt.Sprintf("%d", limit))
	q.Set("offset", "0")

	if params.Keyword != "" {
		q.Set("keyword", params.Keyword)
	}
	if params.NAICS != "" {
		q.Set("naicsCode", params.NAICS)
	}
	if params.PostedFrom != "" {
		q.Set("postedFrom", params.PostedFrom)
	}
	if params.PostedTo != "" {
		q.Set("postedTo", params.PostedTo)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SAM.gov request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SAM.gov returned status %d: %s", resp.StatusCode, string(body))
	}

	var result SearchResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	return &result, nil
}
