package output

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

func cfnEstimate(resourceType, name, scope string, cost float64) *api.ResourceCostEstimate {
	return &api.ResourceCostEstimate{
		ResourceType:          resourceType,
		ResourceName:          name,
		ChangeType:            "added",
		Scope:                 strp(scope),
		NewMonthlyCost:        f64(cost),
		MonthlyCostDifference: f64(cost),
		Components:            []*api.CostComponentEstimate{{Name: "Instance usage", MonthlyCost: cost}},
	}
}

func cfnResponse() *api.AnalyzePrResponse {
	return &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			cfnEstimate("AWS::EC2::Instance", "Web", "stacks/app.yaml", 70),
			cfnEstimate("AWS::RDS::DBInstance", "Main", "stacks/app.yaml/Database", 260),
			cfnEstimate("aws_instance", "worker", "infra", 30),
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 360, TotalMonthlyDifference: 360, EstimableCount: 3, TotalCount: 3},
		UpgradeSuggestions: []*api.UpgradeSuggestion{
			{
				ResourceType: "AWS::EC2::Instance", ResourceName: "Web", Reason: "Upgrade to m6i",
				EstimatedMonthlySavings: f64(4), Location: &api.SourceLocation{File: "stacks/app.yaml", Line: 12},
			},
			{ResourceType: "aws_instance", ResourceName: "worker", File: "infra/main.tf", Reason: "Upgrade to t3"},
		},
		Diagnostics: []*api.Diagnostic{
			{Code: "parameter_defaulted", Message: "InstanceType priced at its default m5.large", File: strp("stacks/app.yaml"), Line: intPtr(4)},
			{Code: "import_value", Message: "An imported value could not be resolved", File: strp("stacks/app.yaml")},
			{Code: "condition_assumed", Message: "IsProd assumed true"},
		},
	}
}

func TestRenderBreakdown_CloudFormationResourcesGroupedByStack(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()
	var buf bytes.Buffer

	RenderBreakdown(&buf, cfnResponse(), ".")

	got := buf.String()
	for _, want := range []string{
		"AWS::EC2::Instance Web",
		"stacks/app.yaml",
		"stacks/app.yaml/Database",
		"AWS::RDS::DBInstance Main",
		"aws_instance.worker",
		"Location",
		"stacks/app.yaml:12",
		"infra/main.tf",
		"Assumptions:",
		"InstanceType priced at its default m5.large (stacks/app.yaml:4)",
		"An imported value could not be resolved (stacks/app.yaml)",
		"IsProd assumed true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("breakdown missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDiff_ShowsAssumptions(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()
	var buf bytes.Buffer

	RenderDiff(&buf, cfnResponse(), ".")

	if !strings.Contains(buf.String(), "+ AWS::EC2::Instance Web") || !strings.Contains(buf.String(), "Assumptions:") {
		t.Errorf("diff output:\n%s", buf.String())
	}
}

func TestRenderMarkdown_CloudFormationLocationsAndAssumptions(t *testing.T) {
	var buf bytes.Buffer

	RenderMarkdown(&buf, cfnResponse(), true)

	got := buf.String()
	for _, want := range []string{
		"| Location | Resource | Type | Cost/mo | Delta |",
		"| `stacks/app.yaml/Database` | + Main | AWS::RDS::DBInstance |",
		"| Resource | Suggestion | Est. Savings | Location |",
		"| AWS::EC2::Instance Web | Upgrade to m6i | $4.00/mo | `stacks/app.yaml:12` |",
		"**Assumptions**",
		"- InstanceType priced at its default m5.large (stacks/app.yaml:4)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("markdown missing %q:\n%s", want, got)
		}
	}
}

func TestBuildModuleTree_OneScopeIsNotAGroup(t *testing.T) {
	tree := buildModuleTree([]*api.ResourceCostEstimate{
		cfnEstimate("AWS::EC2::Instance", "Web", "template.yaml", 70),
		cfnEstimate("AWS::S3::Bucket", "Logs", "template.yaml", 1),
	})

	if len(tree.children) != 0 || len(tree.resources) != 2 {
		t.Errorf("a single template made %d groups", len(tree.children))
	}
}

func TestScopeSegments(t *testing.T) {
	cases := map[string][]string{
		"":                         nil,
		"envs/prod":                {"envs/prod"},
		"stacks/app.yaml":          {"stacks/app.yaml"},
		"app.json/Db/Cache":        {"app.json", "app.json/Db", "app.json/Db/Cache"},
		".hidden/app.template/Net": {".hidden/app.template", ".hidden/app.template/Net"},
	}
	for scope, want := range cases {
		if got := scopeSegments(scope); !reflect.DeepEqual(got, want) {
			t.Errorf("scopeSegments(%q) = %v, want %v", scope, got, want)
		}
	}
}

func TestResourceLabel(t *testing.T) {
	if got := resourceLabel("aws_instance", "web"); got != "aws_instance.web" {
		t.Errorf("terraform label = %q", got)
	}
	if got := resourceLabel("AWS::EC2::Instance", "Web"); got != "AWS::EC2::Instance Web" {
		t.Errorf("cloudformation label = %q", got)
	}
}

// componentIndents returns the column at which each component row starts.
func componentIndents(out string, names ...string) []int {
	var indents []int
	for _, line := range strings.Split(out, "\n") {
		for _, name := range names {
			if strings.Contains(line, "─ "+name) {
				indents = append(indents, strings.Index(line, "─")-len("└"))
			}
		}
	}
	return indents
}

func TestRenderBreakdown_ComponentsLineUpUnderEverySibling(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()
	var buf bytes.Buffer
	first := cfnEstimate("AWS::EC2::Instance", "Web", "stacks/web.yaml", 70)
	last := cfnEstimate("AWS::EC2::Instance", "Worker", "stacks/web.yaml", 140)
	for _, e := range []*api.ResourceCostEstimate{first, last} {
		e.Components = append(e.Components, &api.CostComponentEstimate{Name: "Storage", MonthlyCost: 4, Subresource: strp("root_block_device")})
	}
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{first, last, cfnEstimate("AWS::EC2::Instance", "Batch", "stacks/batch.yaml", 140)},
		CostSummary:           &api.CostSummary{TotalNewMonthly: 350},
	}

	RenderBreakdown(&buf, data, ".")

	out := buf.String()
	usage := componentIndents(out, "Instance usage")
	if len(usage) != 3 || usage[0] != usage[1] || usage[1] != usage[2] {
		t.Errorf("instance usage rows start at %v, want one column:\n%s", usage, out)
	}
	if storage := componentIndents(out, "Storage"); len(storage) != 2 || storage[0] != storage[1] {
		t.Errorf("storage rows start at %v, want one column:\n%s", storage, out)
	}
}

func TestRenderDiff_ARemovedResourceTakesItsComponentsWithIt(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()
	var buf bytes.Buffer
	data := &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{{
			ResourceType: "AWS::EC2::Instance", ResourceName: "Web", ChangeType: "removed", Scope: strp("app.yaml"),
			PreviousMonthlyCost: f64(80.08), MonthlyCostDifference: f64(-80.08),
			Components: []*api.CostComponentEstimate{
				{Name: "Instance usage", MonthlyCost: 70.08},
				{Name: "Storage", MonthlyCost: 10, Subresource: strp("root_block_device")},
				{Name: "IOPS", MonthlyCost: 0, Subresource: strp("root_block_device")},
				{Name: "Throughput", MonthlyCost: 0, Subresource: strp("throughput_only")},
			},
		}},
		CostSummary: &api.CostSummary{TotalPreviousMonthly: 80.08, TotalMonthlyDifference: -80.08},
	}

	RenderDiff(&buf, data, ".")

	out := buf.String()
	for _, want := range []string{"- Instance usage", "-$70.08", "- Storage", "-$10.00"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff missing %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"+ Instance usage", "+$70.08", "IOPS", "Throughput", "throughput_only"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("diff shows %q:\n%s", unwanted, out)
		}
	}
}

func TestRenderDiff_AnAddedComponentThatCostsNothingIsLeftOut(t *testing.T) {
	NoColor = true
	defer func() { NoColor = false }()
	var buf bytes.Buffer
	added := cfnEstimate("AWS::EC2::Instance", "Batch", "crlf.yaml", 140)
	added.Components = append(added.Components, &api.CostComponentEstimate{Name: "IOPS", MonthlyCost: 0, Subresource: strp("root_block_device")})

	RenderDiff(&buf, &api.AnalyzePrResponse{ResourceCostEstimates: []*api.ResourceCostEstimate{added}}, ".")

	if out := buf.String(); strings.Contains(out, "IOPS") || strings.Contains(out, "root_block_device") {
		t.Errorf("diff shows a component that costs nothing:\n%s", out)
	}
}
