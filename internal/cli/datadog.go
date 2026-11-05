package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
	"github.com/LevelFourAI/levelfour-go/datadog"
	"github.com/spf13/cobra"
)

const (
	datadogAPIPath = "/api/v1/providers/datadog"
	datadogWebPath = "/datadog"
	// The code a 404 carries when the organization has no Datadog connection, which
	// a 404 for one item that does not exist does not.
	providerNotEnabled = "provider_not_enabled"
	columnProduct      = "Product"
	columnMonth        = "Month"
	columnSavesMo      = "Saves/mo"
	recentMonths       = 3
	potentialSaving    = "Savings here are potential: never add them to realized savings."
)

var flagDatadogFormat string

var datadogCmd = &cobra.Command{
	Use:   "datadog",
	Short: "The Datadog bill, the contract and what to ask for at renewal",
	Long: `What LevelFour reads from a Datadog connection: the bill product by product,
the contract, the renewal, and the hosts, metrics, logs and other items behind them.

Start a renewal with 'l4 providers datadog renewal' and 'l4 providers datadog plan'.

Estimated figures are usage at what the bill charged a unit of the product: Datadog
bills products, not items.`,
}

var datadogRenewalCmd = &cobra.Command{
	Use:   "renewal [product]",
	Short: "What to ask for at the renewal, the largest products first",
	Long: `Each product on the bill over the last 12 closed months: what it paid, how the
rate compares with Datadog's list price, and the commitment that costs least
across next year. Name a product to read it in full.`,
	Args: cobra.MaximumNArgs(1),
	Example: `  l4 providers datadog renewal
  l4 providers datadog renewal infra_host
  l4 providers datadog renewal --format csv > datadog-renewal.csv
  l4 providers datadog renewal --jq '.data.products[] | select(.rate_vs_list_pct > 0) | .product'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagDatadogFormat != "" && flagDatadogFormat != formatCSV {
			return fmt.Errorf("--format must be csv (got %q)", flagDatadogFormat)
		}
		product := optionalArg(args)
		if flagDatadogFormat == formatCSV {
			if product != "" {
				return errors.New("--format csv writes the whole table, so it takes no product")
			}
			return printDatadogRenewalCSV()
		}
		return readDatadog(func(dd *datadog.Client) (*levelfourgo.DatadogRenewalResponse, error) {
			return dd.GetRenewal(context.Background(), &levelfourgo.GetRenewalAPIV1ProvidersDatadogRenewalGetRequest{})
		}, func(resp *levelfourgo.DatadogRenewalResponse) error {
			return renderDatadogRenewal(resp.GetData(), product)
		})
	},
}

var datadogPlanCmd = &cobra.Command{
	Use:   "plan",
	Short: "Where the renewal can cost less",
	Long: `Each product of the last 12 months against its median month, the largest saving
first. A product counts when its usage grew past the median, runs above a quantity
a contract states, or is one another connected tool also covers.`,
	Example: `  l4 providers datadog plan
  l4 providers datadog plan --jq '.data.scenarios'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return readDatadog(func(dd *datadog.Client) (*levelfourgo.DatadogRenewalPlanResponse, error) {
			return dd.GetRenewalPlan(context.Background())
		}, renderDatadogPlan)
	},
}

var datadogContractCmd = &cobra.Command{
	Use:   "contract",
	Short: "Each contract line against this month's usage",
	Long: `Each contract line: what it commits to, usage so far this month, where the month
is on course to end, and what that bills on demand past the commitment.`,
	Example: `  l4 providers datadog contract
  l4 providers datadog contract --jq '.data.contracts[].lines[] | select(.projected_overage > 0)'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return readDatadog(func(dd *datadog.Client) (*levelfourgo.DatadogContractResponse, error) {
			return dd.GetContract(context.Background())
		}, renderDatadogContract)
	},
}

var datadogHygieneCmd = &cobra.Command{
	Use:   "hygiene",
	Short: "How much of the bill the open savings could take off",
	Long: `A score from 0 to 100 and one rule per savings detector. Each failing rule takes
off its open monthly savings as a share of the last 30 days of spend. A passed rule
holds no open finding, which does not prove the detector has run.`,
	Example: `  l4 providers datadog hygiene
  l4 providers datadog hygiene --jq '.data.score'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return readDatadog(func(dd *datadog.Client) (*levelfourgo.DatadogHygieneResponse, error) {
			return dd.GetHygiene(context.Background())
		}, renderDatadogHygiene)
	},
}

var datadogMonthsCmd = &cobra.Command{
	Use:   "months [product]",
	Short: "Every product month by month",
	Long: `What each product cost over the last three months, the costliest first. Name a
product to read it over the last twelve months and today's, with its usage. A month's
charges can still move until Datadog closes its bill, by the 16th of the next month.`,
	Args: cobra.MaximumNArgs(1),
	Example: `  l4 providers datadog months
  l4 providers datadog months logs_indexed_7day
  l4 providers datadog months --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return readDatadog(func(dd *datadog.Client) (*levelfourgo.DatadogMonthsResponse, error) {
			return dd.GetMonths(context.Background())
		}, func(resp *levelfourgo.DatadogMonthsResponse) error {
			return renderDatadogMonths(resp.GetData(), optionalArg(args))
		})
	},
}

var datadogAWSCmd = &cobra.Command{
	Use:   "aws-reconciliation",
	Short: "What AWS billed for Datadog against what Datadog reports",
	Example: `  l4 providers datadog aws-reconciliation
  l4 providers datadog aws-reconciliation --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return readDatadog(func(dd *datadog.Client) (*levelfourgo.DatadogAwsReconciliationResponse, error) {
			return dd.GetAwsReconciliation(context.Background())
		}, renderDatadogAWS)
	},
}

func optionalArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func readDatadog[T any](read func(*datadog.Client) (T, error), render func(T) error) error {
	client, err := newSDKClientFn()
	if err != nil {
		return err
	}
	if flagWeb {
		return openWeb(datadogWebPath)
	}
	resp, err := read(client.SDK().Datadog)
	if err != nil {
		return handleDatadogError(err)
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(resp)
	}
	return render(resp)
}

func handleDatadogError(err error) error {
	if strings.Contains(err.Error(), providerNotEnabled) {
		output.Info("This organization has no Datadog connection, so there is nothing to read.")
		return nil
	}
	return err
}

// The SDK decodes JSON, and this answer is a spreadsheet.
func printDatadogRenewalCSV() error {
	client, err := newSDKClientFn()
	if err != nil {
		return err
	}
	raw, err := client.Raw().DoRaw(http.MethodGet, datadogAPIPath+"/renewal?format="+formatCSV, nil)
	if err != nil {
		return err
	}
	if raw.StatusCode >= http.StatusBadRequest {
		return handleDatadogError(fmt.Errorf("API error (%d): %s", raw.StatusCode, strings.TrimSpace(string(raw.Body))))
	}
	output.PrintRaw(datadogCSVFor(string(raw.Body)))
	return nil
}

// A pipe gets the API's bytes. A terminal gets rows that cannot make it act.
func datadogCSVFor(body string) string {
	if !output.IsTTY() {
		return body
	}
	return output.SanitizeMultiline(strings.ReplaceAll(body, "\r\n", "\n"))
}

// Anyone who can send a metric or register a host in a Datadog account picks its
// name, so every renderer here prints through these three.
func datadogTable(headers []string, rows [][]string) {
	cleanRows := make([][]string, len(rows))
	for index, row := range rows {
		cleanRows[index] = datadogCleanCells(row)
	}
	output.Table(datadogCleanCells(headers), cleanRows)
}

func datadogCleanCells(cells []string) []string {
	clean := make([]string, len(cells))
	for index, cell := range cells {
		clean[index] = output.Sanitize(cell)
	}
	return clean
}

func datadogKeyValue(key, value string) {
	output.KeyValue(key, output.Sanitize(value))
}

func datadogHeader(title string) {
	output.Header(output.Sanitize(title))
}

func renderDatadogRenewal(renewal *levelfourgo.DatadogRenewalData, name string) error {
	if name != "" {
		return renderDatadogRenewalProduct(renewal.GetProducts(), name)
	}
	if len(renewal.GetProducts()) == 0 {
		output.Info("The Datadog bill holds no closed month yet.")
		return nil
	}

	products := slices.Clone(renewal.GetProducts())
	slices.SortStableFunc(products, func(a, b *levelfourgo.DatadogRenewalProduct) int {
		return cmp.Compare(b.GetPaidUsd(), a.GetPaidUsd())
	})
	rows := make([][]string, 0, len(products))
	for _, product := range products {
		rows = append(rows, []string{
			product.GetLabel(),
			moneyValue(product.GetPaidUsd()),
			moneyValue(product.GetOnDemandUsd()),
			pctOrNotMeasured(product.GetRateVsListPct()),
			moneyValue(product.GetOpenSavingsMonthlyUsd()),
			commitmentQuantity(product.GetRecommended(), product.GetUnit()),
			commitmentAnnual(product.GetRecommended()),
		})
	}
	datadogTable([]string{columnProduct, "Paid, 12 mo", "On demand", "Vs list", "Open savings/mo", "Ask for", "Next year"}, rows)
	output.Info(potentialSaving)
	return nil
}

func renderDatadogRenewalProduct(products []*levelfourgo.DatadogRenewalProduct, name string) error {
	chosen := slices.DeleteFunc(slices.Clone(products), func(product *levelfourgo.DatadogRenewalProduct) bool {
		return product.GetProduct() != name
	})
	if len(chosen) == 0 {
		return fmt.Errorf("the renewal holds no product %q", name)
	}
	for _, product := range chosen {
		renderDatadogRenewalRow(product)
	}
	return nil
}

func renderDatadogRenewalRow(product *levelfourgo.DatadogRenewalProduct) {
	datadogHeader(product.GetLabel())
	datadogKeyValue("Unit", product.GetUnit())
	if usage := product.GetUsage(); usage != nil {
		datadogKeyValue("Usage, median month", unitsValue(usage.GetMedian()))
		datadogKeyValue("Usage, p95 month", unitsValue(usage.GetP95()))
		datadogKeyValue("Usage, latest month", unitsValue(usage.GetLatest()))
	}
	datadogKeyValue("Usage trend", pctOrNotMeasured(product.GetUsageTrendPct()))
	datadogKeyValue("Committed quantity", unitsOrNotMeasured(product.GetCommittedQuantity()))
	datadogKeyValue("Commitment used", pctOrNotMeasured(product.GetUsedPct()))
	datadogKeyValue("Unused commitment", moneyOrNotMeasured(product.GetUnusedCommitmentUsd()))
	datadogKeyValue("On demand, 12 mo", moneyValue(product.GetOnDemandUsd()))
	datadogKeyValue("Paid, 12 mo", moneyValue(product.GetPaidUsd()))
	datadogKeyValue("Effective rate", ratePer(product.GetEffectiveRateUsd(), product.GetRatePer()))
	datadogKeyValue("List rate", ratePer(product.GetListRateUsd(), product.GetRatePer()))
	datadogKeyValue("List tier", textOrNotMeasured(product.GetListTier()))
	datadogKeyValue("Rate vs list", pctOrNotMeasured(product.GetRateVsListPct()))
	datadogKeyValue("Open savings", moneyValue(product.GetOpenSavingsMonthlyUsd())+"/mo")
	datadogKeyValue("Ask for", commitmentQuantity(product.GetRecommended(), product.GetUnit()))
	datadogKeyValue("Next year", commitmentAnnual(product.GetRecommended()))
	datadogKeyValue("Ask for, savings applied", commitmentQuantity(product.GetRecommendedAfterFindings(), product.GetUnit()))
	datadogKeyValue("Next year, savings applied", commitmentAnnual(product.GetRecommendedAfterFindings()))
	if note := product.GetQuantityNote(); note != nil {
		output.Info(*note)
	}
}

// A quantity of 0 is the answer "stay on demand", which a bare 0 does not say.
func commitmentQuantity(commitment *levelfourgo.DatadogCommitmentRecommendation, unit string) string {
	if commitment == nil {
		return notMeasured
	}
	if commitment.GetQuantity() == 0 {
		return "stay on demand"
	}
	return unitsValue(commitment.GetQuantity()) + " " + unit
}

func commitmentAnnual(commitment *levelfourgo.DatadogCommitmentRecommendation) string {
	if commitment == nil {
		return notMeasured
	}
	return moneyValue(commitment.GetAnnualUsd())
}

func ratePer(rate *float64, per int) string {
	if rate == nil {
		return notMeasured
	}
	return moneyValue(*rate) + " per " + strconv.Itoa(per)
}

func renderDatadogPlan(resp *levelfourgo.DatadogRenewalPlanResponse) error {
	plan := resp.GetData()
	output.KPICards([]output.KPICard{
		{Label: "Last 12 months", Value: moneyValue(plan.GetLast12Usd())},
		{Label: "Today, a month", Value: moneyValue(plan.GetMonthlyNowUsd())},
		{Label: "Term", Value: strconv.Itoa(plan.GetTermMonths()) + " months"},
	})
	if len(plan.GetPoints()) == 0 {
		output.Info("No product grew past its median month or runs above its contract.")
		return nil
	}
	rows := make([][]string, 0, len(plan.GetPoints()))
	for _, point := range plan.GetPoints() {
		rows = append(rows, []string{
			point.GetLabel(),
			planRuleName(string(point.GetRule())),
			unitsOrNotMeasured(point.GetUsageMedian()),
			unitsOrNotMeasured(point.GetUsageNow()),
			moneyValue(point.GetMonthlyUsd()),
			moneyValue(point.GetTermUsd()),
		})
	}
	datadogHeader("Where the renewal can cost less")
	datadogTable([]string{columnProduct, "Why", "Median month", "Today", columnSavesMo, "Over the term"}, rows)
	output.Info(potentialSaving)
	return nil
}

func planRuleName(rule string) string {
	switch rule {
	case "grew":
		return "Grew past its median"
	case "above_contract":
		return "Above the contract"
	case "twice":
		return "Another tool also covers it"
	}
	return rule
}

func renderDatadogContract(resp *levelfourgo.DatadogContractResponse) error {
	data := resp.GetData()
	if len(data.GetContracts()) == 0 {
		output.Info("No Datadog contract is on file.")
		return nil
	}
	for _, contract := range data.GetContracts() {
		datadogHeader("Contract " + contract.GetID() + ", month of " + datadogDay(data.GetMonth()))
		datadogTable(
			[]string{"Line", "Unit", "Committed", "Month to date", "Month end", "On demand past it", "Crosses on"},
			contractLineRows(contract.GetLines()),
		)
		for _, line := range contract.GetLines() {
			if note := line.GetProjectionNote(); note != nil {
				output.Info(line.GetDimension() + ": " + *note)
			}
		}
	}
	return nil
}

func contractLineRows(lines []*levelfourgo.DatadogContractLineForecast) [][]string {
	rows := make([][]string, 0, len(lines))
	for _, line := range lines {
		rows = append(rows, []string{
			line.GetDimension(),
			line.GetUnit(),
			unitsValue(line.GetCommitted()),
			unitsValue(line.GetMonthToDate()),
			unitsOrNotMeasured(line.GetProjectedMonthEnd()),
			unitsOrNotMeasured(line.GetProjectedOverage()),
			datadogDayOrNotMeasured(line.GetCrossingDay()),
		})
	}
	return rows
}

func renderDatadogHygiene(resp *levelfourgo.DatadogHygieneResponse) error {
	hygiene := resp.GetData()
	output.KPICards([]output.KPICard{
		{Label: "Score", Value: strconv.Itoa(hygiene.GetScore()) + " of 100"},
		{Label: "Spend, 30 days", Value: moneyValue(hygiene.GetMonthlySpend())},
	})
	rows := make([][]string, 0, len(hygiene.GetRules()))
	for _, rule := range hygiene.GetRules() {
		state := "Failing"
		if rule.GetPassed() {
			state = "Passed"
		}
		rows = append(rows, []string{
			rule.GetLabel(), state, string(rule.GetBill()), strconv.Itoa(rule.GetFindingCount()), moneyValue(rule.GetMonthlySavings()),
		})
	}
	datadogTable([]string{"Rule", "State", "Bill", "Findings", columnSavesMo}, rows)
	output.Info(potentialSaving)
	return nil
}

func renderDatadogMonths(months *levelfourgo.DatadogMonthsData, name string) error {
	if name != "" {
		return renderDatadogProductMonths(months, name)
	}
	if len(months.GetProducts()) == 0 {
		output.Info("The Datadog bill holds no month yet.")
		return nil
	}

	columns := months.GetMonths()
	shown := columns[max(0, len(columns)-recentMonths):]
	headers := []string{columnProduct}
	for _, column := range shown {
		headers = append(headers, monthHeader(column))
	}
	rows := make([][]string, 0, len(months.GetProducts()))
	for _, product := range months.GetProducts() {
		row := []string{product.GetLabel()}
		cells := product.GetMonths()
		for _, month := range cells[max(0, len(cells)-len(shown)):] {
			row = append(row, moneyOrNotMeasured(month.GetCostUsd()))
		}
		rows = append(rows, row)
	}
	datadogTable(headers, rows)
	return nil
}

func renderDatadogProductMonths(months *levelfourgo.DatadogMonthsData, name string) error {
	found := false
	for _, product := range months.GetProducts() {
		if product.GetProduct() != name {
			continue
		}
		found = true
		datadogHeader(product.GetLabel() + ", in " + product.GetUnit())
		rows := make([][]string, 0, len(product.GetMonths()))
		for index, month := range product.GetMonths() {
			rows = append(rows, []string{
				monthHeader(months.GetMonths()[index]),
				unitsOrNotMeasured(month.GetUsage()),
				moneyOrNotMeasured(month.GetCostUsd()),
			})
		}
		datadogTable([]string{columnMonth, "Usage", "Cost"}, rows)
	}
	if !found {
		return fmt.Errorf("the bill holds no product %q", name)
	}
	return nil
}

func datadogDay(day time.Time) string {
	return day.Format(time.DateOnly)
}

func datadogDayOrNotMeasured(day *time.Time) string {
	if day == nil {
		return notMeasured
	}
	return datadogDay(*day)
}

// The month's bill can still move unless Datadog closed it, so its figure says so.
func monthHeader(column *levelfourgo.DatadogMonthColumn) string {
	month := datadogDay(column.GetMonth())
	if column.GetState() == levelfourgo.DatadogMonthColumnStateFinal {
		return month
	}
	return month + " (" + string(column.GetState()) + ")"
}

// A null seller is an answer: the AWS bill names no Datadog line.
func datadogSeller(seller *string) string {
	if seller == nil {
		return "none"
	}
	return *seller
}

func renderDatadogAWS(resp *levelfourgo.DatadogAwsReconciliationResponse) error {
	reconciliation := resp.GetData()
	datadogKeyValue("Invoiced", reconciliation.GetBilledVia())
	datadogKeyValue("Seller on the AWS bill", datadogSeller(reconciliation.GetSeller()))
	rows := make([][]string, 0, len(reconciliation.GetMonths()))
	for _, month := range reconciliation.GetMonths() {
		rows = append(rows, []string{
			datadogDay(month.GetMonth()),
			moneyOrNotMeasured(month.GetAwsBilledUsd()),
			moneyOrNotMeasured(month.GetDatadogReportedUsd()),
			moneyOrNotMeasured(month.GetDifferenceUsd()),
		})
	}
	datadogTable([]string{columnMonth, "AWS billed", "Datadog reports", "Difference"}, rows)
	return nil
}

func init() {
	datadogRenewalCmd.Flags().StringVar(&flagDatadogFormat, "format", "", "Output format: csv writes the whole table as a spreadsheet")

	datadogCmd.AddCommand(datadogRenewalCmd, datadogPlanCmd, datadogContractCmd, datadogHygieneCmd, datadogMonthsCmd, datadogAWSCmd)
}
