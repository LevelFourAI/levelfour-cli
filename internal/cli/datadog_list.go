package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/LevelFourAI/levelfour-go/datadog"
	"github.com/spf13/cobra"
)

const (
	// A host with no team tag holds no value, which "not measured" would misread.
	datadogNoValue = "-"
	fieldEstimated = "estimated_monthly_usd"
	titleEstimated = "Est./mo"
	fieldName      = "name"
	titleName      = "Name"
	fieldMonthly   = "monthly_events"
	titleMonthly   = "Events/mo"
	fieldLimit     = "daily_limit"
	titleLimit     = "Daily limit"
	fieldService   = "service"
	fieldStatus    = "status"
	flagPage       = "page"
	flagPageSize   = "page-size"
	flagSearch     = "search"
	flagSortBy     = "sort-by"
	flagSortOrder  = "sort-order"
)

// datadogColumn is one column of a table, named by the field the API returns.
type datadogColumn struct {
	title string
	field string
	cell  func(any) string
}

// datadogKind is one list. Its sorts and filters are what the SDK request takes,
// and a kind the SDK can read one item of also has a view command.
type datadogKind struct {
	name        string
	short       string
	list        func(context.Context, *datadog.Client, datadogQuery) (any, error)
	view        func(context.Context, *datadog.Client, string) (any, error)
	sorts       []string
	filters     []string
	boolFilters []string
	intFilters  []string
	columns     []datadogColumn
}

// datadogListPage is one page of any list. Its rows stay maps because each list
// holds different fields and each table names the ones it shows.
type datadogListPage struct {
	Items    []map[string]any `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Partial  bool             `json:"partial"`
	Reason   *string          `json:"reason"`
}

var datadogKinds = []datadogKind{
	{
		name: "hosts", short: "Hosts Datadog lists as live",
		list:    listDatadogHosts,
		view:    viewDatadogHost,
		sorts:   []string{fieldName, fieldEstimated, "last_reported_at"},
		filters: []string{"cloud", "app", "team"},
		columns: []datadogColumn{
			{titleName, fieldName, datadogText}, {"Cloud", "cloud", datadogText}, {"Team", "team", datadogText},
			{"Up", "up", datadogText}, {"Last reported", "last_reported_at", datadogText},
			{titleEstimated, fieldEstimated, datadogMoney},
		},
	},
	{
		name: "metrics", short: "Metrics, what queries them and what they cost",
		list:        listDatadogMetrics,
		view:        viewDatadogMetric,
		sorts:       []string{fieldName, "avg_metric_hour", fieldEstimated, "asset_count"},
		filters:     []string{"metric_type"},
		boolFilters: []string{"queried", "configured"},
		columns: []datadogColumn{
			{titleName, fieldName, datadogText}, {"Type", "metric_type", datadogText},
			{"Series/hour", "avg_metric_hour", datadogText}, {"Queried", "queried", datadogText},
			{"Assets", "asset_count", datadogText}, {titleEstimated, fieldEstimated, datadogMoney},
		},
	},
	{
		name: "log-indexes", short: "Log indexes, their retention and volume",
		list:       listDatadogLogIndexes,
		view:       viewDatadogLogIndex,
		sorts:      []string{fieldName, fieldMonthly, fieldEstimated},
		intFilters: []string{"retention_days"},
		columns: []datadogColumn{
			{titleName, fieldName, datadogText}, {"Retention days", "retention_days", datadogText},
			{titleLimit, fieldLimit, datadogText}, {titleMonthly, fieldMonthly, datadogText},
			{titleEstimated, fieldEstimated, datadogMoney},
		},
	},
	{
		name: "log-services", short: "Log volume by service and index",
		list:    listDatadogLogServices,
		view:    viewDatadogLogService,
		sorts:   []string{fieldService, fieldMonthly, fieldEstimated},
		filters: []string{"index"},
		columns: []datadogColumn{
			{columnService, fieldService, datadogText}, {"Index", "index", datadogText},
			{titleMonthly, fieldMonthly, datadogText}, {"Share of index", "share_of_index", datadogShare},
			{titleEstimated, fieldEstimated, datadogMoney},
		},
	},
	{
		name: "apm-services", short: "APM services and their spans",
		list:    listDatadogAPMServices,
		view:    viewDatadogAPMService,
		sorts:   []string{fieldService, "monthly_spans", fieldEstimated},
		filters: []string{"env"},
		columns: []datadogColumn{
			{columnService, fieldService, datadogText}, {"Env", "env", datadogText},
			{"Spans/mo", "monthly_spans", datadogText}, {"Indexed spans", "indexed_spans", datadogText},
			{titleEstimated, fieldEstimated, datadogMoney},
		},
	},
	{
		name: "synthetics", short: "Synthetic tests and how often they run",
		list:    listDatadogSynthetics,
		view:    viewDatadogSynthetic,
		sorts:   []string{fieldName, "monthly_runs", fieldEstimated},
		filters: []string{"test_type", fieldStatus},
		columns: []datadogColumn{
			{titleName, fieldName, datadogText}, {"Type", "test_type", datadogText},
			{"Status", fieldStatus, datadogText}, {"Every (s)", "tick_every", datadogText},
			{"Locations", "location_count", datadogText}, {"Runs/mo", "monthly_runs", datadogText},
			{titleEstimated, fieldEstimated, datadogMoney},
		},
	},
	{
		name: "rum", short: "RUM applications and their sessions",
		list:    listDatadogRUM,
		view:    viewDatadogRUM,
		sorts:   []string{fieldName, "monthly_sessions", fieldEstimated},
		filters: []string{"application_type"},
		columns: []datadogColumn{
			{titleName, fieldName, datadogText}, {"Type", "application_type", datadogText},
			{"Sessions/mo", "monthly_sessions", datadogText}, {titleEstimated, fieldEstimated, datadogMoney},
		},
	},
	{
		name: "log-quotas", short: "Log indexes against their daily quota",
		list:        listDatadogLogQuotas,
		sorts:       []string{fieldName, fieldMonthly, "quota_used_share"},
		boolFilters: []string{"has_quota"},
		columns: []datadogColumn{
			{titleName, fieldName, datadogText}, {titleLimit, fieldLimit, datadogText},
			{"Events/day", "daily_events", datadogText}, {"Quota used", "quota_used_share", datadogShare},
			{titleMonthly, fieldMonthly, datadogText}, {titleEstimated, fieldEstimated, datadogMoney},
		},
	},
	{
		name: "metric-cardinality", short: "Metrics whose series grew",
		list:        listDatadogMetricCardinality,
		sorts:       []string{fieldName, "series_added", "growth_pct"},
		boolFilters: []string{"growing"},
		columns: []datadogColumn{
			{titleName, fieldName, datadogText}, {"Series before", "series_before", datadogText},
			{"Series now", "series_now", datadogText}, {"Added", "series_added", datadogText},
			{"Growth", "growth_pct", datadogPct}, {titleEstimated, fieldEstimated, datadogMoney},
		},
	},
	{
		name: "changes", short: "Who changed what, from Datadog's audit trail",
		list:    listDatadogChanges,
		sorts:   []string{"at", "actor"},
		filters: []string{"actor", "action", "asset_type"},
		columns: []datadogColumn{
			{"At", "at", datadogText}, {"Who", "actor", datadogText}, {"Action", "action", datadogText},
			{"Type", "asset_type", datadogText}, {"Asset", "asset_name", datadogText},
		},
	},
	{
		name: "team-coverage", short: "Spend with no team, product by product",
		list:  listDatadogTeamCoverage,
		sorts: []string{"product", "no_team_usd", "total_usd"},
		columns: []datadogColumn{
			{columnProduct, "product", datadogText}, {"Total", "total_usd", datadogMoney},
			{"No team", "no_team_usd", datadogMoney}, {"No team share", "no_team_share", datadogShare},
		},
	},
	{
		name: "budgets", short: "Budgets against the spend they watch",
		list:    listDatadogBudgets,
		sorts:   []string{fieldName, "spend_usd", "used_share"},
		filters: []string{"scope"},
		columns: []datadogColumn{
			{titleName, fieldName, datadogText}, {"Scope", "scope", datadogText}, {columnMonth, "period", datadogText},
			{"Spend", "spend_usd", datadogMoney}, {"Budget", "budget_usd", datadogMoney},
			{"Used", "used_share", datadogShare}, {"Alert", "alert_name", datadogText},
		},
	},
	{
		name: "aws-resources", short: "Datadog's fee beside the AWS resource",
		list:  listDatadogAWSResources,
		sorts: []string{"resource_id", "aws_monthly_usd", "datadog_estimated_monthly_usd"},
		columns: []datadogColumn{
			{"Host", "datadog_host", datadogText}, {"AWS resource", "resource_id", datadogText},
			{columnService, fieldService, datadogText}, {"AWS/mo", "aws_monthly_usd", datadogMoney},
			{"Datadog est./mo", "datadog_estimated_monthly_usd", datadogMoney},
		},
	},
}

var datadogSavingColumns = []datadogColumn{
	{"Saving", "id", datadogText}, {columnSavesMo, "monthly_savings", datadogMoney}, {"Found by", "detector", datadogText},
}

var datadogChangeColumns = []datadogColumn{
	{"At", "at", datadogText}, {"Who", "actor", datadogText}, {"Action", "action", datadogText},
}

func (kind datadogKind) command() *cobra.Command {
	group := &cobra.Command{Use: kind.name, Short: kind.short}
	group.AddCommand(kind.listCommand())
	if kind.view != nil {
		group.AddCommand(kind.viewCommand())
	}
	return group
}

func (kind datadogKind) listCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: kind.short,
		Args:  cobra.NoArgs,
		Example: fmt.Sprintf(`  l4 providers datadog %[1]s list
  l4 providers datadog %[1]s list --sort-by %[2]s --sort-order desc
  l4 providers datadog %[1]s list --page 2 --page-size 50
  l4 providers datadog %[1]s list --jq '.data.facets'`, kind.name, kind.sorts[0]),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return readDatadog(func(dd *datadog.Client) (any, error) {
				return kind.list(context.Background(), dd, newDatadogQuery(cmd))
			}, kind.renderList)
		},
	}
	cmd.Flags().Int(flagPage, 1, "Page number")
	cmd.Flags().Int(flagPageSize, 20, "Items per page (max 200)")
	cmd.Flags().String(flagSearch, "", "Keep the rows with this in their name")
	cmd.Flags().String(flagSortBy, "", "Sort by: "+strings.Join(kind.sorts, ", "))
	cmd.Flags().String(flagSortOrder, "", "Sort order: asc, desc")
	for _, filter := range kind.filters {
		cmd.Flags().StringArray(datadogFlagName(filter), nil, datadogFilterHelp(filter))
	}
	for _, filter := range kind.boolFilters {
		cmd.Flags().BoolSlice(datadogFlagName(filter), nil, datadogFilterHelp(filter))
	}
	for _, filter := range kind.intFilters {
		cmd.Flags().IntSlice(datadogFlagName(filter), nil, datadogFilterHelp(filter))
	}
	return cmd
}

func (kind datadogKind) viewCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "view <id>",
		Short: "One of them in full, with its open savings and its last 30 days of changes",
		Args:  cobra.ExactArgs(1),
		Example: fmt.Sprintf(`  l4 providers datadog %[1]s list --jq '.data.items[0].id'
  l4 providers datadog %[1]s view <id>
  l4 providers datadog %[1]s view <id> --json`, kind.name),
		RunE: func(_ *cobra.Command, args []string) error {
			return readDatadog(func(dd *datadog.Client) (any, error) {
				return kind.view(context.Background(), dd, args[0])
			}, kind.renderItem)
		},
	}
}

// A query parameter is snake_case and a flag is kebab-case, as across this CLI.
func datadogFlagName(filter string) string {
	return strings.ReplaceAll(filter, "_", "-")
}

func datadogFilterHelp(filter string) string {
	return "Filter by " + strings.ReplaceAll(filter, "_", " ") + " (repeatable)"
}

func newDatadogQuery(cmd *cobra.Command) datadogQuery {
	page, _ := cmd.Flags().GetInt(flagPage)
	pageSize, _ := cmd.Flags().GetInt(flagPageSize)
	search, _ := cmd.Flags().GetString(flagSearch)
	sortBy, _ := cmd.Flags().GetString(flagSortBy)
	sortOrder, _ := cmd.Flags().GetString(flagSortOrder)
	query := datadogQuery{page: &page, pageSize: &pageSize, sortBy: sortBy, sortOrder: sortOrder, flags: cmd.Flags()}
	if search != "" {
		query.search = &search
	}
	return query
}

// A table names the fields it shows by the name the API gives them, so it reads a
// typed response through that same JSON.
func datadogDataOf[T any](resp any) T {
	var envelope struct {
		Data T `json:"data"`
	}
	body, _ := json.Marshal(resp)
	_ = json.Unmarshal(body, &envelope)
	return envelope.Data
}

func (kind datadogKind) renderList(resp any) error {
	page := datadogDataOf[datadogListPage](resp)
	if page.Partial {
		output.Warning("Datadog returned only part of this list, so it can be missing rows.")
	}
	if len(page.Items) == 0 {
		output.Info(emptyDatadogList(kind.name, page.Reason))
		return nil
	}
	renderDatadogTable(kind.columns, page.Items)
	if page.PageSize > 0 {
		pages := (page.Total + page.PageSize - 1) / page.PageSize
		output.PaginationFooter(page.Page, pages, page.Total, page.Page < pages)
	}
	return nil
}

func (kind datadogKind) renderItem(resp any) error {
	item := datadogDataOf[map[string]any](resp)
	for _, column := range kind.columns {
		datadogKeyValue(column.title, column.cell(item[column.field]))
	}
	if savings := datadogRowsOf(item["recommendations"]); len(savings) > 0 {
		datadogHeader("Open savings")
		renderDatadogTable(datadogSavingColumns, savings)
	}
	detail, _ := item["detail"].(map[string]any)
	if changes := datadogRowsOf(detail["changes"]); len(changes) > 0 {
		datadogHeader("Changes, last 30 days")
		renderDatadogTable(datadogChangeColumns, changes)
	}
	output.Info("--json shows everything else this item holds.")
	return nil
}

func renderDatadogTable(columns []datadogColumn, items []map[string]any) {
	headers := make([]string, 0, len(columns))
	for _, column := range columns {
		headers = append(headers, column.title)
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := make([]string, 0, len(columns))
		for _, column := range columns {
			row = append(row, column.cell(item[column.field]))
		}
		rows = append(rows, row)
	}
	datadogTable(headers, rows)
}

// A row inside an item arrives as a generic JSON list, and each table reads it
// the way a list page's rows are read.
func datadogRowsOf(value any) []map[string]any {
	entries, _ := value.([]any)
	rows := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		row, _ := entry.(map[string]any)
		rows = append(rows, row)
	}
	return rows
}

func emptyDatadogList(kind string, reason *string) string {
	if reason != nil {
		return *reason
	}
	return "Nothing in " + kind + " matches."
}

func datadogText(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return unitsValue(typed)
	case bool:
		if typed {
			return wordYes
		}
		return "no"
	}
	return datadogNoValue
}

func datadogMoney(value any) string {
	if amount, ok := value.(float64); ok {
		return moneyValue(amount)
	}
	return notMeasured
}

func datadogPct(value any) string {
	if pct, ok := value.(float64); ok {
		return pctValue(pct)
	}
	return notMeasured
}

// The API states a share as a fraction of 1.
func datadogShare(value any) string {
	if share, ok := value.(float64); ok {
		return pctValue(share * 100)
	}
	return notMeasured
}

func init() {
	for _, kind := range datadogKinds {
		datadogCmd.AddCommand(kind.command())
	}
}
