package hunter

import "strings"

// ScoringConfig controls how opportunities are ranked.
type ScoringConfig struct {
	// MustKeywords: opportunity must contain at least one of these to score > 0.
	// Case-insensitive substring match against title + description.
	MustKeywords []string `yaml:"must_keywords"`

	// BoostKeywords: each match adds BoostWeight to the score.
	BoostKeywords []string `yaml:"boost_keywords"`
	BoostWeight   float64  `yaml:"boost_weight"`

	// PreferNAICS: matching NAICS code adds NAICSWeight.
	PreferNAICS []string `yaml:"prefer_naics"`
	NAICSWeight float64  `yaml:"naics_weight"`

	// DeadlineUrgencyDays: opportunities with fewer days remaining get extra weight.
	DeadlineUrgencyDays int     `yaml:"deadline_urgency_days"`
	UrgencyWeight       float64 `yaml:"urgency_weight"`

	// SmallBizWeight: bonus for small-business set-asides.
	SmallBizWeight float64 `yaml:"small_biz_weight"`

	// MinScore: opportunities below this threshold are excluded.
	MinScore float64 `yaml:"min_score"`
}

// DefaultScoringConfig returns a sensible default tuned for software / IT BD.
func DefaultScoringConfig() ScoringConfig {
	return ScoringConfig{
		MustKeywords: []string{
			"software", "cloud", "data", "AI", "artificial intelligence",
			"machine learning", "analytics", "platform", "digital", "IT services",
			"information technology", "DevOps", "agile", "API", "microservices",
			"cybersecurity", "security", "SaaS",
		},
		BoostKeywords: []string{
			"AI", "artificial intelligence", "machine learning", "cloud-native",
			"DevSecOps", "zero trust", "modernization", "automation",
		},
		BoostWeight:         10,
		PreferNAICS:         []string{"541511", "541512", "541513", "541519", "518210", "334111"},
		NAICSWeight:         15,
		DeadlineUrgencyDays: 14,
		UrgencyWeight:       5,
		SmallBizWeight:      8,
		MinScore:            1,
	}
}

// Score computes a relevance score for an opportunity.
// Returns 0 if MustKeywords is non-empty and none match.
func Score(opp *Opportunity, cfg ScoringConfig) float64 {
	haystack := strings.ToLower(opp.Title + " " + opp.Description)

	// Gate: must contain at least one must-keyword.
	if len(cfg.MustKeywords) > 0 {
		found := false
		for _, kw := range cfg.MustKeywords {
			if strings.Contains(haystack, strings.ToLower(kw)) {
				found = true
				break
			}
		}
		if !found {
			return 0
		}
	}

	score := 1.0 // base for passing the gate

	// Boost keywords.
	for _, kw := range cfg.BoostKeywords {
		if strings.Contains(haystack, strings.ToLower(kw)) {
			score += cfg.BoostWeight
		}
	}

	// NAICS match.
	for _, n := range cfg.PreferNAICS {
		if opp.NAICSCode == n {
			score += cfg.NAICSWeight
			break
		}
	}

	// Deadline urgency.
	days := opp.DaysUntilDeadline()
	if days >= 0 && days <= cfg.DeadlineUrgencyDays {
		score += cfg.UrgencyWeight
	}

	// Small business set-aside.
	if opp.IsSmallBusinessSetAside() {
		score += cfg.SmallBizWeight
	}

	return score
}

// Filter scores each opportunity, sets its Score field, and returns those
// with score >= cfg.MinScore, sorted descending by score.
func Filter(opps []*Opportunity, cfg ScoringConfig) []*Opportunity {
	var out []*Opportunity
	for _, o := range opps {
		s := Score(o, cfg)
		if s >= cfg.MinScore {
			o.Score = s
			out = append(out, o)
		}
	}
	// Insertion sort — result sets are small (hundreds at most).
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Score > out[j-1].Score; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
