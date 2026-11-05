package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
	"github.com/spf13/cobra"
)

var providersCmd = &cobra.Command{
	Use:     "providers",
	Aliases: []string{"integrations"},
	Short:   "Connected providers, and what is specific to each",
}

var providersListCmd = &cobra.Command{
	Use:   "list",
	Short: "List connected providers",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		resp, err := client.SDK().Providers.List(context.Background())
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				output.Info("This feature is not yet available for your account.")
				return nil
			}
			return err
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(resp)
		}

		providers := resp.GetData()
		if len(providers) == 0 {
			output.Info("No providers connected.")
			return nil
		}

		headers := []string{"ID", "Name"}
		var rows [][]string
		for _, p := range providers {
			rows = append(rows, []string{
				fmt.Sprintf("%v", p.GetProviderID()),
				fmt.Sprintf("%v", p.GetProviderName()),
			})
		}
		output.Table(headers, rows)
		return nil
	},
}

var providersViewCmd = &cobra.Command{
	Use:   "view <id>",
	Short: "What one connected provider costs, could save and bills most for",
	Args:  cobra.ExactArgs(1),
	Example: `  l4 providers view aws
  l4 providers view datadog --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			return openWeb(costsWebPath(args[0]))
		}
		client, err := newSDKClientFn()
		if err != nil {
			return err
		}
		resp, err := client.SDK().Costs.GetProviderSummary(context.Background(), args[0], &levelfourgo.GetProviderSummaryCostsRequest{})
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				return fmt.Errorf("%q is not a connected provider: 'l4 providers list' shows them", args[0])
			}
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(resp)
		}
		summary := resp.GetData()
		output.Header(summary.GetProviderName())
		renderCostsSummaryKPIs(summary, nil)
		renderTopServices(summary.GetTopServices())
		return nil
	},
}

func init() {
	providersCmd.AddCommand(providersListCmd, providersViewCmd, datadogCmd)
}
