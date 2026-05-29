package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

func f64(v float64) *float64 { return &v }
func strp(s string) *string  { return &s }
func boolPtr(b bool) *bool   { return &b }
func intPtr(i int) *int      { return &i }

func TestFormatCost(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0, "$0.00"},
		{0.5, "$0.50"},
		{100, "$100.00"},
		{1234.56, "$1,234.56"},
		{1000000, "$1,000,000.00"},
		{-42.50, "-$42.50"},
	}
	for _, tt := range tests {
		got := formatCost(tt.input)
		if got != tt.want {
			t.Errorf("formatCost(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatDeltaCost(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{100, "+$100.00"},
		{-50.25, "-$50.25"},
		{0, "$0.00"},
	}
	for _, tt := range tests {
		got := formatDeltaCost(tt.input)
		if got != tt.want {
			t.Errorf("formatDeltaCost(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{100, "100"},
		{1000, "1,000"},
		{1000000, "1,000,000"},
		{25000000, "25,000,000"},
	}
	for _, tt := range tests {
		got := formatNumber(tt.input)
		if got != tt.want {
			t.Errorf("formatNumber(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderBreakdown_Basic(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_instance",
				ResourceName:   "web",
				ChangeType:     "added",
				NewMonthlyCost: f64(560.64),
				Components: []*api.CostComponentEstimate{
					{Name: "Linux/UNIX usage (on-demand, m5.4xlarge)", MonthlyCost: 560.64, Units: f64(730), UnitLabel: strp("hours")},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalNewMonthly: 560.64,
			EstimableCount:  1,
			TotalCount:      1,
		},
	}

	RenderBreakdown(&buf, data, "./infra/")
	got := buf.String()

	if !strings.Contains(got, "Project:") {
		t.Error("missing Project header")
	}
	if !strings.Contains(got, "aws_instance.web") {
		t.Error("missing resource name")
	}
	if !strings.Contains(got, "Linux/UNIX usage") {
		t.Error("missing component name")
	}
	if !strings.Contains(got, "730") {
		t.Error("missing quantity")
	}
	if !strings.Contains(got, "hours") {
		t.Error("missing unit")
	}
	if !strings.Contains(got, "$560.64") {
		t.Error("missing cost")
	}
	if !strings.Contains(got, "PROJECT TOTAL") {
		t.Error("missing project total")
	}
	if !strings.Contains(got, "Baseline cost") {
		t.Error("missing summary table")
	}
}

func TestRenderBreakdown_TreeConnectors(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_instance",
				ResourceName:   "web",
				ChangeType:     "added",
				NewMonthlyCost: f64(100),
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 80},
					{Name: "Storage", MonthlyCost: 20},
				},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100},
	}

	NoColor = true
	defer func() { NoColor = false }()

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "├─") {
		t.Error("missing tree connector ├─")
	}
	if !strings.Contains(got, "└─") {
		t.Error("missing tree connector └─")
	}
}

func TestRenderBreakdown_SuggestionsAtBottom(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_instance",
				ResourceName:   "web",
				ChangeType:     "added",
				NewMonthlyCost: f64(100),
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 100},
				},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100},
		UpgradeSuggestions: []*api.UpgradeSuggestion{
			{ResourceType: "aws_instance", ResourceName: "web", Reason: "Upgrade to m6i", EstimatedMonthlySavings: f64(10)},
		},
	}

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "Optimization opportunities") {
		t.Error("missing optimization summary section")
	}
	if !strings.Contains(got, "Upgrade to m6i") {
		t.Error("missing recommendation text in bottom section")
	}
	totalIdx := strings.Index(got, "PROJECT TOTAL")
	suggIdx := strings.Index(got, "Optimization opportunities")
	if totalIdx < 0 || suggIdx < 0 || suggIdx < totalIdx {
		t.Error("suggestions should appear after PROJECT TOTAL")
	}
}

func TestRenderBreakdown_ZeroCostFiltered(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_instance",
				ResourceName:   "web",
				NewMonthlyCost: f64(100),
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 100, Units: f64(730), UnitLabel: strp("hours")},
					{Name: "Root volume IOPS", MonthlyCost: 0, Units: f64(0), UnitLabel: strp("IOPS")},
					{Name: "Root volume throughput", MonthlyCost: 0, Units: f64(0), UnitLabel: strp("MB/s")},
				},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100},
	}

	NoColor = true
	defer func() { NoColor = false }()

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if strings.Contains(got, "Root volume IOPS") {
		t.Error("zero-cost component should be filtered out")
	}
	if !strings.Contains(got, "Compute") {
		t.Error("non-zero component should be present")
	}
}

func TestVisibleComponents_AllZero(t *testing.T) {
	components := []*api.CostComponentEstimate{
		{Name: "A", MonthlyCost: 0, Units: f64(0)},
		{Name: "B", MonthlyCost: 0, Units: f64(0)},
	}
	result := visibleComponents(components)
	if len(result) != 2 {
		t.Errorf("when all zero, should return all components, got %d", len(result))
	}
}

func TestVisibleComponents_NilUnitsKept(t *testing.T) {
	components := []*api.CostComponentEstimate{
		{Name: "Gateway", MonthlyCost: 0},
	}
	result := visibleComponents(components)
	if len(result) != 1 {
		t.Errorf("nil units component should be kept, got %d", len(result))
	}
}

func TestRenderBreakdown_QuietMode(t *testing.T) {
	var buf bytes.Buffer
	QuietMode = true
	defer func() { QuietMode = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "web", NewMonthlyCost: f64(100)},
		},
	}

	RenderBreakdown(&buf, data, ".")
	if buf.Len() != 0 {
		t.Errorf("expected empty output in quiet mode, got %q", buf.String())
	}
}

func TestRenderDiff_Modified(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "modified",
				PreviousMonthlyCost:   f64(1125),
				NewMonthlyCost:        f64(284),
				MonthlyCostDifference: f64(-841),
				Components: []*api.CostComponentEstimate{
					{Name: "Instance usage", MonthlyCost: 280, PreviousMonthlyCost: f64(1121)},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalPreviousMonthly:   1125,
			TotalNewMonthly:        284,
			TotalMonthlyDifference: -841,
		},
	}

	NoColor = true
	defer func() { NoColor = false }()

	RenderDiff(&buf, data, "./infra/")
	got := buf.String()

	if !strings.Contains(got, "~") {
		t.Error("missing ~ symbol for modified")
	}
	if !strings.Contains(got, "aws_instance.web") {
		t.Error("missing resource name")
	}
	if !strings.Contains(got, "-$841") {
		t.Error("missing delta amount")
	}
	if !strings.Contains(got, "->") {
		t.Error("missing range arrow")
	}
	if !strings.Contains(got, "Monthly cost change") {
		t.Error("missing summary line")
	}
	if !strings.Contains(got, "Percent:") {
		t.Error("missing percent line")
	}
}

func TestRenderDiff_Added(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_lambda_function",
				ResourceName:          "hello",
				ChangeType:            "added",
				NewMonthlyCost:        f64(437),
				MonthlyCostDifference: f64(437),
				Components: []*api.CostComponentEstimate{
					{Name: "Requests", MonthlyCost: 20},
					{Name: "Duration", MonthlyCost: 417},
				},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 437, TotalMonthlyDifference: 437},
	}

	NoColor = true
	defer func() { NoColor = false }()

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "+") {
		t.Error("missing + symbol for added")
	}
	if !strings.Contains(got, "aws_lambda_function.hello") {
		t.Error("missing resource name")
	}
}

func TestRenderDiff_NoopExcluded(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "unchanged", ChangeType: "noop", NewMonthlyCost: f64(100)},
		},
		CostSummary: &api.CostSummary{},
	}

	NoColor = true
	defer func() { NoColor = false }()

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if strings.Contains(got, "unchanged") {
		t.Error("noop resource should not appear in diff output")
	}
}

func TestRenderDiff_ZeroDiffExcluded(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "same",
				ChangeType:            "modified",
				MonthlyCostDifference: f64(0),
				NewMonthlyCost:        f64(100),
			},
		},
		CostSummary: &api.CostSummary{},
	}

	NoColor = true
	defer func() { NoColor = false }()

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if strings.Contains(got, "aws_instance.same") {
		t.Error("zero-diff modified resource should not appear")
	}
}

func TestRenderMarkdown_Baseline(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "web", ChangeType: "added", NewMonthlyCost: f64(100), MonthlyCostDifference: f64(100)},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, TotalMonthlyDifference: 100},
	}

	RenderMarkdown(&buf, data, false)
	got := buf.String()

	if !strings.Contains(got, "## Cost Estimate") {
		t.Error("missing markdown header")
	}
	if !strings.Contains(got, "| Resource |") {
		t.Error("missing table header")
	}
	if !strings.Contains(got, "**Monthly estimate:**") {
		t.Error("missing monthly estimate")
	}
}

func TestRenderMarkdown_WithSuggestions(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "web", ChangeType: "added", NewMonthlyCost: f64(100)},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100},
		UpgradeSuggestions: []*api.UpgradeSuggestion{
			{ResourceType: "aws_instance", ResourceName: "web", Reason: "Use Graviton", EstimatedMonthlySavings: f64(10)},
			{ResourceType: "aws_instance", ResourceName: "this", ModulePath: strp("module.backend"), Reason: "Upgrade", EstimatedMonthlySavings: f64(5)},
		},
	}

	RenderMarkdown(&buf, data, false)
	got := buf.String()

	if !strings.Contains(got, "Optimization suggestions") {
		t.Error("missing optimization section")
	}
	if !strings.Contains(got, "Use Graviton") {
		t.Error("missing suggestion text")
	}
	if !strings.Contains(got, "module.backend.aws_instance.this") {
		t.Error("missing module-qualified suggestion in markdown")
	}
}

func TestSymbolAndStyle(t *testing.T) {
	tests := []struct {
		change string
		sym    string
	}{
		{"added", "+"},
		{"removed", "-"},
		{"modified", "~"},
		{"noop", " "},
	}
	for _, tt := range tests {
		sym, _ := symbolAndStyle(tt.change)
		if sym != tt.sym {
			t.Errorf("symbolAndStyle(%q) symbol = %q, want %q", tt.change, sym, tt.sym)
		}
	}
}

func TestPtrF(t *testing.T) {
	v := 42.5
	if got := ptrF(&v); got != 42.5 {
		t.Errorf("ptrF(&42.5) = %v, want 42.5", got)
	}
	if got := ptrF(nil); got != 0 {
		t.Errorf("ptrF(nil) = %v, want 0", got)
	}
}

func TestPadRight_NopadWhenLongEnough(t *testing.T) {
	got := padRight("hello", 3)
	if got != "hello" {
		t.Errorf("padRight(\"hello\", 3) = %q, want %q", got, "hello")
	}
	got = padRight("abc", 3)
	if got != "abc" {
		t.Errorf("padRight(\"abc\", 3) = %q, want %q", got, "abc")
	}
}

func TestPadLeft_NopadWhenLongEnough(t *testing.T) {
	got := padLeft("hello", 3)
	if got != "hello" {
		t.Errorf("padLeft(\"hello\", 3) = %q, want %q", got, "hello")
	}
	got = padLeft("abc", 3)
	if got != "abc" {
		t.Errorf("padLeft(\"abc\", 3) = %q, want %q", got, "abc")
	}
}

func TestFormatQty_Fractional(t *testing.T) {
	got := formatQty(f64(1.5))
	if got != "1.50" {
		t.Errorf("formatQty(1.5) = %q, want %q", got, "1.50")
	}
	got = formatQty(f64(0.333))
	if got != "0.33" {
		t.Errorf("formatQty(0.333) = %q, want %q", got, "0.33")
	}
}

func TestFormatNumber_Negative(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{-1, "-1"},
		{-1000, "-1,000"},
		{-1234567, "-1,234,567"},
	}
	for _, tt := range tests {
		got := formatNumber(tt.input)
		if got != tt.want {
			t.Errorf("formatNumber(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestRenderDiff_QuietMode(t *testing.T) {
	var buf bytes.Buffer
	QuietMode = true
	defer func() { QuietMode = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "web", ChangeType: "added", NewMonthlyCost: f64(100), MonthlyCostDifference: f64(100)},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, TotalMonthlyDifference: 100},
	}

	RenderDiff(&buf, data, ".")
	if buf.Len() != 0 {
		t.Errorf("expected empty output in quiet mode, got %q", buf.String())
	}
}

func TestRenderDiffComponent_Removed(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "modified",
				PreviousMonthlyCost:   f64(200),
				NewMonthlyCost:        f64(100),
				MonthlyCostDifference: f64(-100),
				Components: []*api.CostComponentEstimate{
					{Name: "Storage", MonthlyCost: 0, PreviousMonthlyCost: f64(100)},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalPreviousMonthly:   200,
			TotalNewMonthly:        100,
			TotalMonthlyDifference: -100,
		},
	}

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "- Storage") {
		t.Error("missing removed component with - symbol")
	}
}

func TestRenderDiffSummary_ZeroPreviousCost(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "new",
				ChangeType:            "added",
				NewMonthlyCost:        f64(500),
				MonthlyCostDifference: f64(500),
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 500},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalPreviousMonthly:   0,
			TotalNewMonthly:        500,
			TotalMonthlyDifference: 500,
		},
	}

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if strings.Contains(got, "Percent:") {
		t.Error("should not show percent line when previous cost is 0")
	}
	if !strings.Contains(got, "Monthly cost change") {
		t.Error("missing summary header")
	}
}

func TestRenderDiffComponent_ZeroDiffEarlyReturn(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "modified",
				PreviousMonthlyCost:   f64(100),
				NewMonthlyCost:        f64(120),
				MonthlyCostDifference: f64(20),
				Components: []*api.CostComponentEstimate{
					{Name: "Unchanged component", MonthlyCost: 50, PreviousMonthlyCost: f64(50)},
					{Name: "Changed component", MonthlyCost: 70, PreviousMonthlyCost: f64(50)},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalPreviousMonthly:   100,
			TotalNewMonthly:        120,
			TotalMonthlyDifference: 20,
		},
	}

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if strings.Contains(got, "Unchanged component") {
		t.Error("zero-diff component with previous cost should be skipped")
	}
	if !strings.Contains(got, "Changed component") {
		t.Error("changed component should appear")
	}
}

func TestRenderDiffSummary_PositivePercent(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "modified",
				PreviousMonthlyCost:   f64(100),
				NewMonthlyCost:        f64(150),
				MonthlyCostDifference: f64(50),
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 150, PreviousMonthlyCost: f64(100)},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalPreviousMonthly:   100,
			TotalNewMonthly:        150,
			TotalMonthlyDifference: 50,
		},
	}

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "Percent: +50%") {
		t.Errorf("expected positive percent line, got %q", got)
	}
}

func TestRenderDiffSummary_WithSuggestions(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "modified",
				PreviousMonthlyCost:   f64(100),
				NewMonthlyCost:        f64(200),
				MonthlyCostDifference: f64(100),
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 200, PreviousMonthlyCost: f64(100)},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalPreviousMonthly:   100,
			TotalNewMonthly:        200,
			TotalMonthlyDifference: 100,
		},
		UpgradeSuggestions: []*api.UpgradeSuggestion{
			{ResourceType: "aws_instance", ResourceName: "web", Reason: "Use Graviton", EstimatedMonthlySavings: f64(20)},
		},
	}

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "Optimization opportunities") {
		t.Error("missing optimization section in diff summary")
	}
}

func TestRenderMarkdown_DiffMode(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "web", ChangeType: "modified", NewMonthlyCost: f64(200), MonthlyCostDifference: f64(-50)},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 200, TotalMonthlyDifference: -50},
	}

	RenderMarkdown(&buf, data, true)
	got := buf.String()

	if !strings.Contains(got, "## Cost Estimate (diff)") {
		t.Error("missing diff header")
	}
	if !strings.Contains(got, "**Delta:**") {
		t.Error("missing delta line")
	}
}

func TestRenderMarkdown_ZeroDelta(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "web", ChangeType: "modified", NewMonthlyCost: f64(100), MonthlyCostDifference: f64(0)},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, TotalMonthlyDifference: 0},
	}

	RenderMarkdown(&buf, data, false)
	got := buf.String()

	if strings.Contains(got, "**Delta:**") {
		t.Error("should not show delta line when totalDiff is 0")
	}
}

func TestSplitModulePath(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"module.vpc", []string{"module.vpc"}},
		{"module.a.module.b", []string{"module.a", "module.a.module.b"}},
		{"module.a.module.b.module.c", []string{"module.a", "module.a.module.b", "module.a.module.b.module.c"}},
		{"module.session_cache", []string{"module.session_cache"}},
		{"aws_instance.web", []string{"aws_instance.web"}},
	}
	for _, tt := range tests {
		got := splitModulePath(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitModulePath(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitModulePath(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestBuildModuleTree_RootOnly(t *testing.T) {
	estimates := []*api.ResourceCostEstimate{
		{ResourceType: "aws_instance", ResourceName: "web"},
		{ResourceType: "aws_ebs_volume", ResourceName: "data"},
	}
	tree := buildModuleTree(estimates)
	if len(tree.resources) != 2 {
		t.Errorf("expected 2 root resources, got %d", len(tree.resources))
	}
	if len(tree.children) != 0 {
		t.Errorf("expected 0 children, got %d", len(tree.children))
	}
}

func TestBuildModuleTree_SingleModule(t *testing.T) {
	estimates := []*api.ResourceCostEstimate{
		{ResourceType: "aws_instance", ResourceName: "web"},
		{ResourceType: "aws_elasticache_cluster", ResourceName: "this", ModulePath: strp("module.cache")},
	}
	tree := buildModuleTree(estimates)
	if len(tree.resources) != 1 {
		t.Errorf("expected 1 root resource, got %d", len(tree.resources))
	}
	if len(tree.children) != 1 {
		t.Fatalf("expected 1 child module, got %d", len(tree.children))
	}
	child := tree.children[0]
	if child.path != "module.cache" {
		t.Errorf("child path = %q, want %q", child.path, "module.cache")
	}
	if len(child.resources) != 1 {
		t.Errorf("expected 1 resource in module.cache, got %d", len(child.resources))
	}
}

func TestBuildModuleTree_NestedModules(t *testing.T) {
	estimates := []*api.ResourceCostEstimate{
		{ResourceType: "aws_instance", ResourceName: "deep", ModulePath: strp("module.parent.module.child")},
		{ResourceType: "aws_vpc", ResourceName: "main", ModulePath: strp("module.parent")},
	}
	tree := buildModuleTree(estimates)
	if len(tree.resources) != 0 {
		t.Errorf("expected 0 root resources, got %d", len(tree.resources))
	}
	if len(tree.children) != 1 {
		t.Fatalf("expected 1 top-level child, got %d", len(tree.children))
	}
	parent := tree.children[0]
	if parent.path != "module.parent" {
		t.Errorf("parent path = %q, want %q", parent.path, "module.parent")
	}
	if len(parent.resources) != 1 {
		t.Errorf("expected 1 resource in module.parent, got %d", len(parent.resources))
	}
	if len(parent.children) != 1 {
		t.Fatalf("expected 1 child of module.parent, got %d", len(parent.children))
	}
	child := parent.children[0]
	if child.path != "module.parent.module.child" {
		t.Errorf("child path = %q, want %q", child.path, "module.parent.module.child")
	}
	if len(child.resources) != 1 {
		t.Errorf("expected 1 resource in module.parent.module.child, got %d", len(child.resources))
	}
}

func TestRenderBreakdown_ModuleGrouping(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_instance",
				ResourceName:   "web",
				ChangeType:     "added",
				NewMonthlyCost: f64(100),
				Components:     []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 100}},
			},
			{
				ResourceType:   "aws_elasticache_cluster",
				ResourceName:   "this",
				ChangeType:     "added",
				NewMonthlyCost: f64(265),
				ModulePath:     strp("module.cache"),
				Components:     []*api.CostComponentEstimate{{Name: "Nodes", MonthlyCost: 265, Units: f64(2), UnitLabel: strp("nodes")}},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 365, EstimableCount: 2, TotalCount: 2},
	}

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "aws_instance.web") {
		t.Error("missing root resource")
	}
	if !strings.Contains(got, "module.cache") {
		t.Error("missing module header")
	}
	if !strings.Contains(got, "aws_elasticache_cluster.this") {
		t.Error("missing module resource")
	}

	modIdx := strings.Index(got, "module.cache")
	resIdx := strings.Index(got, "aws_elasticache_cluster.this")
	if modIdx > resIdx {
		t.Error("module header should appear before module resource")
	}
}

func TestRenderBreakdown_NestedModuleGrouping(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_instance",
				ResourceName:   "deep",
				ChangeType:     "added",
				NewMonthlyCost: f64(50),
				ModulePath:     strp("module.parent.module.child"),
				Components:     []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 50}},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 50, EstimableCount: 1, TotalCount: 1},
	}

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "module.parent") {
		t.Error("missing parent module header")
	}
	if !strings.Contains(got, "module.parent.module.child") {
		t.Error("missing child module header")
	}
	if !strings.Contains(got, "aws_instance.deep") {
		t.Error("missing nested resource")
	}

	parentIdx := strings.Index(got, "module.parent\n")
	if parentIdx < 0 {
		parentIdx = strings.Index(got, "module.parent ")
	}
	childIdx := strings.Index(got, "module.parent.module.child")
	resIdx := strings.Index(got, "aws_instance.deep")
	if parentIdx > childIdx || childIdx > resIdx {
		t.Error("nesting order wrong: parent > child > resource expected")
	}
}

func TestRenderDiff_ModuleGrouping(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "this",
				ChangeType:            "modified",
				ModulePath:            strp("module.backend"),
				PreviousMonthlyCost:   f64(100),
				NewMonthlyCost:        f64(200),
				MonthlyCostDifference: f64(100),
				Components:            []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 200, PreviousMonthlyCost: f64(100)}},
			},
		},
		CostSummary: &api.CostSummary{TotalPreviousMonthly: 100, TotalNewMonthly: 200, TotalMonthlyDifference: 100},
	}

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "module.backend") {
		t.Error("missing module header in diff")
	}
	if !strings.Contains(got, "aws_instance.this") {
		t.Error("missing resource in module diff")
	}
}

func TestRenderMarkdown_ModuleColumn(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "web", ChangeType: "added", NewMonthlyCost: f64(100)},
			{ResourceType: "aws_elasticache_cluster", ResourceName: "this", ChangeType: "added", NewMonthlyCost: f64(200), ModulePath: strp("module.cache")},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 300},
	}

	RenderMarkdown(&buf, data, false)
	got := buf.String()

	if !strings.Contains(got, "| Module |") {
		t.Error("missing Module column header")
	}
	if !strings.Contains(got, "`module.cache`") {
		t.Error("missing module path in table")
	}
}

func TestRenderMarkdown_NoModuleColumn(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "web", ChangeType: "added", NewMonthlyCost: f64(100)},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100},
	}

	RenderMarkdown(&buf, data, false)
	got := buf.String()

	if strings.Contains(got, "| Module |") {
		t.Error("Module column should not appear when no resources have module paths")
	}
}

func TestRenderSuggestionsTable_GroupedByResource(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	suggestions := []*api.UpgradeSuggestion{
		{ResourceType: "aws_instance", ResourceName: "web", Reason: "Upgrade to m6i", EstimatedMonthlySavings: f64(10)},
		{ResourceType: "aws_instance", ResourceName: "web", Reason: "Use gp3", EstimatedMonthlySavings: f64(5)},
		{ResourceType: "aws_ebs_volume", ResourceName: "data", Reason: "Use io2", EstimatedMonthlySavings: nil},
	}

	renderSuggestionsTable(&buf, suggestions)
	got := buf.String()

	if !strings.Contains(got, "Optimization opportunities (3)") {
		t.Error("missing header with count")
	}
	if !strings.Contains(got, "Upgrade to m6i") {
		t.Error("missing first suggestion")
	}
	if !strings.Contains(got, "Use gp3") {
		t.Error("missing second suggestion")
	}
	if !strings.Contains(got, "aws_ebs_volume.data") {
		t.Error("missing second resource")
	}

	firstIdx := strings.Index(got, "aws_instance.web")
	secondIdx := strings.LastIndex(got, "aws_instance.web")
	if firstIdx != secondIdx {
		t.Error("resource name should be deduplicated (appear once only)")
	}
}

func TestRenderSuggestionsTable_WithModulePath(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	suggestions := []*api.UpgradeSuggestion{
		{ResourceType: "aws_instance", ResourceName: "this", ModulePath: strp("module.backend"), Reason: "Upgrade", EstimatedMonthlySavings: f64(10)},
	}

	renderSuggestionsTable(&buf, suggestions)
	got := buf.String()

	if !strings.Contains(got, "module.backend.aws_instance.this") {
		t.Error("missing module-qualified resource name in suggestions table")
	}
}

func TestRenderComponentRow_NameWidthFloor(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	c := &api.CostComponentEstimate{Name: "Compute", MonthlyCost: 10}
	renderComponentRow(&buf, c, "└─", 50, 55, 10, 14, 14)
	got := buf.String()

	if !strings.Contains(got, "Compute") {
		t.Error("missing component name with large indent")
	}
	if !strings.Contains(got, "$10.00") {
		t.Error("missing cost with large indent")
	}
}

func TestRenderMarkdown_WithModulesResourceRows(t *testing.T) {
	var buf bytes.Buffer

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{ResourceType: "aws_instance", ResourceName: "web", ChangeType: "added", NewMonthlyCost: f64(100), ModulePath: strp("module.net")},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, TotalMonthlyDifference: 100},
	}

	RenderMarkdown(&buf, data, true)
	got := buf.String()

	if !strings.Contains(got, "`module.net`") {
		t.Error("missing module path in resource row")
	}
	if !strings.Contains(got, "## Cost Estimate (diff)") {
		t.Error("missing diff header")
	}
}

func TestTreeContinuation(t *testing.T) {
	if got := treeContinuation(0, 2); got != "│  " {
		t.Errorf("treeContinuation(0, 2) = %q, want %q", got, "│  ")
	}
	if got := treeContinuation(1, 2); got != "   " {
		t.Errorf("treeContinuation(1, 2) = %q, want %q", got, "   ")
	}
}

func TestIsZeroCostResource(t *testing.T) {
	tests := []struct {
		name string
		res  *api.ResourceCostEstimate
		want bool
	}{
		{
			name: "nil NewMonthlyCost and no components",
			res:  &api.ResourceCostEstimate{},
			want: true,
		},
		{
			name: "nil NewMonthlyCost with zero-cost components",
			res: &api.ResourceCostEstimate{
				Components: []*api.CostComponentEstimate{
					{Name: "A", MonthlyCost: 0},
					{Name: "B", MonthlyCost: 0},
				},
			},
			want: true,
		},
		{
			name: "zero NewMonthlyCost with zero-cost components",
			res: &api.ResourceCostEstimate{
				NewMonthlyCost: f64(0),
				Components: []*api.CostComponentEstimate{
					{Name: "A", MonthlyCost: 0},
				},
			},
			want: true,
		},
		{
			name: "non-zero NewMonthlyCost",
			res: &api.ResourceCostEstimate{
				NewMonthlyCost: f64(100),
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 100},
				},
			},
			want: false,
		},
		{
			name: "nil NewMonthlyCost but non-zero component",
			res: &api.ResourceCostEstimate{
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 50},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isZeroCostResource(tt.res)
			if got != tt.want {
				t.Errorf("isZeroCostResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupBySubresource(t *testing.T) {
	sub1 := "root_block_device"
	sub2 := "ebs_block_device"

	components := []*api.CostComponentEstimate{
		{Name: "Instance usage", MonthlyCost: 100},
		{Name: "CPU credits", MonthlyCost: 10},
		{Name: "Storage (gp3)", MonthlyCost: 4, Subresource: &sub1},
		{Name: "IOPS", MonthlyCost: 0, Subresource: &sub1},
		{Name: "Storage (io2)", MonthlyCost: 20, Subresource: &sub2},
	}

	topLevel, groups := groupBySubresource(components)

	if len(topLevel) != 2 {
		t.Fatalf("expected 2 top-level components, got %d", len(topLevel))
	}
	if topLevel[0].Name != "Instance usage" {
		t.Errorf("topLevel[0].Name = %q, want %q", topLevel[0].Name, "Instance usage")
	}
	if topLevel[1].Name != "CPU credits" {
		t.Errorf("topLevel[1].Name = %q, want %q", topLevel[1].Name, "CPU credits")
	}

	if len(groups) != 2 {
		t.Fatalf("expected 2 subresource groups, got %d", len(groups))
	}
	if groups[0].name != "root_block_device" {
		t.Errorf("groups[0].name = %q, want %q", groups[0].name, "root_block_device")
	}
	if len(groups[0].components) != 2 {
		t.Errorf("expected 2 components in root_block_device, got %d", len(groups[0].components))
	}
	if groups[1].name != "ebs_block_device" {
		t.Errorf("groups[1].name = %q, want %q", groups[1].name, "ebs_block_device")
	}
	if len(groups[1].components) != 1 {
		t.Errorf("expected 1 component in ebs_block_device, got %d", len(groups[1].components))
	}
}

func TestGroupBySubresource_AllTopLevel(t *testing.T) {
	components := []*api.CostComponentEstimate{
		{Name: "A", MonthlyCost: 10},
		{Name: "B", MonthlyCost: 20},
	}

	topLevel, groups := groupBySubresource(components)

	if len(topLevel) != 2 {
		t.Errorf("expected 2 top-level, got %d", len(topLevel))
	}
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestGroupBySubresource_AllSubresource(t *testing.T) {
	sub := "root_block_device"
	components := []*api.CostComponentEstimate{
		{Name: "Storage", MonthlyCost: 4, Subresource: &sub},
	}

	topLevel, groups := groupBySubresource(components)

	if len(topLevel) != 0 {
		t.Errorf("expected 0 top-level, got %d", len(topLevel))
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if groups[0].name != "root_block_device" {
		t.Errorf("group name = %q, want %q", groups[0].name, "root_block_device")
	}
}

func TestRenderBreakdown_SubresourceGrouping(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	sub := "root_block_device"
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_instance",
				ResourceName:   "api_server",
				ChangeType:     "added",
				NewMonthlyCost: f64(1125.28),
				Components: []*api.CostComponentEstimate{
					{Name: "Instance usage (Linux/UNIX, on-demand, m5.8xlarge)", MonthlyCost: 1121.28, Units: f64(730), UnitLabel: strp("hours")},
					{Name: "Storage (general purpose SSD, gp3)", MonthlyCost: 4.00, Units: f64(50), UnitLabel: strp("GB"), Subresource: &sub},
				},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 1125.28, EstimableCount: 1, TotalCount: 1},
	}

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "aws_instance.api_server") {
		t.Error("missing resource name")
	}
	if !strings.Contains(got, "Instance usage") {
		t.Error("missing top-level component")
	}
	if !strings.Contains(got, "root_block_device") {
		t.Error("missing subresource group name")
	}
	if !strings.Contains(got, "Storage (general purpose SSD, gp3)") {
		t.Error("missing subresource component")
	}

	subIdx := strings.Index(got, "root_block_device")
	storageIdx := strings.Index(got, "Storage (general purpose SSD, gp3)")
	instanceIdx := strings.Index(got, "Instance usage")
	if instanceIdx > subIdx {
		t.Error("top-level component should appear before subresource group")
	}
	if subIdx > storageIdx {
		t.Error("subresource name should appear before its components")
	}

	if !strings.Contains(got, "├─") || !strings.Contains(got, "└─") {
		t.Error("missing tree connectors for subresource nesting")
	}
}

func TestRenderBreakdown_FreeResourceFilteredInModule(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_instance",
				ResourceName:   "paid",
				ChangeType:     "added",
				ModulePath:     strp("module.backend"),
				NewMonthlyCost: f64(100),
				Components:     []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 100}},
			},
			{
				ResourceType:   "aws_cloudwatch_log_group",
				ResourceName:   "free_log",
				ChangeType:     "added",
				ModulePath:     strp("module.backend"),
				NewMonthlyCost: f64(0),
				Components:     []*api.CostComponentEstimate{{Name: "Storage", MonthlyCost: 0, Units: f64(0), UnitLabel: strp("GB")}},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, EstimableCount: 2, TotalCount: 2},
	}

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "aws_instance.paid") {
		t.Error("paid resource should be present")
	}
	if strings.Contains(got, "aws_cloudwatch_log_group.free_log") {
		t.Error("zero-cost resource inside module should be filtered out")
	}
}

func TestRenderBreakdown_FreeResourceKeptAtRoot(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_cloudwatch_log_group",
				ResourceName:   "free_log",
				ChangeType:     "added",
				NewMonthlyCost: f64(0),
				Components:     []*api.CostComponentEstimate{{Name: "Storage", MonthlyCost: 0, Units: f64(0), UnitLabel: strp("GB")}},
			},
			{
				ResourceType:   "aws_instance",
				ResourceName:   "web",
				ChangeType:     "added",
				NewMonthlyCost: f64(100),
				Components:     []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 100}},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, EstimableCount: 2, TotalCount: 2},
	}

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "aws_cloudwatch_log_group.free_log") {
		t.Error("zero-cost resource at root level (depth 0) should still be shown")
	}
	if !strings.Contains(got, "aws_instance.web") {
		t.Error("paid resource should be present")
	}
}

func TestRenderDiff_SubresourceGrouping(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	sub := "root_block_device"
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "modified",
				PreviousMonthlyCost:   f64(1125),
				NewMonthlyCost:        f64(570),
				MonthlyCostDifference: f64(-555),
				Components: []*api.CostComponentEstimate{
					{Name: "Instance usage", MonthlyCost: 560, PreviousMonthlyCost: f64(1121)},
					{Name: "Storage (gp3)", MonthlyCost: 10, PreviousMonthlyCost: f64(4), Subresource: &sub},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalPreviousMonthly:   1125,
			TotalNewMonthly:        570,
			TotalMonthlyDifference: -555,
		},
	}

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "aws_instance.web") {
		t.Error("missing resource name")
	}
	if !strings.Contains(got, "Instance usage") {
		t.Error("missing top-level diff component")
	}
	if !strings.Contains(got, "root_block_device") {
		t.Error("missing subresource group name in diff output")
	}
	if !strings.Contains(got, "Storage (gp3)") {
		t.Error("missing subresource diff component")
	}

	instanceIdx := strings.Index(got, "Instance usage")
	subIdx := strings.Index(got, "root_block_device")
	storageIdx := strings.Index(got, "Storage (gp3)")
	if instanceIdx > subIdx {
		t.Error("top-level component should appear before subresource group in diff")
	}
	if subIdx > storageIdx {
		t.Error("subresource name should appear before its components in diff")
	}
}

func TestRenderSuggestionsTable_HidesGravitonByDefault(t *testing.T) {
	origGraviton := GravitonEnabled
	GravitonEnabled = false
	defer func() { GravitonEnabled = origGraviton }()

	var buf bytes.Buffer
	suggestions := []*api.UpgradeSuggestion{
		{
			ResourceType:            "aws_instance",
			ResourceName:            "web",
			Category:                "old_gen",
			Reason:                  "Upgrade from t2 to t3",
			EstimatedMonthlySavings: f64(10.0),
		},
		{
			ResourceType:            "aws_db_instance",
			ResourceName:            "db",
			Category:                "graviton",
			Reason:                  "Switch to Graviton",
			EstimatedMonthlySavings: f64(25.0),
		},
	}
	renderSuggestionsTable(&buf, suggestions)
	got := buf.String()

	if !strings.Contains(got, "old_gen") && !strings.Contains(got, "Upgrade from t2") {
		t.Error("non-graviton suggestion should be shown")
	}
	if strings.Contains(got, "Switch to Graviton") {
		t.Error("graviton suggestion should be hidden when GravitonEnabled=false")
	}
	if !strings.Contains(got, "(1):") {
		t.Error("header should show count of 1 visible suggestion")
	}
}

func TestRenderSuggestionsTable_ShowsGravitonWhenEnabled(t *testing.T) {
	origGraviton := GravitonEnabled
	GravitonEnabled = true
	defer func() { GravitonEnabled = origGraviton }()

	var buf bytes.Buffer
	suggestions := []*api.UpgradeSuggestion{
		{
			ResourceType:            "aws_instance",
			ResourceName:            "web",
			Category:                "old_gen",
			Reason:                  "Upgrade from t2 to t3",
			EstimatedMonthlySavings: f64(10.0),
		},
		{
			ResourceType:            "aws_db_instance",
			ResourceName:            "db",
			Category:                "graviton",
			Reason:                  "Switch to Graviton",
			EstimatedMonthlySavings: f64(25.0),
		},
	}
	renderSuggestionsTable(&buf, suggestions)
	got := buf.String()

	if !strings.Contains(got, "Switch to Graviton") {
		t.Error("graviton suggestion should be shown when GravitonEnabled=true")
	}
	if !strings.Contains(got, "(2):") {
		t.Error("header should show count of 2 visible suggestions")
	}
}

func TestRenderMarkdown_HidesGravitonByDefault(t *testing.T) {
	origGraviton := GravitonEnabled
	GravitonEnabled = false
	defer func() { GravitonEnabled = origGraviton }()

	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		CostSummary: &api.CostSummary{TotalNewMonthly: 100.0},
		UpgradeSuggestions: []*api.UpgradeSuggestion{
			{
				ResourceType:            "aws_db_instance",
				ResourceName:            "db",
				Category:                "graviton",
				Reason:                  "Switch to Graviton",
				EstimatedMonthlySavings: f64(25.0),
			},
		},
	}
	RenderMarkdown(&buf, data, false)
	got := buf.String()

	if strings.Contains(got, "Switch to Graviton") {
		t.Error("graviton suggestion should be hidden in markdown when GravitonEnabled=false")
	}
	if strings.Contains(got, "Optimization suggestions") {
		t.Error("optimization section should not appear when all suggestions are filtered")
	}
}

func TestRenderBreakdownFooter_ExcludesGravitonFromSavings(t *testing.T) {
	origGraviton := GravitonEnabled
	origNoColor := NoColor
	GravitonEnabled = false
	NoColor = true
	defer func() {
		GravitonEnabled = origGraviton
		NoColor = origNoColor
	}()

	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		CostSummary: &api.CostSummary{
			TotalNewMonthly: 500.0,
			EstimableCount:  2,
			TotalCount:      2,
		},
		UpgradeSuggestions: []*api.UpgradeSuggestion{
			{
				ResourceType:            "aws_instance",
				ResourceName:            "web",
				Category:                "old_gen",
				Reason:                  "Upgrade t2 to t3",
				EstimatedMonthlySavings: f64(10.0),
			},
			{
				ResourceType:            "aws_db_instance",
				ResourceName:            "db",
				Category:                "graviton",
				Reason:                  "Switch to Graviton",
				EstimatedMonthlySavings: f64(25.0),
			},
		},
	}
	renderBreakdownFooter(&buf, data, 55+10+14+2, 14, "test")
	got := buf.String()

	if strings.Contains(got, "$35.00/mo") {
		t.Error("total savings should not include graviton ($35 = $10 + $25)")
	}
	if !strings.Contains(got, "$10.00/mo") {
		t.Error("total savings should show only non-graviton savings ($10)")
	}
}

func TestHasEstimatedComponents(t *testing.T) {
	withEstimated := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 100},
					{Name: "Requests", MonthlyCost: 5, IsEstimated: boolPtr(true)},
				},
			},
		},
	}
	if !hasEstimatedComponents(withEstimated) {
		t.Error("expected true when a component has IsEstimated")
	}

	withoutEstimated := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 100},
				},
			},
		},
	}
	if hasEstimatedComponents(withoutEstimated) {
		t.Error("expected false when no component has IsEstimated")
	}

	empty := &api.AnalyzePrResponse{}
	if hasEstimatedComponents(empty) {
		t.Error("expected false for empty result")
	}
}

func TestRenderBreakdownFooter_EstimatedDisclaimer(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()

	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				Components: []*api.CostComponentEstimate{
					{Name: "Requests", MonthlyCost: 5, IsEstimated: boolPtr(true)},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalNewMonthly: 5,
			EstimableCount:  1,
			TotalCount:      3,
			FreeCount:       intPtr(2),
		},
	}
	renderBreakdownFooter(&buf, data, 55, 16, "test")
	got := buf.String()

	if !strings.Contains(got, "free (no usage cost)") {
		t.Error("expected 'free (no usage cost)' label")
	}
	if !strings.Contains(got, "Costs are estimated. Actual usage may vary.") {
		t.Error("expected estimated disclaimer")
	}
}

func TestRenderBreakdownFooter_NoDisclaimerWithoutEstimated(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()

	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				Components: []*api.CostComponentEstimate{
					{Name: "Compute", MonthlyCost: 100},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalNewMonthly: 100,
			EstimableCount:  1,
			TotalCount:      1,
		},
	}
	renderBreakdownFooter(&buf, data, 55, 12, "test")
	got := buf.String()

	if strings.Contains(got, "Costs are estimated") {
		t.Error("should not show disclaimer when no estimated components")
	}
}

func TestRenderDiffSummary_ResourceDetectionAndDisclaimer(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()

	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_cloudwatch_log_group",
				ResourceName:          "app",
				ChangeType:            "added",
				NewMonthlyCost:        f64(5),
				MonthlyCostDifference: f64(5),
				Components: []*api.CostComponentEstimate{
					{Name: "Ingestion", MonthlyCost: 5, IsEstimated: boolPtr(true)},
				},
			},
		},
		CostSummary: &api.CostSummary{
			TotalPreviousMonthly:   0,
			TotalNewMonthly:        5,
			TotalMonthlyDifference: 5,
			EstimableCount:         1,
			TotalCount:             4,
			FreeCount:              intPtr(3),
		},
	}

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if !strings.Contains(got, "4 cloud resources were detected") {
		t.Error("expected resource detection summary in diff output")
	}
	if !strings.Contains(got, "3 were free (no usage cost)") {
		t.Error("expected free label with clarification")
	}
	if !strings.Contains(got, "Costs are estimated. Actual usage may vary.") {
		t.Error("expected estimated disclaimer in diff output")
	}
}

func TestRenderMarkdown_EstimatedDisclaimer(t *testing.T) {
	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_cloudwatch_log_group",
				ResourceName:          "app",
				ChangeType:            "added",
				NewMonthlyCost:        f64(5),
				MonthlyCostDifference: f64(5),
				Components: []*api.CostComponentEstimate{
					{Name: "Ingestion", MonthlyCost: 5, IsEstimated: boolPtr(true)},
				},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 5, TotalMonthlyDifference: 5},
	}

	RenderMarkdown(&buf, data, false)
	got := buf.String()

	if !strings.Contains(got, "*Costs are estimated. Actual usage may vary.*") {
		t.Error("expected markdown disclaimer")
	}
}

func TestRenderBreakdownFooter_FreeAndUnsupported(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()

	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{Components: []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 100}}},
		},
		CostSummary: &api.CostSummary{
			TotalNewMonthly: 100,
			EstimableCount:  2,
			TotalCount:      10,
			FreeCount:       intPtr(5),
		},
	}
	renderBreakdownFooter(&buf, data, 55, 16, "test")
	got := buf.String()

	if !strings.Contains(got, "10 cloud resources were detected") {
		t.Error("expected total count")
	}
	if !strings.Contains(got, "2 were estimated") {
		t.Error("expected estimable count")
	}
	if !strings.Contains(got, "5 were free (no usage cost)") {
		t.Error("expected free count")
	}
	if !strings.Contains(got, "3 are not yet supported") {
		t.Error("expected unsupported count")
	}
}

func TestRenderBreakdownFooter_NoUnsupportedLine(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()

	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{Components: []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 50}}},
		},
		CostSummary: &api.CostSummary{
			TotalNewMonthly: 50,
			EstimableCount:  1,
			TotalCount:      4,
			FreeCount:       intPtr(3),
		},
	}
	renderBreakdownFooter(&buf, data, 55, 16, "test")
	got := buf.String()

	if strings.Contains(got, "not yet supported") {
		t.Error("should not show unsupported line when all resources are estimated or free")
	}
}

func TestRenderDiffSummary_FreeAndUnsupported(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()

	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "added",
				NewMonthlyCost:        f64(100),
				MonthlyCostDifference: f64(100),
			},
		},
		CostSummary: &api.CostSummary{
			TotalNewMonthly:        100,
			TotalMonthlyDifference: 100,
			EstimableCount:         1,
			TotalCount:             8,
			FreeCount:              intPtr(5),
		},
	}
	RenderDiff(&buf, data, "test")
	got := buf.String()

	if !strings.Contains(got, "5 were free (no usage cost)") {
		t.Error("expected free count in diff summary")
	}
	if !strings.Contains(got, "2 are not yet supported") {
		t.Error("expected unsupported count in diff summary")
	}
}

func TestRenderMarkdown_ResourceDetectionSummary(t *testing.T) {
	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "added",
				NewMonthlyCost:        f64(100),
				MonthlyCostDifference: f64(100),
			},
		},
		CostSummary: &api.CostSummary{
			TotalNewMonthly: 100,
			EstimableCount:  1,
			TotalCount:      6,
			FreeCount:       intPtr(3),
		},
	}
	RenderMarkdown(&buf, data, false)
	got := buf.String()

	if !strings.Contains(got, "6 cloud resources were detected:") {
		t.Error("expected resource detection in markdown")
	}
	if !strings.Contains(got, "- 1 were estimated") {
		t.Error("expected estimable count in markdown")
	}
	if !strings.Contains(got, "- 3 were free") {
		t.Error("expected free count in markdown")
	}
	if !strings.Contains(got, "- 2 are not yet supported") {
		t.Error("expected unsupported count in markdown")
	}
}

func TestIsUnsupportedResource(t *testing.T) {
	tests := []struct {
		name string
		e    *api.ResourceCostEstimate
		want bool
	}{
		{"nil note", &api.ResourceCostEstimate{}, false},
		{"free note", &api.ResourceCostEstimate{Note: strp("Free resource")}, false},
		{"aws unsupported", &api.ResourceCostEstimate{Note: strp("AWS resource \u2014 pricing not yet supported")}, true},
		{"azure unsupported", &api.ResourceCostEstimate{Note: strp("Azure resource \u2014 pricing not yet supported")}, true},
		{"gcp unsupported", &api.ResourceCostEstimate{Note: strp("GCP resource \u2014 pricing not yet supported")}, true},
		{"random note", &api.ResourceCostEstimate{Note: strp("some other note")}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isUnsupportedResource(tt.e); got != tt.want {
				t.Errorf("isUnsupportedResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRenderBreakdown_UnsupportedResourceFiltered(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	unsupNote := strp("AWS resource \u2014 pricing not yet supported")
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType: "aws_unknown_thing",
				ResourceName: "mystery",
				ChangeType:   "added",
				Note:         unsupNote,
			},
			{
				ResourceType:   "aws_instance",
				ResourceName:   "web",
				ChangeType:     "added",
				NewMonthlyCost: f64(100),
				Components:     []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 100}},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, EstimableCount: 1, TotalCount: 2},
	}

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if strings.Contains(got, "aws_unknown_thing") {
		t.Error("unsupported resource should be hidden from breakdown")
	}
	if !strings.Contains(got, "aws_instance.web") {
		t.Error("priced resource should be present")
	}
}

func TestIsFreeResource(t *testing.T) {
	free := strp("Free resource")
	other := strp("AWS resource \u2014 pricing not yet supported")

	tests := []struct {
		name string
		e    *api.ResourceCostEstimate
		want bool
	}{
		{"nil note", &api.ResourceCostEstimate{}, false},
		{"free note", &api.ResourceCostEstimate{Note: free}, true},
		{"other note", &api.ResourceCostEstimate{Note: other}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFreeResource(tt.e); got != tt.want {
				t.Errorf("isFreeResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRenderBreakdown_FreeResourceFilteredAtRoot(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	freeNote := strp("Free resource")
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:   "aws_iam_role",
				ResourceName:   "app",
				ChangeType:     "added",
				NewMonthlyCost: f64(0),
				Note:           freeNote,
			},
			{
				ResourceType:   "aws_instance",
				ResourceName:   "web",
				ChangeType:     "added",
				NewMonthlyCost: f64(100),
				Components:     []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 100}},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, EstimableCount: 1, TotalCount: 2, FreeCount: intPtr(1)},
	}

	RenderBreakdown(&buf, data, ".")
	got := buf.String()

	if strings.Contains(got, "aws_iam_role.app") {
		t.Error("free resource should be hidden even at root level")
	}
	if !strings.Contains(got, "aws_instance.web") {
		t.Error("paid resource should be present")
	}
}

func TestRenderDiff_FreeResourceFiltered(t *testing.T) {
	var buf bytes.Buffer
	NoColor = true
	defer func() { NoColor = false }()

	freeNote := strp("Free resource")
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_security_group",
				ResourceName:          "sg",
				ChangeType:            "added",
				NewMonthlyCost:        f64(0),
				MonthlyCostDifference: f64(0),
				Note:                  freeNote,
			},
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "added",
				NewMonthlyCost:        f64(100),
				MonthlyCostDifference: f64(100),
				Components:            []*api.CostComponentEstimate{{Name: "Compute", MonthlyCost: 100}},
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, TotalMonthlyDifference: 100, EstimableCount: 1, TotalCount: 2, FreeCount: intPtr(1)},
	}

	RenderDiff(&buf, data, ".")
	got := buf.String()

	if strings.Contains(got, "aws_security_group.sg") {
		t.Error("free resource should be hidden in diff view")
	}
	if !strings.Contains(got, "aws_instance.web") {
		t.Error("paid resource should be present in diff view")
	}
}

func TestRenderMarkdown_FreeResourceFiltered(t *testing.T) {
	var buf bytes.Buffer

	freeNote := strp("Free resource")
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType:          "aws_iam_policy",
				ResourceName:          "admin",
				ChangeType:            "added",
				NewMonthlyCost:        f64(0),
				MonthlyCostDifference: f64(0),
				Note:                  freeNote,
			},
			{
				ResourceType:          "aws_instance",
				ResourceName:          "web",
				ChangeType:            "added",
				NewMonthlyCost:        f64(100),
				MonthlyCostDifference: f64(100),
			},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 100, EstimableCount: 1, TotalCount: 2, FreeCount: intPtr(1)},
	}

	RenderMarkdown(&buf, data, false)
	got := buf.String()

	if strings.Contains(got, "aws_iam_policy") {
		t.Error("free resource should be hidden in markdown")
	}
	if !strings.Contains(got, "aws_instance") {
		t.Error("paid resource should be present in markdown")
	}
}

func TestRenderSuggestionsTable_AllFilteredReturnsEmpty(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()
	GravitonEnabled = false
	defer func() { GravitonEnabled = false }()

	var buf bytes.Buffer
	suggestions := []*api.UpgradeSuggestion{
		{ResourceType: "aws_instance", ResourceName: "web", Reason: "Switch to Graviton", Category: "graviton"},
	}
	renderSuggestionsTable(&buf, suggestions)
	got := buf.String()

	if strings.Contains(got, "Optimization") {
		t.Error("should not render table when all suggestions are filtered")
	}
}

func TestRenderSuggestionsTable_DuplicateResourceWithModule(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()

	var buf bytes.Buffer
	suggestions := []*api.UpgradeSuggestion{
		{ResourceType: "aws_instance", ResourceName: "web", ModulePath: strp("module.app"), Reason: "Upgrade instance", EstimatedMonthlySavings: f64(10)},
		{ResourceType: "aws_instance", ResourceName: "web", ModulePath: strp("module.app"), Reason: "Use gp3 volume", EstimatedMonthlySavings: f64(5)},
	}
	renderSuggestionsTable(&buf, suggestions)
	got := buf.String()

	count := strings.Count(got, "module.app.aws_instance.web")
	if count != 1 {
		t.Errorf("module-qualified resource name should appear exactly once, got %d", count)
	}
	if !strings.Contains(got, "Upgrade instance") || !strings.Contains(got, "Use gp3 volume") {
		t.Error("both suggestions should be present")
	}
}

func TestRenderSuggestionsTable_NoSavingsFallback(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()

	var buf bytes.Buffer
	suggestions := []*api.UpgradeSuggestion{
		{ResourceType: "aws_instance", ResourceName: "web", Reason: "Upgrade to m6i", Category: "perf_upgrade"},
	}
	renderSuggestionsTable(&buf, suggestions)
	got := buf.String()

	if !strings.Contains(got, "better performance, same cost") {
		t.Errorf("expected fallback savings text, got %q", got)
	}
}
