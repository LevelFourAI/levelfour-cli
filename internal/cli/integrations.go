package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var integrationsCmd = &cobra.Command{
	Use:   "integrations",
	Short: "View connected cloud providers",
}

var integrationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List connected cloud providers",
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

func init() {
	integrationsCmd.AddCommand(integrationsListCmd)
}
