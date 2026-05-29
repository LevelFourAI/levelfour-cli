package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
	"github.com/spf13/cobra"
)

var (
	flagFiltersProvider string
	flagFiltersStart    string
	flagFiltersEnd      string
)

const dimRegion = "region"

var supportedFilterDimensions = []string{"service", dimRegion, "account", "tag-key", "tag-value"}

var costsFiltersCmd = &cobra.Command{
	Use:   "filters [dimension]",
	Short: "Discover available filter dimensions and values for cost breakdown",
	Long: `Lists filter dimensions and their distinct values available for the current tenant.

Without an argument, prints every supported dimension alongside its available values.
With a dimension argument, prints just that dimension's values.

Supported dimensions: ` + strings.Join(supportedFilterDimensions, ", ") + `.`,
	Example: `  l4 costs filters
  l4 costs filters service
  l4 costs filters region --start 2026-01-01 --end 2026-01-31
  l4 costs filters --json | jq '.data.services[]'`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			return openWeb("/spending")
		}

		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		ctx := context.Background()
		providerID, err := resolveProviderID(ctx, client, flagFiltersProvider)
		if err != nil {
			return err
		}

		req := &levelfourgo.GetProviderFiltersCostsRequest{}
		if flagFiltersStart != "" {
			req.Start = api.StringPtr(flagFiltersStart)
		}
		if flagFiltersEnd != "" {
			req.End = api.StringPtr(flagFiltersEnd)
		}

		resp, err := client.SDK().Costs.GetProviderFilters(ctx, providerID, req)
		if err != nil {
			return err
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(resp)
		}

		data := resp.GetData()
		if data == nil {
			output.Info("No filter options available.")
			return nil
		}

		if len(args) == 1 {
			return renderFilterDimension(args[0], data)
		}

		renderAllFilters(data)
		return nil
	},
}

func renderAllFilters(data *levelfourgo.ProviderFilterOptionsData) {
	output.KPICards([]output.KPICard{
		{Label: "Provider", Value: data.GetProviderID()},
		{Label: "Services", Value: fmt.Sprintf("%d", len(data.GetServices()))},
		{Label: "Regions", Value: fmt.Sprintf("%d", len(data.GetRegions()))},
		{Label: "Accounts", Value: fmt.Sprintf("%d", len(data.GetAccounts()))},
		{Label: "Tag Keys", Value: fmt.Sprintf("%d", len(data.GetTagKeys()))},
	})

	headers := []string{"Dimension", "Flag", "Count", "Sample"}
	rows := [][]string{
		{"service", "--service", fmt.Sprintf("%d", len(data.GetServices())), sampleValues(data.GetServices())},
		{dimRegion, "--region", fmt.Sprintf("%d", len(data.GetRegions())), sampleValues(data.GetRegions())},
		{"account", "--account", fmt.Sprintf("%d", len(data.GetAccounts())), sampleValues(data.GetAccounts())},
		{"tag-key", "--tag-key", fmt.Sprintf("%d", len(data.GetTagKeys())), sampleValues(data.GetTagKeys())},
		{"tag-value", "--tag-value", fmt.Sprintf("%d", len(data.GetTagValues())), sampleValues(data.GetTagValues())},
	}
	output.Table(headers, rows)
	fmt.Fprintln(output.Stdout)
	output.Info("Run 'l4 costs filters <dimension>' to list all values for a dimension.")
}

func renderFilterDimension(dim string, data *levelfourgo.ProviderFilterOptionsData) error {
	var values []string
	var label string

	switch strings.ToLower(dim) {
	case "service", "services":
		values = data.GetServices()
		label = columnService
	case dimRegion, "regions":
		values = data.GetRegions()
		label = "Region"
	case "account", "accounts", "account_id", "account-id":
		values = data.GetAccounts()
		label = columnAccount
	case "tag-key", "tag_key", "tagkey", "tag-keys", "tag_keys":
		values = data.GetTagKeys()
		label = "Tag Key"
	case "tag-value", "tag_value", "tagvalue", "tag-values", "tag_values":
		values = data.GetTagValues()
		label = "Tag Value"
	default:
		return fmt.Errorf("unknown dimension %q; supported: %s", dim, strings.Join(supportedFilterDimensions, ", "))
	}

	if len(values) == 0 {
		output.Info(fmt.Sprintf("No %s values available for this provider and date range.", label))
		return nil
	}

	sorted := append([]string(nil), values...)
	sort.Strings(sorted)

	headers := []string{label}
	rows := make([][]string, 0, len(sorted))
	for _, v := range sorted {
		rows = append(rows, []string{v})
	}
	output.Table(headers, rows)
	return nil
}

func sampleValues(values []string) string {
	if len(values) == 0 {
		return dashSymbol
	}
	limit := len(values)
	if limit > 3 {
		limit = 3
	}
	sample := strings.Join(values[:limit], ", ")
	if len(values) > 3 {
		sample += fmt.Sprintf(", … (+%d more)", len(values)-3)
	}
	return sample
}

func init() {
	costsFiltersCmd.Flags().StringVar(&flagFiltersProvider, "provider", "", "Provider ID (aws, gcp, azure, k8s); auto-detected if omitted")
	costsFiltersCmd.Flags().StringVar(&flagFiltersStart, "start", "", "Start date (ISO 8601)")
	costsFiltersCmd.Flags().StringVar(&flagFiltersEnd, "end", "", "End date (ISO 8601)")

	costsCmd.AddCommand(costsFiltersCmd)
}
