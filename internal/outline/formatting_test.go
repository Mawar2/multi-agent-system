package outline

import "testing"

const solicitationFull = `
Proposals shall be typed in Times New Roman, 12pt.
Margins: 1 inch on all sides.
Page limit: 25 pages.
Required documents: Past Performance Volume, Technical Approach.
`

const solicitationPartial = `
Font: Arial, 11pt.
Not to exceed 30 pages.
No margins or attachments stated here.
`

const solicitationEmpty = `This solicitation provides no formatting guidance whatsoever.`

// TestExtractFormattingRules_AllPresent verifies that a solicitation with all
// four formatting fields (font, margins, page limit, artifacts) populates every
// field and leaves NotSpecified empty.
func TestExtractFormattingRules_AllPresent(t *testing.T) {
	rules := ExtractFormattingRules(solicitationFull)

	if rules.Font == "" {
		t.Error("Font should be extracted from solicitation")
	}
	if rules.Margins == "" {
		t.Error("Margins should be extracted from solicitation")
	}
	if rules.PageLimit == "" {
		t.Error("PageLimit should be extracted from solicitation")
	}
	if len(rules.Artifacts) == 0 {
		t.Error("Artifacts should be extracted from solicitation")
	}
	if len(rules.NotSpecified) != 0 {
		t.Errorf("NotSpecified should be empty when all rules are present, got %v", rules.NotSpecified)
	}
}

// TestExtractFormattingRules_NonePresent verifies that a solicitation with no
// formatting guidance leaves all value fields empty and lists all four fields
// in NotSpecified.
func TestExtractFormattingRules_NonePresent(t *testing.T) {
	rules := ExtractFormattingRules(solicitationEmpty)

	if rules.Font != "" {
		t.Errorf("Font should be empty, got %q", rules.Font)
	}
	if rules.Margins != "" {
		t.Errorf("Margins should be empty, got %q", rules.Margins)
	}
	if rules.PageLimit != "" {
		t.Errorf("PageLimit should be empty, got %q", rules.PageLimit)
	}
	if len(rules.Artifacts) != 0 {
		t.Errorf("Artifacts should be empty, got %v", rules.Artifacts)
	}
	if len(rules.NotSpecified) != 4 {
		t.Errorf("all 4 fields should be in NotSpecified, got %v", rules.NotSpecified)
	}
}

// TestExtractFormattingRules_Partial verifies that a solicitation with only
// some formatting fields correctly populates found rules and lists absent ones
// in NotSpecified.
func TestExtractFormattingRules_Partial(t *testing.T) {
	rules := ExtractFormattingRules(solicitationPartial)

	if rules.Font == "" {
		t.Error("Font should be extracted from partial solicitation")
	}
	if rules.PageLimit == "" {
		t.Error("PageLimit should be extracted from partial solicitation")
	}

	notSpec := make(map[string]bool, len(rules.NotSpecified))
	for _, s := range rules.NotSpecified {
		notSpec[s] = true
	}
	if !notSpec["margins"] {
		t.Errorf("margins should be in NotSpecified for partial solicitation, got %v", rules.NotSpecified)
	}
	if !notSpec["artifacts"] {
		t.Errorf("artifacts should be in NotSpecified for partial solicitation, got %v", rules.NotSpecified)
	}
	if notSpec["font"] {
		t.Error("font should NOT be in NotSpecified — it was found in the solicitation")
	}
	if notSpec["page_limit"] {
		t.Error("page_limit should NOT be in NotSpecified — it was found in the solicitation")
	}
}
