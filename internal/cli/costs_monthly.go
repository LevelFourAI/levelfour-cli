package cli

import (
	"context"
	"fmt"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var flagMonthlyProvider string

var costsMonthlyCmd = &cobra.Command{
	Use:   "monthly",
	Short: "Monthly spending aggregated per month",
	Example: `  l4 costs monthly
  l4 costs monthly --json | jq '.data.data_points[]'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			return openWeb("/spending")
		}

		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		resp, err := client.SDK().Costs.GetMonthlyCosts(context.Background())
		if err != nil {
			return err
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(resp)
		}

		data := resp.GetData()
		if data == nil || len(data.GetDataPoints()) == 0 {
			output.Info("No monthly spending data found.")
			return nil
		}

		var total float64
		for _, p := range data.GetDataPoints() {
			total += p.GetAmount()
		}

		output.KPICards([]output.KPICard{
			{Label: "Total", Value: fmt.Sprintf("$%.2f", total)},
			{Label: "Months", Value: fmt.Sprintf("%d", len(data.GetDataPoints()))},
		})

		headers := []string{"Month", "Amount"}
		rows := make([][]string, 0, len(data.GetDataPoints()))
		for _, p := range data.GetDataPoints() {
			rows = append(rows, []string{
				p.GetMonth(),
				fmt.Sprintf("$%.2f", p.GetAmount()),
			})
		}
		output.Table(headers, rows)
		return nil
	},
}

func init() {
	costsMonthlyCmd.Flags().StringVar(&flagMonthlyProvider, "provider", "", "Provider ID (aws, gcp, azure, k8s); auto-detected if omitted")
	costsCmd.AddCommand(costsMonthlyCmd)
}
