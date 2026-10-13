package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

const (
	hyperlink   = "\x1b]8;;evil\x07"
	clearScreen = "\x1b[2J"
)

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"Web" + hyperlink:          "Web�]8;;evil�",
		"note" + clearScreen:       "note�[2J",
		"bell\a del\x7f csi\u009b": "bell� del� csi�",
		"raw \x9b byte":            "raw � byte",
		"two\nlines\tand tab":      "two�lines�and tab",
		"plain AWS::EC2::Instance": "plain AWS::EC2::Instance",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	if got := SanitizeMultiline("two\nlines\tok" + clearScreen); got != "two\nlines\tok�[2J" {
		t.Errorf("SanitizeMultiline = %q", got)
	}
}

func hostileResponse() *api.AnalyzePrResponse {
	return &api.AnalyzePrResponse{
		ResourceCostEstimates: []*api.ResourceCostEstimate{
			{
				ResourceType: "AWS::EC2::Instance" + clearScreen, ResourceName: "Web" + hyperlink, ChangeType: "added",
				Service: "EC2" + hyperlink, ModulePath: strp("module.x" + hyperlink), Scope: strp("app.yaml" + hyperlink),
				Note: strp("note" + clearScreen), NewMonthlyCost: f64(10), MonthlyCostDifference: f64(10),
				Components: []*api.CostComponentEstimate{{
					Name: "Usage" + hyperlink, MonthlyCost: 10, Units: f64(1), UnitLabel: strp("hours" + clearScreen),
					Subresource: strp("root" + hyperlink), Attribute: strp("InstanceType" + hyperlink),
				}},
			},
			{ResourceType: "aws_instance", ResourceName: "plain", ChangeType: "removed", PreviousMonthlyCost: f64(5), MonthlyCostDifference: f64(-5), Scope: strp("other")},
		},
		CostSummary: &api.CostSummary{TotalNewMonthly: 10, TotalMonthlyDifference: 5, EstimableCount: 2, TotalCount: 2},
		UpgradeSuggestions: []*api.UpgradeSuggestion{{
			ResourceType: "AWS::EC2::Instance" + clearScreen, ResourceName: "Web" + hyperlink, File: "app.yaml" + hyperlink,
			Attribute: "InstanceType" + hyperlink, CurrentValue: "m5" + clearScreen, SuggestedValue: "m6i" + hyperlink,
			Category: "perf_upgrade", Reason: "Upgrade" + hyperlink, EstimatedMonthlySavings: f64(2),
			Note: strp("line one\nline two" + clearScreen), ModulePath: strp("module.x" + hyperlink), Scope: strp("app.yaml" + hyperlink),
			Location:            &api.SourceLocation{File: "app.yaml" + hyperlink, Line: 3},
			CompanionAttributes: []*api.CompanionAttribute{{Attribute: "Iops" + hyperlink, Value: "3000" + clearScreen, Reason: "gp3" + hyperlink}},
			GravitonAlternative: &api.GravitonAlternative{SuggestedValue: "m7g" + hyperlink, Reason: "Graviton" + clearScreen},
		}},
		Diagnostics: []*api.Diagnostic{
			{Code: "parameter_defaulted" + hyperlink, Message: "Env priced at its default" + clearScreen, File: strp("app.yaml" + hyperlink), Line: intPtr(2)},
			{Code: "condition_assumed", Message: "IsProd assumed true" + hyperlink},
		},
	}
}

func assertNoEscape(t *testing.T, name, got string) {
	t.Helper()
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("%s printed a control character:\n%q", name, got)
	}
}

func TestRenderers_StripControlCharactersFromTemplateAndAPIText(t *testing.T) {
	NoColor = true
	GravitonEnabled = true
	defer func() { NoColor = false; GravitonEnabled = false }()
	data := hostileResponse()

	var breakdown, diff, markdown bytes.Buffer
	RenderBreakdown(&breakdown, data, "project"+hyperlink)
	RenderDiff(&diff, data, "project"+hyperlink)
	RenderMarkdown(&markdown, data, true)

	assertNoEscape(t, "breakdown", breakdown.String())
	assertNoEscape(t, "diff", diff.String())
	assertNoEscape(t, "markdown", markdown.String())
	if !strings.Contains(markdown.String(), "Env priced at its default") {
		t.Errorf("the diagnostic itself was lost:\n%s", markdown.String())
	}
	if data.ResourceCostEstimates[0].ResourceName != "Web"+hyperlink {
		t.Error("rendering changed the response, so JSON output would no longer be raw")
	}
}

func TestNoticesStripControlCharacters(t *testing.T) {
	outBuf, errBuf := setupOutput(t)

	ProgressList("Uploading 1 file:"+clearScreen, []string{"app.yaml" + hyperlink})
	Warning("Skipping " + hyperlink)
	Info("Found " + clearScreen)
	InfoLabel("Found " + hyperlink)
	Success("done" + clearScreen)
	WarnLabel("careful" + hyperlink)
	ErrorLabel("failed" + clearScreen)
	Error("broke" + hyperlink)

	assertNoEscape(t, "stdout", outBuf.String())
	assertNoEscape(t, "stderr", errBuf.String())
}

func TestSanitize_StripsFormatCharactersThatSpoofANameOnScreen(t *testing.T) {
	cases := map[string]string{
		"app\u202Elmay.json":               "app\uFFFDlmay.json",
		"a\u202Ab\u202Bc\u202Cd\u202De":    "a\uFFFDb\uFFFDc\uFFFDd\uFFFDe",
		"x\u2066y\u2067z\u2068w\u2069":     "x\uFFFDy\uFFFDz\uFFFDw\uFFFD",
		"zero\u200Bwidth\u200Fmark":        "zero\uFFFDwidth\uFFFDmark",
		"\uFEFFbom":                        "\uFFFDbom",
		"line\u2028para\u2029end":          "line\uFFFDpara\uFFFDend",
		"caf\u00E9 \u00FCber \u65E5\u672C": "caf\u00E9 \u00FCber \u65E5\u672C",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
		if got := SanitizeMultiline(in); got != want {
			t.Errorf("SanitizeMultiline(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNotices_KeepATemplatePathOnOneLine(t *testing.T) {
	outBuf, errBuf := setupOutput(t)
	forged := "a.yaml\nWarning: \tforged"

	Warning("Skipping " + forged + ": not tracked by git")
	Info("Found " + forged)
	InfoLabel("Found " + forged)

	for name, got := range map[string]string{"stdout": outBuf.String(), "stderr": errBuf.String()} {
		for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
			if strings.HasPrefix(line, "Warning: ") && !strings.Contains(line, "Skipping") {
				t.Errorf("%s carries a forged line: %q", name, got)
			}
		}
		if strings.Contains(got, "\t") {
			t.Errorf("%s kept a tab: %q", name, got)
		}
	}
	if strings.Count(errBuf.String(), "\n") != 1 {
		t.Errorf("stderr = %q, want one line", errBuf.String())
	}
}

func TestJSONOutput_EscapesFormatCharactersAndKeepsTheValue(t *testing.T) {
	name := "app\u202Elmay.json"
	value := map[string]string{"resource_name": name}

	for _, print := range map[string]func(){
		"json": func() { PrintJSON(value) },
		"jq": func() {
			JQExpression = "."
			defer func() { JQExpression = "" }()
			_ = PrintResult(value)
		},
	} {
		outBuf, _ := setupOutput(t)
		print()

		if strings.Contains(outBuf.String(), "\u202E") {
			t.Errorf("a right-to-left override reached stdout raw: %q", outBuf.String())
		}
		if !strings.Contains(outBuf.String(), `\u202e`) {
			t.Errorf("stdout = %q, want the override as \\u202e", outBuf.String())
		}
		var decoded map[string]string
		if err := json.Unmarshal(outBuf.Bytes(), &decoded); err != nil || decoded["resource_name"] != name {
			t.Errorf("decoded = %v, %v; want the original value", decoded, err)
		}
	}
}

func TestEscapeFormatRunes_UsesSurrogatesAboveTheBasicPlane(t *testing.T) {
	got := string(escapeFormatRunes([]byte("\"tag" + string(rune(0xE0001)) + "\"")))
	if got != `"tag\udb40\udc01"` {
		t.Errorf("escapeFormatRunes = %q", got)
	}
}
