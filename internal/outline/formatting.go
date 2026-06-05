package outline

import (
	"regexp"
	"strings"
)

// FormattingRules holds government formatting requirements extracted from a solicitation.
// Empty string fields and nil Artifacts mean the solicitation did not state that rule.
// Callers must check NotSpecified to distinguish "not stated" from "no value required."
type FormattingRules struct {
	Font         string   // typeface and size, e.g. "Times New Roman, 12pt"
	Margins      string   // margin spec, e.g. "1 inch on all sides"
	PageLimit    string   // page cap, e.g. "25 pages"
	Artifacts    []string // required attachments or deliverables
	NotSpecified []string // names of fields absent from the solicitation
}

var (
	reFont      = regexp.MustCompile(`(?i)(?:font:\s*|typed?\s+in\s+)([^.\n]{3,80})`)
	reMargins   = regexp.MustCompile(`(?i)margins?:\s*([^.\n]{3,80})`)
	rePageLimit = regexp.MustCompile(`(?i)(?:page\s+limit[:\s]+|not\s+to\s+exceed\s+|limited\s+to\s+)(\d[\d\s]*pages?)`)
	reArtifact  = regexp.MustCompile(`(?i)required\s+(?:attachments?|deliverables?|documents?)[:\s]+([^\n]+)`)
)

// ExtractFormattingRules parses raw solicitation text for stated formatting requirements.
// Any field not found in the text is listed in NotSpecified — rules are never invented.
func ExtractFormattingRules(solicitation string) FormattingRules {
	var r FormattingRules

	if m := reFont.FindStringSubmatch(solicitation); len(m) > 1 {
		r.Font = strings.TrimSpace(m[1])
	} else {
		r.NotSpecified = append(r.NotSpecified, "font")
	}

	if m := reMargins.FindStringSubmatch(solicitation); len(m) > 1 {
		r.Margins = strings.TrimSpace(m[1])
	} else {
		r.NotSpecified = append(r.NotSpecified, "margins")
	}

	if m := rePageLimit.FindStringSubmatch(solicitation); len(m) > 1 {
		r.PageLimit = strings.TrimSpace(m[1])
	} else {
		r.NotSpecified = append(r.NotSpecified, "page_limit")
	}

	if ms := reArtifact.FindAllStringSubmatch(solicitation, -1); len(ms) > 0 {
		for _, m := range ms {
			if len(m) > 1 {
				r.Artifacts = append(r.Artifacts, strings.TrimSpace(m[1]))
			}
		}
	} else {
		r.NotSpecified = append(r.NotSpecified, "artifacts")
	}

	return r
}
