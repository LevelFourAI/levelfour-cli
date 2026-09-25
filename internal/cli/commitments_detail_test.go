package cli

import (
	"strings"
	"testing"
)

const (
	detailBody = `{"data":{"id":"ri-a1b2","provider":"aws","service":"ec2","service_label":"EC2",` +
		`"account_id":"111122223333","account_name":"prod","region":"us-east-1",` +
		`"kind":"reserved_instance","start_date":"2025-10-07","end_date":"2026-10-07",` +
		`"term_months":12,"payment_option":"all_upfront","monthly_commitment_usd":1033.33,` +
		`"status":"active","current_utilization_pct":98.2,"current_coverage_pct":61.4,` +
		`"is_commitable":true,"protects_monthly":12400,"exchangeable":true,` +
		`"consumers":[{"account_id":"444455556666","account_name":"data",` +
		`"covered_spend_monthly":900,"covered_hours":720}],` +
		`"recommendations":[{"id":"REC-1234","kind":"buy_reserved_instance","monthly_savings":210,` +
		`"confidence":"high","summary":"Buy four more"}]}}`

	// A plan with nothing the export fills: term, payment option and coverage
	// all have to read as unmeasured rather than as zero or blank.
	planDetailBody = `{"data":{"id":"sp-9f3e","provider":"aws","service":"compute",` +
		`"service_label":"Compute","account_id":"111122223333","account_name":"payer",` +
		`"kind":"savings_plan","start_date":"","end_date":"","term_months":0,` +
		`"payment_option":null,"monthly_commitment_usd":0,"status":"active",` +
		`"current_utilization_pct":100,"current_coverage_pct":0,"exchangeable":null}}`

	renewalBody = `{"data":{"commitment_id":"ri-a1b2","service":"ec2",` +
		`"holder_account_id":"111122223333","end_at":"2026-10-07T00:00:00Z",` +
		`"buy_after_utc":"2026-10-07T00:00:00Z","expires_in_seconds":1555200,` +
		`"units_held":40.0,"units_consumed":33.5,"units_recommended":36.0,` +
		`"protects_monthly":12400,"rightsizing_monthly":620,` +
		`"pending_changes":[{"recommendation_id":"REC-1234","service":"ec2","account":"prod",` +
		`"monthly_savings":210,"status":"available"}],"exchangeable":true,"cancellable":false}}`

	renewalUndatedBody = `{"data":{"commitment_id":"cud-1","service":"compute",` +
		`"holder_account_id":"proj-1","end_at":null,"buy_after_utc":null,` +
		`"expires_in_seconds":null,"units_held":1.0,"units_consumed":null,"units_recommended":1.0,` +
		`"protects_monthly":0,"rightsizing_monthly":0,"pending_changes":[],` +
		`"exchangeable":null,"cancellable":null}}`
)

func detailServer(t *testing.T, body string) {
	t.Helper()
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/detail/":       body,
		"/renewal-plan/": renewalBody,
	}))
}

func TestCommitmentsViewShowsTheWholeCommitment(t *testing.T) {
	detailServer(t, detailBody)

	out, _, err := executeCommand(t, "commitments", "view", "ri-a1b2")
	if err != nil {
		t.Fatalf("view error: %v", err)
	}
	for _, want := range []string{"ri-a1b2", "EC2", "12 months", "all_upfront", "61.4%",
		"data", "REC-1234", "l4 rec accept"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommitmentsViewNamesEveryUnmeasuredField(t *testing.T) {
	detailServer(t, planDetailBody)

	out, _, err := executeCommand(t, "commitments", "view", "sp-9f3e")
	if err != nil {
		t.Fatalf("view error: %v", err)
	}
	// Term, start, end, payment option, coverage and exchangeable are all absent.
	if got := strings.Count(out.String(), notMeasured); got < 6 {
		t.Errorf("expected every absent field to say so, found %d:\n%s", got, out.String())
	}
}

// A Savings Plan id is an ARN, so it has to survive the path it is written into.
func TestCommitmentsViewEscapesAnARNIntoThePath(t *testing.T) {
	arn := "arn:aws:savingsplans::111122223333:savingsplan/abc"
	detailServer(t, detailBody)

	if _, _, err := executeCommand(t, "commitments", "view", arn); err != nil {
		t.Fatalf("view error: %v", err)
	}
}

func TestCommitmentsViewJSONAndWeb(t *testing.T) {
	detailServer(t, detailBody)
	out, _, err := executeCommand(t, "commitments", "view", "ri-a1b2", "--json")
	if err != nil {
		t.Fatalf("view --json error: %v", err)
	}
	if !strings.Contains(out.String(), "ri-a1b2") {
		t.Errorf("JSON output should carry the payload:\n%s", out.String())
	}

	detailServer(t, detailBody)
	original := openBrowser
	var opened string
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = original }()
	if _, _, err = executeCommand(t, "commitments", "view", "ri-a1b2", "--web"); err != nil {
		t.Fatalf("view --web error: %v", err)
	}
	if !strings.Contains(opened, "commitment=ri-a1b2") {
		t.Errorf("opened %q, want the drawer for this commitment", opened)
	}
}

func TestCommitmentsViewReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "commitments", "view", "ri-a1b2")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}
}

func TestCommitmentsViewUnauthenticated(t *testing.T) {
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "view", "ri-a1b2"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

func TestCommitmentsRenewalSizesTheRepurchase(t *testing.T) {
	detailServer(t, detailBody)

	out, _, err := executeCommand(t, "commitments", "renewal", "ri-a1b2")
	if err != nil {
		t.Fatalf("renewal error: %v", err)
	}
	for _, want := range []string{"40", "33.5", "36", "2026-10-07T00:00:00Z", "18d",
		"$12400.00/mo", "$620.00/mo", "REC-1234", "l4 rec accept"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommitmentsRenewalNamesAnUndatedPlan(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/renewal-plan/": renewalUndatedBody,
	}))

	out, _, err := executeCommand(t, "commitments", "renewal", "cud-1")
	if err != nil {
		t.Fatalf("renewal error: %v", err)
	}
	if strings.Count(out.String(), notMeasured) < 4 {
		t.Errorf("an undated plan should say so rather than showing blanks:\n%s", out.String())
	}
}

// The CSV matches the dashboard's own download column for column, so a plan
// exported from either surface lands in the same spreadsheet.
func TestCommitmentsRenewalCSVMatchesTheDashboard(t *testing.T) {
	detailServer(t, detailBody)

	out, _, err := executeCommand(t, "commitments", "renewal", "ri-a1b2", "--format", "csv")
	if err != nil {
		t.Fatalf("renewal --format csv error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected a header and one row, got:\n%s", out.String())
	}
	wantHeader := strings.Join(purchasePlanCSVHeader, ",")
	if strings.TrimSpace(lines[0]) != wantHeader {
		t.Errorf("header = %q, want %q", lines[0], wantHeader)
	}
	if !strings.Contains(lines[1], "ri-a1b2,ec2,111122223333,2026-10-07T00:00:00Z,40,33.5,36,12400,620") {
		t.Errorf("row = %q", lines[1])
	}
}

func TestCommitmentsRenewalCSVLeavesAnAbsentInstantEmpty(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/renewal-plan/": renewalUndatedBody,
	}))

	out, _, err := executeCommand(t, "commitments", "renewal", "cud-1", "--format", "csv")
	if err != nil {
		t.Fatalf("renewal --format csv error: %v", err)
	}
	if !strings.Contains(out.String(), "cud-1,compute,proj-1,,1,,1,0,0") {
		t.Errorf("an absent instant and an unmeasured consumption should be empty cells:\n%s", out.String())
	}
}

func TestCommitmentsRenewalJSONAndWeb(t *testing.T) {
	detailServer(t, detailBody)
	if _, _, err := executeCommand(t, "commitments", "renewal", "ri-a1b2", "--json"); err != nil {
		t.Fatalf("renewal --json error: %v", err)
	}

	detailServer(t, detailBody)
	original := openBrowser
	openBrowser = func(string) error { return nil }
	defer func() { openBrowser = original }()
	if _, _, err := executeCommand(t, "commitments", "renewal", "ri-a1b2", "--web"); err != nil {
		t.Fatalf("renewal --web error: %v", err)
	}
}

func TestCommitmentsRenewalIsUnavailableWhereLifecycleIsNotMeasured(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{
		"/renewal-plan/": renewalBody,
	}))

	out, _, err := executeCommand(t, "commitments", "renewal", "cud-1")
	if err != nil {
		t.Fatalf("an unavailable command is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not measured for Google Cloud") {
		t.Errorf("output should name why:\n%s", out.String())
	}
}

func TestCommitmentsRenewalReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "commitments", "renewal", "ri-a1b2")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}
}

func TestCommitmentsRenewalUnauthenticated(t *testing.T) {
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "renewal", "ri-a1b2"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}
