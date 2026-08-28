package mcp

import (
	"strings"
	"testing"
)

func promptNamed(t *testing.T, name string) promptDef {
	t.Helper()
	for _, p := range prompts {
		if p.name == name {
			return p
		}
	}
	t.Fatalf("no prompt named %q", name)
	return promptDef{}
}

func TestPromptsFallBackWhenTheArgumentIsBlank(t *testing.T) {
	for _, p := range prompts {
		t.Run(p.name, func(t *testing.T) {
			blank := p.render(map[string]string{p.arg: "   "})
			if !strings.Contains(blank, p.fallback) {
				t.Errorf("blank argument did not fall back to %q", p.fallback)
			}
			if !strings.Contains(p.render(nil), p.fallback) {
				t.Errorf("missing argument did not fall back to %q", p.fallback)
			}
			given := p.render(map[string]string{p.arg: "GIVEN"})
			if !strings.Contains(given, "GIVEN") {
				t.Errorf("argument was ignored: %s", given)
			}
			if strings.Contains(given, emDash) {
				t.Error("prompt contains an em-dash")
			}
		})
	}
}

func TestPromptsNameTheToolsTheySequence(t *testing.T) {
	cases := map[string][]string{
		"monthly_bill_review":         {"get-cost-summary", "get-cost-growth", "get-cost-series"},
		"quarterly_commitment_review": {"get-commitment-overview", "list-commitments"},
		"waste_by_team":               {"get-costs-by-tag", "list-recommendations"},
	}
	for name, wanted := range cases {
		body := promptNamed(t, name).render(nil)
		for _, tool := range wanted {
			if !strings.Contains(body, tool) {
				t.Errorf("prompt %s does not call %s", name, tool)
			}
		}
	}
}
