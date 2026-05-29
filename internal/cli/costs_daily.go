package cli

import (
	"context"
	"fmt"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
	"github.com/spf13/cobra"
)

var (
	flagDailyProvider string
	flagDailyStart    string
	flagDailyEnd      string
)

var costsDailyCmd = &cobra.Command{
	Use:   "daily",
	Short: "Daily spending aggregated per day for the given date range",
	Example: `  l4 costs daily
  l4 costs daily --start 2026-01-01 --end 2026-01-31
  l4 costs daily --json | jq '.data.data_points[]'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			return openWeb("/spending")
		}

		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		req := &levelfourgo.GetDailyCostsCostsRequest{}
		if flagDailyStart != "" {
			req.Start = api.StringPtr(flagDailyStart)
		}
		if flagDailyEnd != "" {
			req.End = api.StringPtr(flagDailyEnd)
		}

		resp, err := client.SDK().Costs.GetDailyCosts(context.Background(), req)
		if err != nil {
			return err
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(resp)
		}

		data := resp.GetData()
		if data == nil || len(data.GetDataPoints()) == 0 {
			output.Info("No daily spending data found.")
			return nil
		}

		output.KPICards([]output.KPICard{
			{Label: "Total", Value: fmt.Sprintf("$%.2f", data.GetTotal())},
			{Label: "Range", Value: formatRangeLabel(data.GetStartDate(), data.GetEndDate())},
			{Label: "Days", Value: fmt.Sprintf("%d", len(data.GetDataPoints()))},
		})

		headers := []string{"Date", "Amount"}
		rows := make([][]string, 0, len(data.GetDataPoints()))
		for _, p := range data.GetDataPoints() {
			rows = append(rows, []string{
				p.GetDate(),
				fmt.Sprintf("$%.2f", p.GetAmount()),
			})
		}
		output.Table(headers, rows)
		return nil
	},
}

func init() {
	costsDailyCmd.Flags().StringVar(&flagDailyProvider, "provider", "", "Provider ID (aws, gcp, azure, k8s); auto-detected if omitted")
	costsDailyCmd.Flags().StringVar(&flagDailyStart, "start", "", "Start date (ISO 8601)")
	costsDailyCmd.Flags().StringVar(&flagDailyEnd, "end", "", "End date (ISO 8601)")

	costsCmd.AddCommand(costsDailyCmd)
}
