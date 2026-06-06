package hunter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fixture builds a minimal SearchResult JSON payload.
func fixture(total int, opps []Opportunity) string {
	r := SearchResult{TotalRecords: total, Opportunities: opps}
	b, _ := json.Marshal(r)
	return string(b)
}

// newClientWithHTTP creates a Client pointed at a custom base URL with a custom
// http.Client — used only in tests to wire up an httptest.Server.
func newClientWithHTTP(apiKey, baseURL string, httpClient *http.Client) *Client {
	return &Client{
		apiKey:     apiKey,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

// newTestClient returns a Client wired to srv.
func newTestClient(t *testing.T, apiKey string, srv *httptest.Server) *Client {
	t.Helper()
	return newClientWithHTTP(apiKey, srv.URL, srv.Client())
}

// ─── NewClient ────────────────────────────────────────────────────────────────

func TestNewClient(t *testing.T) {
	c := NewClient("test-key")
	if c == nil {
		t.Fatal("NewClient returned nil")
	}
	if c.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "test-key")
	}
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
	if c.httpClient.Timeout != defaultTimeout {
		t.Errorf("timeout = %v, want %v", c.httpClient.Timeout, defaultTimeout)
	}
}

// ─── Search: keyword ──────────────────────────────────────────────────────────

func TestSearch_Keywords(t *testing.T) {
	want := []Opportunity{{NoticeID: "N001", Title: "Cloud services"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("keyword") != "cloud" {
			http.Error(w, "bad keyword", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("api_key") == "" {
			http.Error(w, "missing api_key", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, fixture(1, want))
	}))
	defer srv.Close()

	c := newTestClient(t, "k1", srv)
	got, err := c.Search(context.Background(), SearchParams{Keywords: "cloud"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalRecords != 1 {
		t.Errorf("TotalRecords = %d, want 1", got.TotalRecords)
	}
	if len(got.Opportunities) != 1 || got.Opportunities[0].NoticeID != "N001" {
		t.Errorf("Opportunities = %v, want [{N001 ...}]", got.Opportunities)
	}
}

// ─── Search: NAICS filtering ──────────────────────────────────────────────────

func TestSearch_NAICSFilter(t *testing.T) {
	want := []Opportunity{{NoticeID: "N002", NaicsCode: "541511"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("naicsCode") != "541511" {
			http.Error(w, "wrong NAICS", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, fixture(1, want))
	}))
	defer srv.Close()

	c := newTestClient(t, "k2", srv)
	got, err := c.Search(context.Background(), SearchParams{NAICSCode: "541511"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalRecords != 1 || got.Opportunities[0].NaicsCode != "541511" {
		t.Errorf("unexpected result: %+v", got)
	}
}

// ─── Search: pagination ───────────────────────────────────────────────────────

func TestSearch_Pagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := r.URL.Query().Get("limit")
		offset := r.URL.Query().Get("offset")
		if limit != "5" || offset != "10" {
			http.Error(w, fmt.Sprintf("want limit=5&offset=10, got limit=%s&offset=%s", limit, offset), http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, fixture(100, []Opportunity{{NoticeID: "page-2"}}))
	}))
	defer srv.Close()

	c := newTestClient(t, "k3", srv)
	got, err := c.Search(context.Background(), SearchParams{Limit: 5, Offset: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalRecords != 100 {
		t.Errorf("TotalRecords = %d, want 100", got.TotalRecords)
	}
}

// ─── Search: default limit ────────────────────────────────────────────────────

func TestSearch_DefaultLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "10" {
			http.Error(w, "expected default limit=10", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, fixture(0, nil))
	}))
	defer srv.Close()

	c := newTestClient(t, "k4", srv)
	_, err := c.Search(context.Background(), SearchParams{}) // Limit zero → default
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── Search: date range ───────────────────────────────────────────────────────

func TestSearch_DateRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		from := r.URL.Query().Get("postedFrom")
		to := r.URL.Query().Get("postedTo")
		if from != "01/01/2025" || to != "12/31/2025" {
			http.Error(w, "wrong dates", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, fixture(0, nil))
	}))
	defer srv.Close()

	c := newTestClient(t, "k5", srv)
	_, err := c.Search(context.Background(), SearchParams{
		PostedFrom: "01/01/2025",
		PostedTo:   "12/31/2025",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ─── Search: field deserialisation ───────────────────────────────────────────

func TestSearch_FieldDeserialisation(t *testing.T) {
	archiveDate := "2026-12-31"
	setAside := "SBA"
	deadline := "2026-06-30T23:59:00-05:00"
	desc := "Software development services"

	payload := SearchResult{
		TotalRecords: 1,
		Opportunities: []Opportunity{
			{
				NoticeID:           "FULL-001",
				Title:              "Full field test",
				SolicitationNumber: "SOL-2026-001",
				FullParentPathName: "DoD/Army",
				PostedDate:         "2026-01-15",
				Type:               "Solicitation",
				BaseType:           "Solicitation",
				ArchiveType:        "auto30",
				ArchiveDate:        &archiveDate,
				SetAside:           &setAside,
				ResponseDeadline:   &deadline,
				NaicsCode:          "541512",
				ClassificationCode: "D307",
				Active:             "Yes",
				Award: &Award{
					Date:   "2026-03-01",
					Amount: 1500000.0,
					Number: "W123-456",
					Awardee: &Awardee{
						Name: "Acme Corp",
						Duns: "123456789",
						UEI:  "ACME1234567",
					},
				},
				Description:      &desc,
				OrganizationType: "OFFICE",
				UILink:           "https://sam.gov/opp/FULL-001/view",
			},
		},
	}

	b, _ := json.Marshal(payload)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(b)
	}))
	defer srv.Close()

	c := newTestClient(t, "k6", srv)
	got, err := c.Search(context.Background(), SearchParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Opportunities) != 1 {
		t.Fatalf("expected 1 opportunity, got %d", len(got.Opportunities))
	}

	opp := got.Opportunities[0]
	if opp.NoticeID != "FULL-001" {
		t.Errorf("NoticeID = %q, want FULL-001", opp.NoticeID)
	}
	if opp.Award == nil {
		t.Fatal("Award is nil")
	}
	if opp.Award.Amount != 1500000.0 {
		t.Errorf("Award.Amount = %v, want 1500000.0", opp.Award.Amount)
	}
	if opp.Award.Awardee == nil {
		t.Fatal("Award.Awardee is nil")
	}
	if opp.Award.Awardee.UEI != "ACME1234567" {
		t.Errorf("Awardee.UEI = %q, want ACME1234567", opp.Award.Awardee.UEI)
	}
	if opp.ArchiveDate == nil || *opp.ArchiveDate != archiveDate {
		t.Errorf("ArchiveDate = %v, want %q", opp.ArchiveDate, archiveDate)
	}
	if opp.Description == nil || *opp.Description != desc {
		t.Errorf("Description = %v, want %q", opp.Description, desc)
	}
}

// ─── Error paths ──────────────────────────────────────────────────────────────

func TestSearch_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := newTestClient(t, "bad-key", srv)
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention 401, got: %v", err)
	}
}

func TestSearch_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(t, "k7", srv)
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for 500 status, got nil")
	}
}

func TestSearch_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{not valid json`)
	}))
	defer srv.Close()

	c := newTestClient(t, "k8", srv)
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error should mention decode, got: %v", err)
	}
}

func TestSearch_InvalidBaseURL(t *testing.T) {
	// A URL containing a control character is rejected by http.NewRequestWithContext.
	c := &Client{
		apiKey:     "k",
		baseURL:    "http://host\x00bad",
		httpClient: &http.Client{Timeout: time.Second},
	}
	_, err := c.Search(context.Background(), SearchParams{})
	if err == nil {
		t.Fatal("expected error for invalid base URL, got nil")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("error should mention 'create request', got: %v", err)
	}
}

// ─── Context cancellation ─────────────────────────────────────────────────────

func TestSearch_ContextCancellation(t *testing.T) {
	// Server that blocks until the test cancels the context.
	ready := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(ready)
		<-r.Context().Done() // block until client disconnects
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := newTestClient(t, "k9", srv)

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Search(ctx, SearchParams{})
		errCh <- err
	}()

	<-ready  // wait until server has accepted the connection
	cancel() // cancel the context

	err := <-errCh
	if err == nil {
		t.Fatal("expected error after context cancellation, got nil")
	}
}

// ─── Empty result set ─────────────────────────────────────────────────────────

func TestSearch_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, fixture(0, nil))
	}))
	defer srv.Close()

	c := newTestClient(t, "k10", srv)
	got, err := c.Search(context.Background(), SearchParams{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalRecords != 0 {
		t.Errorf("TotalRecords = %d, want 0", got.TotalRecords)
	}
	if len(got.Opportunities) != 0 {
		t.Errorf("Opportunities = %v, want []", got.Opportunities)
	}
}

// ─── Combined keyword + NAICS ─────────────────────────────────────────────────

func TestSearch_KeywordsAndNAICS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("keyword") != "AI" || r.URL.Query().Get("naicsCode") != "541715" {
			http.Error(w, "wrong params", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, fixture(2, []Opportunity{
			{NoticeID: "AI-001", NaicsCode: "541715"},
			{NoticeID: "AI-002", NaicsCode: "541715"},
		}))
	}))
	defer srv.Close()

	c := newTestClient(t, "k11", srv)
	got, err := c.Search(context.Background(), SearchParams{Keywords: "AI", NAICSCode: "541715"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.TotalRecords != 2 || len(got.Opportunities) != 2 {
		t.Errorf("unexpected result: total=%d opps=%d", got.TotalRecords, len(got.Opportunities))
	}
}
