package cli

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

// An API that predates usage ignores the measure it was sent and answers with costs. Printing
// those under a unit would state dollars as hours, so the answer has to say it measured usage.
var errUsageNotMeasured = errors.New("the API answered with costs: it does not measure usage yet")

func runCostsUsage(client *api.SDKClient, providerID string, state costsFilterState) error {
	body, err := getBreakdown(client, providerID, buildCostsRawParams(providerID, state, "table"))
	if err != nil {
		return err
	}
	data, figures, err := decodeUsageBreakdown(body)
	if err != nil {
		return err
	}
	// Checked first: a script reading the raw answer asked for a quantity too.
	if output.HasFormattingFlags() {
		output.PrintRaw(string(body))
		return nil
	}
	renderBreakdownKPIs(data, figures)
	items := data.GetItems()
	if len(items) == 0 {
		output.Info(fmt.Sprintf("No usage measured in %s in that window. Usage billed in another unit is not converted.", figures.unit))
		return nil
	}
	headers, rows := breakdownRows(usageColumns(figures), wrapCostItems(items), terminalWidth(), state.groupBy)
	output.Table(headers, rows)
	if pg := data.GetPagination(); pg != nil {
		output.PaginationFooter(pg.GetCurrentPage(), pg.GetTotalPages(), pg.GetTotalItems(), pg.GetHasNext())
	}
	return nil
}

func decodeUsageBreakdown(body []byte) (*levelfourgo.ProviderServiceBreakdownData, measured, error) {
	var answer struct {
		Data *levelfourgo.ProviderServiceBreakdownData `json:"data"`
	}
	if err := json.Unmarshal(body, &answer); err != nil || answer.Data == nil {
		return nil, measured{}, errUnexpectedBody(body)
	}
	echoed := answer.Data.GetExtraProperties()
	figures := measured{measure: dataString(echoed, "measure"), unit: dataString(echoed, "unit")}
	if !figures.isUsage() {
		return nil, measured{}, errUsageNotMeasured
	}
	return answer.Data, figures, nil
}

// The breakdown's columns for a usage quantity. Usage has no comparison window, so the two columns
// that hold one are left out.
func usageColumns(figures measured) []costColumn {
	columns := make([]costColumn, 0, len(costColumns))
	for _, column := range costColumns {
		switch column.header {
		case columnChange, columnPrevCost:
		case columnCost:
			columns = append(columns, costColumn{figures.header(), 0, func(item costItem) string {
				return figures.amount(item.GetCost())
			}})
		default:
			columns = append(columns, column)
		}
	}
	return columns
}
