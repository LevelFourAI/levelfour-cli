package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
	"github.com/LevelFourAI/levelfour-go/option"
	"github.com/spf13/cobra"
)

const (
	formatCSV  = "csv"
	formatJSON = "json"
)

var (
	flagExportFormat  string
	flagExportPeriod  string
	flagExportAccount string
	flagExportOut     string
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export data in bulk formats (CSV, JSON)",
}

var exportCostsCmd = &cobra.Command{
	Use:   "costs",
	Short: "Export cost data",
	Example: `- Export 90 days of costs as CSV

  $ l4 export costs --format csv --period 90d > costs.csv

- Export as JSON to a file

  $ l4 export costs --format json --period 30d --out costs.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		params := make(map[string]string)
		if flagExportPeriod != "" {
			params["period"] = flagExportPeriod
		}
		if flagExportAccount != "" {
			params["account_id"] = flagExportAccount
		}

		if flagExportFormat == formatCSV {
			params["format"] = formatCSV
			path := "/api/v1/costs/breakdown" + api.BuildQueryString(params)
			raw, rawErr := client.Raw().DoRaw("GET", path, nil)
			if rawErr != nil {
				return rawErr
			}
			if raw.StatusCode >= 400 {
				return fmt.Errorf("API error (%d): %s", raw.StatusCode, string(raw.Body))
			}
			return writeOutput(raw.Body)
		}

		path := "/api/v1/costs/breakdown" + api.BuildQueryString(params)
		raw, rawErr := client.Raw().DoRaw("GET", path, nil)
		if rawErr != nil {
			return rawErr
		}
		if raw.StatusCode >= 400 {
			return raw.DecodeError()
		}
		data, _ := json.MarshalIndent(json.RawMessage(raw.Body), "", "  ")
		return writeOutput(data)
	},
}

var exportRecommendationsCmd = &cobra.Command{
	Use:   "recommendations",
	Short: "Export recommendations",
	Example: `- Export recommendations as CSV

  $ l4 export recommendations --format csv > recommendations.csv`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		allItems, err := fetchAllRecommendations(client, flagExportAccount)
		if err != nil {
			return err
		}

		if flagExportFormat == formatCSV {
			headers := []string{"ID", "Service", "Environment", "Monthly Savings", "Annual Savings", "Savings %"}
			var rows [][]string
			for _, rec := range allItems {
				env := ""
				if rec.GetEnvironment() != nil {
					env = *rec.GetEnvironment()
				}
				rows = append(rows, []string{
					rec.GetRecommendationID(),
					rec.GetService(),
					env,
					fmt.Sprintf("%.2f", rec.GetMonthlySavings()),
					fmt.Sprintf("%.2f", rec.GetAnnualSavings()),
					fmt.Sprintf("%.1f%%", rec.GetSavingsPercentage()),
				})
			}

			var buf []byte
			buf = appendCSV(buf, headers, rows)
			return writeOutput(buf)
		}

		envelope := map[string]interface{}{
			"data": map[string]interface{}{
				"items": allItems,
			},
		}
		data, _ := json.MarshalIndent(envelope, "", "  ")
		return writeOutput(data)
	},
}

func fetchAllRecommendations(client *api.SDKClient, accountID string) ([]*levelfourgo.RecommendationItem, error) {
	const pageSize = 100
	var allItems []*levelfourgo.RecommendationItem
	page := 1

	for {
		var opts []option.RequestOption
		if accountID != "" {
			opts = append(opts, option.WithQueryParameters(url.Values{"account_id": {accountID}}))
		}
		resp, err := client.SDK().Recommendations.List(context.Background(), &levelfourgo.ListRecommendationsRequest{
			Page:     api.IntPtr(page),
			PageSize: api.IntPtr(pageSize),
		}, opts...)
		if err != nil {
			return nil, err
		}

		allItems = append(allItems, resp.Results...)

		if len(resp.Results) < pageSize {
			break
		}
		page++
	}

	return allItems, nil
}

var exportCommitmentsCmd = &cobra.Command{
	Use:   "commitments",
	Short: "Export the commitment ledger",
	Example: `- Export every commitment as CSV

  $ l4 export commitments --format csv > commitments.csv

- Export Google Cloud commitments as JSON to a file

  $ l4 export commitments --provider gcp --format json --out commitments.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, provider, err := commitmentClient()
		if err != nil {
			return err
		}
		return exportPortfolio(client, provider)
	},
}

func exportPortfolio(client *api.SDKClient, provider string) error {
	portfolio, body, err := fetchPortfolio(client, provider)
	if err != nil {
		return handleCommitmentsError(err)
	}
	if flagExportFormat != formatCSV {
		return writeIndentedJSON(body)
	}

	headers := []string{
		"id", "service", "kind", "holder_account_id", "holder_account_name", "end_at",
		"utilization_pct", "coverage_pct", "protects_monthly", "rightsizing_monthly", "status",
	}
	rows := make([][]string, 0, len(portfolio.Rows))
	for _, row := range portfolio.Rows {
		rows = append(rows, []string{
			row.ID,
			row.Service,
			row.Kind,
			row.HolderAccountID,
			row.HolderAccountName,
			csvText(row.EndAt),
			csvFloat(row.UtilizationPct),
			csvCoverage(row.Kind, row.CoveragePct),
			csvOptionalFloat(row.ProtectsMonthly),
			csvOptionalFloat(row.RightsizingMonthly),
			row.Status,
		})
	}
	return writeOutput(appendCSV(nil, headers, rows))
}

// writeIndentedJSON re-indents a payload the API already parsed on the way in,
// so the marshal cannot fail, which is why the error is dropped here the way
// output.PrintJSON drops its own.
func writeIndentedJSON(body []byte) error {
	data, _ := json.MarshalIndent(json.RawMessage(body), "", "  ")
	return writeOutput(data)
}

// csvCoverage leaves a Savings Plan's coverage cell empty, which is what a
// spreadsheet reads as no value. A zero there would claim the plan covers none
// of the eligible usage, and nothing measures what one plan covers.
func csvCoverage(kind string, pct float64) string {
	if kind == kindSavingsPlan {
		return ""
	}
	return csvFloat(pct)
}

func csvFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func csvOptionalFloat(v *float64) string {
	if v == nil {
		return ""
	}
	return csvFloat(*v)
}

func csvText(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func writeOutput(data []byte) error {
	if flagExportOut != "" {
		return os.WriteFile(flagExportOut, data, 0o600)
	}
	_, err := fmt.Fprint(output.Stdout, string(data))
	return err
}

func appendCSV(buf []byte, headers []string, rows [][]string) []byte {
	line := ""
	for i, h := range headers {
		if i > 0 {
			line += ","
		}
		line += csvEscape(h)
	}
	buf = append(buf, []byte(line+"\n")...)

	for _, row := range rows {
		line = ""
		for i, cell := range row {
			if i > 0 {
				line += ","
			}
			line += csvEscape(cell)
		}
		buf = append(buf, []byte(line+"\n")...)
	}
	return buf
}

func csvEscape(s string) string {
	needsQuote := false
	for _, c := range s {
		if c == ',' || c == '"' || c == '\n' {
			needsQuote = true
			break
		}
	}
	if !needsQuote {
		return s
	}
	escaped := "\""
	for _, c := range s {
		if c == '"' {
			escaped += "\"\""
		} else {
			escaped += string(c)
		}
	}
	return escaped + "\""
}

func init() {
	for _, cmd := range []*cobra.Command{exportCostsCmd, exportRecommendationsCmd, exportCommitmentsCmd} {
		cmd.Flags().StringVar(&flagExportFormat, "format", "csv", "Output format: csv, json")
		cmd.Flags().StringVar(&flagExportOut, "out", "", "Write to file instead of stdout")
	}
	for _, cmd := range []*cobra.Command{exportCostsCmd, exportRecommendationsCmd} {
		cmd.Flags().StringVar(&flagExportAccount, "account", "", "Filter by account")
	}
	exportCostsCmd.Flags().StringVar(&flagExportPeriod, "period", "", "Time period: 7d, 30d, 90d, 365d")
	exportCommitmentsCmd.Flags().StringVar(&flagCommitmentsProvider, "provider", "",
		"Provider ID (aws, gcp); auto-detected if omitted")

	exportCmd.AddCommand(exportCostsCmd)
	exportCmd.AddCommand(exportRecommendationsCmd)
	exportCmd.AddCommand(exportCommitmentsCmd)
}
