package hunter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// samResponse builds a well-formed SAM.gov mock response body.
func samResponse(total int, opps []Opportunity) []byte {
	result := SearchResult{
		TotalRecords:  total,
		Opportunities: opps,
	}
	b, _ := json.Marshal(result)
	return b
}

// newTestClient returns a Client pointed at server using server's http.Client.
func newTestClient(server *httptest.Server) *Client {
	return &Client{
		apiKey:     "test-api-key",
		baseURL:    server.URL,
		httpClient: server.Client(),
	}
}

// TestSearch_KeywordSuccess verifies a successful keyword search returns parsed opportunities.
func TestSearch_KeywordSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("api_key"); got != "test-api-key" {
			t.Errorf("expected api_key=test-api-key, got %q", got)
		}
		if got := r.URL.Query().Get("q"); got != "software development" {
			t.Errorf("expected q=software development, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(samResponse(2, []Opportunity{
			{NoticeID: "N001", Title: "Software Dev Services", NAICS: "541511", Active: "Yes"},
			{NoticeID: "N002", Title: "Software Testing", NAICS: "541511", Active: "Yes"},
		}))
	}))
	defer server.Close()

	client := newTestClient(server)
	result, err := client.Search(context.Background(), SearchOptions{Keywords: "software development"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.TotalRecords != 2 {
		t.Errorf("TotalRecords = %d, want 2", result.TotalRecords)
	}
	if len(result.Opportunities) != 2 {
		t.Fatalf("len(Opportunities) = %d, want 2", len(result.Opportunities))
	}
	if result.Opportunities[0].NoticeID != "N001" {
		t.Errorf("Opportunities[0].NoticeID = %q, want N001", result.Opportunities[0].NoticeID)
	}
}

// TestSearch_NAICSFilter verifies that naicsCode is forwarded to the API.
func TestSearch_NAICSFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("naicsCode"); got != "541512" {
			t.Errorf("expected naicsCode=541512, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(samResponse(1, []Opportunity{
			{NoticeID: "N100", Title: "IT Services", NAICS: "541512", Department: "DoD"},
		}))
	}))
	defer server.Close()

	client := newTestClient(server)
	result, err := client.Search(context.Background(), SearchOptions{NAICSCode: "541512"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Opportunities) != 1 {
		t.Fatalf("len(Opportunities) = %d, want 1", len(result.Opportunities))
	}
	if result.Opportunities[0].NAICS != "541512" {
		t.Errorf("NAICS = %q, want 541512", result.Opportunities[0].NAICS)
	}
}

// TestSearch_KeywordAndNAICS verifies both filters are forwarded together.
func TestSearch_KeywordAndNAICS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("q") != "cloud" {
			t.Errorf("expected q=cloud, got %q", q.Get("q"))
		}
		if q.Get("naicsCode") != "541519" {
			t.Errorf("expected naicsCode=541519, got %q", q.Get("naicsCode"))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(samResponse(0, nil))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Search(context.Background(), SearchOptions{Keywords: "cloud", NAICSCode: "541519"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

// TestSearch_DefaultLimit verifies that a zero Limit is sent as 10.
func TestSearch_DefaultLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "10" {
			t.Errorf("expected limit=10, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(samResponse(0, nil))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Search(context.Background(), SearchOptions{}) // Limit=0 → default 10
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

// TestSearch_ExplicitLimit verifies that a positive Limit is forwarded as-is.
func TestSearch_ExplicitLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "25" {
			t.Errorf("expected limit=25, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(samResponse(0, nil))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Search(context.Background(), SearchOptions{Limit: 25})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

// TestSearch_PaginationOffset verifies the offset parameter is forwarded.
func TestSearch_PaginationOffset(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("offset"); got != "20" {
			t.Errorf("expected offset=20, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(samResponse(0, nil))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Search(context.Background(), SearchOptions{Offset: 20})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

// TestSearch_HTTP401 verifies that a 401 response returns a descriptive error.
func TestSearch_HTTP401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Search(context.Background(), SearchOptions{})
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
}

// TestSearch_HTTP500 verifies that a 500 response returns a descriptive error.
func TestSearch_HTTP500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Search(context.Background(), SearchOptions{})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
}

// TestSearch_InvalidJSON verifies that malformed JSON returns a decode error.
func TestSearch_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not valid json`))
	}))
	defer server.Close()

	client := newTestClient(server)
	_, err := client.Search(context.Background(), SearchOptions{})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestSearch_ContextCancellation verifies that a cancelled context aborts the request.
func TestSearch_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond — the context should fire first.
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := newTestClient(server)
	_, err := client.Search(ctx, SearchOptions{})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// TestSearch_EmptyResults verifies that an empty opportunities slice is handled correctly.
func TestSearch_EmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(samResponse(0, []Opportunity{}))
	}))
	defer server.Close()

	client := newTestClient(server)
	result, err := client.Search(context.Background(), SearchOptions{Keywords: "nonexistent"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.TotalRecords != 0 {
		t.Errorf("TotalRecords = %d, want 0", result.TotalRecords)
	}
	if len(result.Opportunities) != 0 {
		t.Errorf("len(Opportunities) = %d, want 0", len(result.Opportunities))
	}
}

// TestSearch_InvalidBaseURL verifies that a malformed base URL returns an error from
// http.NewRequestWithContext before any network I/O occurs.
func TestSearch_InvalidBaseURL(t *testing.T) {
	client := &Client{
		apiKey:     "test-key",
		baseURL:    "://bad url\x00",
		httpClient: &http.Client{},
	}
	_, err := client.Search(context.Background(), SearchOptions{})
	if err == nil {
		t.Fatal("expected error for invalid base URL, got nil")
	}
}

// TestNewClient verifies that NewClient wires the API key and default base URL.
func TestNewClient(t *testing.T) {
	c := NewClient("my-key")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.apiKey != "my-key" {
		t.Errorf("apiKey = %q, want my-key", c.apiKey)
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
	if c.httpClient == nil {
		t.Error("httpClient is nil")
	}
}

// TestOpportunityFields verifies that all Opportunity fields are deserialised.
func TestOpportunityFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(samResponse(1, []Opportunity{
			{
				NoticeID:     "FULL-001",
				Title:        "Full Field Test",
				NAICS:        "541715",
				PostedDate:   "2026-06-01",
				ResponseDate: "2026-07-01",
				Description:  "Test description",
				Department:   "Department of Commerce",
				Type:         "Solicitation",
				Active:       "Yes",
			},
		}))
	}))
	defer server.Close()

	client := newTestClient(server)
	result, err := client.Search(context.Background(), SearchOptions{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Opportunities) != 1 {
		t.Fatalf("len(Opportunities) = %d, want 1", len(result.Opportunities))
	}

	opp := result.Opportunities[0]
	checks := map[string]string{
		"NoticeID":     opp.NoticeID,
		"Title":        opp.Title,
		"NAICS":        opp.NAICS,
		"PostedDate":   opp.PostedDate,
		"ResponseDate": opp.ResponseDate,
		"Description":  opp.Description,
		"Department":   opp.Department,
		"Type":         opp.Type,
		"Active":       opp.Active,
	}
	want := map[string]string{
		"NoticeID":     "FULL-001",
		"Title":        "Full Field Test",
		"NAICS":        "541715",
		"PostedDate":   "2026-06-01",
		"ResponseDate": "2026-07-01",
		"Description":  "Test description",
		"Department":   "Department of Commerce",
		"Type":         "Solicitation",
		"Active":       "Yes",
	}
	for field, got := range checks {
		if got != want[field] {
			t.Errorf("%s = %q, want %q", field, got, want[field])
		}
	}
}
