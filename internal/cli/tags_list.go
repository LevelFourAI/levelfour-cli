package cli

import (
	"strconv"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var tagsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List provider and virtual tag keys with their spend",
	Long: `List provider and virtual tag keys with their spend.

Spend and share cover the last 30 complete days unless --start and --end set
another window.`,
	Args: cobra.NoArgs,
	Example: `  l4 tags list
  l4 tags list --origin virtual
  l4 tags list --provider gcp --search team
  l4 tags list --start 2026-08-01 --end 2026-08-31`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runTagsList()
	},
}

func runTagsList() error {
	if err := validateChoice(paramOrigin, flagTagsOrigin, tagOrigins); err != nil {
		return err
	}
	if err := validateChoice(paramProvider, flagTagsProvider, tagProviders); err != nil {
		return err
	}
	if err := validateWindow(); err != nil {
		return err
	}
	params := windowParams()
	setParam(params, paramOrigin, flagTagsOrigin)
	setParam(params, paramProvider, flagTagsProvider)
	setParam(params, paramSearch, flagTagsSearch)
	envelope, rows, err := listTagKeys(params)
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	if len(rows) == 0 {
		output.Info("No tag keys found.")
		return nil
	}
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{
			r.Name,
			r.ID,
			r.Origin,
			orDash(strings.Join(r.Providers, ", ")),
			strconv.Itoa(r.ValueCount),
			strconv.Itoa(r.ResourceCount),
			formatSpend(r.Spend),
			formatShare(r.SpendSharePct),
			orDash(r.Status),
		})
	}
	output.Table([]string{"Name", "ID", "Origin", "Providers", "Values", "Resources", "Spend", "Share", "Status"}, tableRows)
	return nil
}

func init() {
	tagsListCmd.Flags().StringVar(&flagTagsOrigin, paramOrigin, "", "Key origin: "+strings.Join(tagOrigins, ", "))
	addProviderFlag(tagsListCmd, tagProviders)
	tagsListCmd.Flags().StringVar(&flagTagsSearch, paramSearch, "", "Filter keys by name")
	addWindowFlags(tagsListCmd)
	tagsCmd.AddCommand(tagsListCmd)
}
