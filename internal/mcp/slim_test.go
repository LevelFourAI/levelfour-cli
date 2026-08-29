package mcp

import (
	"strings"
	"testing"
)

func TestSlimRecommendationDropsOperatorArtifacts(t *testing.T) {
	detail := map[string]any{
		"recommendation_id":   "REC-1234",
		"iam_policy":          map[string]any{"Statement": []any{}},
		"iam_role_arn":        "arn:aws:iam::1:role/x",
		"manual_instructions": "do the thing",
		"actions": map[string]any{
			"description": "kept",
			"metrics":     "duplicated at the top level",
			"iam_policy":  "also an operator artifact",
		},
	}
	got := slimRecommendation(detail)

	for _, dropped := range []string{"iam_policy", "iam_role_arn", "manual_instructions"} {
		if _, present := got[dropped]; present {
			t.Errorf("slim kept %q", dropped)
		}
	}
	actions := got["actions"].(map[string]any)
	if actions["description"] != "kept" {
		t.Error("slim dropped actions.description")
	}
	for _, dropped := range []string{"metrics", "iam_policy"} {
		if _, present := actions[dropped]; present {
			t.Errorf("slim kept actions.%s", dropped)
		}
	}
	if got["recommendation_id"] != "REC-1234" {
		t.Error("slim dropped the id")
	}
}

func TestSlimRecommendationLeavesUnexpectedShapesAlone(t *testing.T) {
	got := slimRecommendation(map[string]any{"actions": "not an object", "metrics": "not a list"})
	if got["actions"] != "not an object" || got["metrics"] != "not a list" {
		t.Errorf("slim rewrote a payload it did not understand: %v", got)
	}
}

func TestSlimRecommendationCapsMetricSeries(t *testing.T) {
	points := make([]any, maxMetricPoints+5)
	for i := range points {
		points[i] = i
	}
	detail := map[string]any{"metrics": []any{
		map[string]any{"name": "cpu", "data_points": points},
		map[string]any{"name": "short", "data_points": []any{1, 2}},
		map[string]any{"name": "no points"},
		"not a series",
	}}

	series := slimRecommendation(detail)["metrics"].([]any)

	capped := series[0].(map[string]any)
	kept := capped["data_points"].([]any)
	if len(kept) != maxMetricPoints {
		t.Errorf("kept %d points, want %d", len(kept), maxMetricPoints)
	}
	if kept[0] != 5 {
		t.Errorf("kept the oldest points, want the most recent: first = %v", kept[0])
	}
	if !strings.Contains(capped["truncated"].(string), "most recent 60 of 65") {
		t.Errorf("truncation note = %v", capped["truncated"])
	}

	if _, present := series[1].(map[string]any)["truncated"]; present {
		t.Error("a short series was marked truncated")
	}
	if series[3] != "not a series" {
		t.Error("a non-series entry was rewritten")
	}
}

func TestShapeWhoamiRenamesProviderList(t *testing.T) {
	got := shapeWhoami(map[string]any{
		"organization": "org_acme",
		"plan":         "Pro",
		"role":         "api-key",
		"accounts": []any{
			map[string]any{"provider": "aws"},
			map[string]any{"provider": "gcp"},
			map[string]any{"provider": ""},
			map[string]any{"name": "no provider field"},
			"not an account",
		},
	})

	providers := got["enabled_providers"].([]any)
	if len(providers) != 2 || providers[0] != "aws" || providers[1] != "gcp" {
		t.Errorf("enabled_providers = %v", providers)
	}
	if got["organization"] != "org_acme" || got["plan"] != "Pro" || got["role"] != "api-key" {
		t.Errorf("shapeWhoami = %v", got)
	}
	if got["served_by"] == nil {
		t.Error("shapeWhoami did not say which surface answered")
	}
}

func TestShapeWhoamiWithNoAccounts(t *testing.T) {
	got := shapeWhoami(map[string]any{"accounts": "not a list"})
	if len(got["enabled_providers"].([]any)) != 0 {
		t.Errorf("enabled_providers = %v, want empty", got["enabled_providers"])
	}
}
