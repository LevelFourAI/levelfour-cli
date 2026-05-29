package cli

import (
	"context"
	"fmt"
	"sync"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
	"github.com/spf13/cobra"
)

var flagSummaryProvider string

var costsSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Spending and savings overview with KPIs and top services",
	Example: `  l4 costs summary
  l4 costs summary --provider aws
  l4 costs summary --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			return openWeb("/spending")
		}

		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		ctx := context.Background()
		providerID, err := resolveProviderID(ctx, client, flagSummaryProvider)
		if err != nil {
			return err
		}

		type provResult struct {
			data *levelfourgo.ProviderSpendingSummaryResponse
			err  error
		}
		type savResult struct {
			data *levelfourgo.SavedSummaryResponse
			err  error
		}

		var prov provResult
		var savings savResult
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			defer wg.Done()
			data, err := client.SDK().Costs.GetProviderSummary(ctx, providerID)
			prov = provResult{data, err}
		}()
		go func() {
			defer wg.Done()
			data, err := client.SDK().Recommendations.Audit.GetSummary(ctx)
			savings = savResult{data, err}
		}()
		wg.Wait()

		if prov.err != nil {
			return prov.err
		}
		if savings.err != nil {
			return savings.err
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(map[string]interface{}{
				"provider": prov.data,
				"savings":  savings.data,
			})
		}

		provData := prov.data.GetData()
		savData := savings.data.GetData()

		renderCostsSummaryKPIs(provData, savData)
		renderTopServices(provData.GetTopServices())
		return nil
	},
}

func renderCostsSummaryKPIs(p *levelfourgo.ProviderSpendingSummaryData, s *levelfourgo.SavedSummaryData) {
	cards := []output.KPICard{
		{Label: "This Month", Value: fmt.Sprintf("$%.2f", p.GetMonthlySpending())},
		{Label: "Forecasted", Value: fmt.Sprintf("$%.2f", p.GetForecastedMonthlyCosts())},
		{Label: "MoM Δ", Value: formatOptionalPercent(p.GetMonthlySpendingPercentage())},
		{Label: "Potential Savings", Value: fmt.Sprintf("$%.2f", p.GetPotentialSavings())},
	}
	if s != nil {
		cards = append(cards, output.KPICard{
			Label: "Saved Monthly",
			Value: fmt.Sprintf("$%.2f", s.GetTotalMonthlySavings()),
		})
		cards = append(cards, output.KPICard{
			Label: "Saved Annual",
			Value: fmt.Sprintf("$%.2f", s.GetTotalAnnualSavings()),
		})
	}
	output.KPICards(cards)
}

func renderTopServices(top []*levelfourgo.ProviderTopService) {
	if len(top) == 0 {
		return
	}
	output.Header("Top Services")
	headers := []string{columnService, "Cost", "Change"}
	rows := make([][]string, 0, len(top))
	for _, t := range top {
		rows = append(rows, []string{
			t.GetService(),
			fmt.Sprintf("$%.2f", t.GetCost()),
			formatChangePercentage(t.GetChangePercentage()),
		})
	}
	output.Table(headers, rows)
}

func formatOptionalPercent(p *float64) string {
	if p == nil {
		return dashSymbol
	}
	return formatChangePercentage(p)
}

func init() {
	costsSummaryCmd.Flags().StringVar(&flagSummaryProvider, "provider", "", "Provider ID (aws, gcp, azure, k8s); auto-detected if omitted")
	costsCmd.AddCommand(costsSummaryCmd)
}
