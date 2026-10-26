package cli

import (
	"fmt"
	"net/url"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var commitmentsViewCmd = &cobra.Command{
	Use:   "view <id>",
	Short: "Everything known about one commitment",
	Args:  cobra.ExactArgs(1),
	Example: `  l4 commitments view ri-0a1b2c3d
  l4 commitments view arn:aws:savingsplans::111122223333:savingsplan/abc
  l4 commitments view ri-0a1b2c3d --json`,
	// The only commitments command that needs no provider: a commitment is
	// fetched by id, so resolving one would be a round trip whose answer is
	// thrown away. Only the dashboard link needs it.
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			_, provider, err := commitmentClient()
			if err != nil {
				return err
			}
			return openWeb(commitmentsWebPath(provider, instrumentRI, args[0]))
		}
		client, err := newSDKClientFn()
		if err != nil {
			return err
		}
		return runCommitmentView(client, args[0])
	},
}

var commitmentsRenewalCmd = &cobra.Command{
	Use:   "renewal <id>",
	Short: "What to repurchase when a commitment expires, and when to buy",
	Args:  cobra.ExactArgs(1),
	Example: `  l4 commitments renewal ri-0a1b2c3d
  l4 commitments renewal ri-0a1b2c3d --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, provider, err := commitmentClient()
		if err != nil {
			return err
		}
		if flagWeb {
			return openWeb(commitmentsWebPath(provider, instrumentRI, args[0]))
		}
		if unavailableForProvider(provider, renewalMeasuredFor,
			"Renewal planning is not measured for %s: the commitment lifecycle export is not enabled.") {
			return nil
		}
		return runCommitmentRenewal(client, args[0])
	},
}

func runCommitmentView(client *api.SDKClient, id string) error {
	detail, body, err := api.GetCommitments[api.CommitmentDetail](client.Raw(), "/detail/"+url.PathEscape(id), nil)
	if err != nil {
		return handleCommitmentsError(err)
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(body)
	}

	output.Header(detail.ServiceLabel + " " + providerKindLabel(detail.Provider, detail.Kind))
	output.KeyValue("ID", detail.ID)
	output.KeyValue("Account", detail.AccountName)
	output.KeyValue("Region", detail.Region)
	output.KeyValue("Term", termLabel(detail))
	output.KeyValue("Starts", endDateCell(detail.StartDate))
	output.KeyValue("Ends", endDateCell(detail.EndDate))
	output.KeyValue("Status", detail.Status)
	output.KeyValue("Payment", textOrNotMeasured(detail.PaymentOption))
	output.KeyValue("Committed", moneyValue(detail.MonthlyCommitmentUSD)+"/mo")
	output.KeyValue("Utilization", pctOrNotMeasured(detail.CurrentUtilizationPct))
	output.KeyValue("Coverage", coverageCell(detail.Kind, detail.CurrentCoveragePct))
	output.KeyValue("Protects", monthlyOrNotMeasured(detail.ProtectsMonthly))
	output.KeyValue("Exchangeable", boolOrNotMeasured(detail.Exchangeable))

	renderConsumers(detail.Consumers)
	renderDetailRecommendations(detail.Recommendations)
	return nil
}

// termLabel reads zero months as unknown: a term the provider's data did not
// carry, such as one from a Google Cloud export that is not enabled, is absent
// rather than instantaneous.
func termLabel(detail api.CommitmentDetail) string {
	if detail.TermMonths <= 0 {
		return notMeasured
	}
	return fmt.Sprintf("%d months", detail.TermMonths)
}

func renderConsumers(consumers []api.CommitmentConsumer) {
	if len(consumers) == 0 {
		return
	}
	output.Header("Consumed by")
	rows := make([][]string, 0, len(consumers))
	for _, consumer := range consumers {
		rows = append(rows, []string{
			consumer.AccountName,
			moneyValue(consumer.CoveredSpendMonthly) + "/mo",
			fmt.Sprintf("%.0f", consumer.CoveredHours),
		})
	}
	output.Table([]string{columnAccount, "Covered", "Hours"}, rows)
}

func renderDetailRecommendations(recommendations []api.CommitmentDetailRecommendation) {
	if len(recommendations) == 0 {
		return
	}
	output.Header("Recommendations")
	rows := make([][]string, 0, len(recommendations))
	for _, rec := range recommendations {
		rows = append(rows, []string{
			rec.ID,
			rec.Kind,
			moneyValue(rec.MonthlySavings) + "/mo",
			rec.Confidence,
			rec.Summary,
		})
	}
	output.Table([]string{"ID", "Kind", "Savings", "Confidence", "Summary"}, rows)
	output.Info("Act on one with: l4 rec accept <id>")
}

func runCommitmentRenewal(client *api.SDKClient, id string) error {
	plan, body, err := api.GetCommitments[api.CommitmentRenewalPlan](
		client.Raw(), "/renewal-plan/"+url.PathEscape(id), nil)
	if err != nil {
		return handleCommitmentsError(err)
	}
	if flagRenewalFormat == formatCSV {
		printRenewalPlanCSV(plan)
		return nil
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(body)
	}

	output.Header("Renewal plan for " + plan.CommitmentID)
	output.KeyValue("Service", plan.Service)
	output.KeyValue("Holder", plan.HolderAccountID)
	output.KeyValue("Ends", textOrNotMeasured(plan.EndAt))
	output.KeyValue("Expires in", expiryCountdown(plan.ExpiresInSeconds))
	// An instant rather than a date: a replacement bills from the moment it is
	// bought, so buying on the right day at the wrong hour pays for both.
	output.KeyValue("Buy after (UTC)", textOrNotMeasured(plan.BuyAfterUTC))

	renderRenewalSizing(plan)
	// Two different amounts, never added: one is what lapsing would cost, the
	// other is what buying less would save.
	output.KeyValue("Renewal protects", monthlyOrNotMeasured(plan.ProtectsMonthly))
	output.KeyValue("Rightsizing recovers", monthlyOrNotMeasured(plan.RightsizingMonthly))
	output.KeyValue("Exchangeable", boolOrNotMeasured(plan.Exchangeable))
	output.KeyValue("Cancellable", boolOrNotMeasured(plan.Cancellable))

	renderPendingChanges(plan.PendingChanges)
	return nil
}

// renderRenewalSizing never prints the zeros a plan nothing could size arrives
// with, since a zero reads as "renew none of it". A reservation still states
// the units it holds; only a held amount that arrived as zero is blanked.
func renderRenewalSizing(plan api.CommitmentRenewalPlan) {
	held, recommended := unitsValue(plan.UnitsHeld), unitsValue(plan.UnitsRecommended)
	notUSD := plan.SizingBasis == sizingBasisNotUSD
	if notUSD {
		recommended = notMeasured
		if plan.UnitsHeld == 0 {
			held = notMeasured
		}
	}
	output.Header("Sizing")
	output.Table(
		[]string{"Units held", "Units consumed", "Units recommended"},
		[][]string{{held, unitsOrNotMeasured(plan.UnitsConsumed), recommended}},
	)
	switch {
	case notUSD:
		output.Info("Azure bills or quotes this in a currency other than USD. LevelFour renews only commitments priced in USD, so renew it in the Azure portal.")
	case plan.Decision == decisionLetExpire:
		output.Info("Used too little over the measured window to pay for itself, so let it expire on its end date.")
	}
}

// renderPendingChanges lists the changes that should land before the renewal.
// Capacity a pending change would release is capacity the renewal must not
// reserve for another term.
func renderPendingChanges(changes []api.RenewalPendingChange) {
	if len(changes) == 0 {
		return
	}
	output.Header("Land these before renewing")
	rows := make([][]string, 0, len(changes))
	for _, change := range changes {
		rows = append(rows, []string{
			change.RecommendationID,
			change.Service,
			change.Account,
			moneyValue(change.MonthlySavings) + "/mo",
			change.Status,
		})
	}
	output.Table([]string{"ID", columnService, columnAccount, "Savings", "Status"}, rows)
	output.Info("Accept one with: l4 rec accept <id>")
}

func init() {
	commitmentsCmd.AddCommand(commitmentsViewCmd)
	commitmentsCmd.AddCommand(commitmentsRenewalCmd)
}
