package cli

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

const alertsPath = "/api/v1/cost-alerts"

// Every alert id is a UUID. Checked before the request so an argument such as ".." or an empty
// string never becomes part of a path.
var alertIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`)

var (
	flagAlertsLimit  int
	flagAlertsOffset int
)

var alertsCmd = &cobra.Command{
	Use:     "alerts",
	Aliases: []string{"costalerts", "alert"},
	Short:   "Manage cost alerts",
	Long: `Manage cost alerts.

An alert watches one source, an account or a saved cost view, and notifies the
recipients and Slack channels you name on it. Writes need a read-write API key.`,
}

var alertsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cost alerts, and which are firing",
	Example: `  l4 alerts list
  l4 alerts list --json`,
	RunE: func(_ *cobra.Command, _ []string) error {
		envelope, err := getJSON(alertsPath)
		if err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(envelope)
		}

		alerts := envelopeList(envelope)
		if len(alerts) == 0 {
			output.Info("No cost alerts configured.")
			return nil
		}

		rows := make([][]string, 0, len(alerts))
		for _, alert := range alerts {
			rows = append(rows, []string{
				dataString(alert, "id"),
				dataString(alert, "name"),
				dataString(alert, "source_id"),
				alertKind(alert),
				alertStatus(alert),
			})
		}
		output.Table([]string{"ID", "Name", "Source", "Watches", "Status"}, rows)
		return nil
	},
}

var alertsGetCmd = &cobra.Command{
	Use:     "get <id>",
	Short:   "Read one cost alert",
	Args:    oneAlertID,
	Example: `  l4 alerts get 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234`,
	RunE: func(_ *cobra.Command, args []string) error {
		envelope, err := getJSON(alertPath(args[0]))
		if err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(envelope)
		}
		printAlert(envelopeData(envelope))
		return nil
	},
}

var alertsSourcesCmd = &cobra.Command{
	Use:   "sources",
	Short: "List what an alert can watch",
	Long: `List what an alert can watch.

Every id here is valid in the source_id field of a rule file.`,
	Example: `  l4 alerts sources
  l4 alerts sources --json`,
	RunE: func(_ *cobra.Command, _ []string) error {
		envelope, err := getJSON(alertsPath + "/sources")
		if err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(envelope)
		}

		sources := envelopeList(envelope)
		if len(sources) == 0 {
			output.Info("No sources available. Connect an account first.")
			return nil
		}

		rows := make([][]string, 0, len(sources))
		for _, source := range sources {
			rows = append(rows, []string{
				dataString(source, "id"),
				dataString(source, "label"),
				dataString(source, "group"),
				dataString(source, "provider"),
			})
		}
		output.Table([]string{"ID", "Label", "Group", "Provider"}, rows)
		return nil
	},
}

var alertsEventsCmd = &cobra.Command{
	Use:   "events <id>",
	Short: "What an alert has said, newest first",
	Args:  oneAlertID,
	Example: `  l4 alerts events 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234
  l4 alerts events 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234 --limit 10 --offset 10`,
	RunE: func(_ *cobra.Command, args []string) error {
		query := api.BuildQueryString(map[string]string{
			"limit":  strconv.Itoa(flagAlertsLimit),
			"offset": strconv.Itoa(flagAlertsOffset),
		})
		envelope, err := getJSON(alertPath(args[0]) + "/events" + query)
		if err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(envelope)
		}

		events := envelopeList(envelope)
		if len(events) == 0 {
			output.Info("This alert has not fired yet.")
			return nil
		}

		rows := make([][]string, 0, len(events))
		for _, event := range events {
			rows = append(rows, []string{
				formatDate(dataString(event, "created_at")),
				eventScope(event),
				output.StatusBadge(dataString(event, "state")),
				dataAmount(event, "observed_usd"),
				dataAmount(event, "threshold_usd"),
				dataString(event, "cost_basis"),
				yesNo(event, "notified"),
			})
		}
		output.Table(
			[]string{"When", "Scope", "State", "Observed", "Threshold", "Basis", "Notified"},
			rows,
		)
		return nil
	},
}

func oneAlertID(_ *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected one cost alert id, got %d", len(args))
	}
	if !alertIDPattern.MatchString(args[0]) {
		return fmt.Errorf("%q is not a cost alert id: ids are UUIDs, as 'l4 alerts list' shows them", args[0])
	}
	return nil
}

func alertPath(id string) string {
	return alertsPath + "/" + id
}

func printAlert(alert map[string]interface{}) {
	output.Header(dataString(alert, "name"))
	output.KeyValue("ID", dataString(alert, "id"))
	output.KeyValue("Source", dataString(alert, "source_id"))
	output.KeyValue("Watches", alertKind(alert))
	output.KeyValue("Status", alertStatus(alert))
	output.KeyValue("Recipients", recipientList(alert))
	output.KeyValue("Slack channels", channelList(alert))
	output.KeyValue("Rule", compactJSON(alert["params"]))
}

func alertKind(alert map[string]interface{}) string {
	params, _ := alert["params"].(map[string]interface{})
	return dataString(params, "kind")
}

// Firing outranks enabled, because it is the one state that asks for something.
func alertStatus(alert map[string]interface{}) string {
	if enabled, _ := alert["enabled"].(bool); !enabled {
		if by := dataString(alert, "paused_by"); by != "" {
			return output.StatusBadge("paused") + " by " + by
		}
		return output.StatusBadge("paused")
	}
	watching, _ := alert["watching"].([]interface{})
	for _, entry := range watching {
		if state, ok := entry.(map[string]interface{}); ok && dataString(state, "state") == "firing" {
			return output.StatusBadge("firing")
		}
	}
	return output.StatusBadge("enabled")
}

func recipientList(alert map[string]interface{}) string {
	raw, _ := alert["recipients"].([]interface{})
	addresses := make([]string, 0, len(raw))
	for _, entry := range raw {
		if recipient, ok := entry.(map[string]interface{}); ok {
			addresses = append(addresses, dataString(recipient, "email"))
		}
	}
	return joinedOrNone(addresses)
}

// The name is the one the channel had when the alert was saved; the id is what a rule file takes.
func channelList(alert map[string]interface{}) string {
	raw, _ := alert["slack_channels"].([]interface{})
	channels := make([]string, 0, len(raw))
	for _, entry := range raw {
		if channel, ok := entry.(map[string]interface{}); ok {
			channels = append(channels, fmt.Sprintf("#%s (%s)", dataString(channel, "name"), dataString(channel, "id")))
		}
	}
	return joinedOrNone(channels)
}

func joinedOrNone(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}

// An empty instance key means the source as a whole.
func eventScope(event map[string]interface{}) string {
	if key := dataString(event, "instance_key"); key != "" {
		return key
	}
	return "whole source"
}

func dataAmount(data map[string]interface{}, key string) string {
	switch value := data[key].(type) {
	case float64:
		return fmt.Sprintf("%.2f", value)
	case string:
		return value
	}
	return ""
}

func yesNo(data map[string]interface{}, key string) string {
	if flag, _ := data[key].(bool); flag {
		return wordYes
	}
	return "no"
}

func compactJSON(value interface{}) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func init() {
	alertsEventsCmd.Flags().IntVar(&flagAlertsLimit, "limit", 50, "Events per page, 1 to 200")
	alertsEventsCmd.Flags().IntVar(&flagAlertsOffset, "offset", 0, "Events to skip")

	alertsCmd.AddCommand(alertsListCmd)
	alertsCmd.AddCommand(alertsGetCmd)
	alertsCmd.AddCommand(alertsSourcesCmd)
	alertsCmd.AddCommand(alertsEventsCmd)
}
