package hunter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	samBaseURL    = "https://api.sam.gov/opportunities/v2/search"
	defaultLimit  = 100
	clientTimeout = 30 * time.Second
)

// SAMClient fetches contract opportunities from the SAM.gov REST API.
type SAMClient struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string // overrideable for tests
}

// NewSAMClient creates a SAMClient authenticated with the given API key.
func NewSAMClient(apiKey string) *SAMClient {
	return &SAMClient{
		apiKey:  apiKey,
		baseURL: samBaseURL,
		httpClient: &http.Client{
			Timeout: clientTimeout,
		},
	}
}

// SearchParams controls what SAM.gov returns.
type SearchParams struct {
	Keywords   []string // full-text keywords, OR-joined
	NAICSCodes []string // NAICS industry codes (e.g. "541511")
	PostedFrom time.Time
	PostedTo   time.Time
	// Types filters by notice type: "o"=solicitation, "p"=presolicitation,
	// "r"=sources sought, "s"=special notice, "a"=award.
	// Empty means all active types.
	Types []string
	Limit int // max results per page (default 100, max 1000)
}

// Search returns opportunities matching params.
// It fetches all pages when the total exceeds the page limit.
func (c *SAMClient) Search(ctx context.Context, params SearchParams) ([]*Opportunity, error) {
	if params.Limit <= 0 {
		params.Limit = defaultLimit
	}

	var all []*Opportunity
	offset := 0

	for {
		page, total, err := c.fetchPage(ctx, params, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)

		offset += len(page)
		if offset >= total || len(page) == 0 {
			break
		}
		// Respect context cancellation between pages.
		select {
		case <-ctx.Done():
			return all, ctx.Err()
		default:
		}
	}

	return all, nil
}

// fetchPage retrieves one page of results and returns (opportunities, totalRecords, error).
func (c *SAMClient) fetchPage(ctx context.Context, params SearchParams, offset int) ([]*Opportunity, int, error) {
	q := url.Values{}
	q.Set("api_key", c.apiKey)
	q.Set("limit", strconv.Itoa(params.Limit))
	q.Set("offset", strconv.Itoa(offset))

	if !params.PostedFrom.IsZero() {
		q.Set("postedFrom", params.PostedFrom.Format("01/02/2006"))
	}
	if !params.PostedTo.IsZero() {
		q.Set("postedTo", params.PostedTo.Format("01/02/2006"))
	}
	for _, k := range params.Keywords {
		q.Add("keyword", k)
	}
	for _, n := range params.NAICSCodes {
		q.Add("naics", n)
	}
	for _, t := range params.Types {
		q.Add("ptype", t)
	}

	reqURL := c.baseURL + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("sam: build request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("sam: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("sam: read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("sam: HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var envelope samResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, 0, fmt.Errorf("sam: decode response: %w", err)
	}

	return envelope.OpportunitiesData, envelope.TotalRecords, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
