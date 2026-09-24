package cli

import (
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

const flagNameCommitment = "commitment"

var (
	flagPurchaseType       string
	flagPurchaseCommitment float64
	flagPurchasePayer      string
)

var (
	planTypes        = []string{"compute", "database"}
	purchaseProfiles = []string{"conservative", "balanced", "max_savings"}
)

var planTypeLabels = map[string]string{
	"compute":  "Compute",
	"database": "Database",
}

var profileLabels = map[string]string{
	"conservative": "Conservative",
	"balanced":     "Balanced",
	"max_savings":  "Max savings",
}

var paymentLabels = map[string]string{
	"no_upfront":      "No Upfront",
	"partial_upfront": "Partial Upfront",
	"all_upfront":     "All Upfront",
}

const sourceCostExplorer = "cost_explorer_recommendation"

var sourceLabels = map[string]string{
	"cur":              "CUR",
	sourceCostExplorer: "Cost Explorer",
}

var unsizedReasons = map[string]string{
	"schema_pending":          "the daily data is still being prepared",
	"no_cur":                  "no daily billing data is loaded for this payer",
	"insufficient_history":    "too few days of billing data yet",
	"rates_pending":           "Savings Plan rates are not fetched yet",
	"payer_not_connected":     "the payer account is not connected",
	"no_on_demand_equivalent": "no eligible on-demand spend to size",
}

var profileHeaders = []string{"Profile", "Commitment/hr", "Utilization", "Coverage", "Savings/mo", "Capped by"}

func checkPlanType() error {
	if flagPurchaseType == "" {
		return fmt.Errorf("--type is required: choose one of %s", strings.Join(planTypes, ", "))
	}
	return validateChoice("type", flagPurchaseType, planTypes)
}

func optionalCommitment(cmd *cobra.Command) (*float64, error) {
	if !cmd.Flags().Changed(flagNameCommitment) {
		return nil, nil
	}
	if flagPurchaseCommitment <= 0 {
		return nil, fmt.Errorf("invalid --commitment %s: it must be above zero", quantityValue(flagPurchaseCommitment))
	}
	commitment := flagPurchaseCommitment
	return &commitment, nil
}

func labelOf(labels map[string]string, key string) string {
	if label, ok := labels[key]; ok {
		return label
	}
	return key
}

func planName(planType string) string {
	return labelOf(planTypeLabels, planType) + " Savings Plan"
}

func purchaseTerm(months int, payment string) string {
	return fmt.Sprintf("%d months, %s", months, labelOf(paymentLabels, payment))
}

// AWS sells Savings Plans to the tenth of a cent an hour.
func commitmentValue(v float64) string {
	return fmt.Sprintf("$%.3f", v)
}

func unsizedReason(reason *string) string {
	if reason == nil {
		return notMeasured
	}
	return labelOf(unsizedReasons, *reason)
}

func cappedByCell(guards []string) string {
	if len(guards) == 0 {
		return "none"
	}
	return strings.Join(guards, ", ")
}

func pickLabel(profile *string) string {
	if profile == nil {
		return "your own size"
	}
	return labelOf(profileLabels, *profile)
}

func profileLabel(name string, recommended *string) string {
	label := labelOf(profileLabels, name)
	if recommended != nil && *recommended == name {
		return label + " (recommended)"
	}
	return label
}

func profileRows(profiles map[string]api.PurchaseProfile, recommended *string) [][]string {
	rows := make([][]string, 0, len(profiles))
	for _, name := range purchaseProfiles {
		if profile, ok := profiles[name]; ok {
			rows = append(rows, []string{
				profileLabel(name, recommended),
				commitmentValue(profile.CommitmentHourly),
				pctOrNotMeasured(profile.UtilizationPct),
				pctOrNotMeasured(profile.CoveragePct),
				moneyValue(profile.NetSavingsMonthlyAtYourRates) + "/mo",
				cappedByCell(profile.CappedBy),
			})
		}
	}
	return rows
}

func renderProposals(proposals []api.PurchaseProposal) {
	renderProposalTable(proposals)
	for _, proposal := range proposals {
		payer := textOrNotMeasured(proposal.PayerAccountID)
		if proposal.Profiles == nil {
			output.Info(fmt.Sprintf("No %s proposal for %s: %s.",
				planName(proposal.PlanType), payer, unsizedReason(proposal.UnavailableReason)))
		}
		if raised := proposal.Raised; raised != nil {
			output.Info(fmt.Sprintf("%s is raised for the %s on %s at %s/hr (%s), and nobody has decided it yet.",
				raised.RecommendationID, planName(proposal.PlanType), payer,
				commitmentValue(raised.CommitmentHourly), pickLabel(raised.Profile)))
		}
	}
}

func renderProposalTable(proposals []api.PurchaseProposal) {
	var rows [][]string
	for _, proposal := range proposals {
		payer, plan := textOrNotMeasured(proposal.PayerAccountID), labelOf(planTypeLabels, proposal.PlanType)
		for _, cells := range profileRows(proposal.Profiles, proposal.RecommendedProfile) {
			rows = append(rows, append([]string{payer, plan}, cells...))
		}
	}
	if len(rows) == 0 {
		return
	}
	output.Header("Savings Plan proposals")
	output.Table(append([]string{"Payer", "Plan"}, profileHeaders...), rows)
	output.Info("Each proposal is a 12-month, No Upfront plan sized on the daily floor. Savings are monthly, at your own rates.")
	output.Info("Replay another size with 'l4 commitments simulate', or raise one with 'l4 commitments propose'.")
}
