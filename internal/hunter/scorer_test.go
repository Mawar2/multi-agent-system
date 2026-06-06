package hunter

import "testing"

func TestScore_GateBlocksNoMustKeywordMatch(t *testing.T) {
	cfg := ScoringConfig{
		MustKeywords: []string{"software"},
		MinScore:     1,
	}
	opp := &Opportunity{Title: "Road maintenance contract"}
	if s := Score(opp, cfg); s != 0 {
		t.Errorf("want 0 (gate blocked), got %.1f", s)
	}
}

func TestScore_GatePassesMustKeywordMatch(t *testing.T) {
	cfg := ScoringConfig{
		MustKeywords: []string{"software"},
		MinScore:     1,
	}
	opp := &Opportunity{Title: "Software development services"}
	if s := Score(opp, cfg); s < 1 {
		t.Errorf("want >= 1 (gate passed), got %.1f", s)
	}
}

func TestScore_GateSkippedWhenNoneConfigured(t *testing.T) {
	cfg := ScoringConfig{MinScore: 1}
	opp := &Opportunity{Title: "Anything at all"}
	if s := Score(opp, cfg); s < 1 {
		t.Errorf("want >= 1 (no gate), got %.1f", s)
	}
}

func TestScore_MustKeywordMatchedInDescription(t *testing.T) {
	cfg := ScoringConfig{MustKeywords: []string{"cloud"}}
	opp := &Opportunity{Title: "Support contract", Description: "Provide cloud hosting services"}
	if s := Score(opp, cfg); s < 1 {
		t.Errorf("want >= 1 (matched in description), got %.1f", s)
	}
}

func TestScore_BoostKeyword(t *testing.T) {
	cfg := ScoringConfig{
		BoostKeywords: []string{"AI"},
		BoostWeight:   10,
	}
	opp := &Opportunity{Title: "AI platform development"}
	if s := Score(opp, cfg); s != 11 {
		t.Errorf("want 11 (1 base + 10 boost), got %.1f", s)
	}
}

func TestScore_MultipleBoostKeywords(t *testing.T) {
	cfg := ScoringConfig{
		BoostKeywords: []string{"AI", "cloud"},
		BoostWeight:   10,
	}
	opp := &Opportunity{Title: "AI and cloud services"}
	if s := Score(opp, cfg); s != 21 {
		t.Errorf("want 21 (1 base + 2×10 boost), got %.1f", s)
	}
}

func TestScore_NAICSMatch(t *testing.T) {
	cfg := ScoringConfig{
		PreferNAICS: []string{"541511"},
		NAICSWeight: 15,
	}
	opp := &Opportunity{NAICSCode: "541511"}
	if s := Score(opp, cfg); s != 16 {
		t.Errorf("want 16 (1 base + 15 NAICS), got %.1f", s)
	}
}

func TestScore_NAICSNoMatchNoBonus(t *testing.T) {
	cfg := ScoringConfig{
		PreferNAICS: []string{"541511"},
		NAICSWeight: 15,
	}
	opp := &Opportunity{NAICSCode: "999999"}
	if s := Score(opp, cfg); s != 1 {
		t.Errorf("want 1 (base only), got %.1f", s)
	}
}

func TestScore_SmallBizSetAside(t *testing.T) {
	cfg := ScoringConfig{SmallBizWeight: 8}
	opp := &Opportunity{TypeOfSetAside: "Small Business"}
	if s := Score(opp, cfg); s != 9 {
		t.Errorf("want 9 (1 base + 8 small biz), got %.1f", s)
	}
}

func TestScore_SmallBizNA(t *testing.T) {
	cfg := ScoringConfig{SmallBizWeight: 8}
	opp := &Opportunity{TypeOfSetAside: "N/A"}
	if s := Score(opp, cfg); s != 1 {
		t.Errorf("want 1 (N/A gets no bonus), got %.1f", s)
	}
}

func TestFilter_SortsDescending(t *testing.T) {
	cfg := ScoringConfig{
		BoostKeywords: []string{"AI", "cloud"},
		BoostWeight:   10,
		MinScore:      1,
	}
	opps := []*Opportunity{
		{Title: "cloud services"}, // 11
		{Title: "AI and cloud"},   // 21
		{Title: "basic services"}, // 1
	}
	result := Filter(opps, cfg)
	if len(result) != 3 {
		t.Fatalf("want 3, got %d", len(result))
	}
	for i := 1; i < len(result); i++ {
		if result[i].Score > result[i-1].Score {
			t.Errorf("results not sorted descending: [%d]=%.1f > [%d]=%.1f",
				i, result[i].Score, i-1, result[i-1].Score)
		}
	}
}

func TestFilter_ExcludesBelowMinScore(t *testing.T) {
	cfg := ScoringConfig{
		BoostKeywords: []string{"AI"},
		BoostWeight:   10,
		MinScore:      5,
	}
	opps := []*Opportunity{
		{Title: "basic"}, // score 1, below MinScore 5
		{Title: "AI"},    // score 11, above MinScore
	}
	result := Filter(opps, cfg)
	if len(result) != 1 {
		t.Fatalf("want 1 result, got %d", len(result))
	}
	if result[0].Title != "AI" {
		t.Errorf("wrong opportunity passed filter: %s", result[0].Title)
	}
}

func TestFilter_SetsScoreField(t *testing.T) {
	cfg := ScoringConfig{
		BoostKeywords: []string{"cloud"},
		BoostWeight:   10,
		MinScore:      1,
	}
	opp := &Opportunity{Title: "cloud platform"}
	result := Filter([]*Opportunity{opp}, cfg)
	if len(result) != 1 {
		t.Fatal("expected 1 result")
	}
	if result[0].Score != 11 {
		t.Errorf("want score 11, got %.1f", result[0].Score)
	}
}

func TestDefaultScoringConfig(t *testing.T) {
	cfg := DefaultScoringConfig()
	if cfg.MinScore <= 0 {
		t.Error("want positive MinScore")
	}
	if len(cfg.MustKeywords) == 0 {
		t.Error("want non-empty MustKeywords")
	}
	if cfg.BoostWeight <= 0 {
		t.Error("want positive BoostWeight")
	}
}
