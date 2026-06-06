package hunter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newClientWithURL creates a test client with a custom base URL.
func newClientWithURL(apiKey, baseURL string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		http:    &http.Client{},
	}
}

// samResponse mirrors the shape of a real SAM.gov API response.
type samResponse struct {
	TotalRecords      int            `json:"totalRecords"`
	OpportunitiesData []*Opportunity `json:"opportunitiesData"`
}

func newMockServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestSearch_success(t *testing.T) {
	want := &SearchResult{
		TotalRecords: 2,
		Opportunities: []*Opportunity{
			{NoticeID: "abc123", Title: "Cloud Platform Dev", NAICS: "541511"},
			{NoticeID: "def456", Title: "Cybersecurity Support", NAICS: "541512"},
		},
	}

	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") == "" {
			http.Error(w, "missing api_key", http.StatusUnauthorized)
			return
		}
		resp := samResponse{
			TotalRecords:      want.TotalRecords,
			OpportunitiesData: want.Opportunities,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	c := newClientWithURL("test-key", srv.URL)
	got, err := c.Search(context.Background(), SearchParams{Keyword: "cloud"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got.TotalRecords != want.TotalRecords {
		t.Errorf("TotalRecords = %d, want %d", got.TotalRecords, want.TotalRecords)
	}
	if len(got.Opportunities) != len(want.Opportunities) {
		t.Fatalf("len(Opportunities) = %d, want %d", len(got.Opportunities), len(want.Opportunities))
	}
	if got.Opportunities[0].NoticeID != "abc123" {
		t.Errorf("Opportunities[0].NoticeID = %q, want %q", got.Opportunities[0].NoticeID, "abc123")
	}
}

func TestSearch_defaultLimit(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "25" {
			http.Error(w, "unexpected limit", http.StatusBadRequest)
			return
		}
		resp := samResponse{TotalRecords: 0, OpportunitiesData: []*Opportunity{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	c := newClientWithURL("key", srv.URL)
	_, err := c.Search(context.Background(), SearchParams{})
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}
}

func TestSearch_customLimit(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "10" {
			http.Error(w, "unexpected limit", http.StatusBadRequest)
			return
		}
		resp := samResponse{TotalRecords: 0, OpportunitiesData: []*Opportunity{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	c := newClientWithURL("key", srv.URL)
	_, err := c.Search(context.Background(), SearchParams{Limit: 10})
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}
}

func TestSearch_queryParams(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("keyword") != "software" {
			http.Error(w, "missing keyword", http.StatusBadRequest)
			return
		}
		if q.Get("naicsCode") != "541511" {
			http.Error(w, "missing naicsCode", http.StatusBadRequest)
			return
		}
		if q.Get("postedFrom") != "01/01/2024" {
			http.Error(w, "missing postedFrom", http.StatusBadRequest)
			return
		}
		resp := samResponse{TotalRecords: 1, OpportunitiesData: []*Opportunity{{NoticeID: "x"}}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	c := newClientWithURL("key", srv.URL)
	_, err := c.Search(context.Background(), SearchParams{
		Keyword:    "software",
		NAICS:      "541511",
		PostedFrom: "01/01/2024",
	})
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}
}

func TestSearch_postedToParam(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("postedTo") != "12/31/2024" {
			http.Error(w, "missing postedTo", http.StatusBadRequest)
			return
		}
		resp := samResponse{TotalRecords: 0, OpportunitiesData: []*Opportunity{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	c := newClientWithURL("key", srv.URL)
	_, err := c.Search(context.Background(), SearchParams{PostedTo: "12/31/2024"})
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}
}

func TestSearch_nonOKStatus(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	})

	c := newClientWithURL("bad-key", srv.URL)
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestSearch_invalidJSON(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	})

	c := newClientWithURL("key", srv.URL)
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSearch_emptyResults(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := samResponse{TotalRecords: 0, OpportunitiesData: []*Opportunity{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	c := newClientWithURL("key", srv.URL)
	got, err := c.Search(context.Background(), SearchParams{})
	if err != nil {
		t.Fatalf("Search() unexpected error: %v", err)
	}
	if got.TotalRecords != 0 {
		t.Errorf("TotalRecords = %d, want 0", got.TotalRecords)
	}
	if len(got.Opportunities) != 0 {
		t.Errorf("len(Opportunities) = %d, want 0", len(got.Opportunities))
	}
}

func TestSearch_cancelledContext(t *testing.T) {
	srv := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := samResponse{TotalRecords: 0, OpportunitiesData: []*Opportunity{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	c := newClientWithURL("key", srv.URL)
	_, err := c.Search(ctx, SearchParams{})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestSearch_badURL(t *testing.T) {
	c := &Client{
		apiKey:  "key",
		baseURL: "://invalid-url",
		http:    &http.Client{},
	}
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for invalid base URL, got nil")
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient("my-api-key")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.apiKey != "my-api-key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "my-api-key")
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
	if c.http == nil {
		t.Error("http client is nil")
	}
}
