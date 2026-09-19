package cli

import (
	"fmt"
	"strconv"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var tagsCoverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "How much spend and how many resources carry a tag",
	Long: `How much spend and how many resources carry a tag, with provider tags alone
and with virtual tags added.`,
	Args: cobra.NoArgs,
	Example: `  l4 tags coverage
  l4 tags coverage --provider aws --start 2026-08-01 --end 2026-08-31`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runTagsCoverage()
	},
}

type tagCoverageBasis struct {
	Total             float64 `json:"total"`
	ProviderTagged    float64 `json:"provider_tagged"`
	WithVirtual       float64 `json:"with_virtual"`
	ProviderTaggedPct int     `json:"provider_tagged_pct"`
	WithVirtualPct    int     `json:"with_virtual_pct"`
}

type tagCoverage struct {
	Window     tagWindow        `json:"window"`
	BySpend    tagCoverageBasis `json:"by_spend"`
	ByResource tagCoverageBasis `json:"by_resource"`
}

func runTagsCoverage() error {
	if err := validateChoice(paramProvider, flagTagsProvider, tagProviders); err != nil {
		return err
	}
	if err := validateWindow(); err != nil {
		return err
	}
	params := windowParams()
	setParam(params, paramProvider, flagTagsProvider)
	envelope, err := getJSON(withQuery("/api/v1/tags/coverage", params))
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	var coverage tagCoverage
	if err := decodeData(envelope, &coverage); err != nil {
		return err
	}
	output.KeyValue("Window", windowLabel(coverage.Window))
	output.Table(
		[]string{"Basis", "Total", "Provider tags", "With virtual tags", "Untagged"},
		[][]string{
			coverageRow("Spend", coverage.BySpend, formatSpend),
			coverageRow("Resources", coverage.ByResource, formatCount),
		},
	)
	return nil
}

// The server rounds each tagged share once, so untagged is 100 minus it, as the dashboard shows.
func coverageRow(label string, b tagCoverageBasis, format func(float64) string) []string {
	return []string{
		label,
		format(b.Total),
		fmt.Sprintf("%s (%d%%)", format(b.ProviderTagged), b.ProviderTaggedPct),
		fmt.Sprintf("%s (%d%%)", format(b.WithVirtual), b.WithVirtualPct),
		fmt.Sprintf("%d%%", 100-b.WithVirtualPct),
	}
}

func formatCount(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64)
}

func init() {
	addProviderFlag(tagsCoverageCmd, tagProviders)
	addWindowFlags(tagsCoverageCmd)
	tagsCmd.AddCommand(tagsCoverageCmd)
}
