// Package hunter implements the Hunter agent, which polls SAM.gov for federal
// contract opportunities, scores them for relevance, and creates GitHub issues
// in a tracking repository for each promising find.
package hunter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Config controls the Hunter agent's behavior.
type Config struct {
	// SAMAPIKey is the api.sam.gov API key.
	SAMAPIKey string `yaml:"sam_api_key"`

	// GitHubToken is used to create issues in the tracking repo.
	GitHubToken string `yaml:"github_token"`

	// TrackingRepo is the GitHub repo where opportunity issues are filed, e.g. "Mawar2/bd-tracker".
	TrackingRepoOwner string `yaml:"tracking_repo_owner"`
	TrackingRepoName  string `yaml:"tracking_repo_name"`

	// Search controls what SAM.gov returns.
	Search SearchConfig `yaml:"search"`

	// Scoring controls opportunity filtering and ranking.
	Scoring ScoringConfig `yaml:"scoring"`

	// PollIntervalMinutes is how often to query SAM.gov (default 60).
	PollIntervalMinutes int `yaml:"poll_interval_minutes"`

	// LookbackDays is how far back to fetch newly posted opportunities (default 1).
	LookbackDays int `yaml:"lookback_days"`
}

// SearchConfig mirrors SAM.gov SearchParams but is YAML-serializable.
type SearchConfig struct {
	Keywords   []string `yaml:"keywords"`
	NAICSCodes []string `yaml:"naics_codes"`
	// Types: "o"=solicitation, "p"=presolicitation, "r"=sources sought, "s"=special notice
	Types []string `yaml:"types"`
}

func (c *Config) applyDefaults() {
	if c.PollIntervalMinutes <= 0 {
		c.PollIntervalMinutes = 60
	}
	if c.LookbackDays <= 0 {
		c.LookbackDays = 1
	}
	if c.Scoring.MinScore == 0 {
		c.Scoring = DefaultScoringConfig()
	}
}

// Hunter polls SAM.gov and files GitHub issues for relevant opportunities.
type Hunter struct {
	cfg       Config
	sam       *SAMClient
	seen      map[string]struct{} // noticeIDs already filed
	ghHTTP    *http.Client
	ghBaseURL string // overrideable for tests; default "https://api.github.com"
}

// New creates a Hunter from config.
func New(cfg Config) *Hunter {
	cfg.applyDefaults()
	return &Hunter{
		cfg:       cfg,
		sam:       NewSAMClient(cfg.SAMAPIKey),
		seen:      make(map[string]struct{}),
		ghHTTP:    &http.Client{Timeout: 15 * time.Second},
		ghBaseURL: "https://api.github.com",
	}
}

// Run starts the Hunter loop and blocks until ctx is cancelled.
func (h *Hunter) Run(ctx context.Context) {
	log.Printf("[Hunter] Starting — tracking repo: %s/%s, poll interval: %dm",
		h.cfg.TrackingRepoOwner, h.cfg.TrackingRepoName, h.cfg.PollIntervalMinutes)

	// First tick immediately.
	h.tick(ctx)

	ticker := time.NewTicker(time.Duration(h.cfg.PollIntervalMinutes) * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[Hunter] Stopping")
			return
		case <-ticker.C:
			h.tick(ctx)
		}
	}
}

// tick performs one poll-score-file cycle.
func (h *Hunter) tick(ctx context.Context) {
	log.Println("[Hunter] Polling SAM.gov...")

	params := SearchParams{
		Keywords:   h.cfg.Search.Keywords,
		NAICSCodes: h.cfg.Search.NAICSCodes,
		Types:      h.cfg.Search.Types,
		PostedFrom: time.Now().AddDate(0, 0, -h.cfg.LookbackDays),
		PostedTo:   time.Now(),
	}

	opps, err := h.sam.Search(ctx, params)
	if err != nil {
		log.Printf("[Hunter] SAM.gov search error: %v", err)
		return
	}
	log.Printf("[Hunter] Fetched %d opportunities from SAM.gov", len(opps))

	relevant := Filter(opps, h.cfg.Scoring)
	log.Printf("[Hunter] %d passed scoring filter (min score %.1f)", len(relevant), h.cfg.Scoring.MinScore)

	filed := 0
	for _, opp := range relevant {
		if _, ok := h.seen[opp.NoticeID]; ok {
			continue
		}
		if err := h.fileIssue(ctx, opp); err != nil {
			log.Printf("[Hunter] Failed to file issue for %s: %v", opp.NoticeID, err)
			continue
		}
		h.seen[opp.NoticeID] = struct{}{}
		filed++
	}
	log.Printf("[Hunter] Filed %d new GitHub issues", filed)
}

// fileIssue creates a GitHub issue in the tracking repo for an opportunity.
func (h *Hunter) fileIssue(ctx context.Context, opp *Opportunity) error {
	title := fmt.Sprintf("[SAM.gov] %s", opp.Title)
	body := buildIssueBody(opp)

	payload := map[string]any{
		"title":  title,
		"body":   body,
		"labels": []string{"opportunity", "sam-gov"},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	apiURL := fmt.Sprintf("%s/repos/%s/%s/issues",
		h.ghBaseURL, h.cfg.TrackingRepoOwner, h.cfg.TrackingRepoName)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+h.cfg.GitHubToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := h.ghHTTP.Do(req)
	if err != nil {
		return fmt.Errorf("github request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github HTTP %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	log.Printf("[Hunter] Filed issue: %s (score %.1f, deadline %s)",
		opp.NoticeID, opp.Score, opp.ResponseDeadline)
	return nil
}

// buildIssueBody renders a GitHub issue body for an opportunity.
func buildIssueBody(opp *Opportunity) string {
	days := opp.DaysUntilDeadline()
	deadlineStr := opp.ResponseDeadline
	if days >= 0 {
		deadlineStr = fmt.Sprintf("%s (%d days)", opp.ResponseDeadline, days)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## Federal Contract Opportunity\n\n")
	fmt.Fprintf(&sb, "| Field | Value |\n|---|---|\n")
	fmt.Fprintf(&sb, "| **Notice ID** | `%s` |\n", opp.NoticeID)
	fmt.Fprintf(&sb, "| **Solicitation #** | `%s` |\n", opp.SolicitationNumber)
	fmt.Fprintf(&sb, "| **Type** | %s |\n", opp.Type)
	fmt.Fprintf(&sb, "| **Department** | %s |\n", opp.Department)
	fmt.Fprintf(&sb, "| **Agency** | %s |\n", opp.SubTier)
	fmt.Fprintf(&sb, "| **NAICS** | %s — %s |\n", opp.NAICSCode, opp.NAICSDescription)
	fmt.Fprintf(&sb, "| **Set-Aside** | %s |\n", opp.TypeOfSetAside)
	fmt.Fprintf(&sb, "| **Posted** | %s |\n", opp.PostedDate)
	fmt.Fprintf(&sb, "| **Response Deadline** | %s |\n", deadlineStr)
	fmt.Fprintf(&sb, "| **Relevance Score** | %.1f |\n", opp.Score)
	fmt.Fprintf(&sb, "| **SAM.gov Link** | [View Opportunity](%s) |\n\n", opp.UILink)

	if opp.Description != "" {
		fmt.Fprintf(&sb, "## Description\n\n%s\n\n", truncate(opp.Description, 2000))
	}

	fmt.Fprintf(&sb, "## Action Items\n\n")
	fmt.Fprintf(&sb, "- [ ] Review full solicitation on SAM.gov\n")
	fmt.Fprintf(&sb, "- [ ] Assess fit against capabilities\n")
	fmt.Fprintf(&sb, "- [ ] Identify teaming partners if needed\n")
	fmt.Fprintf(&sb, "- [ ] Decide: bid / no-bid\n")
	fmt.Fprintf(&sb, "- [ ] Draft capability statement if bidding\n")
	fmt.Fprintf(&sb, "\n---\n*Discovered by Hunter agent · %s*\n", time.Now().Format("2006-01-02"))

	return sb.String()
}
