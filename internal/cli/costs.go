package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
	"github.com/spf13/cobra"
)

var costsCmd = &cobra.Command{
	Use:   "costs",
	Short: "View cloud spending data",
}

var (
	flagBreakdownAccount     []string
	flagBreakdownStart       string
	flagBreakdownEnd         string
	flagBreakdownPreset      string
	flagBreakdownGranularity string
	flagBreakdownGroupBy     []string
	flagBreakdownSortBy      string
	flagBreakdownSortByDate  string
	flagBreakdownSortOrder   string
	flagBreakdownService     []string
	flagBreakdownEnvironment []string
	flagBreakdownRegion      []string
	flagBreakdownTagKey      []string
	flagBreakdownTagValue    []string
	flagBreakdownProvider    string
	flagBreakdownFormat      string
	flagBreakdownTUI         bool
)

var costsBreakdownCmd = &cobra.Command{
	Use:   "breakdown",
	Short: "Per-service cost breakdown with filters, grouping, and pagination",
	Example: `  l4 costs breakdown
  l4 costs breakdown --preset 30D
  l4 costs breakdown --start 2026-01-01 --end 2026-01-31
  l4 costs breakdown --group-by service --group-by region
  l4 costs breakdown --service EC2 --service RDS
  l4 costs breakdown --group-by tag --tag-key Environment --tag-value production
  l4 costs breakdown --granularity monthly --preset 6M
  l4 costs breakdown --sort-by-date 2026-01-15 --sort-order desc
  l4 costs breakdown --format csv > costs.csv`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			return openWeb("/spending")
		}

		page, _ := cmd.Flags().GetInt("page")
		pageSize, _ := cmd.Flags().GetInt("page-size")

		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		ctx := context.Background()
		providerID, err := resolveProviderID(ctx, client, flagBreakdownProvider)
		if err != nil {
			return err
		}

		state := costsFilterState{
			start:       flagBreakdownStart,
			end:         flagBreakdownEnd,
			preset:      flagBreakdownPreset,
			granularity: flagBreakdownGranularity,
			groupBy:     flagBreakdownGroupBy,
			service:     flagBreakdownService,
			environment: flagBreakdownEnvironment,
			account:     flagBreakdownAccount,
			region:      flagBreakdownRegion,
			tagKey:      flagBreakdownTagKey,
			tagValue:    flagBreakdownTagValue,
			sortBy:      flagBreakdownSortBy,
			sortByDate:  flagBreakdownSortByDate,
			sortOrder:   flagBreakdownSortOrder,
			page:        page,
			pageSize:    pageSize,
			providerID:  providerID,
			format:      flagBreakdownFormat,
		}

		if err := state.validate(); err != nil {
			return err
		}

		if flagBreakdownFormat == formatCSV || flagBreakdownFormat == "raw" {
			return runCostsBreakdownRaw(client, providerID, state, flagBreakdownFormat)
		}

		req := buildCostsListRequest(state)
		req.Format = api.StringPtr("table")

		var resp *levelfourgo.ListByProviderCostsResponse

		err = tuicommon.RunWithSpinner("Loading cost breakdown...", output.L4SpinnerTheme(), func(_ context.Context) error {
			var fetchErr error
			resp, fetchErr = client.SDK().Costs.ListByProvider(ctx, providerID, req)
			return fetchErr
		})
		if err != nil {
			return err
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(resp)
		}

		tableResp := resp.GetProviderServiceBreakdownResponse()
		if tableResp == nil || tableResp.GetData() == nil {
			output.Info("No cost breakdown data found.")
			return nil
		}

		data := tableResp.GetData()
		items := data.GetItems()

		if flagBreakdownTUI && isTerminal() {
			return runCostsTUI(client, providerID, data, items, data.GetPagination(), pageSize, state)
		}

		renderCostsBreakdownKPIs(data)

		if len(items) == 0 {
			output.Info("No cost breakdown data found.")
			return nil
		}

		headers, rows := buildCostsBreakdownRows(items, terminalWidth(), state.groupBy)
		output.Table(headers, rows)

		if pg := data.GetPagination(); pg != nil {
			output.PaginationFooter(
				pg.GetCurrentPage(),
				pg.GetTotalPages(),
				pg.GetTotalItems(),
				pg.GetHasNext(),
			)
		}

		return nil
	},
}

func runCostsBreakdownRaw(client *api.SDKClient, providerID string, state costsFilterState, format string) error {
	params := buildCostsRawParams(providerID, state, format)
	path := fmt.Sprintf("/api/v1/providers/%s/costs/breakdown%s", providerID, api.BuildQueryStringMulti(params))
	raw, rawErr := client.Raw().DoRaw("GET", path, nil)
	if rawErr != nil {
		return rawErr
	}
	if raw.StatusCode >= 400 {
		return fmt.Errorf("API error (%d): %s", raw.StatusCode, strings.TrimSpace(string(raw.Body)))
	}
	output.PrintRaw(string(raw.Body))
	return nil
}

func renderCostsBreakdownKPIs(data *levelfourgo.ProviderServiceBreakdownData) {
	if data == nil {
		return
	}
	cards := []output.KPICard{
		{Label: "Period Total", Value: fmt.Sprintf("$%.2f", data.GetTotalPeriodCost())},
	}
	if rng := formatRangeLabel(data.GetStartDate(), data.GetEndDate()); rng != "" {
		cards = append(cards, output.KPICard{Label: "Range", Value: rng})
	}
	if n := countItems(data); n > 0 {
		cards = append(cards, output.KPICard{Label: "Rows", Value: fmt.Sprintf("%d", n)})
	}
	if prov := data.GetProviderName(); prov != "" {
		cards = append(cards, output.KPICard{Label: "Provider", Value: prov})
	}
	output.KPICards(cards)
}

func countItems(data *levelfourgo.ProviderServiceBreakdownData) int {
	if pg := data.GetPagination(); pg != nil {
		return pg.GetTotalItems()
	}
	return len(data.GetItems())
}

func init() {
	costsBreakdownCmd.Flags().Int("page", 1, "Page number")
	costsBreakdownCmd.Flags().Int("page-size", 20, "Items per page (max 100)")
	costsBreakdownCmd.Flags().StringVar(&flagBreakdownProvider, "provider", "", "Provider ID (aws, gcp, azure, k8s); auto-detected if omitted")
	costsBreakdownCmd.Flags().StringVar(&flagBreakdownStart, "start", "", "Start date (ISO 8601, e.g. 2026-01-01)")
	costsBreakdownCmd.Flags().StringVar(&flagBreakdownEnd, "end", "", "End date (ISO 8601, e.g. 2026-01-31)")
	costsBreakdownCmd.Flags().StringVar(&flagBreakdownPreset, "preset", "", "Date preset: 30D, 6M, 12M (overrides --start/--end)")
	costsBreakdownCmd.Flags().StringVar(&flagBreakdownGranularity, "granularity", "", "Granularity: daily, monthly")
	costsBreakdownCmd.Flags().StringArrayVar(&flagBreakdownGroupBy, "group-by", nil, "Group by: service, account_id, region, tag (repeatable)")
	costsBreakdownCmd.Flags().StringArrayVar(&flagBreakdownService, "service", nil, "Filter by service (repeatable)")
	costsBreakdownCmd.Flags().StringArrayVar(&flagBreakdownEnvironment, "environment", nil, "Filter by environment (repeatable)")
	costsBreakdownCmd.Flags().StringArrayVar(&flagBreakdownAccount, "account", nil, "Filter by account ID (repeatable)")
	costsBreakdownCmd.Flags().StringArrayVar(&flagBreakdownRegion, "region", nil, "Filter by region (repeatable)")
	costsBreakdownCmd.Flags().StringArrayVar(&flagBreakdownTagKey, "tag-key", nil, "Filter by tag key; required when --group-by includes tag (repeatable)")
	costsBreakdownCmd.Flags().StringArrayVar(&flagBreakdownTagValue, "tag-value", nil, "Filter by tag value (repeatable)")
	costsBreakdownCmd.Flags().StringVar(&flagBreakdownSortBy, "sort-by", "", "Sort field: cost, previous_cost, change_percentage, service, region, account_id")
	costsBreakdownCmd.Flags().StringVar(&flagBreakdownSortByDate, "sort-by-date", "", "Sort by a specific date column (YYYY-MM-DD daily, YYYY-MM monthly); overrides --sort-by")
	costsBreakdownCmd.Flags().StringVar(&flagBreakdownSortOrder, "sort-order", "", "Sort order: asc, desc")
	costsBreakdownCmd.Flags().StringVar(&flagBreakdownFormat, "format", "table", "Output format: table, csv, raw")
	costsBreakdownCmd.Flags().BoolVar(&flagBreakdownTUI, "tui", false, "Use interactive split-pane TUI")

	costsCmd.AddCommand(costsBreakdownCmd)
}
