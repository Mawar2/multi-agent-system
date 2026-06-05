// Package outline implements the Outline agent for the Kaimi pipeline.
//
// The Outline agent (Zone 2) generates a document outline for a selected
// opportunity. It extracts government formatting requirements from the
// solicitation description and attaches them to the result so the writing
// team has them up front.
//
// Formatting rules are extracted by ExtractFormattingRules, which parses
// the solicitation text for font, margin, page-limit, and required-artifact
// specifications. Any field not explicitly stated is listed in
// FormattingRules.NotSpecified — rules are never invented.
package outline
