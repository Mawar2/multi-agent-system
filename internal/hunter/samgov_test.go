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

// fixture returns a test server that responds with the given samGovResponse JSON.
func fixture(t *testing.T, status int, body samGovResponse) (*httptest.Server, *SAMGovClient) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)

	client := NewSAMGovClient("test-key")
	client.baseURL = srv.URL
	return srv, client
}

func TestSearch_Success(t *testing.T) {
	_, client := fixture(t, http.StatusOK, samGovResponse{
		TotalRecords: 2,
		OpportunitiesData: []samOpportunity{
			{
				NoticeID:         "abc123",
				Title:            "Cloud Platform Services",
				Type:             "Solicitation",
				PostedDate:       "2026-05-01",
				ResponseDeadLine: "2026-06-30",
				NAICSCode:        "541511",
				OrganizationHierarchy: []struct {
					Name string `json:"name"`
				}{{Name: "Department of Defense"}},
				Description:    "Seeking cloud services.",
				UILink:         "https://sam.gov/opp/abc123",
				TypeOfSetAside: "SBA",
			},
			{
				NoticeID: "def456",
				Title:    "AI Research Support",
				Type:     "Presolicitation",
			},
		},
	})

	opps, err := client.Search(context.Background(), SearchQuery{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(opps) != 2 {
		t.Fatalf("expected 2 opportunities, got %d", len(opps))
	}

	first := opps[0]
	if first.NoticeID != "abc123" {
		t.Errorf("NoticeID: got %q, want %q", first.NoticeID, "abc123")
	}
	if first.Title != "Cloud Platform Services" {
		t.Errorf("Title: got %q", first.Title)
	}
	if first.Agency != "Department of Defense" {
		t.Errorf("Agency: got %q", first.Agency)
	}
	if first.SetAside != "SBA" {
		t.Errorf("SetAside: got %q", first.SetAside)
	}
	if first.UIURL != "https://sam.gov/opp/abc123" {
		t.Errorf("UIURL: got %q", first.UIURL)
	}

	second := opps[1]
	if second.Agency != "" {
		t.Errorf("expected empty agency when hierarchy missing, got %q", second.Agency)
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	_, client := fixture(t, http.StatusOK, samGovResponse{TotalRecords: 0})

	opps, err := client.Search(context.Background(), SearchQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opps) != 0 {
		t.Errorf("expected 0 opportunities, got %d", len(opps))
	}
}

func TestSearch_HTTPError(t *testing.T) {
	_, client := fixture(t, http.StatusUnauthorized, samGovResponse{})

	_, err := client.Search(context.Background(), SearchQuery{})
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention status 401, got: %v", err)
	}
}

func TestSearch_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json{{{"))
	}))
	t.Cleanup(srv.Close)

	client := NewSAMGovClient("test-key")
	client.baseURL = srv.URL

	_, err := client.Search(context.Background(), SearchQuery{})
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestSearch_ServerDown(t *testing.T) {
	client := NewSAMGovClient("test-key")
	client.baseURL = "http://127.0.0.1:0" // nothing listening

	_, err := client.Search(context.Background(), SearchQuery{})
	if err == nil {
		t.Fatal("expected error when server is unreachable")
	}
}

func TestSearch_QueryParams(t *testing.T) {
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(samGovResponse{})
	}))
	t.Cleanup(srv.Close)

	client := NewSAMGovClient("my-api-key")
	client.baseURL = srv.URL

	_, _ = client.Search(context.Background(), SearchQuery{
		Keywords:    []string{"software", "cloud"},
		NAICSCodes:  []string{"541511", "541512"},
		SetAsides:   []string{"SBA"},
		PostedAfter: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Limit:       25,
	})

	q := captured.URL.Query()
	if q.Get("api_key") != "my-api-key" {
		t.Errorf("api_key: got %q", q.Get("api_key"))
	}
	if q.Get("limit") != "25" {
		t.Errorf("limit: got %q", q.Get("limit"))
	}
	if !strings.Contains(q.Get("keywords"), "software") {
		t.Errorf("keywords missing 'software': %q", q.Get("keywords"))
	}
	if !strings.Contains(q.Get("naicsCode"), "541511") {
		t.Errorf("naicsCode missing: %q", q.Get("naicsCode"))
	}
	if q.Get("typeOfSetAside") != "SBA" {
		t.Errorf("typeOfSetAside: got %q", q.Get("typeOfSetAside"))
	}
	if q.Get("postedFrom") == "" {
		t.Error("postedFrom should be set when PostedAfter is provided")
	}
}

func TestSearch_DefaultLimit(t *testing.T) {
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(samGovResponse{})
	}))
	t.Cleanup(srv.Close)

	client := NewSAMGovClient("key")
	client.baseURL = srv.URL

	_, _ = client.Search(context.Background(), SearchQuery{Limit: 0})

	if captured.URL.Query().Get("limit") != "100" {
		t.Errorf("expected default limit 100, got %q", captured.URL.Query().Get("limit"))
	}
}

func TestSearch_NoDateRange(t *testing.T) {
	var captured *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(samGovResponse{})
	}))
	t.Cleanup(srv.Close)

	client := NewSAMGovClient("key")
	client.baseURL = srv.URL

	_, _ = client.Search(context.Background(), SearchQuery{})

	if captured.URL.Query().Get("postedFrom") != "" {
		t.Error("postedFrom should not be set when PostedAfter is zero")
	}
}

func TestNewSAMGovClient(t *testing.T) {
	c := NewSAMGovClient("test-key")
	if c == nil {
		t.Fatal("NewSAMGovClient returned nil")
	}
	if c.apiKey != "test-key" {
		t.Errorf("apiKey: got %q", c.apiKey)
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL: got %q", c.baseURL)
	}
	if c.http == nil {
		t.Error("http client should not be nil")
	}
}

func TestToOpportunities_EmptyOrg(t *testing.T) {
	raw := []samOpportunity{{NoticeID: "x", OrganizationHierarchy: nil}}
	opps := toOpportunities(raw)
	if len(opps) != 1 {
		t.Fatalf("expected 1, got %d", len(opps))
	}
	if opps[0].Agency != "" {
		t.Errorf("expected empty agency, got %q", opps[0].Agency)
	}
}

func TestSearch_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// slow response — context will cancel before this completes
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := NewSAMGovClient("key")
	client.baseURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := client.Search(ctx, SearchQuery{})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}
