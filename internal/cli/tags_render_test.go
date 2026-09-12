package cli

import (
	"reflect"
	"testing"
)

func strPtr(s string) *string { return &s }

func TestConfigLines(t *testing.T) {
	aws := []string{"aws"}
	names := keyNames{"vtk_apps02": "Applications"}
	tests := []struct {
		name   string
		config tagConfig
		want   []string
	}{
		{
			"no rules",
			tagConfig{Assign: tagAssign{Type: assignValue, Value: strPtr("orphan")}},
			[]string{"orphan <- no rules"},
		},
		{
			"several rules and a bounded time frame",
			tagConfig{
				Assign:    tagAssign{Type: assignValue, Value: strPtr("data")},
				TimeFrame: &tagTimeFrame{Start: "2026-07-01", End: "2026-09-30"},
				Filter: tagFilter{Rules: []tagRule{
					{Providers: []string{"gcp", "aws"}, Conditions: []tagCondition{{Dimension: "service", Operator: opIs, Values: []string{"BigQuery"}}}},
					{Providers: aws},
				}},
			},
			[]string{"data <- aws, gcp where service is BigQuery (2026-07-01 to 2026-09-30)", "or aws"},
		},
		{
			"cost based on a virtual key with an empty input",
			tagConfig{
				Assign:    tagAssign{Type: assignCostBased, Source: &tagSource{Origin: tagOriginVirtual, Key: "vtk_apps02"}, InputFilter: &tagFilter{}},
				TimeFrame: &tagTimeFrame{End: "2026-06-30"},
				Filter:    tagFilter{Rules: []tagRule{{Providers: aws}}},
			},
			[]string{"split by virtual key Applications <- aws (until 2026-06-30)", "measured on no rules"},
		},
		{
			"a value that needs quoting and an empty time frame",
			tagConfig{
				Assign:    tagAssign{Type: assignValue, Value: strPtr("a, b")},
				TimeFrame: &tagTimeFrame{},
				Filter:    tagFilter{Rules: []tagRule{{}}},
			},
			[]string{`"a, b" <- no provider`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configLines(tt.config, names); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("configLines() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConditionText(t *testing.T) {
	names := keyNames{"applications": "Applications"}
	tests := []struct {
		condition tagCondition
		want      string
	}{
		{tagCondition{Dimension: "charge_type", Values: []string{"Usage"}}, "charge type is Usage"},
		{tagCondition{Dimension: "tag", Key: "env", Operator: "not_contains", Values: []string{"prod"}}, "tag env does not contain prod"},
		{tagCondition{Dimension: dimensionVirtualTag, Key: "APPLICATIONS", Operator: opIsNot, Values: []string{"app1", " padded"}}, `virtual tag Applications is not app1, " padded"`},
		{tagCondition{Dimension: "region", Operator: "ends_with", Values: nil}, "region ends with (no values)"},
		{tagCondition{Dimension: "resource", Operator: "matches_regex", Values: []string{""}}, `resource matches_regex ""`},
		{tagCondition{Dimension: dimensionTagged, Key: "team"}, "has tag team"},
		{tagCondition{Dimension: dimensionTagged}, "has any tag"},
		{tagCondition{Dimension: dimensionTagged, Key: "team", Operator: opIsNot}, "lacks tag team"},
		{tagCondition{Dimension: dimensionTagged, Operator: opIsNot}, "has no tag"},
	}
	for _, tt := range tests {
		if got := conditionText(tt.condition, names); got != tt.want {
			t.Errorf("conditionText(%+v) = %q, want %q", tt.condition, got, tt.want)
		}
	}
}

func TestCollapsedText(t *testing.T) {
	names := newKeyNames([]tagKeyRow{{ID: "vtk_env01", Name: "Environment"}})
	tests := []struct {
		key  tagCollapsedKey
		want string
	}{
		{tagCollapsedKey{Source: tagSource{Origin: tagOriginProvider, Key: "env"}}, "provider key env on every provider"},
		{tagCollapsedKey{Source: tagSource{Origin: tagOriginProvider, Key: "env"}, Scope: &tagScope{}}, "provider key env on every provider"},
		{tagCollapsedKey{Source: tagSource{Origin: tagOriginVirtual, Key: "vtk_env01"}, Scope: &tagScope{Providers: []string{"gcp"}}, ValuePrefix: "eng-"}, `virtual key Environment on gcp with prefix "eng-"`},
		{tagCollapsedKey{Source: tagSource{Origin: tagOriginVirtual, Key: "Unknown"}}, "virtual key Unknown on every provider"},
	}
	for _, tt := range tests {
		if got := collapsedText(tt.key, names); got != tt.want {
			t.Errorf("collapsedText() = %q, want %q", got, tt.want)
		}
	}
}

func TestAssignTextEdges(t *testing.T) {
	if got := assignText(tagAssign{Type: assignCostBased}, nil); got != "split by no source" {
		t.Errorf("cost based without a source = %q", got)
	}
	if got := assignText(tagAssign{Type: "business_metric"}, nil); got != "business_metric" {
		t.Errorf("unknown assign = %q", got)
	}
	if got := assignText(tagAssign{Type: assignValue}, nil); got != `""` {
		t.Errorf("value without a value = %q", got)
	}
	if got := assignText(tagAssign{Type: assignPercent, Splits: []tagSplit{{Value: "infra", Pct: 33.5}}}, nil); got != "infra 33.5%" {
		t.Errorf("percent = %q", got)
	}
}

func TestEffectiveFromLabel(t *testing.T) {
	if got := effectiveFromLabel(nil); got != "current month" {
		t.Errorf("nil = %q", got)
	}
	if got := effectiveFromLabel(strPtr("")); got != "current month" {
		t.Errorf("empty = %q", got)
	}
	if got := effectiveFromLabel(strPtr("2026-06")); got != "2026-06" {
		t.Errorf("set = %q", got)
	}
}

func TestPrintNumbered(t *testing.T) {
	outBuf, _ := captureOutput(t)
	printNumbered("Nothing", nil)
	if outBuf.Len() != 0 {
		t.Errorf("an empty list must print nothing: %q", outBuf.String())
	}
	items := make([][]string, 10)
	for i := range items {
		items[i] = []string{"first"}
	}
	items[9] = []string{"tenth", "or more"}
	printNumbered("Configs", items)
	assertContains(t, outBuf.String(), "  1. first", "  10. tenth", "      or more")
}

func TestDetailNamesKeys(t *testing.T) {
	virtual := tagSource{Origin: "virtual", Key: "vtk_apps02"}
	provider := tagSource{Origin: "provider", Key: "team"}
	namesKey := tagFilter{Rules: []tagRule{{Conditions: []tagCondition{{Dimension: "virtual_tag", Key: "vtk_apps02"}}}}}
	plain := tagFilter{Rules: []tagRule{{Conditions: []tagCondition{{Dimension: "service"}}}}}

	tests := []struct {
		name   string
		detail tagKeyDetail
		want   bool
	}{
		{"nothing nested", tagKeyDetail{tagDefinition: tagDefinition{
			CollapsedKeys: []tagCollapsedKey{{Source: provider}},
			Configs:       []tagConfig{{Filter: plain, Assign: tagAssign{Type: "value"}}},
		}}, false},
		{"a collapsed key", tagKeyDetail{tagDefinition: tagDefinition{
			CollapsedKeys: []tagCollapsedKey{{Source: virtual}},
		}}, true},
		{"a condition on the config's own filter", tagKeyDetail{tagDefinition: tagDefinition{
			Configs: []tagConfig{{Filter: namesKey, Assign: tagAssign{Type: "value"}}},
		}}, true},
		{"a condition on a cost split's input filter", tagKeyDetail{tagDefinition: tagDefinition{
			Configs: []tagConfig{{Filter: plain, Assign: tagAssign{Type: "cost_based", InputFilter: &namesKey, Source: &provider}}},
		}}, true},
		{"a cost split reading another key", tagKeyDetail{tagDefinition: tagDefinition{
			Configs: []tagConfig{{Filter: plain, Assign: tagAssign{Type: "cost_based", InputFilter: &plain, Source: &virtual}}},
		}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detailNamesKeys(tt.detail); got != tt.want {
				t.Errorf("detailNamesKeys = %v, want %v", got, tt.want)
			}
		})
	}
}
