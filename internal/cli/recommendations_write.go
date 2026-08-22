package cli

import (
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagRecYes         bool
	flagRecReason      string
	flagRecExplanation string
	flagRecMethod      string
)

const (
	decisionAccepted = "accepted"
	decisionRejected = "rejected"

	defaultImplementationMethod = "one-click"
)

// rejectionReasons mirrors the pattern the API enforces on
// SavingsDecisionRequest.reason. Reason stays optional on reject.
var rejectionReasons = []string{"operational", "strategy", "not_applicable", "other"}

// implementationMethods mirrors the pattern the API enforces on
// ExecutionRequestBody.implementation_method.
var implementationMethods = []string{"one-click", "iac", "one-click-plus-iac", "manual"}

var recommendationsAcceptCmd = &cobra.Command{
	Use:   "accept <id>",
	Short: "Accept a savings recommendation",
	Args:  cobra.ExactArgs(1),
	Example: `  l4 rec accept CLICK-243
  l4 rec accept CLICK-243 --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runDecision(args[0], decisionAccepted)
	},
}

var recommendationsRejectCmd = &cobra.Command{
	Use:   "reject <id>",
	Short: "Reject a savings recommendation",
	Long: `Reject a savings recommendation.

The reason is optional. You can add or change it later from the dashboard.`,
	Args: cobra.ExactArgs(1),
	Example: `  l4 rec reject CLICK-243
  l4 rec reject CLICK-243 --reason operational
  l4 rec reject CLICK-243 --reason other --explanation "Owned by a team that is migrating off"`,
	RunE: func(_ *cobra.Command, args []string) error {
		if flagRecReason != "" && !slices.Contains(rejectionReasons, flagRecReason) {
			return fmt.Errorf("invalid --reason %q: choose one of %s", flagRecReason, strings.Join(rejectionReasons, ", "))
		}
		return runDecision(args[0], decisionRejected)
	},
}

var recommendationsExecuteCmd = &cobra.Command{
	Use:   "execute <id>",
	Short: "Request execution of an accepted savings recommendation",
	Args:  cobra.ExactArgs(1),
	Example: `  l4 rec execute CLICK-243
  l4 rec execute CLICK-243 --method iac
  l4 rec execute CLICK-243 --method manual --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		if !slices.Contains(implementationMethods, flagRecMethod) {
			return fmt.Errorf("invalid --method %q: choose one of %s", flagRecMethod, strings.Join(implementationMethods, ", "))
		}
		return runExecute(args[0])
	},
}

func runDecision(id, decision string) error {
	verb := "Accept"
	if decision == decisionRejected {
		verb = "Reject"
	}
	if !flagRecYes && !confirmAction(fmt.Sprintf("%s recommendation %s?", verb, id)) {
		output.Info("Aborted.")
		return nil
	}

	payload := map[string]string{"decision": decision}
	if decision == decisionRejected {
		if flagRecReason != "" {
			payload["reason"] = flagRecReason
		}
		if flagRecExplanation != "" {
			payload["explanation"] = flagRecExplanation
		}
	}

	envelope, err := postWrite("/api/v1/recommendations/"+url.PathEscape(id)+"/decision", payload)
	if err != nil {
		return err
	}

	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}

	data := envelopeData(envelope)
	output.Success(fmt.Sprintf("Recommendation %s %s", id, decision))
	if v := dataString(data, "saving_accepted_by"); v != "" {
		output.KeyValue("By", v)
	}
	if v := dataString(data, "saving_accepted_at"); v != "" {
		output.KeyValue("At", formatDate(v))
	}
	if v := dataString(data, "rejection_reason"); v != "" {
		output.KeyValue("Reason", v)
	}
	if v := dataString(data, "rejection_explanation"); v != "" {
		output.KeyValue("Explanation", v)
	}
	return nil
}

func runExecute(id string) error {
	if !flagRecYes && !confirmAction(fmt.Sprintf("Execute recommendation %s using the %s method?", id, flagRecMethod)) {
		output.Info("Aborted.")
		return nil
	}

	envelope, err := postWrite("/api/v1/recommendations/audit/execution-requests", map[string]string{
		"recommendation_id":     id,
		"implementation_method": flagRecMethod,
	})
	if err != nil {
		return err
	}

	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}

	data := envelopeData(envelope)
	output.Success(fmt.Sprintf("Execution requested for recommendation %s", id))
	if v := dataString(data, "status"); v != "" {
		output.KeyValue("Status", output.StatusBadge(v))
	}
	if v := dataString(data, "implementation_method"); v != "" {
		output.KeyValue("Method", v)
	}
	return nil
}

func init() {
	for _, c := range []*cobra.Command{recommendationsAcceptCmd, recommendationsRejectCmd, recommendationsExecuteCmd} {
		c.Flags().BoolVarP(&flagRecYes, "yes", "y", false, "Skip the confirmation prompt")
	}

	recommendationsRejectCmd.Flags().StringVar(&flagRecReason, "reason", "", "Rejection reason: operational, strategy, not_applicable, other")
	recommendationsRejectCmd.Flags().StringVar(&flagRecExplanation, "explanation", "", "Free text explanation, used when --reason is 'other'")

	recommendationsExecuteCmd.Flags().StringVar(&flagRecMethod, "method", defaultImplementationMethod, "Implementation method: one-click, iac, one-click-plus-iac, manual")

	recommendationsCmd.AddCommand(recommendationsAcceptCmd)
	recommendationsCmd.AddCommand(recommendationsRejectCmd)
	recommendationsCmd.AddCommand(recommendationsExecuteCmd)
}
