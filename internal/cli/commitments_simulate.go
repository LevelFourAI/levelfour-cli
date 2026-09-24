package cli

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagSimulateTerm     string
	flagSimulatePayment  string
	flagSimulateLookback string
)

var (
	termChoices      = []string{"1y", "3y"}
	termMonthsByFlag = map[string]string{"1y": "12", "3y": "36"}
	paymentOptions   = []string{"no_upfront", "partial_upfront", "all_upfront"}
	lookbackChoices  = []string{"30", "60"}
)

var commitmentsSimulateCmd = &cobra.Command{
	Use:   "simulate",
	Short: "Replay a Savings Plan commitment over the recent daily bill",
	Long: `Replay a Savings Plan commitment, in dollars an hour, over the payer's recent
daily bill: the eligible on-demand spend it would have covered, the commitment
it would have left unused, its utilization, coverage and monthly savings, beside
the sized profiles and AWS's own recommendation.

A day averages its hours, so every figure is an upper bound on the hourly truth.
It buys nothing and raises nothing. AWS sells a Database Savings Plan for 1
year, No Upfront, only. --payer may be omitted when the organization has one
payer.

Raise a size with 'l4 commitments propose'.`,
	Example: `  l4 commitments simulate --type compute --commitment 12.5
  l4 commitments simulate --type compute --commitment 12.5 --term 3y --payment all_upfront
  l4 commitments simulate --type database --commitment 2 --payer 111122223333 --lookback 30 --json`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		params, err := simulationParams(cmd)
		if err != nil {
			return err
		}
		return runCommitmentsSimulate(params)
	},
}

func simulatedCommitment(cmd *cobra.Command) (float64, error) {
	commitment, err := optionalCommitment(cmd)
	if err != nil {
		return 0, err
	}
	if commitment == nil {
		return 0, errors.New("--commitment is required: the dollars an hour to replay, for example 12.5")
	}
	return *commitment, nil
}

func simulationParams(cmd *cobra.Command) (map[string]string, error) {
	if err := checkPlanType(); err != nil {
		return nil, err
	}
	commitment, err := simulatedCommitment(cmd)
	if err != nil {
		return nil, err
	}
	if err := requireChoice("term", flagSimulateTerm, termChoices); err != nil {
		return nil, err
	}
	if err := validateChoice("payment", flagSimulatePayment, paymentOptions); err != nil {
		return nil, err
	}
	if err := validateChoice("lookback", flagSimulateLookback, lookbackChoices); err != nil {
		return nil, err
	}
	return map[string]string{
		"plan_type":         flagPurchaseType,
		"commitment_hourly": quantityValue(commitment),
		"term_months":       termMonthsByFlag[flagSimulateTerm],
		"payment_option":    flagSimulatePayment,
		"payer_account_id":  flagPurchasePayer,
		"lookback_days":     flagSimulateLookback,
	}, nil
}

func runCommitmentsSimulate(params map[string]string) error {
	path := api.CommitmentsPath + "/purchase-simulation" + api.BuildQueryString(params)
	simulation, body, err := requestData[api.PurchaseSimulation](http.MethodGet, path, nil, nil)
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(body)
	}
	renderSimulation(simulation)
	return nil
}

func renderSimulation(simulation api.PurchaseSimulation) {
	output.Header(fmt.Sprintf("%s at %s/hr, %s", planName(simulation.PlanType),
		commitmentValue(simulation.Candidate.CommitmentHourly),
		purchaseTerm(simulation.TermMonths, simulation.PaymentOption)))
	output.KeyValue("Payer", textOrNotMeasured(simulation.PayerAccountID))
	output.KeyValue("Window", windowCell(simulation.Window))
	if simulation.Totals == nil {
		output.Info(unsizedSentence("Nothing to replay", simulation.UnavailableReason))
		return
	}
	renderSimulationTotals(*simulation.Totals)
	output.Header("Sized profiles")
	output.Table(profileHeaders, profileRows(simulation.Profiles, nil))
	renderAWSRecommendation(simulation.AWS)
	for _, caveat := range simulation.Caveats {
		output.Info(caveat)
	}
}

func windowCell(window *api.PurchaseWindow) string {
	if window == nil {
		return notMeasured
	}
	return fmt.Sprintf("%s to %s, %d days (%d missing)", window.FirstDay, window.LastDay, window.Days, window.MissingDays)
}

func renderSimulationTotals(totals api.PurchaseTotals) {
	output.KPICards([]output.KPICard{
		{Label: "Utilization", Value: pctOrNotMeasured(totals.UtilizationPct)},
		{Label: "Coverage", Value: pctOrNotMeasured(totals.CoveragePct)},
		{Label: "Savings", Value: moneyValue(totals.NetSavingsMonthly) + "/mo"},
		{Label: "At your rates", Value: moneyValue(totals.NetSavingsMonthlyAtYourRates) + "/mo"},
	})
	output.Header("Over the replayed days")
	output.Table(
		[]string{"Eligible on demand", "Covered", "Uncovered", "Commitment", "Used", "Unused"},
		[][]string{{
			moneyValue(totals.EligibleOnDemand),
			moneyValue(totals.CoveredOnDemand),
			moneyValue(totals.UncoveredOnDemand),
			moneyValue(totals.CommitmentCost),
			moneyValue(totals.UsedCommitment),
			moneyValue(totals.WastedCommitment),
		}},
	)
}

func renderAWSRecommendation(aws *api.PurchaseAWSBenchmark) {
	if aws == nil {
		return
	}
	output.KeyValue("AWS recommends", commitmentValue(aws.HourlyCommitmentToPurchase)+"/hr")
	output.KeyValue("Profiles capped at", commitmentValue(aws.Cap)+"/hr")
}

func init() {
	addPurchaseTargetFlags(commitmentsSimulateCmd)
	commitmentsSimulateCmd.Flags().Float64Var(&flagPurchaseCommitment, flagNameCommitment, 0,
		"Dollars an hour to commit, required")
	commitmentsSimulateCmd.Flags().StringVar(&flagSimulateTerm, "term", "1y", "Term: "+strings.Join(termChoices, ", "))
	commitmentsSimulateCmd.Flags().StringVar(&flagSimulatePayment, "payment", "no_upfront",
		"Payment option: "+strings.Join(paymentOptions, ", "))
	commitmentsSimulateCmd.Flags().StringVar(&flagSimulateLookback, "lookback", "60",
		"Days of the daily bill to replay: 30 or 60")
	commitmentsCmd.AddCommand(commitmentsSimulateCmd)
}
