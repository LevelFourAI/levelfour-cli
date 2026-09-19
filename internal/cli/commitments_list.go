package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	kindReservedInstance = "reserved_instance"
	kindSavingsPlan      = "savings_plan"
	kindCUD              = "committed_use_discount"
	kindSpendCommitment  = "spend_commitment"

	defaultExpiryWindow = "90d"

	// The route's ceiling. Every caller below walks the pages rather than
	// trusting one to be enough.
	listPageSize = "200"
)

var kindLabels = map[string]string{
	kindReservedInstance: "RI",
	kindSavingsPlan:      "SP",
	kindCUD:              "CUD",
	kindSpendCommitment:  "Spend",
}

var kindByFlag = map[string]string{
	instrumentRI: kindReservedInstance,
	instrumentSP: kindSavingsPlan,
	"cud":        kindCUD,
	"spend":      kindSpendCommitment,
}

var kindsByProvider = map[string][]string{
	providerAWS: {kindReservedInstance, kindSavingsPlan},
	providerGCP: {kindCUD, kindSpendCommitment},
}

var (
	flagListBasis          string
	flagListKind           string
	flagListStatus         string
	flagListExpiringWithin string
	flagExpiringWithin     string
	flagExpiringFailWithin string
)

var commitmentsListCmd = &cobra.Command{
	Use:   "list",
	Short: "Every commitment held, soonest to expire first",
	Example: `  l4 commitments list
  l4 commitments list --basis net
  l4 commitments list --kind ri --expiring-within 90d
  l4 commitments list --provider gcp
  l4 commitments list --jq '.data.rows[].id'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, provider, err := commitmentClient()
		if err != nil {
			return err
		}
		if flagWeb {
			return openWeb(commitmentsWebPath(provider, instrumentRI, ""))
		}
		kind, err := resolveKind(provider, flagListKind)
		if err != nil {
			return err
		}
		if provider == providerAWS {
			return runPortfolioList(client, provider, kind)
		}
		return runPlainList(client, provider, kind)
	},
}

var commitmentsExpiringCmd = &cobra.Command{
	Use:   "expiring",
	Short: "Commitments lapsing soon, and an exit code when they are too close",
	Long: `Lists the commitments whose term ends inside a window.

With --fail-within the command exits 2 when anything lapses inside that window,
which is what makes it usable as a pipeline check. Without it the command only
reports, and always exits 0.`,
	Example: `  l4 commitments expiring
  l4 commitments expiring --within 180d
  l4 commitments expiring --fail-within 30d
  l4 commitments expiring --within 180d --fail-within 30d`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, provider, err := commitmentClient()
		if err != nil {
			return err
		}
		if flagWeb {
			return openWeb(commitmentsWebPath(provider, instrumentRI, ""))
		}
		if unavailableForProvider(provider,
			"Commitment expiry is not measured for %s: the commitment lifecycle export is not enabled. Nothing checked.") {
			return nil
		}
		return runCommitmentsExpiring(cmd, client, provider)
	},
}

func runPortfolioList(client *api.SDKClient, provider, kind string) error {
	// Parsed before the request, so a typo in the window costs nothing.
	narrowing := flagListExpiringWithin != ""
	var window time.Duration
	if narrowing {
		parsed, parseErr := parseWindow(flagListExpiringWithin)
		if parseErr != nil {
			return parseErr
		}
		window = parsed
	}

	portfolio, body, err := fetchPortfolio(client, provider)
	if err != nil {
		return handleCommitmentsError(err)
	}

	rows := portfolio.Rows
	if narrowing {
		rows = withinWindow(rows, window)
	}
	rows = filterPortfolio(rows, kind, flagListStatus)

	if output.HasFormattingFlags() {
		noteUnappliedFilters(map[string]string{
			"--kind":            flagListKind,
			"--status":          flagListStatus,
			"--expiring-within": flagListExpiringWithin,
		})
		return output.PrintResult(body)
	}

	renderPortfolioTotals(portfolio.Totals, portfolio.Basis)
	if len(rows) == 0 {
		output.Info("No commitments match.")
		return nil
	}
	renderPortfolioRows(rows)
	return nil
}

// runPlainList filters kind and status here rather than in the query. It holds
// every page already, so filtering in this client costs one pass over rows it
// has, and it behaves the same against an API whose own kind filter accepts all
// four kinds and one whose filter predates two of them.
func runPlainList(client *api.SDKClient, provider, kind string) error {
	items, err := fetchAllCommitments(client, provider)
	if err != nil {
		return handleCommitmentsError(err)
	}

	if output.HasFormattingFlags() {
		noteUnappliedFilters(map[string]string{"--kind": flagListKind, "--status": flagListStatus})
		return output.PrintResult(map[string][]api.CommitmentListItem{"items": items})
	}

	items = filterCommitmentItems(items, kind, flagListStatus)
	if len(items) == 0 {
		output.Info("No commitments match.")
		return nil
	}
	renderCommitmentItems(items)
	output.KeyValue("Commitments", strconv.Itoa(len(items)))
	return nil
}

// fetchAllCommitments walks every page. Stopping at the first would understate
// a portfolio in silence: the count printed and the rows exported would both be
// short with nothing saying so. `fetchAllRecommendations` in export.go walks its
// pages the same way.
//
// The assembled items are the whole payload rather than a page of it, which is
// why a formatting flag prints these rather than one response envelope.
// CommitmentListItem mirrors every field the route returns, so nothing is lost
// in the round trip.
func fetchAllCommitments(client *api.SDKClient, provider string) ([]api.CommitmentListItem, error) {
	var items []api.CommitmentListItem
	for page := 1; ; page++ {
		listing, _, err := api.GetCommitments[api.CommitmentList](client.Raw(), "",
			map[string]string{
				"provider":  provider,
				"page":      strconv.Itoa(page),
				"page_size": listPageSize,
			})
		if err != nil {
			return nil, err
		}
		items = append(items, listing.Items...)
		if !listing.Pagination.HasNext {
			return items, nil
		}
	}
}

// noteUnappliedFilters says which flags shaped the table and not the payload.
// The list route cannot express these filters, so they run in this client, and
// a formatting flag that prints the payload prints rows the table dropped.
func noteUnappliedFilters(named map[string]string) {
	set := make([]string, 0, len(named))
	for name, value := range named {
		if value != "" {
			set = append(set, name)
		}
	}
	if len(set) == 0 {
		return
	}
	sort.Strings(set)
	output.Info(fmt.Sprintf(
		"The payload below is every commitment the API returned. %s narrowed the table only; narrow the payload with --jq.",
		strings.Join(set, " and ")))
}

func runCommitmentsExpiring(cmd *cobra.Command, client *api.SDKClient, provider string) error {
	windows, err := expiryWindows(cmd)
	if err != nil {
		return err
	}

	portfolio, body, fetchErr := fetchPortfolio(client, provider)
	if fetchErr != nil {
		return handleCommitmentsError(fetchErr)
	}

	if output.HasFormattingFlags() {
		noteUnappliedFilters(map[string]string{"--within": windows.listLabel})
	}
	if err := reportExpiring(portfolio, body, windows); err != nil {
		return err
	}
	return expiryGate(portfolio, windows)
}

func reportExpiring(portfolio api.CommitmentPortfolio, body json.RawMessage, windows expiryWindowSet) error {
	if output.HasFormattingFlags() {
		return output.PrintResult(body)
	}
	listed := withinWindow(portfolio.Rows, windows.list)
	if len(listed) == 0 {
		output.Info(fmt.Sprintf("No commitments lapse within %s.", windows.listLabel))
		return nil
	}
	renderPortfolioRows(listed)
	return nil
}

// expiryGate runs whatever the output format is. A pipeline that asks for JSON
// still armed the gate, and a build that keeps passing because someone added
// --json is the one failure this command exists to prevent. Both notices go to
// stderr under a formatting flag, so the payload on stdout stays parseable.
func expiryGate(portfolio api.CommitmentPortfolio, windows expiryWindowSet) error {
	if !windows.gated {
		return nil
	}
	breaching := withinWindow(portfolio.Rows, windows.fail)
	if len(breaching) == 0 {
		output.Success(fmt.Sprintf("Nothing lapses within %s.", flagExpiringFailWithin))
		return nil
	}
	output.Error(fmt.Sprintf("%d commitment(s) lapse within %s.", len(breaching), flagExpiringFailWithin))
	return ErrIssuesFound
}

// The body stays a json.RawMessage all the way to output.PrintResult. Widened
// to []byte it marshals as base64, and --jq is handed a string instead of the
// payload.
func fetchPortfolio(client *api.SDKClient, provider string) (api.CommitmentPortfolio, json.RawMessage, error) {
	portfolio, body, err := api.GetCommitments[api.CommitmentPortfolio](client.Raw(), "/portfolio",
		map[string]string{"provider": provider, "basis": flagListBasis})
	return portfolio, body, err
}

type expiryWindowSet struct {
	list      time.Duration
	listLabel string
	fail      time.Duration
	gated     bool
}

// expiryWindows keeps the gate from firing on rows the listing never showed. A
// caller who set only --fail-within gets a listing of the same width, so the
// output explains the exit code it just produced.
func expiryWindows(cmd *cobra.Command) (expiryWindowSet, error) {
	windows := expiryWindowSet{listLabel: flagExpiringWithin, gated: flagExpiringFailWithin != ""}
	if windows.gated && !cmd.Flags().Changed("within") {
		windows.listLabel = flagExpiringFailWithin
	}

	var err error
	if windows.list, err = parseWindow(windows.listLabel); err != nil {
		return windows, err
	}
	if !windows.gated {
		return windows, nil
	}
	windows.fail, err = parseWindow(flagExpiringFailWithin)
	return windows, err
}

// resolveKind maps the flag onto a stored kind and refuses one belonging to
// another provider, which would otherwise return an empty table that reads as
// "you hold none of these".
func resolveKind(provider, flag string) (string, error) {
	if flag == "" {
		return "", nil
	}
	kind, ok := kindByFlag[flag]
	if !ok {
		return "", fmt.Errorf("--kind must be one of ri, sp, cud, spend (got %q)", flag)
	}
	for _, allowed := range kindsByProvider[provider] {
		if allowed == kind {
			return kind, nil
		}
	}
	return "", fmt.Errorf("--kind %s is not a kind %s uses", flag, providerLabel(provider))
}

func filterPortfolio(rows []api.CommitmentPortfolioRow, kind, status string) []api.CommitmentPortfolioRow {
	kept := make([]api.CommitmentPortfolioRow, 0, len(rows))
	for _, row := range rows {
		if matches(row.Kind, kind) && matches(row.Status, status) {
			kept = append(kept, row)
		}
	}
	return kept
}

func filterCommitmentItems(items []api.CommitmentListItem, kind, status string) []api.CommitmentListItem {
	kept := make([]api.CommitmentListItem, 0, len(items))
	for _, item := range items {
		if matches(item.Kind, kind) && matches(item.Status, status) {
			kept = append(kept, item)
		}
	}
	return kept
}

func matches(value, wanted string) bool {
	return wanted == "" || value == wanted
}

// renderPortfolioTotals prints the API's own arithmetic. Nothing is summed here:
// the monthly commitment column reads zero on some No Upfront terms, so a
// client-side total would silently understate the portfolio.
func renderPortfolioTotals(totals api.CommitmentPortfolioTotals, basis string) {
	output.KPICards([]output.KPICard{
		{Label: "Commitments", Value: strconv.Itoa(totals.CommitmentCount)},
		{Label: "Fee (" + basis + ")", Value: moneyValue(portfolioFee(totals, basis)) + "/mo"},
		{Label: "Protects", Value: moneyValue(totals.ProtectsMonthly) + "/mo"},
		{Label: "Rightsizing", Value: moneyValue(totals.RightsizingMonthly) + "/mo"},
		{Label: "Expiring 30d", Value: strconv.Itoa(totals.ExpiringWithin30d)},
	})
}

func portfolioFee(totals api.CommitmentPortfolioTotals, basis string) float64 {
	if basis == "list" {
		return totals.FeeMonthlyList
	}
	return totals.FeeMonthlyNet
}

func renderPortfolioRows(rows []api.CommitmentPortfolioRow) {
	headers := []string{"ID", columnService, "Kind", "Holder", "Expires", "Util", "Coverage", "Protects/mo", "Note"}
	table := make([][]string, 0, len(rows))
	for _, row := range rows {
		table = append(table, []string{
			row.ID,
			row.ServiceLabel,
			kindLabel(row.Kind),
			row.HolderAccountName,
			expiryCountdown(row.ExpiresInSeconds),
			pctValue(row.UtilizationPct),
			coverageCell(row.Kind, row.CoveragePct),
			moneyOrNotMeasured(row.ProtectsMonthly),
			saturationNote(row.Saturated),
		})
	}
	output.Table(headers, table)
}

func renderCommitmentItems(items []api.CommitmentListItem) {
	headers := []string{"ID", columnService, "Kind", columnAccount, "Ends", "Status", "Util", "Coverage", "Committed/mo"}
	table := make([][]string, 0, len(items))
	for _, item := range items {
		table = append(table, []string{
			item.ID,
			item.ServiceLabel,
			kindLabel(item.Kind),
			item.AccountName,
			endDateCell(item.EndDate),
			item.Status,
			pctValue(item.CurrentUtilizationPct),
			coverageCell(item.Kind, item.CurrentCoveragePct),
			moneyValue(item.MonthlyCommitmentUSD),
		})
	}
	output.Table(headers, table)
}

// endDateCell keeps an absent term honest. Committed Use Discount lifecycle
// needs an export that is not enabled, so the date arrives empty rather than
// wrong, and an empty cell would read as a commitment that never ends.
func endDateCell(endDate string) string {
	if endDate == "" {
		return notMeasured
	}
	return endDate
}

// coverageCell refuses to print a Savings Plan's coverage. Cost Explorer cannot
// filter coverage by plan type, so what one plan covers is unmeasurable, and the
// API sends zero where it has nothing to send. Printed as 0% that zero claims
// the plan covers none of the eligible usage, which is a different statement.
func coverageCell(kind string, pct float64) string {
	if kind == kindSavingsPlan {
		return notMeasured
	}
	return pctValue(pct)
}

func kindLabel(kind string) string {
	if label, ok := kindLabels[kind]; ok {
		return label
	}
	return kind
}

func init() {
	commitmentsListCmd.Flags().StringVar(&flagListBasis, "basis", "net",
		"Fee basis: net (after the agreement) or list")
	commitmentsListCmd.Flags().StringVar(&flagListKind, "kind", "",
		"Restrict to one kind: ri, sp, cud, spend")
	commitmentsListCmd.Flags().StringVar(&flagListStatus, "status", "",
		"Restrict to one status: active, expiring, expired, cancelled")
	commitmentsListCmd.Flags().StringVar(&flagListExpiringWithin, "expiring-within", "",
		"Only commitments lapsing inside this window, for example 90d")

	commitmentsExpiringCmd.Flags().StringVar(&flagExpiringWithin, "within", defaultExpiryWindow,
		"Window to list, for example 30d, 12w, 6m")
	commitmentsExpiringCmd.Flags().StringVar(&flagExpiringFailWithin, "fail-within", "",
		"Exit code 2 when a commitment lapses inside this window")

	commitmentsCmd.AddCommand(commitmentsListCmd)
	commitmentsCmd.AddCommand(commitmentsExpiringCmd)
}
