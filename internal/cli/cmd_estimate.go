package cli

import (
	"fmt"
	"path/filepath"

	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	tf "github.com/LevelFourAI/levelfour-cli/internal/terraform"
	"github.com/spf13/cobra"
)

var (
	flagEstNewFormat          string
	flagEstNewOutFile         string
	flagEstNewFailAbove       float64
	flagEstNewRegion          string
	flagEstNewGCPRegion       string
	flagEstNewAzureRegion     string
	flagEstNewVarFile         []string
	flagEstNewVar             []string
	flagEstNewMaxResources    int
	flagEstNewDownloadModules bool
)

var estimateNewCmd = &cobra.Command{
	Use:   "estimate [path|file ...]",
	Short: "Estimate infrastructure costs and show optimization opportunities",
	Long: `Parse Terraform files locally and estimate monthly cloud costs
before resources are created or modified.

Accepts directories, individual .tf files, or a mix of both.
Use 'l4 diff' to compare costs against your git branch; no snapshot needed.
Use --out-file only when you need a baseline outside of git (e.g. standalone
files not in a repo, or saving a CI artifact to diff in a later pipeline step).`,
	Example: `- Estimate current directory

  $ l4 estimate

- Estimate a specific directory or file

  $ l4 estimate ./infra/
  $ l4 estimate main.tf

- Save a snapshot for non-git comparisons

  $ l4 estimate --out-file baseline.json
  $ l4 diff baseline.json

- Fail in CI if monthly cost exceeds threshold

  $ l4 estimate --fail-above 500 -q`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dirs, files, err := classifyInputs(args)
		if err != nil {
			return err
		}

		projectCfg := config.LoadProjectConfig(".")

		opts := tf.ParseOptions{
			VarFiles:        flagEstNewVarFile,
			Vars:            flagEstNewVar,
			MaxResources:    flagEstNewMaxResources,
			DownloadModules: flagEstNewDownloadModules,
		}

		snapshots, warnings, err := parseSnapshots(dirs, files, opts)
		if err != nil {
			return fmt.Errorf("failed to parse Terraform files: %w", err)
		}

		snapshots = filterExcludedResources(snapshots, projectCfg.ExcludedResourceTypes)

		for _, w := range warnings {
			output.Warning(w)
		}

		if len(snapshots) == 0 {
			output.Info("No Terraform resources found.")
			return nil
		}

		output.InfoLabel(fmt.Sprintf("Found %d resources in %s", len(snapshots), buildProjectLabel(args)))

		detected := detectProviderRegions(dirs, files, opts)

		region := flagEstNewRegion
		if !cmd.Flags().Changed("region") {
			if projectCfg.RegionOverride != nil {
				region = *projectCfg.RegionOverride
			} else if detected.AWS != "" {
				region = detected.AWS
			}
		}

		providerRegions := buildProviderRegions(
			region, flagEstNewGCPRegion, flagEstNewAzureRegion,
			projectCfg.ProviderRegions, detected,
			cmd.Flags().Changed("gcp-region"), cmd.Flags().Changed("azure-region"),
		)

		output.GravitonEnabled = projectCfg.GravitonForManagedServicesEnabled

		client, apiErr := newSDKClientFn()
		if apiErr != nil {
			return apiErr
		}

		changes := snapshotsToAddedChanges(snapshots)

		usageDir := "."
		if len(dirs) > 0 {
			usageDir = dirs[0]
		} else if len(files) > 0 {
			usageDir = filepath.Dir(files[0])
		}
		usageOverrides := loadUsageOverrides(usageDir)

		data, err := postAnalysis(client, snapshots, changes, region, providerRegions, usageOverrides)
		if err != nil {
			return err
		}

		if flagEstNewOutFile != "" {
			if writeErr := saveSnapshots(flagEstNewOutFile, snapshots); writeErr != nil {
				return writeErr
			}
		}

		if flagEstNewFormat == formatJSON || output.HasFormattingFlags() {
			return output.PrintResult(data)
		}

		projectLabel := buildProjectLabel(args)

		if flagEstNewFormat == "github-comment" {
			output.RenderMarkdown(output.Stdout, data, false)
			return checkFailAbove(data, flagEstNewFailAbove)
		}

		output.RenderBreakdown(output.Stdout, data, projectLabel)
		return checkFailAbove(data, flagEstNewFailAbove)
	},
}

func init() {
	estimateNewCmd.Flags().StringVar(&flagEstNewFormat, "format", "table", "Output format: table, json, github-comment")
	estimateNewCmd.Flags().StringVar(&flagEstNewOutFile, "out-file", "", "Save resource snapshot for later use with 'l4 diff'")
	estimateNewCmd.Flags().Float64Var(&flagEstNewFailAbove, "fail-above", 0, "Exit code 2 if monthly cost exceeds this threshold")
	estimateNewCmd.Flags().StringVar(&flagEstNewRegion, "region", "us-east-1", "AWS region for pricing")
	estimateNewCmd.Flags().StringVar(&flagEstNewGCPRegion, "gcp-region", "", "GCP region for pricing (default: auto-detect or us-central1)")
	estimateNewCmd.Flags().StringVar(&flagEstNewAzureRegion, "azure-region", "", "Azure region for pricing (default: auto-detect or eastus)")
	estimateNewCmd.Flags().StringArrayVar(&flagEstNewVarFile, "var-file", nil, "Terraform variable files")
	estimateNewCmd.Flags().StringArrayVar(&flagEstNewVar, "var", nil, "Terraform variables (key=value)")
	estimateNewCmd.Flags().IntVar(&flagEstNewMaxResources, "max-resources", 500, "Maximum number of resources to include")
	_ = estimateNewCmd.Flags().MarkHidden("max-resources")
	estimateNewCmd.Flags().BoolVar(&flagEstNewDownloadModules, "download-modules", true, "Download and resolve remote Terraform modules for accurate estimates")
}
