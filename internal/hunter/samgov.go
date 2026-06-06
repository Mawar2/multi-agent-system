package hunter

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.sam.gov/opportunities/v2/search"
	defaultLimit   = 100
	dateLayout     = "01/02/2006"
)

// SAMGovClient queries the SAM.gov Opportunities v2 API.
type SAMGovClient struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

// NewSAMGovClient creates a client using the provided SAM.gov API key.
func NewSAMGovClient(apiKey string) *SAMGovClient {
	return &SAMGovClient{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// samGovResponse maps the SAM.gov API JSON envelope.
type samGovResponse struct {
	TotalRecords      int              `json:"totalRecords"`
	OpportunitiesData []samOpportunity `json:"opportunitiesData"`
}

// samOpportunity maps a single record from the SAM.gov API.
type samOpportunity struct {
	NoticeID              string `json:"noticeId"`
	Title                 string `json:"title"`
	Type                  string `json:"type"`
	PostedDate            string `json:"postedDate"`
	ResponseDeadLine      string `json:"responseDeadLine"`
	NAICSCode             string `json:"naicsCode"`
	OrganizationHierarchy []struct {
		Name string `json:"name"`
	} `json:"organizationHierarchy"`
	Description    string `json:"description"`
	UILink         string `json:"uiLink"`
	TypeOfSetAside string `json:"typeOfSetAside"`
}

// Search queries SAM.gov and returns matching opportunities.
func (c *SAMGovClient) Search(ctx context.Context, q SearchQuery) ([]Opportunity, error) {
	params := c.buildParams(q)

	reqURL := c.baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("SAM.gov request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SAM.gov returned status %d", resp.StatusCode)
	}

	var result samGovResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return toOpportunities(result.OpportunitiesData), nil
}

func (c *SAMGovClient) buildParams(q SearchQuery) url.Values {
	p := url.Values{}
	p.Set("api_key", c.apiKey)

	limit := q.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	p.Set("limit", strconv.Itoa(limit))

	if !q.PostedAfter.IsZero() {
		p.Set("postedFrom", q.PostedAfter.Format(dateLayout))
		p.Set("postedTo", time.Now().Format(dateLayout))
	}

	if len(q.Keywords) > 0 {
		p.Set("keywords", strings.Join(q.Keywords, " "))
	}

	if len(q.NAICSCodes) > 0 {
		p.Set("naicsCode", strings.Join(q.NAICSCodes, ","))
	}

	if len(q.SetAsides) > 0 {
		p.Set("typeOfSetAside", strings.Join(q.SetAsides, ","))
	}

	return p
}

func toOpportunities(raw []samOpportunity) []Opportunity {
	out := make([]Opportunity, 0, len(raw))
	for _, r := range raw {
		agency := ""
		if len(r.OrganizationHierarchy) > 0 {
			agency = r.OrganizationHierarchy[0].Name
		}
		out = append(out, Opportunity{
			NoticeID:         r.NoticeID,
			Title:            r.Title,
			Type:             r.Type,
			PostedDate:       r.PostedDate,
			ResponseDeadline: r.ResponseDeadLine,
			NAICSCode:        r.NAICSCode,
			Agency:           agency,
			Description:      r.Description,
			UIURL:            r.UILink,
			SetAside:         r.TypeOfSetAside,
		})
	}
	return out
}
