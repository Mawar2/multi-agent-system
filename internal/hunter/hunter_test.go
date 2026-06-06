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

// ------- SAMClient tests -------

func TestSAMClientSearch_success(t *testing.T) {
	want := []*Opportunity{
		{NoticeID: "abc123", Title: "Cloud Software Services", NAICSCode: "541511"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") == "" {
			t.Error("expected api_key query param")
		}
		json.NewEncoder(w).Encode(samResponse{ //nolint:errcheck
			TotalRecords:      1,
			OpportunitiesData: want,
		})
	}))
	defer srv.Close()

	client := NewSAMClient("test-key")
	client.baseURL = srv.URL

	got, err := client.Search(context.Background(), SearchParams{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].NoticeID != "abc123" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestSAMClientSearch_httpError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid api_key"}`)) //nolint:errcheck
	}))
	defer srv.Close()

	client := NewSAMClient("bad-key")
	client.baseURL = srv.URL

	_, err := client.Search(context.Background(), SearchParams{Limit: 10})
	if err == nil {
		t.Fatal("expected error for HTTP 401")
	}
}

func TestSAMClientSearch_pagination(t *testing.T) {
	// Return 2 pages: first has 2 items (limit=2), second has 1 item.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		offset := r.URL.Query().Get("offset")
		var opps []*Opportunity
		total := 3
		if offset == "0" || offset == "" {
			opps = []*Opportunity{
				{NoticeID: "a1"}, {NoticeID: "a2"},
			}
		} else {
			opps = []*Opportunity{{NoticeID: "a3"}}
		}
		json.NewEncoder(w).Encode(samResponse{ //nolint:errcheck
			TotalRecords:      total,
			OpportunitiesData: opps,
		})
	}))
	defer srv.Close()

	client := NewSAMClient("key")
	client.baseURL = srv.URL

	got, err := client.Search(context.Background(), SearchParams{Limit: 2})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 results, got %d", len(got))
	}
	if calls != 2 {
		t.Errorf("expected 2 HTTP calls for pagination, got %d", calls)
	}
}

// ------- Opportunity tests -------

func TestOpportunity_DaysUntilDeadline(t *testing.T) {
	future := time.Now().Add(72 * time.Hour).Format(time.RFC3339)
	opp := &Opportunity{ResponseDeadline: future}
	days := opp.DaysUntilDeadline()
	if days < 2 || days > 4 {
		t.Errorf("expected ~3 days, got %d", days)
	}

	opp.ResponseDeadline = ""
	if opp.DaysUntilDeadline() != -1 {
		t.Error("empty deadline should return -1")
	}
}

func TestOpportunity_IsSmallBusinessSetAside(t *testing.T) {
	opp := &Opportunity{TypeOfSetAside: "Total Small Business"}
	if !opp.IsSmallBusinessSetAside() {
		t.Error("expected true for small business set-aside")
	}
	opp.TypeOfSetAside = ""
	if opp.IsSmallBusinessSetAside() {
		t.Error("expected false when TypeOfSetAside is empty")
	}
}

// ------- Scorer tests -------

func TestFilter_noMustKeywordMatch(t *testing.T) {
	cfg := DefaultScoringConfig()
	opps := []*Opportunity{
		{NoticeID: "x1", Title: "Janitorial Services", Description: "cleaning floors"},
	}
	out := Filter(opps, cfg)
	if len(out) != 0 {
		t.Errorf("expected 0 results when no must-keyword matches, got %d", len(out))
	}
}

func TestFilter_mustKeywordMatch(t *testing.T) {
	cfg := DefaultScoringConfig()
	opps := []*Opportunity{
		{NoticeID: "x1", Title: "Cloud Software Development", NAICSCode: "541511"},
	}
	out := Filter(opps, cfg)
	if len(out) != 1 {
		t.Errorf("expected 1 result, got %d", len(out))
	}
	if out[0].Score <= 0 {
		t.Errorf("expected positive score, got %.1f", out[0].Score)
	}
}

func TestFilter_sortedDescending(t *testing.T) {
	cfg := DefaultScoringConfig()
	// Second opp has more boost keywords → higher score.
	opps := []*Opportunity{
		{NoticeID: "low", Title: "software services"},
		{NoticeID: "high", Title: "AI machine learning cloud software", NAICSCode: "541511"},
	}
	out := Filter(opps, cfg)
	if len(out) < 2 {
		t.Fatalf("expected 2 results, got %d", len(out))
	}
	if out[0].Score < out[1].Score {
		t.Errorf("results not sorted descending: %.1f < %.1f", out[0].Score, out[1].Score)
	}
}

func TestFilter_minScore(t *testing.T) {
	cfg := DefaultScoringConfig()
	cfg.MinScore = 1000 // impossibly high
	opps := []*Opportunity{
		{NoticeID: "x1", Title: "software"},
	}
	out := Filter(opps, cfg)
	if len(out) != 0 {
		t.Errorf("expected 0 results with min score 1000")
	}
}

// ------- Issue body test -------

func TestBuildIssueBody_containsNoticeID(t *testing.T) {
	opp := &Opportunity{
		NoticeID:   "test-notice-42",
		Title:      "IT Modernization",
		Department: "Dept of Defense",
		UILink:     "https://sam.gov/opp/test-notice-42/view",
		Score:      25.5,
		NAICSCode:  "541511",
	}
	body := buildIssueBody(opp)
	if !contains(body, "test-notice-42") {
		t.Error("body missing notice ID")
	}
	if !contains(body, "SAM.gov Link") {
		t.Error("body missing SAM.gov link")
	}
	if !contains(body, "Action Items") {
		t.Error("body missing action items")
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
