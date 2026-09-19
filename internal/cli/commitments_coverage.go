package cli

import (
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var flagCoverageInstrument string

var commitmentsCoverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "How much of the eligible bill a commitment covers",
	Long: `Coverage is eligible usage on a commitment over all eligible usage.

It is not utilization, which is commitment hours used over commitment hours
purchased. Coverage asks how exposed you are to on-demand rates; utilization asks
whether what you bought is being used. Full utilization alongside low coverage
means the commitment is undersized.

Every percentage here is weighted by the dollars behind it, so a small
reservation cannot move the figure as far as a large one.`,
	Example: `  l4 commitments coverage
  l4 commitments coverage --instrument sp
  l4 commitments coverage --provider gcp
  l4 commitments coverage --jq '.data.totals.coverage_pct'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, provider, err := commitmentClient()
		if err != nil {
			return err
		}
		if flagCoverageInstrument != "" && !isInstrument(flagCoverageInstrument) {
			return fmt.Errorf("--instrument must be ri or sp (got %q)", flagCoverageInstrument)
		}
		if flagWeb {
			return openWeb(commitmentsWebPath(provider, coverageSection(), ""))
		}
		return runCommitmentsCoverage(client, provider)
	},
}

// The dashboard has a card per instrument, so --instrument picks which one to
// land on. Unset opens the page's own default.
func coverageSection() string {
	if flagCoverageInstrument == "" {
		return instrumentRI
	}
	return flagCoverageInstrument
}

func runCommitmentsCoverage(client *api.SDKClient, provider string) error {
	coverage, body, err := api.GetCommitments[api.CommitmentCoverage](client.Raw(), "/coverage-rates",
		map[string]string{"provider": provider})
	if err != nil {
		return handleCommitmentsError(err)
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(body)
	}

	if !coverage.Measured {
		output.Info(fmt.Sprintf("Coverage is not measured for %s yet.", providerLabel(provider)))
		return nil
	}

	renderCoverageTotals(coverage.Totals)
	renderCoverageServices(coverage.Services, flagCoverageInstrument)
	return nil
}

func renderCoverageTotals(totals *api.CoverageRateTotals) {
	if totals == nil {
		return
	}
	output.KPICards([]output.KPICard{
		{Label: "Covered", Value: pctOrNotMeasured(totals.CoveragePct)},
		{Label: "Covered spend", Value: monthlyOrNotMeasured(totals.CoveredMonthly)},
		{Label: "Uncovered spend", Value: moneyValue(totals.UncoveredMonthly) + "/mo"},
	})
}

func renderCoverageServices(services []api.CoverageRateService, instrument string) {
	shown := filterCoverageServices(services, instrument)
	if len(shown) == 0 {
		output.Info("No service has a measured coverage figure.")
		return
	}

	output.Header("By service")
	rows := make([][]string, 0, len(shown))
	for _, service := range shown {
		rows = append(rows, []string{
			strings.ToUpper(service.Instrument),
			service.Dimension,
			pctValue(service.CoveragePct),
			textOrNotMeasured(service.MeasuredOn),
		})
	}
	output.Table([]string{"Kind", columnService, "Covered", "Measured on"}, rows)
}

func filterCoverageServices(services []api.CoverageRateService, instrument string) []api.CoverageRateService {
	kept := make([]api.CoverageRateService, 0, len(services))
	for _, service := range services {
		if matches(service.Instrument, instrument) {
			kept = append(kept, service)
		}
	}
	return kept
}

func init() {
	commitmentsCoverageCmd.Flags().StringVar(&flagCoverageInstrument, "instrument", "",
		"Restrict to one instrument: ri or sp")

	commitmentsCmd.AddCommand(commitmentsCoverageCmd)
}
