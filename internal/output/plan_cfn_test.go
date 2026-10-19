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
