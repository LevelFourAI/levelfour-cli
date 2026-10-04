package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
)

// A row's figure, which is a usage quantity when the report measures usage.
const rowAmountField = "cost"

type reportRequest struct {
	ProviderID string              `json:"provider_id"`
	GroupBy    []string            `json:"group_by"`
	Filters    map[string][]string `json:"filters"`
	Measure    string              `json:"measure"`
	Unit       string              `json:"unit"`
}

type savedReport struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Scope         string `json:"scope"`
	CreatedByName string `json:"created_by_name"`
	UpdatedAt     string `json:"updated_at"`
	Requests      struct {
		Chart []reportRequest `json:"chart"`
		Table []reportRequest `json:"table"`
	} `json:"requests"`
}

// A run replays the table requests. A report saved with a chart alone has only those to show.
func (r savedReport) asked() []reportRequest {
	if len(r.Requests.Table) > 0 {
		return r.Requests.Table
	}
	return r.Requests.Chart
}

func (r reportRequest) measures() string {
	if r.Measure == measureUsage {
		return fmt.Sprintf("%s (%s)", measureUsage, r.Unit)
	}
	return measureCost
}

func (r reportRequest) filtered() string {
	names := make([]string, 0, len(r.Filters))
	for name := range r.Filters {
		names = append(names, name)
	}
	sort.Strings(names)
	return orDash(strings.Join(names, ", "))
}

type reportRun struct {
	Providers []providerRun `json:"providers"`
}

type providerRun struct {
	ProviderID string       `json:"provider_id"`
	Table      *reportTable `json:"table"`
	Error      struct {
		Message string `json:"message"`
	} `json:"error"`
}

type reportTable struct {
	ProviderName    string                   `json:"provider_name"`
	StartDate       string                   `json:"start_date"`
	EndDate         string                   `json:"end_date"`
	TotalPeriodCost *float64                 `json:"total_period_cost"`
	Measure         string                   `json:"measure"`
	Unit            string                   `json:"unit"`
	Items           []map[string]interface{} `json:"items"`
	Pagination      struct {
		CurrentPage int  `json:"current_page"`
		TotalPages  int  `json:"total_pages"`
		TotalItems  int  `json:"total_items"`
		HasNext     bool `json:"has_next"`
	} `json:"pagination"`
}

// The columns a report row may carry, in the order a table reads best. A row holds a value only
// for the dimensions its report groups by.
var reportDimensions = []struct {
	key    string
	header string
}{
	{"service", columnService},
	{"account_id", columnAccount},
	{"account_name", "Account Name"},
	{dimRegion, "Region"},
	{"environment", "Environment"},
	{dimUsageType, columnUsageType},
	{"resource", "Resource"},
	{"tag_key", "Tag Key"},
	{"tag_value", "Tag Value"},
	{"cost_category", "Cost Category"},
	{"purchase_type", "Purchase Type"},
	{"instance_type", "Instance Type"},
	{"charge_type", "Charge Type"},
	{"sku", "SKU"},
	{"project", "Project"},
	{dimVirtualTag, columnVirtualTag},
}

func renderSavedReport(report savedReport) {
	output.KPICards([]output.KPICard{
		{Label: "Report", Value: report.Name},
		{Label: "Scope", Value: report.Scope},
		{Label: columnCreatedBy, Value: orDash(report.CreatedByName)},
		{Label: columnUpdated, Value: dayOf(report.UpdatedAt)},
	})
	asked := report.asked()
	if len(asked) == 0 {
		output.Info(reportNotRunnable)
		return
	}
	rows := make([][]string, 0, len(asked))
	for _, request := range asked {
		rows = append(rows, []string{
			providerLabel(request.ProviderID),
			request.measures(),
			orDash(strings.Join(request.GroupBy, ", ")),
			request.filtered(),
		})
	}
	output.Table([]string{"Provider", "Measures", "Grouped by", "Filtered by"}, rows)
}

func renderProviderRun(run providerRun) {
	if run.Table == nil {
		output.Header(providerLabel(run.ProviderID))
		output.Info(run.Error.Message)
		return
	}
	table := run.Table
	figures := measured{measure: table.Measure, unit: table.Unit}
	output.Header(table.ProviderName)
	output.KPICards([]output.KPICard{
		{Label: "Total", Value: totalSent(table.TotalPeriodCost, figures)},
		{Label: "Range", Value: formatRangeLabel(table.StartDate, table.EndDate)},
		{Label: "Rows", Value: strconv.Itoa(table.Pagination.TotalItems)},
	})
	if len(table.Items) == 0 {
		output.Info("Nothing was billed in that window.")
		return
	}
	headers, rows := reportRows(table.Items, figures)
	output.Table(headers, rows)
	output.PaginationFooter(
		table.Pagination.CurrentPage,
		table.Pagination.TotalPages,
		table.Pagination.TotalItems,
		table.Pagination.HasNext,
	)
}

func reportRows(items []map[string]interface{}, figures measured) ([]string, [][]string) {
	var keys, headers []string
	for _, dimension := range reportDimensions {
		if anyRowNames(items, dimension.key) {
			keys = append(keys, dimension.key)
			headers = append(headers, dimension.header)
		}
	}
	headers = append(headers, figures.header())

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := make([]string, 0, len(headers))
		for _, key := range keys {
			row = append(row, orDash(dataString(item, key)))
		}
		rows = append(rows, append(row, rowAmount(item, figures)))
	}
	return headers, rows
}

func anyRowNames(items []map[string]interface{}, key string) bool {
	for _, item := range items {
		if dataString(item, key) != "" {
			return true
		}
	}
	return false
}

// A row with no figure prints none: a zero there would state a cost the API never sent.
func rowAmount(item map[string]interface{}, figures measured) string {
	amount, sent := item[rowAmountField].(float64)
	if !sent {
		return "-"
	}
	return figures.amount(amount)
}

func totalSent(total *float64, figures measured) string {
	if total == nil {
		return "-"
	}
	return figures.total(*total)
}
