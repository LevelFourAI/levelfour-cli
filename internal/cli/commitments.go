package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var commitmentsCmd = &cobra.Command{
	Use:     "commitments",
	Aliases: []string{"cmt"},
	Short:   "Reserved Instances, Savings Plans and Committed Use Discounts",
}

var (
	flagCommitmentsProvider string
	flagSummaryPeriod       string
	flagSummaryScope        string
)

var commitmentsSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Coverage, utilization and what the commitments are earning",
	Example: `  l4 commitments summary
  l4 commitments summary --provider gcp
  l4 commitments summary --period 2026-08 --scope all
  l4 commitments summary --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, provider, err := commitmentClient()
		if err != nil {
			return err
		}
		if flagWeb {
			return openWeb(commitmentsWebPath(provider, instrumentRI, ""))
		}
		return runCommitmentsSummary(client, provider)
	},
}

// commitmentSummary holds the three reads the screen is built from. The rate is
// absent for every provider but AWS, where nothing exports an on-demand
// equivalent to divide by.
type commitmentSummary struct {
	overview  api.CommitmentsOverview
	byService api.CommitmentsByService
	coverage  api.CommitmentCoverage
	esr       *api.CommitmentEsr
	bodies    map[string]json.RawMessage
}

func runCommitmentsSummary(client *api.SDKClient, provider string) error {
	summary, err := fetchCommitmentSummary(client, provider)
	if err != nil {
		return handleCommitmentsError(err)
	}

	if output.HasFormattingFlags() {
		return output.PrintResult(summary.bodies)
	}

	renderSummaryKPIs(summary, provider)
	renderSummaryMeasuredShare(summary.esr)
	renderByService(summary.byService, provider)
	return nil
}

func fetchCommitmentSummary(client *api.SDKClient, provider string) (*commitmentSummary, error) {
	summary := &commitmentSummary{bodies: map[string]json.RawMessage{}}
	params := map[string]string{"provider": provider}
	wantRate := provider == providerAWS

	var overviewBody, byServiceBody, esrBody, coverageBody json.RawMessage
	var overviewErr, byServiceErr, esrErr, coverageErr error
	var esr api.CommitmentEsr

	err := tuicommon.RunWithSpinner("Loading commitments...", output.L4SpinnerTheme(), func(_ context.Context) error {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			summary.overview, overviewBody, overviewErr = api.GetCommitments[api.CommitmentsOverview](
				client.Raw(), "/overview", params)
		}()
		go func() {
			defer wg.Done()
			summary.byService, byServiceBody, byServiceErr = api.GetCommitments[api.CommitmentsByService](
				client.Raw(), "/by-service", params)
		}()
		if wantRate {
			wg.Add(1)
			go func() {
				defer wg.Done()
				esr, esrBody, esrErr = api.GetCommitments[api.CommitmentEsr](client.Raw(), "/esr",
					map[string]string{"scope": flagSummaryScope, "period": flagSummaryPeriod})
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			summary.coverage, coverageBody, coverageErr = api.GetCommitments[api.CommitmentCoverage](
				client.Raw(), "/coverage-rates", params)
		}()
		wg.Wait()
		return firstError(overviewErr, byServiceErr, esrErr, ignoreMissingRoute(coverageErr))
	})
	if err != nil {
		return nil, err
	}

	summary.bodies["overview"] = overviewBody
	summary.bodies["by_service"] = byServiceBody
	summary.bodies["coverage"] = coverageBody
	if wantRate {
		summary.esr = &esr
		summary.bodies["esr"] = esrBody
	}
	return summary, nil
}

// Summary predates the coverage read, so a deployment that serves the rest but
// not that route still gets a summary, with the Coverage figure unmeasured. Any
// other failure is a real one and propagates.
func ignoreMissingRoute(err error) error {
	if errors.Is(err, api.ErrCommitmentsUnavailable) {
		return nil
	}
	return err
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// The overview's own coverage averages percentages across commitments, so a
// hundred dollar reservation moves it as far as a fifty thousand dollar one.
// This is the spend-weighted figure `l4 commitments coverage` prints, so the two
// commands cannot disagree about the same estate.
func weightedCoverage(coverage api.CommitmentCoverage) string {
	if !coverage.Measured || coverage.Totals == nil {
		return notMeasured
	}
	return pctOrNotMeasured(coverage.Totals.CoveragePct)
}

func renderSummaryKPIs(summary *commitmentSummary, provider string) {
	overview := summary.overview
	cards := []output.KPICard{
		{Label: "Effective Rate", Value: effectiveRate(summary.esr)},
		{Label: "Coverage", Value: weightedCoverage(summary.coverage)},
		{Label: "Committed", Value: moneyValue(overview.TotalCommittedMonthly) + "/mo"},
		{Label: "Unused", Value: moneyValue(overview.EstimatedWasteMonthly) + "/mo"},
		{Label: "Commitments", Value: fmt.Sprintf("%d", overview.CommitmentCount)},
		{Label: "Expiring", Value: expiringCount(overview, provider)},
	}
	output.KPICards(cards)

	if summary.esr != nil && summary.esr.Totals != nil && summary.esr.Totals.Measured {
		output.KeyValue("Earned by commitments", pctOrNotMeasured(summary.esr.Totals.CommitmentRatePct))
		output.KeyValue("Earned by the agreement", pctOrNotMeasured(summary.esr.Totals.NegotiatedRatePct))
	}
}

// effectiveRate never falls back to the overview's own rate fields. Outside AWS
// they are filled with the coverage percentage and with zeros, which read as a
// measurement and are not one.
func effectiveRate(esr *api.CommitmentEsr) string {
	if esr == nil || esr.Totals == nil || !esr.Totals.Measured {
		return notMeasured
	}
	return pctOrNotMeasured(esr.Totals.TotalRatePct)
}

// expiringCount refuses to print a zero it cannot stand behind. Outside AWS the
// count needs a commitment lifecycle export that is not enabled, so it is always
// zero, and a zero here reads as "nothing expires soon".
func expiringCount(overview api.CommitmentsOverview, provider string) string {
	if provider != providerAWS {
		return notMeasured
	}
	return fmt.Sprintf("%d", overview.ExpiringCount)
}

func renderSummaryMeasuredShare(esr *api.CommitmentEsr) {
	if esr == nil || esr.MeasuredSharePct >= 100 {
		return
	}
	output.Info(fmt.Sprintf(
		"Rate measured over %s of eligible spend. The rest has no on-demand equivalent to compare against.",
		pctValue(esr.MeasuredSharePct)))
}

// instrumentLabels name the two slots per provider. The payload always calls
// them ri and sp, but on Google Cloud they carry resource-based and spend-based
// Committed Use Discounts, and printing "RI" there names a product that does
// not exist.
var instrumentLabels = map[string][2]string{
	providerAWS: {"RI", "SP"},
	providerGCP: {"CUD", "Spend"},
}

func instrumentNames(provider string) [2]string {
	if names, ok := instrumentLabels[provider]; ok {
		return names
	}
	return [2]string{"RI", "SP"}
}

func renderByService(byService api.CommitmentsByService, provider string) {
	if len(byService.Services) == 0 {
		output.Info("No commitments found for this provider.")
		return
	}

	names := instrumentNames(provider)
	showRI, showSP := instrumentsInUse(byService)

	output.Header("By service")
	headers := []string{columnService}
	if showRI {
		headers = append(headers, names[0]+" Util", names[0]+" Cov", names[0]+" Unused")
	}
	if showSP {
		headers = append(headers, names[1]+" Util", names[1]+" Cov", names[1]+" Unused")
	}

	rows := make([][]string, 0, len(byService.Services))
	for _, service := range byService.Services {
		row := []string{service.ServiceLabel}
		if showRI {
			row = append(row, breakdownCells(service.RI)...)
		}
		if showSP {
			row = append(row, breakdownCells(service.SP)...)
		}
		rows = append(rows, row)
	}
	output.Table(headers, rows)
}

// instrumentsInUse drops a whole instrument's columns when nothing is held in
// it, rather than printing a block of zeros beside the one that is real.
func instrumentsInUse(byService api.CommitmentsByService) (ri bool, sp bool) {
	for _, service := range byService.Services {
		ri = ri || service.RI.CommitmentCount > 0
		sp = sp || service.SP.CommitmentCount > 0
	}
	return ri, sp
}

func breakdownCells(breakdown api.CommitmentInstrumentBreakdown) []string {
	return []string{
		pctValue(breakdown.UtilizationPct),
		pctValue(breakdown.CoveragePct),
		moneyValue(breakdown.UnusedMonthly),
	}
}

// commitmentClient pairs the authenticated client with the provider every
// commitments command resolves the same way.
func commitmentClient() (*api.SDKClient, string, error) {
	client, err := newSDKClientFn()
	if err != nil {
		return nil, "", err
	}
	provider, err := resolveCommitmentProvider(context.Background(), client, flagCommitmentsProvider)
	if err != nil {
		return nil, "", err
	}
	return client, provider, nil
}

func init() {
	commitmentsCmd.PersistentFlags().StringVar(&flagCommitmentsProvider, "provider", "",
		"Provider ID (aws, gcp); auto-detected if omitted")
	commitmentsSummaryCmd.Flags().StringVar(&flagSummaryPeriod, "period", "",
		"Month to measure the rate over (YYYY-MM); the latest complete month if omitted")
	commitmentsSummaryCmd.Flags().StringVar(&flagSummaryScope, "scope", "eligible",
		"Rate scope: eligible (usage a commitment could cover) or all")

	commitmentsCmd.AddCommand(commitmentsSummaryCmd)
}
