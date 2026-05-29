package cli

import (
	"context"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show API health and evaluation status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		resp, err := client.SDK().Health.HealthReady(context.Background())
		if err != nil {
			output.Error("API unreachable: " + err.Error())
			return nil
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(resp)
		}

		output.Header("API Status")
		output.KeyValue("Status", output.StatusBadge(resp.Status))
		output.KeyValue("Base URL", client.BaseURL)
		return nil
	},
}
