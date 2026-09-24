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
	flagRecYes           bool
	flagRecReason        string
	flagRecExplanation   string
	flagRecMethod        string
	flagRecRequestMethod string
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
	Example: `  l4 rec accept REC-1234
  l4 rec accept REC-1234 --yes`,
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
	Example: `  l4 rec reject REC-1234
  l4 rec reject REC-1234 --reason operational
  l4 rec reject REC-1234 --reason other --explanation "Owned by a team that is migrating off"`,
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
	Long: `Request execution of an accepted savings recommendation.

Somebody other than the credential that executes it must have accepted it.
A recommendation that buys a commitment, such as a renewal, starts only when an
organization admin releases it in the dashboard, so the API refuses it here:
file it for release with 'l4 rec request' instead.`,
	Args: cobra.ExactArgs(1),
	Example: `  l4 rec execute REC-1234
  l4 rec execute REC-1234 --method iac
  l4 rec execute REC-1234 --method manual --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		if err := checkImplementationMethod(flagRecMethod); err != nil {
			return err
		}
		return runExecute(args[0])
	},
}

var recommendationsRequestCmd = &cobra.Command{
	Use:   "request <id>",
	Short: "Ask an organization admin to release a savings recommendation",
	Long: `Ask an organization admin to release a savings recommendation.

The request lands in Needs Approval in the dashboard, where an admin releases
it. A commitment renewal takes only 'one-click' or 'manual', and cannot be
requested again once it is released.`,
	Args: cobra.ExactArgs(1),
	Example: `  l4 rec request RENEW-12 --method one-click
  l4 rec request RENEW-12 --method manual --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		if err := checkImplementationMethod(flagRecRequestMethod); err != nil {
			return err
		}
		return runRequest(args[0])
	},
}

func checkImplementationMethod(method string) error {
	choices := strings.Join(implementationMethods, ", ")
	if method == "" {
		return fmt.Errorf("--method is required: choose one of %s", choices)
	}
	if !slices.Contains(implementationMethods, method) {
		return fmt.Errorf("invalid --method %q: choose one of %s", method, choices)
	}
	return nil
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

func runRequest(id string) error {
	if !flagRecYes && !confirmAction(fmt.Sprintf("Ask an organization admin to release %s using %s?", id, flagRecRequestMethod)) {
		output.Info("Aborted.")
		return nil
	}

	envelope, err := postWrite("/api/v1/recommendations/"+url.PathEscape(id)+"/execution/request", map[string]string{
		"implementation_method": flagRecRequestMethod,
	})
	if err != nil {
		return err
	}

	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}

	data := envelopeData(envelope)
	output.Success(fmt.Sprintf("Release requested for recommendation %s", id))
	if v := dataString(data, "execution_request_status"); v != "" {
		output.KeyValue("Status", output.StatusBadge(v))
	}
	output.KeyValue("Method", flagRecRequestMethod)
	output.Info("An organization admin releases it from Needs Approval in the dashboard.")
	return nil
}

func init() {
	for _, c := range []*cobra.Command{
		recommendationsAcceptCmd, recommendationsRejectCmd, recommendationsExecuteCmd, recommendationsRequestCmd,
	} {
		c.Flags().BoolVarP(&flagRecYes, wordYes, "y", false, "Skip the confirmation prompt")
	}

	recommendationsRejectCmd.Flags().StringVar(&flagRecReason, "reason", "", "Rejection reason: "+strings.Join(rejectionReasons, ", "))
	recommendationsRejectCmd.Flags().StringVar(&flagRecExplanation, "explanation", "", "Free text explanation, used when --reason is 'other'")

	recommendationsExecuteCmd.Flags().StringVar(&flagRecMethod, "method", defaultImplementationMethod, "Implementation method: "+strings.Join(implementationMethods, ", "))
	recommendationsRequestCmd.Flags().StringVar(&flagRecRequestMethod, "method", "", "Implementation method, required: "+strings.Join(implementationMethods, ", "))

	recommendationsCmd.AddCommand(recommendationsAcceptCmd)
	recommendationsCmd.AddCommand(recommendationsRejectCmd)
	recommendationsCmd.AddCommand(recommendationsExecuteCmd)
	recommendationsCmd.AddCommand(recommendationsRequestCmd)
}
