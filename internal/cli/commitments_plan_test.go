package cli

import (
	"strings"
	"testing"
)

const (
	uncoveredRows = `[{"service":"ec2","organization_id":"111122223333","payer_account_id":"111122223333",` +
		`"plan_type":"compute","platform":"Linux/UNIX","on_demand_hourly_avg":12.4,"on_demand_hourly_min":8.1,` +
		`"volatility_ratio":1.53,"recommended_kind":"savings_plan","suggested_commitment_hourly":7.7,` +
		`"grain":"daily","source":"cur","window_days":30}]`

	costExplorerRows = `[{"service":"rds","organization_id":"444455556666","payer_account_id":"444455556666",` +
		`"plan_type":"database","platform":"Any","on_demand_hourly_avg":3.2,"on_demand_hourly_min":2.9,` +
		`"volatility_ratio":1.1,"recommended_kind":null,"suggested_commitment_hourly":0,` +
		`"grain":"hourly","source":"cost_explorer_recommendation","window_days":60}]`

	sizedProfiles = `{"conservative":{"commitment_hourly":3.2,"capped_by":["aws_hourly_minimum"],` +
		`"utilization_pct":99.9,"coverage_pct":48.0,"net_savings_monthly_at_your_rates":1400},` +
		`"balanced":{"commitment_hourly":4.1,"capped_by":[],` +
		`"utilization_pct":98.7,"coverage_pct":60.1,"net_savings_monthly_at_your_rates":1700},` +
		`"max_savings":{"commitment_hourly":5,"capped_by":["aws_cap","aws_utilization"],` +
		`"utilization_pct":null,"coverage_pct":70.2,"net_savings_monthly_at_your_rates":1900}}`

	buyRecommendationsBody = `{"data":[{"id":"REC-1234","kind":"buy_savings_plan",` +
		`"title":"Buy a one year plan","detail":"Sized against the floor",` +
		`"monthly_savings":2100,"confidence":"high"}]}`

	contractsBody = `{"data":[{"id":"c-1","vendor":"acme","contract_monthly_usd":5000,` +
		`"latest_metered_usd":6200,"overage_exceeds_floor":true,` +
		`"months":[{"period":"2026-08-01","contract_usd":5000,"metered_usd":6200}]}]}`

	floorContractBody = `{"data":[{"id":"c-2","vendor":"beta","contract_monthly_usd":null,` +
		`"latest_metered_usd":100,"overage_exceeds_floor":false,"months":[]}]}`
)

func proposal(planType, fields string) string {
	return `{"plan_type":"` + planType + `","term_months":12,"payment_option":"no_upfront",` +
		`"caveats":["Sized on daily totals."],` + fields + `}`
}

func purchasePlanBody(uncovered string, proposals ...string) string {
	return `{"data":{"provider":"aws","uncovered":` + uncovered + `,"proposals":[` + strings.Join(proposals, ",") + `]}}`
}

func planServer(t *testing.T, provider, planBody string) {
	t.Helper()
	useCommitmentsServer(t, commitmentsServer(t, []string{provider}, map[string]string{
		"/purchase-plan":   planBody,
		"/recommendations": buyRecommendationsBody,
	}))
}

func TestCommitmentsPlanSizesAgainstTheDailyFloor(t *testing.T) {
	planServer(t, providerAWS, purchasePlanBody(uncoveredRows))

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	for _, want := range []string{"ec2", "Linux/UNIX", "$12.40", "$8.10", "1.53x", "SP", "$7.700", "CUR", "daily",
		"REC-1234", "$2100.00/mo", "against the daily floor", "in commitment dollars an hour",
		"hides its quietest hours"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Savings Plan proposals") {
		t.Errorf("no proposal should print no proposal table:\n%s", out.String())
	}
}

func TestCommitmentsPlanShowsCostExplorerRowsWithNoSize(t *testing.T) {
	planServer(t, providerAWS, purchasePlanBody(costExplorerRows))

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	for _, want := range []string{"Cost Explorer", "hourly", notMeasured} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "$0.000") {
		t.Errorf("a Cost Explorer row suggests no size, not a size of zero:\n%s", out.String())
	}
}

func TestCommitmentsPlanCSVAppendsSourceAndGrain(t *testing.T) {
	planServer(t, providerAWS, purchasePlanBody(uncoveredRows, proposal("compute",
		`"payer_account_id":"111122223333","profiles":`+sizedProfiles)))

	out, _, err := executeCommand(t, "commitments", "plan", "--format", "csv")
	if err != nil {
		t.Fatalf("plan --format csv error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected a header and one row, got:\n%s", out.String())
	}
	if want := "Service,Platform,On demand/hr,Floor/hr,Volatility,Buy,Suggested/hr,Source,Grain"; lines[0] != want {
		t.Errorf("header = %q, want %q", lines[0], want)
	}
	if !strings.HasPrefix(lines[1], "ec2,Linux/UNIX,") || !strings.HasSuffix(lines[1], ",CUR,daily") {
		t.Errorf("row = %q", lines[1])
	}
}

func TestCommitmentsPlanWithNothingUncovered(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/purchase-plan":   purchasePlanBody(`[]`),
		"/recommendations": `{"data":[]}`,
	}))

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if !strings.Contains(out.String(), "No uncovered on-demand base measured") {
		t.Errorf("output should say there is nothing uncovered:\n%s", out.String())
	}
	for _, unwanted := range []string{"Recommended purchases", "Savings Plan proposals"} {
		if strings.Contains(out.String(), unwanted) {
			t.Errorf("an empty list should print no %q heading:\n%s", unwanted, out.String())
		}
	}
}

func TestCommitmentsPlanPrintsEachProposal(t *testing.T) {
	sized := proposal("compute", `"payer_account_id":"111122223333","recommended_profile":"balanced",`+
		`"raised":{"recommendation_id":"BUY-7","commitment_hourly":4.1,"profile":"balanced"},"profiles":`+sizedProfiles)
	unsized := proposal("database", `"payer_account_id":"111122223333","unavailable_reason":"no_cur",`+
		`"profiles":null,"recommended_profile":null,"raised":null`)
	planServer(t, providerAWS, purchasePlanBody(uncoveredRows, sized, unsized))

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	for _, want := range []string{"Savings Plan proposals", "Conservative", "Balanced (recommended)", "Max savings",
		"$3.200", "$4.100", "$5.000", "99.9%", "60.1%", "$1700.00/mo", "aws_hourly_minimum", "none",
		"aws_cap, aws_utilization", notMeasured, "12-month, No Upfront", "at your own rates",
		"l4 commitments simulate", "l4 commitments propose",
		"No Database Savings Plan proposal for 111122223333: no daily billing data is loaded for this payer.",
		"BUY-7 is raised for the Compute Savings Plan on 111122223333 at $4.100/hr (Balanced)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Conservative (recommended)") {
		t.Errorf("only the recommended profile is marked:\n%s", out.String())
	}
}

func TestCommitmentsPlanNotesWhatItCannotName(t *testing.T) {
	unknownReason := proposal("compute", `"payer_account_id":null,"unavailable_reason":"something_new","profiles":null`)
	ownSize := proposal("compute", `"payer_account_id":"111122223333","profiles":`+sizedProfiles+
		`,"raised":{"recommendation_id":"BUY-8","commitment_hourly":2.5,"profile":null}`)
	planServer(t, providerAWS, purchasePlanBody(uncoveredRows, unknownReason, ownSize))

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	for _, want := range []string{"No Compute Savings Plan proposal for not measured: something_new.",
		"BUY-8 is raised for the Compute Savings Plan on 111122223333 at $2.500/hr (your own size)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "(recommended)") {
		t.Errorf("a proposal with no recommended profile marks none:\n%s", out.String())
	}
}

func TestCommitmentsPlanJSONAndWeb(t *testing.T) {
	planServer(t, providerAWS, purchasePlanBody(uncoveredRows))
	out, _, err := executeCommand(t, "commitments", "plan", "--json")
	if err != nil {
		t.Fatalf("plan --json error: %v", err)
	}
	for _, key := range []string{"purchase_plan", "proposals", "recommendations"} {
		if !strings.Contains(out.String(), key) {
			t.Errorf("JSON output missing %q:\n%s", key, out.String())
		}
	}

	planServer(t, providerAWS, purchasePlanBody(uncoveredRows))
	original := openBrowser
	openBrowser = func(string) error { return nil }
	defer func() { openBrowser = original }()
	if _, _, err = executeCommand(t, "commitments", "plan", "--web"); err != nil {
		t.Fatalf("plan --web error: %v", err)
	}
}

func TestCommitmentsPlanIsUnavailableWithoutAnUncoveredBase(t *testing.T) {
	planServer(t, providerGCP, purchasePlanBody(uncoveredRows))

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("an unavailable command is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "AWS only") {
		t.Errorf("output should name why:\n%s", out.String())
	}
}

func TestCommitmentsPlanReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}
}

func TestCommitmentsPlanUnauthenticated(t *testing.T) {
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "plan"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

func TestCommitmentsContractsSeparatesTheFloorFromTheMeteredLeg(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/contracts": contractsBody,
	}))

	out, _, err := executeCommand(t, "commitments", "contracts")
	if err != nil {
		t.Fatalf("contracts error: %v", err)
	}
	for _, want := range []string{"acme", "$5000.00", "$6200.00", "yes, billing above it"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommitmentsContractsNamesAnUnknownFloor(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/contracts": floorContractBody,
	}))

	out, _, err := executeCommand(t, "commitments", "contracts")
	if err != nil {
		t.Fatalf("contracts error: %v", err)
	}
	if !strings.Contains(out.String(), notMeasured) {
		t.Errorf("a contract with no floor should say so:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "no") {
		t.Errorf("a contract under its floor should say so:\n%s", out.String())
	}
}

func TestCommitmentsContractsWithNone(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/contracts": `{"data":[]}`,
	}))

	out, _, err := executeCommand(t, "commitments", "contracts")
	if err != nil {
		t.Fatalf("contracts error: %v", err)
	}
	if !strings.Contains(out.String(), "No marketplace or private-pricing contracts") {
		t.Errorf("output should say there are none:\n%s", out.String())
	}
}

func TestCommitmentsContractsJSONAndWeb(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/contracts": contractsBody,
	}))
	out, _, err := executeCommand(t, "commitments", "contracts", "--json")
	if err != nil {
		t.Fatalf("contracts --json error: %v", err)
	}
	if !strings.Contains(out.String(), "acme") {
		t.Errorf("JSON output should carry the payload:\n%s", out.String())
	}

	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/contracts": contractsBody,
	}))
	original := openBrowser
	openBrowser = func(string) error { return nil }
	defer func() { openBrowser = original }()
	if _, _, err = executeCommand(t, "commitments", "contracts", "--web"); err != nil {
		t.Fatalf("contracts --web error: %v", err)
	}
}

func TestCommitmentsContractsIsUnavailableOutsideAWS(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{
		"/contracts": contractsBody,
	}))

	out, _, err := executeCommand(t, "commitments", "contracts")
	if err != nil {
		t.Fatalf("an unavailable command is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "AWS only") {
		t.Errorf("output should name why:\n%s", out.String())
	}
}

func TestCommitmentsContractsReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "commitments", "contracts")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}
}

func TestCommitmentsContractsUnauthenticated(t *testing.T) {
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "contracts"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}
