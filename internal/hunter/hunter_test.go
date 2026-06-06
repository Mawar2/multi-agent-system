package hunter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	c := NewClient("test-key")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.apiKey != "test-key" {
		t.Errorf("expected apiKey 'test-key', got %q", c.apiKey)
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("expected default baseURL, got %q", c.baseURL)
	}
	if c.httpClient == nil {
		t.Error("expected non-nil httpClient")
	}
}

func TestSearchOpportunities_Success(t *testing.T) {
	want := []*Opportunity{
		{
			ID:     "abc123",
			Title:  "AI Software Development Services",
			Type:   "Presolicitation",
			Agency: "Department of Defense",
			NAICS:  "541512",
		},
		{
			ID:    "def456",
			Title: "Machine Learning Platform",
			Type:  "Combined Synopsis/Solicitation",
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify API key is passed
		if r.URL.Query().Get("api_key") != "test-key" {
			http.Error(w, "missing api_key", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("keyword") == "" {
			http.Error(w, "missing keyword", http.StatusBadRequest)
			return
		}

		result := SearchResult{
			TotalRecords:  len(want),
			Opportunities: want,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.baseURL = ts.URL

	got, err := c.SearchOpportunities(context.Background(), "artificial intelligence", 10)
	if err != nil {
		t.Fatalf("SearchOpportunities() unexpected error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d opportunities, got %d", len(want), len(got))
	}
	if got[0].ID != want[0].ID {
		t.Errorf("expected first ID %q, got %q", want[0].ID, got[0].ID)
	}
}

func TestSearchOpportunities_EmptyKeyword(t *testing.T) {
	c := NewClient("test-key")
	_, err := c.SearchOpportunities(context.Background(), "", 10)
	if err == nil {
		t.Fatal("expected error for empty keyword, got nil")
	}
}

func TestSearchOpportunities_DefaultLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := SearchResult{TotalRecords: 0, Opportunities: []*Opportunity{}}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.baseURL = ts.URL

	// limit <= 0 should default to 10 without error
	got, err := c.SearchOpportunities(context.Background(), "software", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Error("expected non-nil slice, got nil")
	}
}

func TestSearchOpportunities_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer ts.Close()

	c := NewClient("bad-key")
	c.baseURL = ts.URL

	_, err := c.SearchOpportunities(context.Background(), "cloud", 5)
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
}

func TestSearchOpportunities_InvalidJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json {{{"))
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.baseURL = ts.URL

	_, err := c.SearchOpportunities(context.Background(), "it services", 5)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestSearchOpportunities_NetworkError(t *testing.T) {
	c := NewClient("test-key")
	c.baseURL = "http://127.0.0.1:1" // Nothing listening here

	_, err := c.SearchOpportunities(context.Background(), "software", 5)
	if err == nil {
		t.Fatal("expected network error, got nil")
	}
}
