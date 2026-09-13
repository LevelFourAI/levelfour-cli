package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

const costAlertsPath = "/api/v1/cost-alerts"

var (
	flagAlertsLimit  int
	flagAlertsOffset int
)

var costAlertsCmd = &cobra.Command{
	Use:     "costalerts",
	Aliases: []string{"alerts", "alert"},
	Short:   "Manage cost alerts",
	Long: `Manage cost alerts.

An alert watches one source, an account or a saved cost view, and notifies the
recipients you name on it. Writes need a read-write API key.`,
}

var costAlertsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cost alerts",
	Example: `  l4 costalerts list
  l4 costalerts list --json`,
	RunE: func(_ *cobra.Command, _ []string) error {
		envelope, err := readEnvelope(costAlertsPath)
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

var costAlertsGetCmd = &cobra.Command{
	Use:     "get <id>",
	Short:   "Read one cost alert",
	Args:    cobra.ExactArgs(1),
	Example: `  l4 costalerts get 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234`,
	RunE: func(_ *cobra.Command, args []string) error {
		envelope, err := readEnvelope(costAlertsPath + "/" + url.PathEscape(args[0]))
		if err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(envelope)
		}

		alert := envelopeData(envelope)
		output.Header(dataString(alert, "name"))
		output.KeyValue("ID", dataString(alert, "id"))
		output.KeyValue("Source", dataString(alert, "source_id"))
		output.KeyValue("Watches", alertKind(alert))
		output.KeyValue("Status", alertStatus(alert))
		output.KeyValue("Recipients", recipientList(alert))
		if channel := dataString(alert, "slack_channel"); channel != "" {
			output.KeyValue("Slack channel", channel)
		}
		output.KeyValue("Rule", compactJSON(alert["params"]))
		return nil
	},
}

var costAlertsSourcesCmd = &cobra.Command{
	Use:   "sources",
	Short: "List what an alert can watch",
	Long: `List what an alert can watch.

Every id here is valid in the source_id field of a rule file.`,
	Example: `  l4 costalerts sources
  l4 costalerts sources --json`,
	RunE: func(_ *cobra.Command, _ []string) error {
		envelope, err := readEnvelope(costAlertsPath + "/sources")
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

var costAlertsEventsCmd = &cobra.Command{
	Use:   "events <id>",
	Short: "What an alert has said, newest first",
	Args:  cobra.ExactArgs(1),
	Example: `  l4 costalerts events 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234
  l4 costalerts events 8f2b1c4e-0000-4a1b-9c3d-5e6f70819234 --limit 10 --offset 10`,
	RunE: func(_ *cobra.Command, args []string) error {
		query := api.BuildQueryString(map[string]string{
			"limit":  strconv.Itoa(flagAlertsLimit),
			"offset": strconv.Itoa(flagAlertsOffset),
		})
		envelope, err := readEnvelope(costAlertsPath + "/" + url.PathEscape(args[0]) + "/events" + query)
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

func alertKind(alert map[string]interface{}) string {
	params, _ := alert["params"].(map[string]interface{})
	return dataString(params, "kind")
}

func alertStatus(alert map[string]interface{}) string {
	if enabled, _ := alert["enabled"].(bool); enabled {
		return output.StatusBadge("enabled")
	}
	if by := dataString(alert, "paused_by"); by != "" {
		return output.StatusBadge("paused") + " by " + by
	}
	return output.StatusBadge("paused")
}

func recipientList(alert map[string]interface{}) string {
	raw, _ := alert["recipients"].([]interface{})
	addresses := make([]string, 0, len(raw))
	for _, entry := range raw {
		if recipient, ok := entry.(map[string]interface{}); ok {
			addresses = append(addresses, dataString(recipient, "email"))
		}
	}
	if len(addresses) == 0 {
		return "none"
	}
	return strings.Join(addresses, ", ")
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
	costAlertsEventsCmd.Flags().IntVar(&flagAlertsLimit, "limit", 50, "Events per page, 1 to 200")
	costAlertsEventsCmd.Flags().IntVar(&flagAlertsOffset, "offset", 0, "Events to skip")

	costAlertsCmd.AddCommand(costAlertsListCmd)
	costAlertsCmd.AddCommand(costAlertsGetCmd)
	costAlertsCmd.AddCommand(costAlertsSourcesCmd)
	costAlertsCmd.AddCommand(costAlertsEventsCmd)
}
