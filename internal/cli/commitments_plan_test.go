package cli

import (
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

const (
	uncoveredBody = `{"data":[{"service":"ec2","organization_id":"o-abcdefghij","platform":"Linux/UNIX",` +
		`"on_demand_hourly_avg":12.4,"on_demand_hourly_min":8.1,"volatility_ratio":1.53,` +
		`"recommended_kind":"savings_plan","suggested_commitment_hourly":7.7}]}`

	untypedUncoveredBody = `{"data":[{"service":"rds","organization_id":"o-abcdefghij","platform":"Any",` +
		`"on_demand_hourly_avg":3.2,"on_demand_hourly_min":2.9,"volatility_ratio":1.1,` +
		`"recommended_kind":null,"suggested_commitment_hourly":2.7}]}`

	buyRecommendationsBody = `{"data":[{"id":"REC-1234","kind":"buy_savings_plan",` +
		`"title":"Buy a one year plan","detail":"Sized against the floor",` +
		`"monthly_savings":2100,"confidence":"high"}]}`

	contractsBody = `{"data":[{"id":"c-1","vendor":"acme","contract_monthly_usd":5000,` +
		`"latest_metered_usd":6200,"overage_exceeds_floor":true,` +
		`"months":[{"period":"2026-08-01","contract_usd":5000,"metered_usd":6200}]}]}`

	floorContractBody = `{"data":[{"id":"c-2","vendor":"beta","contract_monthly_usd":null,` +
		`"latest_metered_usd":100,"overage_exceeds_floor":false,"months":[]}]}`
)

func planServer(t *testing.T, provider, uncovered string) {
	t.Helper()
	useCommitmentsServer(t, commitmentsServer(t, []string{provider}, map[string]string{
		"/uncovered":       uncovered,
		"/recommendations": buyRecommendationsBody,
	}))
}

func TestCommitmentsPlanSizesAgainstTheFloor(t *testing.T) {
	planServer(t, providerAWS, uncoveredBody)

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	for _, want := range []string{"ec2", "Linux/UNIX", "$12.40", "$8.10", "1.53x", "SP", "$7.70",
		"REC-1234", "$2100.00/mo", "sized against the floor"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommitmentsPlanNamesAnUnrecommendedSlice(t *testing.T) {
	planServer(t, providerAWS, untypedUncoveredBody)

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if !strings.Contains(out.String(), notMeasured) {
		t.Errorf("a slice with no recommended kind should say so:\n%s", out.String())
	}
}

func TestCommitmentsPlanCSV(t *testing.T) {
	planServer(t, providerAWS, uncoveredBody)

	out, _, err := executeCommand(t, "commitments", "plan", "--format", "csv")
	if err != nil {
		t.Fatalf("plan --format csv error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected a header and one row, got:\n%s", out.String())
	}
	if !strings.HasPrefix(lines[0], "Service,Platform,") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "ec2,Linux/UNIX") {
		t.Errorf("row = %q", lines[1])
	}
}

func TestCommitmentsPlanWithNothingUncovered(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/uncovered":       `{"data":[]}`,
		"/recommendations": `{"data":[]}`,
	}))

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if !strings.Contains(out.String(), "No uncovered on-demand base measured") {
		t.Errorf("output should say there is nothing uncovered:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Recommended purchases") {
		t.Errorf("an empty recommendation list should print no heading:\n%s", out.String())
	}
}

func TestCommitmentsPlanJSONAndWeb(t *testing.T) {
	planServer(t, providerAWS, uncoveredBody)
	out, _, err := executeCommand(t, "commitments", "plan", "--json")
	if err != nil {
		t.Fatalf("plan --json error: %v", err)
	}
	for _, key := range []string{"uncovered", "recommendations"} {
		if !strings.Contains(out.String(), key) {
			t.Errorf("JSON output missing %q:\n%s", key, out.String())
		}
	}

	planServer(t, providerAWS, uncoveredBody)
	original := openBrowser
	openBrowser = func(string) error { return nil }
	defer func() { openBrowser = original }()
	if _, _, err = executeCommand(t, "commitments", "plan", "--web"); err != nil {
		t.Fatalf("plan --web error: %v", err)
	}
}

func TestCommitmentsPlanIsUnavailableWithoutAnUncoveredBase(t *testing.T) {
	planServer(t, providerGCP, uncoveredBody)

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("an unavailable command is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "AWS and Azure only") {
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

// Contracts are listed for the whole organization, so one whose detected
// provider is Google Cloud still sees the floors another provider bills.
func TestCommitmentsContractsListsEveryProviderWhicheverIsDetected(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{
		"/contracts": contractsBody,
	}))

	out, _, err := executeCommand(t, "commitments", "contracts")
	if err != nil {
		t.Fatalf("contracts error: %v", err)
	}
	if !strings.Contains(out.String(), "acme") {
		t.Errorf("output should list the contracts:\n%s", out.String())
	}
}

// DigitalOcean sells reserved terms as contracts, so its customers read them here.
func TestCommitmentsContractsReadsDigitalOcean(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerDigitalOcean}, map[string]string{
		"/contracts": `{"data":[{"id":"digitalocean::contract","vendor":"DigitalOcean",` +
			`"contract_monthly_usd":800,"latest_metered_usd":120,"overage_exceeds_floor":false,` +
			`"months":[{"period":"2026-09-01","contract_usd":800,"metered_usd":120}]}]}`,
	}))

	out, _, err := executeCommand(t, "commitments", "contracts")
	if err != nil {
		t.Fatalf("contracts error: %v", err)
	}
	for _, want := range []string{"DigitalOcean", "$800.00", "$120.00"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
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

const (
	azurePlanBody = `{"data":{"provider":"azure","proposals":[` +
		`{"id":"azure:rec:1","kind":"reserved_instance","scope_id":"/subscriptions/s","subscription_id":"s",` +
		`"sku":"Standard_D4s_v5","region":"eastus","resource_type":"virtualMachines","term_months":12,` +
		`"quantity":2,"hourly_commitment":null,"on_demand_monthly":410.5,"savings_share_pct":38,` +
		`"monthly_savings":156,"raised_recommendation_id":"BUY-AZ-1","unavailable_reason":null,` +
		`"synced_at":"2026-10-24T06:00:00Z"},` +
		`{"id":"azure:rec:2","kind":"savings_plan","scope_id":"/providers/x","subscription_id":null,` +
		`"sku":"Compute_Savings_Plan","region":null,"resource_type":"compute","term_months":36,` +
		`"quantity":null,"hourly_commitment":1.25,"on_demand_monthly":0,"savings_share_pct":20,` +
		`"monthly_savings":0,"raised_recommendation_id":null,"unavailable_reason":"no_bill",` +
		`"synced_at":"2026-10-24T06:00:00Z"},` +
		`{"id":"azure:rec:3","kind":"reserved_instance","scope_id":"/subscriptions/s","subscription_id":"s",` +
		`"sku":"Standard_E2s_v5","region":"","resource_type":"virtualMachines","term_months":12,` +
		`"quantity":null,"hourly_commitment":null,"on_demand_monthly":12,"savings_share_pct":10,` +
		`"monthly_savings":1.2,"raised_recommendation_id":null,"unavailable_reason":null,` +
		`"synced_at":"2026-10-24T06:00:00Z"}]}}`
	emptyAzurePlanBody = `{"data":{"provider":"azure","proposals":[]}}`
)

func azurePlanServer(t *testing.T, body string) {
	t.Helper()
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAzure}, map[string]string{
		"/azure/purchase-plan": body,
	}))
}

func TestCommitmentsPlanForAzureListsItsGroundedPurchases(t *testing.T) {
	azurePlanServer(t, azurePlanBody)

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	for _, want := range []string{
		"azure:rec:1", "Reservation", "Standard_D4s_v5", "eastus", "12 months", "$410.50", "$156.00", "BUY-AZ-1",
		"Savings plan", "any", "36 months", "$1.25/hr", "no on-demand cost on the bill",
		"Standard_E2s_v5", notMeasured, "on demand on your bill",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestCommitmentsPlanForAzureSaysWhenAzureRecommendsNothing(t *testing.T) {
	azurePlanServer(t, emptyAzurePlanBody)

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	if !strings.Contains(out.String(), "Azure recommends no reservation or savings plan to buy.") {
		t.Errorf("an empty plan should say so:\n%s", out.String())
	}
}

func TestCommitmentsPlanForAzureWritesCSV(t *testing.T) {
	azurePlanServer(t, azurePlanBody)
	flagPlanFormat = formatCSV
	t.Cleanup(func() { flagPlanFormat = "" })

	out, _, err := executeCommand(t, "commitments", "plan", "--format", "csv")
	if err != nil {
		t.Fatalf("plan csv error: %v", err)
	}
	if !strings.HasPrefix(out.String(), "ID,Kind,SKU,Region,Term,Size,On demand/mo,Savings/mo,Raised") {
		t.Errorf("csv header wrong:\n%s", out.String())
	}
}

func TestCommitmentsPlanForAzureWritesTheAPIBodyAsJSON(t *testing.T) {
	azurePlanServer(t, azurePlanBody)

	out, _, err := executeCommand(t, "commitments", "plan", "--json")
	if err != nil {
		t.Fatalf("plan json error: %v", err)
	}
	if !strings.Contains(out.String(), `"azure:rec:1"`) {
		t.Errorf("json should carry the API body:\n%s", out.String())
	}
}

func TestCommitmentsPlanForAzureSaysWhenThePlanIsNotServed(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAzure}, map[string]string{}))

	out, _, err := executeCommand(t, "commitments", "plan")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say commitments are not available:\n%s", out.String())
	}
}

// Azure sizes a savings plan below the cent, and rounding it to the cent would
// print a commitment Azure was never asked for.
func TestAzureSizeCellKeepsEveryDigitAzureSized(t *testing.T) {
	hourly, quantity := 0.125, 3.0
	if got := azureSizeCell(api.AzurePurchaseProposal{HourlyCommitment: &hourly}); got != "$0.125/hr" {
		t.Errorf("hourly size = %q, want $0.125/hr", got)
	}
	if got := azureSizeCell(api.AzurePurchaseProposal{Quantity: &quantity}); got != "3" {
		t.Errorf("quantity size = %q, want 3", got)
	}
}
