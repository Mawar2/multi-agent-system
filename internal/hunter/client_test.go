package hunter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// sampleOpportunity returns a filled Opportunity for use in test fixtures.
func sampleOpportunity(id, title string) Opportunity {
	return Opportunity{
		NoticeID:         id,
		Title:            title,
		SolicitationNum:  "SOL-" + id,
		PostedDate:       "2026-06-01",
		ResponseDeadLine: "2026-07-01",
		NaicsCode:        "541511",
		Type:             "Solicitation",
		Active:           "Yes",
		OrganizationName: "Dept of Testing",
		Description:      "Test opportunity " + id,
		PointOfContact: []ContactInfo{
			{Type: "primary", Email: "test@example.gov", Name: "Jane Doe"},
		},
	}
}

// newTestClient returns a Client aimed at the given server URL.
func newTestClient(serverURL string) *Client {
	return &Client{
		apiKey:     "test-api-key",
		baseURL:    serverURL,
		httpClient: http.DefaultClient,
	}
}

func TestNewClient(t *testing.T) {
	c := NewClient("my-key")
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

func TestSearch_Success(t *testing.T) {
	want := &SearchResult{
		TotalRecords: 2,
		Opportunities: []Opportunity{
			sampleOpportunity("opp-1", "Software Development Services"),
			sampleOpportunity("opp-2", "IT Consulting"),
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("api_key") != "test-api-key" {
			http.Error(w, "missing api_key", http.StatusBadRequest)
			return
		}
		if q.Get("keyword") != "software" {
			http.Error(w, "unexpected keyword", http.StatusBadRequest)
			return
		}
		if q.Get("postedFrom") != "2026-01-01" {
			http.Error(w, "unexpected postedFrom", http.StatusBadRequest)
			return
		}
		if q.Get("postedTo") != "2026-06-30" {
			http.Error(w, "unexpected postedTo", http.StatusBadRequest)
			return
		}
		if q.Get("limit") != "10" {
			http.Error(w, "unexpected limit", http.StatusBadRequest)
			return
		}
		if q.Get("offset") != "5" {
			http.Error(w, "unexpected offset", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(want) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	got, err := c.Search(context.Background(), SearchParams{
		Keyword:    "software",
		PostedFrom: "2026-01-01",
		PostedTo:   "2026-06-30",
		Limit:      10,
		Offset:     5,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got.TotalRecords != want.TotalRecords {
		t.Errorf("TotalRecords = %d, want %d", got.TotalRecords, want.TotalRecords)
	}
	if len(got.Opportunities) != len(want.Opportunities) {
		t.Fatalf("len(Opportunities) = %d, want %d", len(got.Opportunities), len(want.Opportunities))
	}
	for i, opp := range got.Opportunities {
		if opp.NoticeID != want.Opportunities[i].NoticeID {
			t.Errorf("Opportunities[%d].NoticeID = %q, want %q", i, opp.NoticeID, want.Opportunities[i].NoticeID)
		}
		if opp.Title != want.Opportunities[i].Title {
			t.Errorf("Opportunities[%d].Title = %q, want %q", i, opp.Title, want.Opportunities[i].Title)
		}
	}
}

func TestSearch_DefaultLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "25" {
			http.Error(w, "want limit=25, got "+got, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&SearchResult{}) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	if _, err := c.Search(context.Background(), SearchParams{Limit: 0}); err != nil {
		t.Fatalf("Search() error = %v", err)
	}
}

func TestSearch_NoKeywordOrDates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("keyword") != "" {
			http.Error(w, "unexpected keyword param", http.StatusBadRequest)
			return
		}
		if q.Get("postedFrom") != "" || q.Get("postedTo") != "" {
			http.Error(w, "unexpected date params", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&SearchResult{TotalRecords: 0}) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	result, err := c.Search(context.Background(), SearchParams{Limit: 1})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if result.TotalRecords != 0 {
		t.Errorf("TotalRecords = %d, want 0", result.TotalRecords)
	}
}

func TestSearch_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestSearch_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // close before the client connects

	c := newTestClient(srv.URL)
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
}

func TestSearch_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json")) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected JSON decode error, got nil")
	}
}

func TestSearch_EmptyOpportunities(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&SearchResult{ //nolint:errcheck
			TotalRecords:  0,
			Opportunities: []Opportunity{},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	result, err := c.Search(context.Background(), SearchParams{})
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

func TestSearch_PointOfContact(t *testing.T) {
	opp := sampleOpportunity("poc-1", "Security Services")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(&SearchResult{ //nolint:errcheck
			TotalRecords:  1,
			Opportunities: []Opportunity{opp},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	result, err := c.Search(context.Background(), SearchParams{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Opportunities) != 1 {
		t.Fatalf("expected 1 opportunity, got %d", len(result.Opportunities))
	}
	got := result.Opportunities[0]
	if len(got.PointOfContact) != 1 {
		t.Fatalf("expected 1 contact, got %d", len(got.PointOfContact))
	}
	if got.PointOfContact[0].Email != "test@example.gov" {
		t.Errorf("contact email = %q, want %q", got.PointOfContact[0].Email, "test@example.gov")
	}
}
