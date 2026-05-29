package cli

import (
	"context"
	"fmt"

	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var whoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show current identity and organization context",
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			return openWeb("/settings")
		}

		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		resp, err := client.SDK().Auth.GetWhoami(context.Background())
		if err != nil {
			return err
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(resp)
		}

		data := resp.GetData()

		org := data.GetOrganization()
		if org == "" {
			org = "N/A"
		}

		key, source := resolveToken()
		baseURL := config.ResolveAPI(flagAPI)

		output.Header("Organization")
		output.KeyValue("Organization", org)
		output.KeyValue("Plan", data.GetPlan())
		output.KeyValue("Role", data.GetRole())

		if len(data.GetAccounts()) > 0 {
			output.KeyValue("Accounts", fmt.Sprintf("%d connected", len(data.GetAccounts())))
			for _, a := range data.GetAccounts() {
				fmt.Fprintf(output.Stdout, "    • %-15s (%s %s)\n", a.GetName(), a.GetProvider(), a.GetAccountID())
			}
		}

		fmt.Fprintln(output.Stdout)
		output.Header("Local")
		output.KeyValue("API", baseURL)
		if key != "" {
			output.KeyValue("Auth", fmt.Sprintf("%s (%s)", source, maskKey(key)))
		}
		output.KeyValue("CLI", fmt.Sprintf("v%s (commit: %s)", Version, Commit))
		return nil
	},
}
