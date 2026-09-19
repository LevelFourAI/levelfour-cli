package cli

import (
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagUtilInstrument  string
	flagUtilStart       string
	flagUtilEnd         string
	flagUtilGranularity string
	flagUtilService     string
)

var commitmentsUtilizationCmd = &cobra.Command{
	Use:   "utilization",
	Short: "How much of what was bought is actually being used",
	Long: `Utilization is commitment hours used over commitment hours purchased.

It is not coverage, which is eligible usage on a commitment over all eligible
usage. The two have different denominators and are never averaged together: full
utilization alongside low coverage means the commitment is undersized, not that
it is performing well.

Every service is measured on its own. EC2 and RDS contract in vCPU while
Redshift, ElastiCache, OpenSearch and MemoryDB contract in nodes, so a figure
averaged across them would describe none of them.`,
	Example: `  l4 commitments utilization
  l4 commitments utilization --instrument sp
  l4 commitments utilization --granularity monthly --start 2026-01-01
  l4 commitments utilization --service ec2`,
	RunE: func(cmd *cobra.Command, args []string) error {
		client, provider, err := commitmentClient()
		if err != nil {
			return err
		}
		if flagWeb {
			return openWeb(commitmentsWebPath(provider, flagUtilInstrument, ""))
		}
		if flagUtilInstrument != instrumentRI && flagUtilInstrument != instrumentSP {
			return fmt.Errorf("--instrument must be ri or sp (got %q)", flagUtilInstrument)
		}
		return runCommitmentUtilization(client, provider)
	},
}

func runCommitmentUtilization(client *api.SDKClient, provider string) error {
	utilization, body, err := api.GetCommitments[api.CommitmentUtilization](client.Raw(), "/utilization",
		map[string]string{
			"provider":    provider,
			"type":        flagUtilInstrument,
			"start":       flagUtilStart,
			"end":         flagUtilEnd,
			"granularity": flagUtilGranularity,
			"service":     flagUtilService,
		})
	if err != nil {
		return handleCommitmentsError(err)
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(body)
	}

	if len(utilization.Services) == 0 {
		output.Info(fmt.Sprintf("No %s utilization measured for %s.",
			strings.ToUpper(flagUtilInstrument), providerLabel(provider)))
		return nil
	}

	renderUtilizationServices(utilization.Services)
	if len(utilization.Services) == 1 {
		renderUtilizationDays(utilization.Services[0], flagUtilInstrument)
	}
	return nil
}

func renderUtilizationServices(services []api.ServiceUtilization) {
	headers := []string{columnService, "Utilization", "Measured on", "Committed"}
	rows := make([][]string, 0, len(services))
	for _, service := range services {
		rows = append(rows, []string{
			service.ServiceLabel,
			pctValue(service.UtilizationPct),
			dimensionSummary(service.Dimensions),
			committedSummary(service),
		})
	}
	output.Table(headers, rows)
}

// dimensionSummary keeps each dimension's own unit beside its own numbers, so a
// service contracted in vCPU never reads as one contracted in nodes.
func dimensionSummary(dimensions []api.UtilizationDimension) string {
	if len(dimensions) == 0 {
		return notMeasured
	}
	parts := make([]string, 0, len(dimensions))
	for _, dimension := range dimensions {
		parts = append(parts, fmt.Sprintf("%.0f/%.0f %s", dimension.Used, dimension.Total, dimension.Unit))
	}
	return strings.Join(parts, ", ")
}

func committedSummary(service api.ServiceUtilization) string {
	if service.ContractedHourlyUSD != nil {
		return moneyValue(*service.ContractedHourlyUSD) + "/hr"
	}
	if service.CommittedMonthlyUSD != nil {
		return moneyValue(*service.CommittedMonthlyUSD) + "/mo"
	}
	return notMeasured
}

// renderUtilizationDays gives each instrument only the columns it measures. A
// plan carries no coverage at all, so the column is absent rather than empty:
// Cost Explorer cannot filter coverage by plan type.
func renderUtilizationDays(service api.ServiceUtilization, instrument string) {
	if len(service.Days) == 0 {
		return
	}
	output.Header(service.ServiceLabel + " over time")

	if instrument == instrumentSP {
		rows := make([][]string, 0, len(service.Days))
		for _, day := range service.Days {
			rows = append(rows, []string{
				day.Date,
				pctOrNotMeasured(day.CommitmentPct),
				moneyOrNotMeasured(day.CommitmentUsedUSD),
				moneyOrNotMeasured(day.CommitmentTotalUSD),
			})
		}
		output.Table([]string{"Date", "Used", "Used $", "Committed $"}, rows)
		return
	}

	rows := make([][]string, 0, len(service.Days))
	for _, day := range service.Days {
		rows = append(rows, []string{
			day.Date,
			pctOrNotMeasured(day.CapacityPct),
			pctOrNotMeasured(day.CoveragePct),
		})
	}
	output.Table([]string{"Date", "Utilization", "Coverage"}, rows)
}

func init() {
	commitmentsUtilizationCmd.Flags().StringVar(&flagUtilInstrument, "instrument", instrumentRI,
		"Instrument to measure: ri or sp")
	commitmentsUtilizationCmd.Flags().StringVar(&flagUtilStart, "start", "",
		"Start date (YYYY-MM-DD)")
	commitmentsUtilizationCmd.Flags().StringVar(&flagUtilEnd, "end", "",
		"End date (YYYY-MM-DD)")
	commitmentsUtilizationCmd.Flags().StringVar(&flagUtilGranularity, "granularity", "daily",
		"Granularity: daily or monthly")
	commitmentsUtilizationCmd.Flags().StringVar(&flagUtilService, "service", "",
		"Restrict to one service; its day-by-day series is printed too")

	commitmentsCmd.AddCommand(commitmentsUtilizationCmd)
}
