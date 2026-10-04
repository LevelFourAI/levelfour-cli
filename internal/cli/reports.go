package cli

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	reportsPath = "/api/v1/reports"

	kindReport = "report"

	defaultReportPage     = 1
	defaultReportPageSize = 50
	maxReportPageSize     = 100

	reportNotRunnable = "This report was saved without the requests that run it. Open and save it again in the dashboard."
)

var (
	flagReportsScope   string
	flagReportStart    string
	flagReportEnd      string
	flagReportPreset   string
	flagReportPage     int
	flagReportPageSize int
)

var reportsCmd = &cobra.Command{
	Use:   "reports",
	Short: "Saved reports: list them, and run one for its numbers",
	Long: `Read the reports your organization saved from the dashboard, and run one.

A saved report is a question about spend: what to group by, which filters to
apply, and whether it measures cost or usage. Running it answers that question
for each provider the report covers, over the window you ask for.

A <report> argument takes a report id or a report name, matched whatever its case.`,
}

var reportsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List the organization's saved reports",
	Args:  cobra.NoArgs,
	Example: `  l4 reports list
  l4 reports list --scope aws`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runReportsList()
	},
}

var reportsGetCmd = &cobra.Command{
	Use:   "get <report>",
	Short: "Show what a saved report asks",
	Args:  cobra.ExactArgs(1),
	Example: `  l4 reports get "EC2 by region"
  l4 reports get 0b9d7c1e-5a3f-4e2b-8c6d-1f0a2b3c4d5e --json`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runReportsGet(args[0])
	},
}

var reportsRunCmd = &cobra.Command{
	Use:   "run <report>",
	Short: "Run a saved report and print its numbers",
	Long: `Run a saved report and print its numbers, one table per provider it covers.

The window is the month so far unless --start and --end, or --preset, set
another. The period a report was last viewed with in the dashboard is not part
of what it saves.`,
	Args: cobra.ExactArgs(1),
	Example: `  l4 reports run "EC2 by region"
  l4 reports run "EC2 by region" --start 2026-08-01 --end 2026-08-31
  l4 reports run "EC2 by region" --preset 30D --page-size 100
  l4 reports run "EC2 by region" --jq '.data.providers[].table.total_period_cost'`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runReportsRun(args[0])
	},
}

func listReports(params url.Values) (map[string]interface{}, []savedReport, error) {
	envelope, err := getJSON(withQuery(reportsPath, params))
	if err != nil {
		return nil, nil, err
	}
	var reports []savedReport
	if err := decodeData(envelope, &reports); err != nil {
		return nil, nil, err
	}
	return envelope, reports, nil
}

func reportNames() ([]storedItem, error) {
	_, reports, err := listReports(url.Values{})
	if err != nil {
		return nil, err
	}
	items := make([]storedItem, 0, len(reports))
	for _, r := range reports {
		items = append(items, storedItem{ID: r.ID, Name: r.Name})
	}
	return items, nil
}

func reportPath(ref string) (string, error) {
	id, err := resolveStoredID(kindReport, ref, reportNames)
	if err != nil {
		return "", err
	}
	return reportsPath + "/" + url.PathEscape(id), nil
}

func runReportsList() error {
	params := url.Values{}
	setParam(params, "scope", flagReportsScope)
	envelope, reports, err := listReports(params)
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	if len(reports) == 0 {
		output.Info("No saved reports found. A report is saved from a board in the dashboard.")
		return nil
	}
	rows := make([][]string, 0, len(reports))
	for _, r := range reports {
		rows = append(rows, []string{r.Name, r.ID, r.Scope, orDash(r.CreatedByName), dayOf(r.UpdatedAt)})
	}
	output.Table([]string{columnName, "ID", "Scope", columnCreatedBy, columnUpdated}, rows)
	return nil
}

func runReportsGet(ref string) error {
	path, err := reportPath(ref)
	if err != nil {
		return err
	}
	envelope, err := getJSON(path)
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	var report savedReport
	if err := decodeData(envelope, &report); err != nil {
		return err
	}
	renderSavedReport(report)
	return nil
}

func reportRunParams() (url.Values, error) {
	if flagReportPage < 1 {
		return nil, fmt.Errorf("--page must be 1 or more (got %d)", flagReportPage)
	}
	if flagReportPageSize < 1 || flagReportPageSize > maxReportPageSize {
		return nil, fmt.Errorf("--page-size must be between 1 and %d (got %d)", maxReportPageSize, flagReportPageSize)
	}
	if flagReportPreset != "" {
		if _, ok := validPresets[flagReportPreset]; !ok {
			return nil, fmt.Errorf("--preset must be one of 30D, 6M, 12M (got %q)", flagReportPreset)
		}
	}
	if err := validateDateRange(flagReportStart, flagReportEnd); err != nil {
		return nil, err
	}
	params := url.Values{}
	params.Set("format", "table")
	params.Set("page", strconv.Itoa(flagReportPage))
	params.Set("page_size", strconv.Itoa(flagReportPageSize))
	setParam(params, "start", flagReportStart)
	setParam(params, "end", flagReportEnd)
	setParam(params, "preset", flagReportPreset)
	return params, nil
}

func runReportsRun(ref string) error {
	params, err := reportRunParams()
	if err != nil {
		return err
	}
	path, err := reportPath(ref)
	if err != nil {
		return err
	}
	envelope, err := getJSON(withQuery(path+"/data", params))
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	var run reportRun
	if err := decodeData(envelope, &run); err != nil {
		return err
	}
	if len(run.Providers) == 0 {
		output.Info(reportNotRunnable)
		return nil
	}
	for _, provider := range run.Providers {
		renderProviderRun(provider)
	}
	return nil
}

func init() {
	reportsListCmd.Flags().StringVar(&flagReportsScope, "scope", "",
		"Only reports made on this provider (aws, gcp, ...), or on all of them (all)")

	reportsRunCmd.Flags().StringVar(&flagReportStart, "start", "", "Window start, YYYY-MM-DD. The default window is the month so far")
	reportsRunCmd.Flags().StringVar(&flagReportEnd, "end", "", "Window end, YYYY-MM-DD")
	reportsRunCmd.Flags().StringVar(&flagReportPreset, "preset", "", "Date preset: 30D, 6M, 12M (overrides --start/--end)")
	reportsRunCmd.Flags().IntVar(&flagReportPage, "page", defaultReportPage, "Page of each provider's rows")
	reportsRunCmd.Flags().IntVar(&flagReportPageSize, "page-size", defaultReportPageSize, "Rows per provider per page (max 100)")

	reportsCmd.AddCommand(reportsListCmd)
	reportsCmd.AddCommand(reportsGetCmd)
	reportsCmd.AddCommand(reportsRunCmd)
}
