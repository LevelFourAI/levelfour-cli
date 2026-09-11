package cli

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var tagsPreviewCmd = &cobra.Command{
	Use:   "preview -f FILE",
	Short: "Try a virtual tag definition against real spend without saving it",
	Long: `Try a virtual tag definition against real spend without saving it.

The file uses the format 'l4 tags apply' reads, and its name is optional here.
The window is at most 31 days, the last 30 complete days by default. Shadowed
spend is cost a later config also matches but an earlier one already claimed.`,
	Args: cobra.NoArgs,
	Example: `  l4 tags preview -f teams.yaml
  l4 tags preview -f teams.yaml --start 2026-08-01 --end 2026-08-31`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runTagsPreview()
	},
}

const previewSamples = 3

type tagPreviewRequest struct {
	tagDefinition
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

type tagPreviewValue struct {
	Name            string   `json:"name"`
	Spend           float64  `json:"spend"`
	ResourceCount   int      `json:"resource_count"`
	SampleResources []string `json:"sample_resources"`
}

type tagPreviewResult struct {
	Window           tagWindow         `json:"window"`
	TotalSpend       float64           `json:"total_spend"`
	UnallocatedSpend float64           `json:"unallocated_spend"`
	ShadowedSpend    float64           `json:"shadowed_spend"`
	Values           []tagPreviewValue `json:"values"`
}

func runTagsPreview() error {
	if err := validateWindow(); err != nil {
		return err
	}
	def, err := loadTagDefinition(flagTagsFile)
	if err != nil {
		return err
	}
	body := tagPreviewRequest{tagDefinition: def, Start: flagTagsStart, End: flagTagsEnd}
	// A preview stores nothing, so it carries no Idempotency-Key.
	envelope, err := sendJSON(http.MethodPost, virtualTagsPath+"/preview", body, nil)
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	var result tagPreviewResult
	if err := decodeData(envelope, &result); err != nil {
		return err
	}
	output.KeyValue("Window", windowLabel(result.Window))
	output.Info("")
	output.KPICards([]output.KPICard{
		{Label: "Total spend", Value: formatSpend(result.TotalSpend)},
		{Label: "Unallocated", Value: formatSpend(result.UnallocatedSpend)},
		{Label: "Shadowed", Value: formatSpend(result.ShadowedSpend)},
	})
	if len(result.Values) == 0 {
		output.Info("No config matched any spend in the window.")
		return nil
	}
	rows := make([][]string, 0, len(result.Values))
	for _, v := range result.Values {
		samples := v.SampleResources[:min(previewSamples, len(v.SampleResources))]
		rows = append(rows, []string{v.Name, formatSpend(v.Spend), strconv.Itoa(v.ResourceCount), orDash(strings.Join(samples, ", "))})
	}
	output.Table([]string{"Value", "Spend", "Resources", "Sample resources"}, rows)
	return nil
}

func init() {
	tagsPreviewCmd.Flags().StringVarP(&flagTagsFile, "file", "f", "", "YAML file holding the virtual tag definition")
	addWindowFlags(tagsPreviewCmd)
	tagsCmd.AddCommand(tagsPreviewCmd)
}
