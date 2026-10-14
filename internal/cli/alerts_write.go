package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagAlertsFile string
	flagAlertsYes  bool
)

// Far above any rule the API accepts, which caps recipients at 20 and channels at 10.
const maxAlertFileBytes = 1 << 20

var errStdinTaken = errors.New("--file - reads the rule from stdin, which leaves nothing to answer the prompt with: pass --yes")

var alertsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a cost alert from a rule file",
	Long: `Create a cost alert from a rule file.

The file is the request body, so a rule can be reviewed in a pull request
before it reaches the API. Read one back with 'l4 alerts get <id> --json'
to see the shape the API returns. Pass --file - to read the rule from stdin.`,
	Example: `  l4 alerts create --file rule.json
  cat rule.json | l4 alerts create --file - --yes`,
	RunE: func(_ *cobra.Command, _ []string) error {
		rule, err := readAlertFile(flagAlertsFile)
		if err != nil {
			return err
		}
		approved, err := confirmAlertFile("Create this cost alert?")
		if err != nil {
			return err
		}
		if !approved {
			output.Info("Aborted.")
			return nil
		}

		envelope, err := postWrite(alertsPath, rule)
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

var alertsUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Change a cost alert from a file of the fields to change",
	Long: `Change a cost alert from a file of the fields to change.

Only the fields in the file change: any of name, params, source_id, recipients
and slack_channel_ids. params replaces the whole rule, so to move one threshold
send the rule back with that figure changed. slack_channel_ids replaces the
channels, and an empty list takes them all off. Pass --file - for stdin.`,
	Args: oneAlertID,
	Example: `  l4 alerts update 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234 --file change.json
  l4 alerts get 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234 --json \
    | jq '{params: (.data.params | .budget = 50000)}' \
    | l4 alerts update 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234 --file - --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		change, err := readAlertFile(flagAlertsFile)
		if err != nil {
			return err
		}
		if len(change) == 0 {
			return fmt.Errorf("%s changes nothing: name at least one field to change", flagAlertsFile)
		}
		id := args[0]
		approved, err := confirmAlertFile(fmt.Sprintf("Change cost alert %s?", id))
		if err != nil {
			return err
		}
		if !approved {
			output.Info("Aborted.")
			return nil
		}

		envelope, err := patchWrite(alertPath(id), change)
		if err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(envelope)
		}
		output.Success(fmt.Sprintf("Cost alert %s changed", id))
		printAlert(envelopeData(envelope))
		return nil
	},
}

var alertsPauseCmd = &cobra.Command{
	Use:   "pause <id>",
	Short: "Stop an alert from notifying, keeping its history",
	Args:  oneAlertID,
	Example: `  l4 alerts pause 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234
  l4 alerts pause 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234 --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		return setAlertEnabled(args[0], false)
	},
}

var alertsResumeCmd = &cobra.Command{
	Use:     "resume <id>",
	Short:   "Let a paused alert notify again",
	Args:    oneAlertID,
	Example: `  l4 alerts resume 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234`,
	RunE: func(_ *cobra.Command, args []string) error {
		return setAlertEnabled(args[0], true)
	},
}

var alertsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a cost alert and its history",
	Long: `Delete a cost alert and its history.

Deleting drops the history behind 'l4 alerts events', and nothing brings it
back. Pause the alert instead to stop the notifications and keep that history.
Outside a terminal, delete needs --yes.`,
	Args:    oneAlertID,
	Example: `  l4 alerts delete 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234 --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		id := args[0]
		approved, err := requireApproval(flagAlertsYes, fmt.Sprintf("Delete cost alert %s and its history?", id), "deleting "+id)
		if err != nil {
			return err
		}
		if !approved {
			output.Info("Aborted.")
			return nil
		}

		if err := deleteWrite(alertPath(id)); err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(map[string]interface{}{"id": id, "deleted": true})
		}
		output.Success(fmt.Sprintf("Cost alert %s deleted", id))
		return nil
	},
}

func setAlertEnabled(id string, enabled bool) error {
	verb, done := "Pause", "paused"
	if enabled {
		verb, done = "Resume", "resumed"
	}
	if !flagAlertsYes && !confirmAction(fmt.Sprintf("%s cost alert %s?", verb, id)) {
		output.Info("Aborted.")
		return nil
	}

	envelope, err := patchWrite(alertPath(id), map[string]bool{"enabled": enabled})
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	output.Success(fmt.Sprintf("Cost alert %s %s", id, done))
	output.KeyValue("Status", alertStatus(envelopeData(envelope)))
	return nil
}

// A prompt reads its answer from stdin, which --file - has already read to the end, so the
// answer would always be empty and the write would abort with nothing said.
func confirmAlertFile(prompt string) (bool, error) {
	if flagAlertsYes {
		return true, nil
	}
	if flagAlertsFile == "-" && isTerminal() {
		return false, errStdinTaken
	}
	return confirmAction(prompt), nil
}

// Decoded rather than forwarded verbatim so a malformed file is reported against
// the file the operator can fix, not as a 422 about a request they did not write.
func readAlertFile(path string) (map[string]interface{}, error) {
	if path == "" {
		return nil, fmt.Errorf("--file is required: pass a JSON file, or - for stdin")
	}

	raw, err := readCapped(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	if len(raw) > maxAlertFileBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes, far more than any alert needs", path, maxAlertFileBytes)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object: %w", path, err)
	}
	return body, nil
}

func init() {
	for _, c := range []*cobra.Command{alertsCreateCmd, alertsUpdateCmd} {
		c.Flags().StringVarP(&flagAlertsFile, "file", "f", "", "Path to a JSON file, or - for stdin")
	}
	for _, c := range []*cobra.Command{alertsCreateCmd, alertsUpdateCmd, alertsPauseCmd, alertsResumeCmd, alertsDeleteCmd} {
		c.Flags().BoolVarP(&flagAlertsYes, wordYes, "y", false, "Skip the confirmation prompt")
		alertsCmd.AddCommand(c)
	}
}

// One byte past the cap is read, so a file at the cap is told apart from one above it.
func readCapped(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(io.LimitReader(stdinReader, maxAlertFileBytes+1))
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	// Read-only, so a failed close loses nothing the read did not already return.
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxAlertFileBytes+1))
}
