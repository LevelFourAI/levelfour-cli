package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagPlanFormat    string
	flagRenewalFormat string
)

var commitmentsPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "The uncovered base, and what buying would cover it",
	Long: `On-demand spend no commitment covers, split by platform and volatility.

Each slice is sized against its hourly floor rather than its average. Sizing
against the average of a volatile base commits to more than the base can
sustain, which strands part of the commitment for its whole term.

On Azure the plan lists Azure's own reservation and savings plan
recommendations instead, each saving applied to what the usage it covers
cost on demand on your bill over the last 30 days.`,
	Example: `  l4 commitments plan
  l4 commitments plan --format csv > purchase-plan.csv
  l4 commitments plan --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, provider, err := commitmentClient()
		if err != nil {
			return err
		}
		if flagWeb {
			return openWeb(commitmentsWebPath(provider, instrumentRI, ""))
		}
		if unavailableForProvider(provider, planMeasuredFor,
			"Purchase planning is measured for AWS and Azure only: nothing exports an uncovered on-demand base for %s.") {
			return nil
		}
		if provider == providerAzure {
			return runAzurePlan(client)
		}
		return runCommitmentsPlan(client, provider)
	},
}

var commitmentsContractsCmd = &cobra.Command{
	Use:   "contracts",
	Short: "Marketplace and private-pricing floors that bill like a commitment",
	Long: `Commitment relationships that behave like a commitment on the bill but appear
in no provider commitment view, with the contracted floor separated from the
metered leg that bills on top of it.

The list covers every connected provider at once, so --provider only picks the
page --web opens. On DigitalOcean, what reserved and committed products charge
is the floor, and the rest of the bill is the metered leg.`,
	Example: `  l4 commitments contracts
  l4 commitments contracts --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, provider, err := commitmentClient()
		if err != nil {
			return err
		}
		if flagWeb {
			return openWeb(commitmentsWebPath(provider, instrumentRI, ""))
		}
		return runCommitmentsContracts(client)
	},
}

func runCommitmentsPlan(client *api.SDKClient, provider string) error {
	slices, recommendations, bodies, err := fetchPlan(client, provider)
	if err != nil {
		return handleCommitmentsError(err)
	}

	if flagPlanFormat == formatCSV {
		printUncoveredCSV(slices)
		return nil
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(bodies)
	}

	if len(slices) == 0 {
		output.Info("No uncovered on-demand base measured.")
	} else {
		renderUncovered(slices)
	}
	renderBuyRecommendations(recommendations)
	return nil
}

func fetchPlan(client *api.SDKClient, provider string) (
	[]api.UncoveredSlice, []api.CommitmentRecommendation, map[string]json.RawMessage, error,
) {
	var slices []api.UncoveredSlice
	var recommendations []api.CommitmentRecommendation
	var uncoveredBody, recommendationsBody json.RawMessage
	var uncoveredErr, recommendationsErr error

	err := tuicommon.RunWithSpinner("Loading purchase plan...", output.L4SpinnerTheme(), func(_ context.Context) error {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			slices, uncoveredBody, uncoveredErr = api.GetCommitments[[]api.UncoveredSlice](
				client.Raw(), "/uncovered", nil)
		}()
		go func() {
			defer wg.Done()
			recommendations, recommendationsBody, recommendationsErr = api.GetCommitments[[]api.CommitmentRecommendation](
				client.Raw(), "/recommendations", map[string]string{"provider": provider})
		}()
		wg.Wait()
		return firstError(uncoveredErr, recommendationsErr)
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return slices, recommendations, map[string]json.RawMessage{
		"uncovered":       uncoveredBody,
		"recommendations": recommendationsBody,
	}, nil
}

// runAzurePlan reads Azure's own recommendations rather than an uncovered base:
// Azure sizes each one, and LevelFour grounds its saving on the bill.
func runAzurePlan(client *api.SDKClient) error {
	plan, body, err := fetchAzurePlan(client)
	if err != nil {
		return handleCommitmentsError(err)
	}
	rows := make([][]string, 0, len(plan.Proposals))
	for _, proposal := range plan.Proposals {
		rows = append(rows, azureProposalCells(proposal))
	}
	switch {
	case flagPlanFormat == formatCSV:
		output.PrintCSV(azureProposalHeaders, rows)
	case output.HasFormattingFlags():
		return output.PrintResult(body)
	case len(rows) == 0:
		output.Info("Azure recommends no reservation or savings plan to buy.")
	default:
		output.Header("Recommended purchases")
		output.Table(azureProposalHeaders, rows)
		output.Info("Each saving is Azure's discount applied to what the usage cost on demand on your bill over the last 30 days.")
	}
	return nil
}

func fetchAzurePlan(client *api.SDKClient) (api.AzurePurchasePlan, json.RawMessage, error) {
	var plan api.AzurePurchasePlan
	var body json.RawMessage
	err := tuicommon.RunWithSpinner("Loading purchase plan...", output.L4SpinnerTheme(), func(_ context.Context) error {
		var fetchErr error
		plan, body, fetchErr = api.GetCommitments[api.AzurePurchasePlan](client.Raw(), "/azure/purchase-plan", nil)
		return fetchErr
	})
	return plan, body, err
}

var azureProposalHeaders = []string{"ID", columnKind, "SKU", columnRegion, "Term", "Size", "On demand/mo", "Savings/mo", "Raised"}

func azureProposalCells(proposal api.AzurePurchaseProposal) []string {
	return []string{
		proposal.ID,
		providerKindLabel(providerAzure, proposal.Kind),
		proposal.SKU,
		azureRegionCell(proposal.Region),
		fmt.Sprintf("%d months", proposal.TermMonths),
		azureSizeCell(proposal),
		moneyValue(proposal.OnDemandMonthly),
		azureSavingsCell(proposal),
		derefString(proposal.RaisedRecommendationID),
	}
}

// A savings plan applies across regions, so it has none of its own.
func azureRegionCell(region *string) string {
	if region == nil || *region == "" {
		return "any"
	}
	return *region
}

func azureSizeCell(proposal api.AzurePurchaseProposal) string {
	// Azure sizes a savings plan to the tenth of a cent an hour, so it is printed whole, never to the cent.
	if proposal.HourlyCommitment != nil {
		return "$" + unitsValue(*proposal.HourlyCommitment) + "/hr"
	}
	if proposal.Quantity != nil {
		return unitsValue(*proposal.Quantity)
	}
	return notMeasured
}

func azureSavingsCell(proposal api.AzurePurchaseProposal) string {
	if proposal.UnavailableReason != nil {
		return "no on-demand cost on the bill"
	}
	return moneyValue(proposal.MonthlySavings)
}

var uncoveredHeaders = []string{
	columnService, "Platform", "On demand/hr", "Floor/hr", "Volatility", "Buy", "Suggested/hr",
}

func renderUncovered(slices []api.UncoveredSlice) {
	output.Header("Uncovered base")
	rows := make([][]string, 0, len(slices))
	for _, slice := range slices {
		rows = append(rows, uncoveredCells(slice))
	}
	output.Table(uncoveredHeaders, rows)
	output.Info("Suggested amounts are sized against the floor, not the average, so a volatile hour cannot strand the commitment.")
}

func uncoveredCells(slice api.UncoveredSlice) []string {
	return []string{
		slice.Service,
		slice.Platform,
		moneyValue(slice.OnDemandHourlyAvg),
		moneyValue(slice.OnDemandHourlyMin),
		fmt.Sprintf("%.2fx", slice.VolatilityRatio),
		kindLabel(textOrNotMeasured(slice.RecommendedKind)),
		moneyValue(slice.SuggestedCommitmentHourly),
	}
}

func printUncoveredCSV(slices []api.UncoveredSlice) {
	rows := make([][]string, 0, len(slices))
	for _, slice := range slices {
		rows = append(rows, uncoveredCells(slice))
	}
	output.PrintCSV(uncoveredHeaders, rows)
}

func renderBuyRecommendations(recommendations []api.CommitmentRecommendation) {
	if len(recommendations) == 0 {
		return
	}
	output.Header("Recommended purchases")
	rows := make([][]string, 0, len(recommendations))
	for _, rec := range recommendations {
		rows = append(rows, []string{
			rec.ID,
			rec.Kind,
			moneyValue(rec.MonthlySavings) + "/mo",
			rec.Confidence,
			rec.Title,
		})
	}
	output.Table([]string{"ID", "Kind", "Savings", "Confidence", "Title"}, rows)
	output.Info("Act on one with: l4 rec accept <id>")
}

func runCommitmentsContracts(client *api.SDKClient) error {
	contracts, body, err := api.GetCommitments[[]api.CommitmentContract](client.Raw(), "/contracts", nil)
	if err != nil {
		return handleCommitmentsError(err)
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(body)
	}
	if len(contracts) == 0 {
		output.Info("No marketplace or private-pricing contracts found.")
		return nil
	}

	headers := []string{"Vendor", "Floor/mo", "Latest metered", "Over floor", "Months"}
	rows := make([][]string, 0, len(contracts))
	for _, contract := range contracts {
		rows = append(rows, []string{
			contract.Vendor,
			moneyOrNotMeasured(contract.ContractMonthlyUSD),
			moneyValue(contract.LatestMeteredUSD),
			overageCell(contract.OverageExceedsFloor),
			strconv.Itoa(len(contract.Months)),
		})
	}
	output.Table(headers, rows)
	return nil
}

// overageCell names what exceeding the floor means, since the floor is paid
// either way and the metered leg bills on top of it.
func overageCell(exceeds bool) string {
	if exceeds {
		return "yes, billing above it"
	}
	return "no"
}

// purchasePlanCSVHeader matches the dashboard's own download column for column,
// so a plan exported from either surface lands in the same spreadsheet.
var purchasePlanCSVHeader = []string{
	"commitment_id", "service", "holder_account_id", "buy_after_utc",
	"units_held", "units_consumed", "units_recommended", fieldDecision, "sizing_basis",
	"protects_monthly", "rightsizing_monthly",
}

// unitsCell leaves an unmeasured amount empty, as the instant beside it is.
func unitsCell(v *float64) string {
	if v == nil {
		return ""
	}
	return unitsValue(*v)
}

func printRenewalPlanCSV(plan api.CommitmentRenewalPlan) {
	buyAfter := ""
	if plan.BuyAfterUTC != nil {
		buyAfter = *plan.BuyAfterUTC
	}
	output.PrintCSV(purchasePlanCSVHeader, [][]string{{
		plan.CommitmentID,
		plan.Service,
		plan.HolderAccountID,
		buyAfter,
		unitsValue(plan.UnitsHeld),
		unitsCell(plan.UnitsConsumed),
		unitsValue(plan.UnitsRecommended),
		plan.Decision,
		plan.SizingBasis,
		csvOptionalFloat(plan.ProtectsMonthly),
		csvOptionalFloat(plan.RightsizingMonthly),
	}})
}

func init() {
	commitmentsPlanCmd.Flags().StringVar(&flagPlanFormat, "format", "table",
		"Output format: table or csv")
	commitmentsRenewalCmd.Flags().StringVar(&flagRenewalFormat, "format", "table",
		"Output format: table or csv")

	commitmentsCmd.AddCommand(commitmentsPlanCmd)
	commitmentsCmd.AddCommand(commitmentsContractsCmd)
}
