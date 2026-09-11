package cli

import (
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var tagsCostsCmd = &cobra.Command{
	Use:   "costs <key>",
	Short: "Spend per value of a tag key",
	Long: `Spend per value of a tag key, and the spend that holds no value.

<key> goes to the API as given: a provider tag key's name, or a virtual tag
key's name or id. The API reads AWS unless --provider says otherwise.`,
	Args: cobra.ExactArgs(1),
	Example: `  l4 tags costs Teams --provider all
  l4 tags costs team --provider aws
  l4 tags costs Teams --provider all --start 2026-08-01 --end 2026-08-31`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runTagsCosts(args[0])
	},
}

type tagCostItem struct {
	Label    string  `json:"label"`
	Total    float64 `json:"total"`
	TotalPct float64 `json:"total_pct"`
}

type tagCostData struct {
	TagKey   string        `json:"tag_key"`
	Teams    []tagCostItem `json:"teams"`
	Unmapped struct {
		Total float64 `json:"total"`
	} `json:"unmapped"`
}

func runTagsCosts(ref string) error {
	if err := validateChoice(paramProvider, flagTagsProvider, tagCostProviders); err != nil {
		return err
	}
	if err := validateWindow(); err != nil {
		return err
	}
	params := windowParams()
	params.Set("tag_key", ref)
	setParam(params, paramProvider, flagTagsProvider)
	envelope, err := getJSON(withQuery("/api/v1/costs/by-tag", params))
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	var data tagCostData
	if err := decodeData(envelope, &data); err != nil {
		return err
	}
	allocated := 0.0
	rows := make([][]string, 0, len(data.Teams))
	for _, item := range data.Teams {
		allocated += item.Total
		rows = append(rows, []string{item.Label, formatSpend(item.Total), formatShare(item.TotalPct)})
	}
	output.KeyValue("Tag key", data.TagKey)
	output.Info("")
	output.KPICards([]output.KPICard{
		{Label: "Allocated", Value: formatSpend(allocated)},
		{Label: "Unallocated", Value: formatSpend(data.Unmapped.Total)},
	})
	if len(rows) == 0 {
		output.Info("No spend holds a value of this key in the window.")
		return nil
	}
	output.Table([]string{"Value", "Spend", "Share"}, rows)
	return nil
}

func init() {
	addProviderFlag(tagsCostsCmd, tagCostProviders)
	addWindowFlags(tagsCostsCmd)
	tagsCmd.AddCommand(tagsCostsCmd)
}
