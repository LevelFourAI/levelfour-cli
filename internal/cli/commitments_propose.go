package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagProposeProfile string
	flagProposeYes     bool
)

var commitmentsProposeCmd = &cobra.Command{
	Use:   "propose",
	Short: "Raise a Savings Plan purchase from its proposal. Buys nothing",
	Long: `Raise one payer's Savings Plan proposal as a savings recommendation, BUY-
followed by a number, for a 12-month, No Upfront plan.

It buys nothing. --profile picks a sized profile from 'l4 commitments plan',
balanced when neither flag is given, or --commitment names a size of your own
in dollars an hour. The API rounds that down to $0.001 and refuses it above
AWS's own recommendation. --payer may be omitted when the organization has one
payer.

Accept the recommendation with 'l4 rec accept', then file it for release with
'l4 rec request --method manual'. Once an organization admin releases it, you
buy the plan yourself, following the steps the dashboard shows.

Running it again on the same pick returns the purchase already raised. The API
refuses another pick while that one is undecided: reject it with 'l4 rec
reject' and propose again.

It needs a read-write key, and outside a terminal it needs --yes.`,
	Example: `  l4 commitments propose --type compute
  l4 commitments propose --type compute --profile max_savings --payer 111122223333
  l4 commitments propose --type database --commitment 1.25 --yes --json`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		pick, err := purchasePickFromFlags(cmd)
		if err != nil {
			return err
		}
		return runCommitmentsPropose(pick)
	},
}

type purchasePick struct {
	PlanType         string   `json:"plan_type"`
	PayerAccountID   string   `json:"payer_account_id,omitempty"`
	Profile          string   `json:"profile,omitempty"`
	CommitmentHourly *float64 `json:"commitment_hourly,omitempty"`
}

// Mirrors the API's own 422s, so a pick it would refuse never costs a request.
func purchasePickFromFlags(cmd *cobra.Command) (purchasePick, error) {
	pick := purchasePick{PlanType: flagPurchaseType, PayerAccountID: flagPurchasePayer, Profile: flagProposeProfile}
	if err := checkPlanType(); err != nil {
		return pick, err
	}
	if err := validateChoice("profile", flagProposeProfile, purchaseProfiles); err != nil {
		return pick, err
	}
	commitment, err := optionalCommitment(cmd)
	if err != nil {
		return pick, err
	}
	if commitment != nil && pick.Profile != "" {
		return pick, errors.New("name --profile or --commitment, not both")
	}
	pick.CommitmentHourly = commitment
	return pick, nil
}

func runCommitmentsPropose(pick purchasePick) error {
	plan := planName(pick.PlanType)
	approved, err := requireApproval(flagProposeYes,
		fmt.Sprintf("Raise a %s purchase? Nothing is bought until an organization admin releases it.", plan),
		"raising a "+plan+" purchase")
	if err != nil {
		return err
	}
	if !approved {
		output.Info("Aborted.")
		return nil
	}
	purchase, body, err := postData[api.CommitmentPurchase](api.CommitmentsPath+"/purchase", pick)
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(body)
	}
	renderPurchase(purchase)
	return nil
}

func renderPurchase(purchase api.CommitmentPurchase) {
	output.Success(purchaseOutcome(purchase))
	output.KeyValue("Payer", purchase.PayerAccountID)
	output.KeyValue("Plan", planName(purchase.PlanType)+", "+purchaseTerm(purchase.TermMonths, purchase.PaymentOption))
	output.KeyValue("Commitment", commitmentValue(purchase.CommitmentHourly)+"/hr")
	output.KeyValue("Profile", pickLabel(purchase.Profile))
	output.KeyValue("Capped by", cappedByCell(purchase.CappedBy))
	output.KeyValue("Savings", moneyValue(purchase.MonthlySavings)+"/mo")
	output.Info("Raising it bought nothing. Once an organization admin releases it, you buy the plan yourself in AWS.")
	output.Info("Accept it with: l4 rec accept " + purchase.RecommendationID)
	output.Info("Then file it for release with: l4 rec request " + purchase.RecommendationID + " --method manual")
}

func purchaseOutcome(purchase api.CommitmentPurchase) string {
	if purchase.Created {
		return "Raised " + purchase.RecommendationID
	}
	return fmt.Sprintf("%s was already raised on this pick. Nothing changed", purchase.RecommendationID)
}

func init() {
	addPurchaseTargetFlags(commitmentsProposeCmd)
	commitmentsProposeCmd.Flags().StringVar(&flagProposeProfile, "profile", "",
		"Sized profile: "+strings.Join(purchaseProfiles, ", ")+"; balanced if neither this nor --commitment is given")
	commitmentsProposeCmd.Flags().Float64Var(&flagPurchaseCommitment, flagNameCommitment, 0,
		"Dollars an hour of your own, instead of a profile")
	commitmentsProposeCmd.Flags().BoolVarP(&flagProposeYes, wordYes, "y", false, "Skip the confirmation prompt")
	commitmentsCmd.AddCommand(commitmentsProposeCmd)
}
