package hunter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func samTestServer(t *testing.T, opps []*Opportunity, total int) *httptest.Server {
	t.Helper()
	resp := samResponse{
		TotalRecords:      total,
		Limit:             100,
		Offset:            0,
		OpportunitiesData: opps,
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") == "" {
			t.Error("expected api_key query param")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
}

func newTestClient(baseURL string) *SAMClient {
	return &SAMClient{
		apiKey:     "test-key",
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func TestSAMClient_Search_BasicResults(t *testing.T) {
	opps := []*Opportunity{
		{NoticeID: "abc123", Title: "Software dev services", NAICSCode: "541511"},
		{NoticeID: "def456", Title: "Cloud platform"},
	}
	srv := samTestServer(t, opps, 2)
	defer srv.Close()

	results, err := newTestClient(srv.URL).Search(context.Background(), SearchParams{Limit: 100})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("want 2, got %d", len(results))
	}
	if results[0].NoticeID != "abc123" {
		t.Errorf("want noticeId abc123, got %s", results[0].NoticeID)
	}
}

func TestSAMClient_Search_EmptyResults(t *testing.T) {
	srv := samTestServer(t, nil, 0)
	defer srv.Close()

	results, err := newTestClient(srv.URL).Search(context.Background(), SearchParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("want 0, got %d", len(results))
	}
}

func TestSAMClient_Search_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
}

func TestSAMClient_Search_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`)) //nolint:errcheck
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestSAMClient_Search_PassesQueryParams(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(samResponse{}) //nolint:errcheck
	}))
	defer srv.Close()

	params := SearchParams{
		Keywords:   []string{"software", "cloud"},
		NAICSCodes: []string{"541511"},
		Types:      []string{"o"},
		PostedFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		PostedTo:   time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC),
		Limit:      50,
	}
	newTestClient(srv.URL).Search(context.Background(), params) //nolint:errcheck

	for _, want := range []string{"api_key=test-key", "limit=50", "postedFrom=01%2F01%2F2026", "naics=541511"} {
		if !strings.Contains(capturedQuery, want) {
			t.Errorf("query string missing %q; got: %s", want, capturedQuery)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello world", 5, "hello…"},
		{"hi", 10, "hi"},
		{"exact", 5, "exact"},
	}
	for _, tc := range tests {
		if got := truncate(tc.input, tc.n); got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}
