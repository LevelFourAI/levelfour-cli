package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagAlertsFile string
	flagAlertsYes  bool
)

var costAlertsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a cost alert from a rule file",
	Long: `Create a cost alert from a rule file.

The file is the request body, so a rule can be reviewed in a pull request
before it reaches the API. Read one back with 'l4 costalerts get <id> --json'
to see the shape the API returns. Pass --file - to read the rule from stdin.`,
	Example: `  l4 costalerts create --file rule.json
  cat rule.json | l4 costalerts create --file - --yes`,
	RunE: func(_ *cobra.Command, _ []string) error {
		rule, err := readRuleFile(flagAlertsFile)
		if err != nil {
			return err
		}
		if !flagAlertsYes && !confirmAction("Create this cost alert?") {
			output.Info("Aborted.")
			return nil
		}

		envelope, err := postWrite(costAlertsPath, rule)
		if err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(envelope)
		}

		alert := envelopeData(envelope)
		output.Success("Cost alert created")
		output.KeyValue("ID", dataString(alert, "id"))
		output.KeyValue("Name", dataString(alert, "name"))
		output.KeyValue("Source", dataString(alert, "source_id"))
		return nil
	},
}

var costAlertsPauseCmd = &cobra.Command{
	Use:   "pause <id>",
	Short: "Stop an alert from notifying",
	Args:  cobra.ExactArgs(1),
	Example: `  l4 costalerts pause 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234
  l4 costalerts pause 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234 --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runSetEnabled(args[0], false)
	},
}

var costAlertsResumeCmd = &cobra.Command{
	Use:     "resume <id>",
	Short:   "Let a paused alert notify again",
	Args:    cobra.ExactArgs(1),
	Example: `  l4 costalerts resume 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runSetEnabled(args[0], true)
	},
}

var costAlertsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a cost alert",
	Long: `Delete a cost alert.

Deleting drops the history behind 'l4 costalerts events'. Pause it instead to
keep that and stop the notifications.`,
	Args:    cobra.ExactArgs(1),
	Example: `  l4 costalerts delete 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234 --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		id := args[0]
		if !flagAlertsYes && !confirmAction(fmt.Sprintf("Delete cost alert %s?", id)) {
			output.Info("Aborted.")
			return nil
		}

		envelope, err := sendWrite("DELETE", costAlertsPath+"/"+url.PathEscape(id), nil)
		if err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(envelope)
		}
		output.Success(fmt.Sprintf("Cost alert %s deleted", id))
		return nil
	},
}

func runSetEnabled(id string, enabled bool) error {
	verb := "Pause"
	done := "paused"
	if enabled {
		verb = "Resume"
		done = "resumed"
	}
	if !flagAlertsYes && !confirmAction(fmt.Sprintf("%s cost alert %s?", verb, id)) {
		output.Info("Aborted.")
		return nil
	}

	envelope, err := sendWrite(
		"PATCH",
		costAlertsPath+"/"+url.PathEscape(id),
		map[string]bool{"enabled": enabled},
	)
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}

	alert := envelopeData(envelope)
	output.Success(fmt.Sprintf("Cost alert %s %s", id, done))
	output.KeyValue("Status", alertStatus(alert))
	return nil
}

// Decoded rather than forwarded verbatim so a malformed file is reported against
// the file the operator can fix, not as a 422 about a request they did not write.
func readRuleFile(path string) (map[string]interface{}, error) {
	if path == "" {
		return nil, fmt.Errorf("--file is required: pass a JSON rule file, or - for stdin")
	}

	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(stdinReader)
	} else {
		raw, err = os.ReadFile(filepath.Clean(path))
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}

	var rule map[string]interface{}
	if err := json.Unmarshal(raw, &rule); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object: %w", path, err)
	}
	return rule, nil
}

func init() {
	costAlertsCreateCmd.Flags().StringVar(&flagAlertsFile, "file", "", "Path to a JSON rule file, or - for stdin")
	for _, c := range []*cobra.Command{
		costAlertsCreateCmd,
		costAlertsPauseCmd,
		costAlertsResumeCmd,
		costAlertsDeleteCmd,
	} {
		c.Flags().BoolVarP(&flagAlertsYes, wordYes, "y", false, "Skip the confirmation prompt")
	}

	costAlertsCmd.AddCommand(costAlertsCreateCmd)
	costAlertsCmd.AddCommand(costAlertsPauseCmd)
	costAlertsCmd.AddCommand(costAlertsResumeCmd)
	costAlertsCmd.AddCommand(costAlertsDeleteCmd)
}
