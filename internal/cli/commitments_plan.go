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
sustain, which strands part of the commitment for its whole term.`,
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
		if unavailableForProvider(provider,
			"Purchase planning is measured for AWS only: nothing exports an uncovered on-demand base for %s.") {
			return nil
		}
		return runCommitmentsPlan(client, provider)
	},
}

var commitmentsContractsCmd = &cobra.Command{
	Use:   "contracts",
	Short: "Marketplace and private-pricing floors that bill like a commitment",
	Long: `Commitment relationships that behave like a commitment on the bill but appear
in no provider commitment view, with the contracted floor separated from the
metered leg that bills on top of it.`,
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
		if unavailableForProvider(provider,
			"Marketplace contracts are measured for AWS only: none are collected for %s.") {
			return nil
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
	"units_held", "units_consumed", "units_recommended",
	"protects_monthly", "rightsizing_monthly",
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
		strconv.Itoa(plan.UnitsHeld),
		strconv.Itoa(plan.UnitsConsumed),
		strconv.Itoa(plan.UnitsRecommended),
		strconv.FormatFloat(plan.ProtectsMonthly, 'f', -1, 64),
		strconv.FormatFloat(plan.RightsizingMonthly, 'f', -1, 64),
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
