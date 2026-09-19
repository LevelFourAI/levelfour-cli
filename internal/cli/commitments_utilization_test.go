package cli

import (
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

const (
	riUtilBody = `{"data":{"instrument":"ri","services":[{"service":"ec2","service_label":"EC2",` +
		`"utilization_pct":98.2,"dimensions":[{"key":"capacity","used":1240,"total":1263,` +
		`"unit":"vCPU","utilization_pct":98.2}],"committed_monthly_usd":1033.33,` +
		`"committed_hourly_usd":null,"contracted_hourly_usd":null,"applied_to":[],` +
		`"days":[{"date":"2026-09-01","capacity_pct":98.2,"capacity_used":1240,` +
		`"capacity_total":1263,"coverage_pct":61.4}]}]}}`

	// A plan day carries no coverage key at all, and the plan holds no dimensions.
	spUtilBody = `{"data":{"instrument":"sp","services":[{"service":"compute",` +
		`"service_label":"Compute","utilization_pct":100,"dimensions":[],` +
		`"committed_monthly_usd":null,"committed_hourly_usd":5.5,"contracted_hourly_usd":5.5,` +
		`"applied_to":[],"days":[{"date":"2026-09-01","commitment_pct":100,` +
		`"commitment_used_usd":5.5,"commitment_total_usd":5.5}]}]}}`

	twoServiceUtilBody = `{"data":{"instrument":"ri","services":[` +
		`{"service":"ec2","service_label":"EC2","utilization_pct":98.2,"dimensions":[],` +
		`"committed_monthly_usd":null,"committed_hourly_usd":null,"contracted_hourly_usd":null,` +
		`"days":[{"date":"2026-09-01","capacity_pct":98.2}]},` +
		`{"service":"rds","service_label":"RDS","utilization_pct":87.5,"dimensions":[],` +
		`"committed_monthly_usd":null,"committed_hourly_usd":null,"contracted_hourly_usd":null,` +
		`"days":[{"date":"2026-09-01","capacity_pct":87.5}]}]}}`
)

func utilizationServer(t *testing.T, provider, body string) {
	t.Helper()
	useCommitmentsServer(t, commitmentsServer(t, []string{provider}, map[string]string{
		"/utilization": body,
	}))
}

func TestCommitmentsUtilizationKeepsEachServiceOnItsOwnUnit(t *testing.T) {
	utilizationServer(t, providerAWS, riUtilBody)

	out, _, err := executeCommand(t, "commitments", "utilization")
	if err != nil {
		t.Fatalf("utilization error: %v", err)
	}
	for _, want := range []string{"EC2", "98.2%", "1240/1263 vCPU", "$1033.33/mo", "2026-09-01", "61.4%"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

// Cost Explorer cannot filter coverage by plan type, so the plan series has no
// coverage column at all rather than an empty one.
func TestCommitmentsUtilizationGivesAPlanNoCoverageColumn(t *testing.T) {
	utilizationServer(t, providerAWS, spUtilBody)

	out, _, err := executeCommand(t, "commitments", "utilization", "--instrument", "sp")
	if err != nil {
		t.Fatalf("utilization error: %v", err)
	}
	if strings.Contains(out.String(), "Coverage") {
		t.Errorf("a plan series must not carry a coverage column:\n%s", out.String())
	}
	for _, want := range []string{"Compute", "$5.50/hr", "Committed $", notMeasured} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommitmentsUtilizationSkipsTheSeriesForSeveralServices(t *testing.T) {
	utilizationServer(t, providerAWS, twoServiceUtilBody)

	out, _, err := executeCommand(t, "commitments", "utilization")
	if err != nil {
		t.Fatalf("utilization error: %v", err)
	}
	if strings.Contains(out.String(), "over time") {
		t.Errorf("a day series only makes sense for one service:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "RDS") {
		t.Errorf("every service should be listed:\n%s", out.String())
	}
}

func TestCommitmentsUtilizationNamesTheProviderItMeasuredNothingFor(t *testing.T) {
	utilizationServer(t, providerGCP, `{"data":{"instrument":"ri","services":[]}}`)

	out, _, err := executeCommand(t, "commitments", "utilization")
	if err != nil {
		t.Fatalf("utilization error: %v", err)
	}
	if !strings.Contains(out.String(), "No RI utilization measured for Google Cloud") {
		t.Errorf("an empty result should name the provider:\n%s", out.String())
	}
}

func TestCommitmentsUtilizationRejectsAnUnknownInstrument(t *testing.T) {
	utilizationServer(t, providerAWS, riUtilBody)

	if _, _, err := executeCommand(t, "commitments", "utilization", "--instrument", "cud"); err == nil {
		t.Error("expected an error for an unknown instrument")
	}
}

func TestCommitmentsUtilizationPassesTheWindowThrough(t *testing.T) {
	utilizationServer(t, providerAWS, riUtilBody)

	if _, _, err := executeCommand(t, "commitments", "utilization",
		"--granularity", "monthly", "--start", "2026-01-01", "--end", "2026-09-01",
		"--service", "ec2"); err != nil {
		t.Fatalf("utilization error: %v", err)
	}
}

func TestCommitmentsUtilizationJSONAndWeb(t *testing.T) {
	utilizationServer(t, providerAWS, riUtilBody)
	out, _, err := executeCommand(t, "commitments", "utilization", "--json")
	if err != nil {
		t.Fatalf("utilization --json error: %v", err)
	}
	if !strings.Contains(out.String(), "vCPU") {
		t.Errorf("JSON output should carry the payload:\n%s", out.String())
	}

	utilizationServer(t, providerAWS, riUtilBody)
	original := openBrowser
	var opened string
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = original }()
	if _, _, err = executeCommand(t, "commitments", "utilization", "--instrument", "sp", "--web"); err != nil {
		t.Fatalf("utilization --web error: %v", err)
	}
	if !strings.Contains(opened, "section=sp") {
		t.Errorf("opened %q, want the plans page", opened)
	}
}

func TestCommitmentsUtilizationReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "commitments", "utilization")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}
}

func TestCommitmentsUtilizationUnauthenticated(t *testing.T) {
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "utilization"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

func TestCommittedSummaryPrefersTheContractedRate(t *testing.T) {
	hourly, monthly := 5.5, 1033.33
	cases := []struct {
		name    string
		service api.ServiceUtilization
		want    string
	}{
		{"contracted", api.ServiceUtilization{ContractedHourlyUSD: &hourly}, "$5.50/hr"},
		{"monthly", api.ServiceUtilization{CommittedMonthlyUSD: &monthly}, "$1033.33/mo"},
		{"neither", api.ServiceUtilization{}, notMeasured},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := committedSummary(tc.service); got != tc.want {
				t.Errorf("committedSummary() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRenderUtilizationDaysSkipsAnEmptySeries(t *testing.T) {
	captureOutput(t)
	renderUtilizationDays(api.ServiceUtilization{ServiceLabel: "EC2"}, instrumentRI)
}
