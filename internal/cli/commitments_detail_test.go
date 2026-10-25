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
		`"units_held":40,"units_consumed":34,"units_recommended":36,` +
		`"sizing_basis":"utilization","decision":"renew","protects_monthly":12400,"rightsizing_monthly":620,` +
		`"pending_changes":[{"recommendation_id":"REC-1234","service":"ec2","account":"prod",` +
		`"monthly_savings":210,"status":"available"}],"exchangeable":true,"cancellable":false}}`

	// An Azure savings plan commits dollars an hour, so its amounts are fractional,
	// and one nothing measured has no consumed amount at all.
	renewalAzurePlanBody = `{"data":{"commitment_id":"azure::savings_plan::sp1","service":"Compute",` +
		`"holder_account_id":"sub-1","end_at":"2026-11-10T00:00:00Z",` +
		`"buy_after_utc":"2026-11-10T00:00:00Z","expires_in_seconds":1555200,` +
		`"units_held":1.5,"units_consumed":null,"units_recommended":1.5,` +
		`"sizing_basis":"unmeasured","decision":"renew",` +
		`"protects_monthly":null,"rightsizing_monthly":null,"pending_changes":[],` +
		`"exchangeable":null,"cancellable":null}}`

	// A savings plan Azure bills in another currency: nothing sizes it, so both
	// unit amounts arrive as zero.
	renewalNotUSDPlanBody = `{"data":{"commitment_id":"azure::savings_plan::sp2","service":"Compute",` +
		`"holder_account_id":"sub-1","end_at":"2026-11-10T00:00:00Z",` +
		`"buy_after_utc":"2026-11-10T00:00:00Z","expires_in_seconds":1555200,` +
		`"units_held":0,"units_consumed":null,"units_recommended":0,` +
		`"sizing_basis":"not_usd","decision":"renew",` +
		`"protects_monthly":null,"rightsizing_monthly":null,"pending_changes":[],` +
		`"exchangeable":true,"cancellable":true}}`

	// A reservation whose renewal Azure quotes in another currency: it still
	// states the units it holds, while nothing sizes or prices the renewal.
	renewalNotUSDReservationBody = `{"data":{"commitment_id":"azure::reservation::r2",` +
		`"service":"Virtual Machines","holder_account_id":"sub-1","end_at":"2026-11-10T00:00:00Z",` +
		`"buy_after_utc":"2026-11-10T00:00:00Z","expires_in_seconds":1555200,` +
		`"units_held":4,"units_consumed":null,"units_recommended":0,` +
		`"sizing_basis":"not_usd","decision":"renew",` +
		`"protects_monthly":null,"rightsizing_monthly":null,"pending_changes":[],` +
		`"exchangeable":true,"cancellable":true}}`

	renewalLetExpireBody = `{"data":{"commitment_id":"azure::reservation::r1","service":"Virtual Machines",` +
		`"holder_account_id":"sub-1","end_at":"2026-11-10T00:00:00Z",` +
		`"buy_after_utc":"2026-11-10T00:00:00Z","expires_in_seconds":1555200,` +
		`"units_held":3,"units_consumed":0,"units_recommended":0,` +
		`"sizing_basis":"utilization","decision":"let_expire",` +
		`"protects_monthly":null,"rightsizing_monthly":null,"pending_changes":[],` +
		`"exchangeable":true,"cancellable":true}}`

	renewalUndatedBody = `{"data":{"commitment_id":"cud-1","service":"compute",` +
		`"holder_account_id":"proj-1","end_at":null,"buy_after_utc":null,` +
		`"expires_in_seconds":null,"units_held":1,"units_consumed":1,"units_recommended":1,` +
		`"sizing_basis":"unmeasured","decision":"renew","protects_monthly":0,"rightsizing_monthly":0,"pending_changes":[],` +
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
	for _, want := range []string{"40", "34", "36", "2026-10-07T00:00:00Z", "18d",
		"$12400.00/mo", "$620.00/mo", "REC-1234", "l4 rec accept"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommitmentsRenewalPlansAnAzureSavingsPlan(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAzure}, map[string]string{
		"/renewal-plan/": renewalAzurePlanBody,
	}))

	out, _, err := executeCommand(t, "commitments", "renewal", "azure::savings_plan::sp1")
	if err != nil {
		t.Fatalf("renewal error: %v", err)
	}
	for _, want := range []string{"1.5", notMeasured, "2026-11-10T00:00:00Z"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	// Azure prices neither amount, so both read as unmeasured rather than $0.00.
	if strings.Contains(out.String(), "$0.00") || strings.Count(out.String(), notMeasured) != 5 {
		t.Errorf("consumed, both amounts and both unknown flags should read as unmeasured:\n%s", out.String())
	}
}

func TestCommitmentsRenewalNamesWhyAPlanInAnotherCurrencyIsNotSized(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAzure}, map[string]string{
		"/renewal-plan/": renewalNotUSDPlanBody,
	}))

	out, _, err := executeCommand(t, "commitments", "renewal", "azure::savings_plan::sp2")
	if err != nil {
		t.Fatalf("renewal error: %v", err)
	}
	if strings.Count(out.String(), notMeasured) != 5 {
		t.Errorf("held, consumed, recommended and both amounts should read as unmeasured:\n%s", out.String())
	}
	if strings.Contains(out.String(), "$0.00") {
		t.Errorf("an unpriced plan should print no money:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "renew it in the Azure portal") {
		t.Errorf("output should say why the plan is not sized:\n%s", out.String())
	}
}

func TestCommitmentsRenewalKeepsTheUnitsAReservationQuotedInAnotherCurrencyHolds(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAzure}, map[string]string{
		"/renewal-plan/": renewalNotUSDReservationBody,
	}))

	out, _, err := executeCommand(t, "commitments", "renewal", "azure::reservation::r2")
	if err != nil {
		t.Fatalf("renewal error: %v", err)
	}
	if strings.Count(out.String(), notMeasured) != 4 {
		t.Errorf("consumed, recommended and both amounts should read as unmeasured:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "│4 ") || !strings.Contains(out.String(), "bills or quotes") {
		t.Errorf("output should keep the units held and name the currency:\n%s", out.String())
	}
}

func TestCommitmentsRenewalAdvisesLettingAnUnusedTermExpire(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAzure}, map[string]string{
		"/renewal-plan/": renewalLetExpireBody,
	}))

	out, _, err := executeCommand(t, "commitments", "renewal", "azure::reservation::r1")
	if err != nil {
		t.Fatalf("renewal error: %v", err)
	}
	if !strings.Contains(out.String(), "let it expire on its end date") {
		t.Errorf("a zero recommendation should say it means letting the term expire:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Azure portal") {
		t.Errorf("a measured plan is not a currency refusal:\n%s", out.String())
	}
}

func TestCommitmentsRenewalCSVLeavesAnUnmeasuredAmountEmpty(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAzure}, map[string]string{
		"/renewal-plan/": renewalAzurePlanBody,
	}))

	out, _, err := executeCommand(t, "commitments", "renewal", "azure::savings_plan::sp1", "--format", "csv")
	if err != nil {
		t.Fatalf("renewal --format csv error: %v", err)
	}
	if !strings.Contains(out.String(), "azure::savings_plan::sp1,Compute,sub-1,2026-11-10T00:00:00Z,1.5,,1.5,renew,unmeasured,,") {
		t.Errorf("an unmeasured amount should be an empty cell:\n%s", out.String())
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
	if !strings.Contains(lines[1], "ri-a1b2,ec2,111122223333,2026-10-07T00:00:00Z,40,34,36,renew,utilization,12400,620") {
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
	if !strings.Contains(out.String(), "cud-1,compute,proj-1,,1,1,1,renew,unmeasured,0,0") {
		t.Errorf("an absent instant should be an empty cell:\n%s", out.String())
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

// Azure sells a reservation, and "RI" names an AWS product, so the heading
// carries Azure's own word.
func TestCommitmentsViewNamesAnAzureReservationInAzureWords(t *testing.T) {
	detailServer(t, strings.Replace(detailBody, `"provider":"aws"`, `"provider":"azure"`, 1))

	out, _, err := executeCommand(t, "commitments", "view", "ri-a1b2")
	if err != nil {
		t.Fatalf("view error: %v", err)
	}
	if !strings.Contains(out.String(), "EC2 "+labelAzureReservation) {
		t.Errorf("an Azure heading should say %q:\n%s", labelAzureReservation, out.String())
	}
}

// An Azure commitment its bill and summary did not measure arrives with a null
// utilization, which must not read as 0% used.
func TestCommitmentsViewSaysWhenNothingMeasuredTheUtilization(t *testing.T) {
	body := strings.Replace(detailBody, `"current_utilization_pct":98.2`, `"current_utilization_pct":null`, 1)
	detailServer(t, strings.Replace(body, `"provider":"aws"`, `"provider":"azure"`, 1))

	out, _, err := executeCommand(t, "commitments", "view", "ri-a1b2")
	if err != nil {
		t.Fatalf("view error: %v", err)
	}
	if !lineWith(out.String(), "Utilization", notMeasured) {
		t.Errorf("an unmeasured utilization should say so:\n%s", out.String())
	}
}
