package hunter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// serveJSON starts a test server that responds with the given payload.
func serveJSON(t *testing.T, status int, payload interface{}) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if payload != nil {
			if err := json.NewEncoder(w).Encode(payload); err != nil {
				t.Fatalf("failed to encode test payload: %v", err)
			}
		}
	}))
	t.Cleanup(srv.Close)
	client := newClientWithHTTP("test-key", srv.URL, srv.Client())
	return srv, client
}

// sampleResponse builds a samResponse with n opportunities.
func sampleResponse(n int) samResponse {
	opps := make([]*Opportunity, n)
	for i := range opps {
		opps[i] = &Opportunity{
			NoticeID:         fmt.Sprintf("notice-%d", i),
			Title:            fmt.Sprintf("Software Contract %d", i),
			Agency:           "DEPT OF DEFENSE",
			Type:             "Solicitation",
			PostedDate:       "2024-08-01",
			ResponseDeadLine: "2024-08-30T17:00:00-05:00",
			NAICSCode:        "541511",
			Active:           "Yes",
		}
	}
	return samResponse{TotalRecords: n, OpportunitiesData: opps}
}

func TestNewClient(t *testing.T) {
	c := NewClient("my-key")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.apiKey != "my-key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "my-key")
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
	if c.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

func TestNewClientWithHTTP(t *testing.T) {
	hc := &http.Client{Timeout: 5 * time.Second}
	c := newClientWithHTTP("key", "http://localhost", hc)
	if c.apiKey != "key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "key")
	}
	if c.baseURL != "http://localhost" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "http://localhost")
	}
	if c.httpClient != hc {
		t.Error("httpClient was not set correctly")
	}
}

func TestSearch_EmptyAPIKey(t *testing.T) {
	c := NewClient("")
	_, err := c.Search(context.Background(), SearchOptions{})
	if err == nil {
		t.Fatal("expected error for empty API key, got nil")
	}
}

func TestSearch_HappyPath(t *testing.T) {
	payload := sampleResponse(3)
	_, client := serveJSON(t, http.StatusOK, payload)

	result, err := client.Search(context.Background(), SearchOptions{
		Keywords: []string{"software", "development"},
		DaysBack: 14,
		Limit:    50,
	})
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}
	if result.Total != 3 {
		t.Errorf("Total = %d, want 3", result.Total)
	}
	if len(result.Opportunities) != 3 {
		t.Errorf("len(Opportunities) = %d, want 3", len(result.Opportunities))
	}
	// Verify first opportunity fields
	opp := result.Opportunities[0]
	if opp.NAICSCode != "541511" {
		t.Errorf("NAICSCode = %q, want %q", opp.NAICSCode, "541511")
	}
	if opp.Agency != "DEPT OF DEFENSE" {
		t.Errorf("Agency = %q, want %q", opp.Agency, "DEPT OF DEFENSE")
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	payload := samResponse{TotalRecords: 0, OpportunitiesData: []*Opportunity{}}
	_, client := serveJSON(t, http.StatusOK, payload)

	result, err := client.Search(context.Background(), SearchOptions{})
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}
	if result.Total != 0 {
		t.Errorf("Total = %d, want 0", result.Total)
	}
	if len(result.Opportunities) != 0 {
		t.Errorf("len(Opportunities) = %d, want 0", len(result.Opportunities))
	}
}

func TestSearch_ServerError(t *testing.T) {
	_, client := serveJSON(t, http.StatusInternalServerError, nil)

	_, err := client.Search(context.Background(), SearchOptions{})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

func TestSearch_Unauthorized(t *testing.T) {
	_, client := serveJSON(t, http.StatusUnauthorized, nil)

	_, err := client.Search(context.Background(), SearchOptions{})
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
}

func TestSearch_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not valid json {{{"))
	}))
	t.Cleanup(srv.Close)
	client := newClientWithHTTP("test-key", srv.URL, srv.Client())

	_, err := client.Search(context.Background(), SearchOptions{})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSearch_ContextCanceled(t *testing.T) {
	// Server that never responds
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // block until context canceled
	}))
	t.Cleanup(srv.Close)
	client := newClientWithHTTP("test-key", srv.URL, &http.Client{Timeout: 5 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Search(ctx, SearchOptions{})
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestSearch_DefaultOptions(t *testing.T) {
	payload := sampleResponse(1)

	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(srv.Close)
	client := newClientWithHTTP("test-key", srv.URL, srv.Client())

	_, err := client.Search(context.Background(), SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}

	// Defaults should include limit=100
	if capturedQuery == "" {
		t.Error("expected query parameters to be set")
	}
	// Verify limit default
	vals, _ := parseQuery(capturedQuery)
	if vals["limit"] != "100" {
		t.Errorf("default limit = %q, want %q", vals["limit"], "100")
	}
}

func TestSearch_NAICSFilter(t *testing.T) {
	// Mix of NAICS codes: some match, some don't
	opps := []*Opportunity{
		{NoticeID: "a", NAICSCode: "541511"}, // IT custom software - matches
		{NoticeID: "b", NAICSCode: "541512"}, // CS design - matches
		{NoticeID: "c", NAICSCode: "236220"}, // commercial construction - no match
		{NoticeID: "d", NAICSCode: "541511"}, // IT custom software - matches
	}
	payload := samResponse{TotalRecords: 4, OpportunitiesData: opps}
	_, client := serveJSON(t, http.StatusOK, payload)

	result, err := client.Search(context.Background(), SearchOptions{
		NAICSCodes: []string{"541511", "541512"},
	})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(result.Opportunities) != 3 {
		t.Errorf("filtered len = %d, want 3", len(result.Opportunities))
	}
	// Total reflects raw API count, not filtered count
	if result.Total != 4 {
		t.Errorf("Total = %d, want 4 (unfiltered)", result.Total)
	}
}

func TestSearch_NoNAICSFilter(t *testing.T) {
	opps := []*Opportunity{
		{NoticeID: "a", NAICSCode: "541511"},
		{NoticeID: "b", NAICSCode: "236220"},
	}
	payload := samResponse{TotalRecords: 2, OpportunitiesData: opps}
	_, client := serveJSON(t, http.StatusOK, payload)

	// Empty NAICSCodes means no filter — all opportunities returned
	result, err := client.Search(context.Background(), SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error: %v", err)
	}
	if len(result.Opportunities) != 2 {
		t.Errorf("unfiltered len = %d, want 2", len(result.Opportunities))
	}
}

func TestApplyDefaults(t *testing.T) {
	// Zero values → defaults applied
	got := applyDefaults(SearchOptions{})
	if got.Limit != defaultLimit {
		t.Errorf("Limit = %d, want %d", got.Limit, defaultLimit)
	}
	if got.DaysBack != defaultDaysBack {
		t.Errorf("DaysBack = %d, want %d", got.DaysBack, defaultDaysBack)
	}

	// Non-zero values → preserved
	custom := applyDefaults(SearchOptions{Limit: 25, DaysBack: 30})
	if custom.Limit != 25 {
		t.Errorf("Limit = %d, want 25", custom.Limit)
	}
	if custom.DaysBack != 30 {
		t.Errorf("DaysBack = %d, want 30", custom.DaysBack)
	}
}

func TestFilterByNAICS(t *testing.T) {
	opps := []*Opportunity{
		{NoticeID: "1", NAICSCode: "541511"},
		{NoticeID: "2", NAICSCode: "541512"},
		{NoticeID: "3", NAICSCode: "999999"},
	}

	// No filter: all pass
	got := filterByNAICS(opps, nil)
	if len(got) != 3 {
		t.Errorf("no filter: len = %d, want 3", len(got))
	}

	// Filter for one code
	got = filterByNAICS(opps, []string{"541511"})
	if len(got) != 1 || got[0].NoticeID != "1" {
		t.Errorf("single filter: got %v", got)
	}

	// Filter for two codes
	got = filterByNAICS(opps, []string{"541511", "541512"})
	if len(got) != 2 {
		t.Errorf("two codes filter: len = %d, want 2", len(got))
	}

	// Filter that matches nothing
	got = filterByNAICS(opps, []string{"000000"})
	if len(got) != 0 {
		t.Errorf("no-match filter: len = %d, want 0", len(got))
	}
}

func TestBuildParams(t *testing.T) {
	c := NewClient("my-api-key")
	opts := SearchOptions{
		Keywords: []string{"software", "it"},
		DaysBack: 7,
		Limit:    50,
	}
	params := c.buildParams(opts)

	if params.Get("api_key") != "my-api-key" {
		t.Errorf("api_key = %q, want %q", params.Get("api_key"), "my-api-key")
	}
	if params.Get("limit") != "50" {
		t.Errorf("limit = %q, want %q", params.Get("limit"), "50")
	}
	if params.Get("q") != "software it" {
		t.Errorf("q = %q, want %q", params.Get("q"), "software it")
	}
	if params.Get("status") != "active" {
		t.Errorf("status = %q, want %q", params.Get("status"), "active")
	}
	// Date params set
	if params.Get("postedFrom") == "" {
		t.Error("postedFrom not set")
	}
	if params.Get("postedTo") == "" {
		t.Error("postedTo not set")
	}
}

func TestBuildParams_NoKeywords(t *testing.T) {
	c := NewClient("key")
	opts := SearchOptions{DaysBack: 7, Limit: 10}
	params := c.buildParams(opts)
	if params.Get("q") != "" {
		t.Errorf("q should be empty without keywords, got %q", params.Get("q"))
	}
}

// parseQuery is a test helper to split raw query strings.
func parseQuery(raw string) (map[string]string, error) {
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string, len(vals))
	for k, v := range vals {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result, nil
}
