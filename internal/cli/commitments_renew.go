package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	flagNameQuantity = "quantity"
	nothingPicked    = "none"
)

var (
	flagRenewOffering string
	flagRenewQuantity float64
	flagRenewYes      bool
)

var commitmentsRenewCmd = &cobra.Command{
	Use:   "renew <id>",
	Short: "Raise the renewal of an expiring commitment. Buys nothing",
	Long: `Raise the renewal of an expiring commitment as a savings recommendation.

Nothing is bought here. The renewal is a RENEW- recommendation: accept it with
'l4 rec accept', then file it for release with 'l4 rec request --method
one-click' or '--method manual'. An organization admin releases it in the
dashboard.

With no flags the recommended option is raised. --offering picks another priced
option, as listed by 'l4 api /api/v1/commitments/renewal-options/<id>', and
--quantity sets how much of it to buy: whole units for a reservation, dollars
an hour for a Savings Plan. Running it again returns the renewal already
raised, or points one nobody has decided on at the new pick. A rejected
renewal is replaced by a new one.

Outside a terminal it needs --yes.`,
	Args: cobra.ExactArgs(1),
	Example: `  l4 commitments renew ri-0a1b2c3d
  l4 commitments renew ri-0a1b2c3d --offering 438012d3-4052-4cc7-b2e3-8d3372e0e706 --quantity 4
  l4 commitments renew ri-0a1b2c3d --yes --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		pick, err := renewalPickFromFlags(cmd)
		if err != nil {
			return err
		}
		return runCommitmentRenew(args[0], pick)
	},
}

type renewalPick struct {
	OfferingID string   `json:"offering_id,omitempty"`
	Quantity   *float64 `json:"quantity,omitempty"`
}

// Mirrors the API's own 422s, so a pick it would refuse never costs a request.
func renewalPickFromFlags(cmd *cobra.Command) (renewalPick, error) {
	pick := renewalPick{OfferingID: flagRenewOffering}
	if !cmd.Flags().Changed(flagNameQuantity) {
		return pick, nil
	}
	if pick.OfferingID == "" {
		return pick, errors.New("--quantity is priced per offering, so it needs --offering")
	}
	if flagRenewQuantity <= 0 {
		return pick, fmt.Errorf("invalid --quantity %s: it must be above zero", quantityValue(flagRenewQuantity))
	}
	quantity := flagRenewQuantity
	pick.Quantity = &quantity
	return pick, nil
}

func runCommitmentRenew(id string, pick renewalPick) error {
	approved, err := requireApproval(flagRenewYes,
		fmt.Sprintf("Raise a renewal for %s? Nothing is bought until an organization admin releases it.", id),
		"raising a renewal for "+id)
	if err != nil {
		return err
	}
	if !approved {
		output.Info("Aborted.")
		return nil
	}
	if output.HasFormattingFlags() {
		return raiseAndPrintRenewal(id, pick)
	}
	return raiseAndRenderRenewal(id, pick)
}

func raiseAndPrintRenewal(id string, pick renewalPick) error {
	_, body, err := raiseRenewal(id, pick)
	if err != nil {
		return err
	}
	return output.PrintResult(body)
}

func raiseAndRenderRenewal(id string, pick renewalPick) error {
	replaced := rejectedRenewalID(id)
	renewal, _, err := raiseRenewal(id, pick)
	if err != nil {
		return err
	}
	renderRenewal(renewal, replaced)
	return nil
}

func renewalRoute(id string) string {
	return "/renewal/" + url.PathEscape(id)
}

// The raise answers a replaced rejection and a first raise alike, so only a read beforehand
// tells them apart. That read only words the outcome, so its failure never blocks the raise.
func rejectedRenewalID(id string) string {
	client, err := newSDKClientFn()
	if err != nil {
		return ""
	}
	renewal, _, err := api.GetCommitments[api.CommitmentRenewal](client.Raw(), renewalRoute(id), nil)
	if err != nil || !renewal.Closed {
		return ""
	}
	return renewal.RecommendationID
}

func raiseRenewal(id string, pick renewalPick) (api.CommitmentRenewal, json.RawMessage, error) {
	return postData[api.CommitmentRenewal](api.CommitmentsPath+renewalRoute(id), pick)
}

func renderRenewal(renewal api.CommitmentRenewal, replaced string) {
	output.Success(renewalOutcome(renewal, replaced))
	output.KeyValue("Recommendation", renewal.RecommendationID)
	output.KeyValue("Offering", offeringCell(renewal.OfferingID))
	output.KeyValue("Quantity", pickedQuantityCell(renewal.Quantity))
	output.KeyValue("Can change", canChangeCell(renewal.Rebindable))
	output.Info("Nothing is bought until an organization admin releases it.")
	renderBlockingChanges(renewal.BlockingChanges)
	if renewal.Rebindable {
		renderRenewalNextSteps(renewal.RecommendationID)
	}
}

func renewalOutcome(renewal api.CommitmentRenewal, replaced string) string {
	switch {
	case renewal.Created && replaced != "":
		return fmt.Sprintf("Raised %s for %s, replacing %s, which was rejected",
			renewal.RecommendationID, renewal.CommitmentID, replaced)
	case renewal.Created:
		return fmt.Sprintf("Raised %s for %s", renewal.RecommendationID, renewal.CommitmentID)
	case renewal.Rebound:
		return fmt.Sprintf("%s now buys the option you picked", renewal.RecommendationID)
	}
	return fmt.Sprintf("%s was already raised for %s. Nothing changed", renewal.RecommendationID, renewal.CommitmentID)
}

func offeringCell(offeringID *string) string {
	if offeringID == nil || *offeringID == "" {
		return nothingPicked
	}
	return *offeringID
}

func pickedQuantityCell(quantity *float64) string {
	if quantity == nil {
		return nothingPicked
	}
	return quantityValue(*quantity)
}

func canChangeCell(rebindable bool) string {
	if rebindable {
		return "yes, until someone accepts, rejects or requests it"
	}
	return "no, it has been decided"
}

func renderBlockingChanges(changes []api.RenewalPendingChange) {
	if len(changes) == 0 {
		return
	}
	output.Header("Accepted changes still to land")
	renderChangesTable(changes)
	output.Info("Each shrinks what the renewal needs to cover, and the sizing does not count on it landing.")
}

func renderRenewalNextSteps(recommendationID string) {
	output.Info("Accept it with: l4 rec accept " + recommendationID)
	output.Info("Then file it for release with: l4 rec request " + recommendationID +
		" --method one-click, or --method manual to buy it yourself")
}

func init() {
	commitmentsRenewCmd.Flags().StringVar(&flagRenewOffering, "offering", "",
		"Offering ID of another priced option; the recommended one if omitted")
	commitmentsRenewCmd.Flags().Float64Var(&flagRenewQuantity, flagNameQuantity, 0,
		"How much to buy: whole units for a reservation, dollars an hour for a Savings Plan. Needs --offering")
	commitmentsRenewCmd.Flags().BoolVarP(&flagRenewYes, wordYes, "y", false, "Skip the confirmation prompt")
	commitmentsCmd.AddCommand(commitmentsRenewCmd)
}
