package output

import (
	"strings"
	"unicode/utf8"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

// Sanitize makes text that came from a template, a file name or the API safe
// to print on one line: every C0 and C1 control character (ESC and BEL among
// them) and every invalid byte becomes U+FFFD, so a crafted name cannot move
// the cursor, clear the screen or plant a hyperlink in the terminal.
func Sanitize(s string) string {
	return sanitize(s, false)
}

// SanitizeMultiline is Sanitize for text that may span lines: it keeps newlines
// and tabs.
func SanitizeMultiline(s string) string {
	return sanitize(s, true)
}

func sanitize(s string, multiline bool) string {
	return strings.Map(func(r rune) rune {
		switch {
		case multiline && (r == '\n' || r == '\t'):
			return r
		case r < 0x20, r >= 0x7f && r <= 0x9f:
			return utf8.RuneError
		}
		return r
	}, s)
}

func sanitizedPtr(s *string) *string {
	if s == nil {
		return nil
	}
	clean := Sanitize(*s)
	return &clean
}

// sanitizedResponse copies the response with every string a renderer prints
// sanitized. JSON output prints the response itself, which encoding escapes.
func sanitizedResponse(data *api.AnalyzePrResponse) *api.AnalyzePrResponse {
	clean := *data
	clean.ResourceCostEstimates = make([]*api.ResourceCostEstimate, len(data.ResourceCostEstimates))
	for i, e := range data.ResourceCostEstimates {
		clean.ResourceCostEstimates[i] = sanitizedEstimate(e)
	}
	clean.UpgradeSuggestions = make([]*api.UpgradeSuggestion, len(data.UpgradeSuggestions))
	for i, s := range data.UpgradeSuggestions {
		clean.UpgradeSuggestions[i] = sanitizedSuggestion(s)
	}
	clean.Diagnostics = make([]*api.Diagnostic, len(data.Diagnostics))
	for i, d := range data.Diagnostics {
		clean.Diagnostics[i] = &api.Diagnostic{Code: Sanitize(d.Code), Message: Sanitize(d.Message), File: sanitizedPtr(d.File), Line: d.Line}
	}
	return &clean
}

func sanitizedEstimate(e *api.ResourceCostEstimate) *api.ResourceCostEstimate {
	clean := *e
	clean.ResourceType = Sanitize(e.ResourceType)
	clean.ResourceName = Sanitize(e.ResourceName)
	clean.Service = Sanitize(e.Service)
	clean.ModulePath = sanitizedPtr(e.ModulePath)
	clean.Scope = sanitizedPtr(e.Scope)
	clean.Note = sanitizedPtr(e.Note)
	clean.Components = make([]*api.CostComponentEstimate, len(e.Components))
	for i, c := range e.Components {
		component := *c
		component.Name = Sanitize(c.Name)
		component.UnitLabel = sanitizedPtr(c.UnitLabel)
		component.Subresource = sanitizedPtr(c.Subresource)
		component.Attribute = sanitizedPtr(c.Attribute)
		clean.Components[i] = &component
	}
	return &clean
}

func sanitizedSuggestion(s *api.UpgradeSuggestion) *api.UpgradeSuggestion {
	clean := *s
	clean.ResourceType = Sanitize(s.ResourceType)
	clean.ResourceName = Sanitize(s.ResourceName)
	clean.File = Sanitize(s.File)
	clean.Attribute = Sanitize(s.Attribute)
	clean.CurrentValue = Sanitize(s.CurrentValue)
	clean.SuggestedValue = Sanitize(s.SuggestedValue)
	clean.Category = Sanitize(s.Category)
	clean.Reason = Sanitize(s.Reason)
	if s.Note != nil {
		note := SanitizeMultiline(*s.Note)
		clean.Note = &note
	}
	clean.ModulePath = sanitizedPtr(s.ModulePath)
	clean.Scope = sanitizedPtr(s.Scope)
	if s.Location != nil {
		clean.Location = &api.SourceLocation{File: Sanitize(s.Location.File), Line: s.Location.Line}
	}
	clean.CompanionAttributes = make([]*api.CompanionAttribute, len(s.CompanionAttributes))
	for i, c := range s.CompanionAttributes {
		clean.CompanionAttributes[i] = &api.CompanionAttribute{Attribute: Sanitize(c.Attribute), Value: Sanitize(c.Value), Reason: Sanitize(c.Reason)}
	}
	if s.GravitonAlternative != nil {
		alternative := *s.GravitonAlternative
		alternative.SuggestedValue = Sanitize(alternative.SuggestedValue)
		alternative.Reason = Sanitize(alternative.Reason)
		clean.GravitonAlternative = &alternative
	}
	return &clean
}
