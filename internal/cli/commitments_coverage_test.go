package cli

import (
	"strings"
	"testing"
)

const (
	coverageBody = `{"data":{"provider":"aws","measured":true,` +
		`"services":[` +
		`{"instrument":"ri","dimension":"redshift","coverage_pct":95,"measured_on":"2026-09-18"},` +
		`{"instrument":"ri","dimension":"rds","coverage_pct":66.6,"measured_on":"2026-09-18"},` +
		`{"instrument":"sp","dimension":"compute","coverage_pct":54.9,"measured_on":null}],` +
		`"accounts":[{"account_id":"111122223333","account_name":"prod","covered_monthly":31400}],` +
		`"uncovered_by_service":[{"service":"ec2","on_demand_monthly":18200}],` +
		`"totals":{"covered_monthly":41200,"uncovered_monthly":24800,"coverage_pct":80.8}}}`

	// Google Cloud stores only the commitment fee, so covered_monthly is absent.
	gcpCoverageBody = `{"data":{"provider":"gcp","measured":true,` +
		`"services":[{"instrument":"ri","dimension":"committed_use_discount",` +
		`"coverage_pct":52,"measured_on":null}],"accounts":[],"uncovered_by_service":[],` +
		`"totals":{"covered_monthly":null,"uncovered_monthly":8000,"coverage_pct":52}}}`

	unmeasuredCoverageBody = `{"data":{"provider":"aws","measured":false,"services":[],` +
		`"accounts":[],"uncovered_by_service":[],"totals":null}}`
)

func coverageServer(t *testing.T, provider, body string) {
	t.Helper()
	useCommitmentsServer(t, commitmentsServer(t, []string{provider}, map[string]string{
		"/coverage-rates": body,
	}))
}

func TestCommitmentsCoverageLeadsWithTheWeightedRate(t *testing.T) {
	coverageServer(t, providerAWS, coverageBody)

	out, _, err := executeCommand(t, "commitments", "coverage")
	if err != nil {
		t.Fatalf("coverage error: %v", err)
	}
	for _, want := range []string{"80.8%", "$41200.00/mo", "$24800.00/mo",
		"redshift", "95.0%", "rds", "66.6%", "2026-09-18"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommitmentsCoverageNarrowsToOneInstrument(t *testing.T) {
	coverageServer(t, providerAWS, coverageBody)

	out, _, err := executeCommand(t, "commitments", "coverage", "--instrument", "sp")
	if err != nil {
		t.Fatalf("coverage error: %v", err)
	}
	if !strings.Contains(out.String(), "compute") || strings.Contains(out.String(), "redshift") {
		t.Errorf("--instrument sp should drop the reservations:\n%s", out.String())
	}
}

func TestCommitmentsCoverageRejectsAnUnknownInstrument(t *testing.T) {
	coverageServer(t, providerAWS, coverageBody)

	if _, _, err := executeCommand(t, "commitments", "coverage", "--instrument", "cud"); err == nil {
		t.Error("expected an error for an unknown instrument")
	}
}

// A sweep that has not run and an estate covering nothing are opposite answers.
func TestCommitmentsCoverageSaysWhenNothingWasMeasured(t *testing.T) {
	coverageServer(t, providerAWS, unmeasuredCoverageBody)

	out, _, err := executeCommand(t, "commitments", "coverage")
	if err != nil {
		t.Fatalf("coverage error: %v", err)
	}
	if !strings.Contains(out.String(), "not measured for AWS yet") {
		t.Errorf("an unmeasured sweep should say so:\n%s", out.String())
	}
	if strings.Contains(out.String(), "0.0%") {
		t.Errorf("nothing measured must never render as a zero rate:\n%s", out.String())
	}
}

func TestCommitmentsCoverageWillNotInventGoogleCloudCoveredSpend(t *testing.T) {
	coverageServer(t, providerGCP, gcpCoverageBody)

	out, _, err := executeCommand(t, "commitments", "coverage")
	if err != nil {
		t.Fatalf("coverage error: %v", err)
	}
	// Anchored on the card, because measured_on is also null in this payload and
	// contributes its own "not measured" elsewhere in the output.
	if !strings.Contains(out.String(), "Covered spend") || strings.Contains(out.String(), notMeasured+"/mo") {
		t.Errorf("an absent covered spend should read as unmeasured, with no stray suffix:\n%s", out.String())
	}
	for _, want := range []string{"52.0%", "committed_use_discount"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommitmentsCoverageWithNoMeasuredService(t *testing.T) {
	coverageServer(t, providerAWS, `{"data":{"provider":"aws","measured":true,"services":[],`+
		`"accounts":[],"uncovered_by_service":[],"totals":{"covered_monthly":0,`+
		`"uncovered_monthly":100,"coverage_pct":0}}}`)

	out, _, err := executeCommand(t, "commitments", "coverage")
	if err != nil {
		t.Fatalf("coverage error: %v", err)
	}
	if !strings.Contains(out.String(), "No service has a measured coverage figure") {
		t.Errorf("output should say no service was measured:\n%s", out.String())
	}
}

// measured true with no totals is not a shape the API sends, but the payload is
// a trust boundary and a nil dereference here would panic the command.
func TestCommitmentsCoverageSurvivesAbsentTotals(t *testing.T) {
	coverageServer(t, providerAWS, `{"data":{"provider":"aws","measured":true,`+
		`"services":[{"instrument":"ri","dimension":"ec2","coverage_pct":71.2,`+
		`"measured_on":"2026-09-18"}],"accounts":[],"uncovered_by_service":[],"totals":null}}`)

	out, _, err := executeCommand(t, "commitments", "coverage")
	if err != nil {
		t.Fatalf("coverage error: %v", err)
	}
	if !strings.Contains(out.String(), "71.2%") {
		t.Errorf("the service table should still render:\n%s", out.String())
	}
}

func TestCommitmentsCoverageJSONAndWeb(t *testing.T) {
	coverageServer(t, providerAWS, coverageBody)
	out, _, err := executeCommand(t, "commitments", "coverage", "--json")
	if err != nil {
		t.Fatalf("coverage --json error: %v", err)
	}
	if !strings.Contains(out.String(), "uncovered_by_service") {
		t.Errorf("JSON output should carry the whole payload:\n%s", out.String())
	}

	coverageServer(t, providerAWS, coverageBody)
	original := openBrowser
	var opened string
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = original }()
	if _, _, err = executeCommand(t, "commitments", "coverage", "--instrument", "sp", "--web"); err != nil {
		t.Fatalf("coverage --web error: %v", err)
	}
	if !strings.Contains(opened, "section=sp") {
		t.Errorf("opened %q, want the instrument the flag asked for", opened)
	}

	coverageServer(t, providerAWS, coverageBody)
	if _, _, err = executeCommand(t, "commitments", "coverage", "--web"); err != nil {
		t.Fatalf("coverage --web error: %v", err)
	}
	if !strings.Contains(opened, "section=ri") {
		t.Errorf("opened %q, want the default section", opened)
	}
}

func TestCommitmentsCoverageReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "commitments", "coverage")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}
}

func TestCommitmentsCoverageUnauthenticated(t *testing.T) {
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "coverage"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

func TestIsInstrument(t *testing.T) {
	for _, valid := range []string{instrumentRI, instrumentSP} {
		if !isInstrument(valid) {
			t.Errorf("isInstrument(%q) = false", valid)
		}
	}
	for _, invalid := range []string{"", "cud", "RI"} {
		if isInstrument(invalid) {
			t.Errorf("isInstrument(%q) = true", invalid)
		}
	}
}
