package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	tf "github.com/LevelFourAI/levelfour-cli/internal/terraform"
	"github.com/spf13/cobra"
)

var (
	flagDiffFormat          string
	flagDiffFailAbove       float64
	flagDiffRegion          string
	flagDiffGCPRegion       string
	flagDiffAzureRegion     string
	flagDiffVarFile         []string
	flagDiffVar             []string
	flagDiffMaxResources    int
	flagDiffDownloadModules bool
	flagDiffBase            string
)

var diffCmd = &cobra.Command{
	Use:   "diff [baseline.json] [path|file ...]",
	Short: "Show cost difference between current and baseline state",
	Long: `Show how your Terraform changes affect monthly cloud costs.

By default, compares the current branch against the git merge-base
(main or master); just point it at your .tf files, no setup needed.
Use --base to compare against a different git ref.

You can also pass a .json snapshot file created by 'l4 estimate --out-file'
for comparisons outside of git (e.g. standalone files or CI artifacts).`,
	Example: `- See cost impact of your changes

  $ l4 diff main.tf
  $ l4 diff ./infra/

- Compare against a specific branch

  $ l4 diff --base main ./infra/

- Compare against a saved snapshot (non-git workflow)

  $ l4 diff baseline.json`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		var baselineFile string
		var tfArgs []string

		for _, a := range args {
			if isJSONFile(a) {
				if baselineFile != "" {
					return fmt.Errorf("only one baseline .json file is supported")
				}
				baselineFile = a
			} else {
				tfArgs = append(tfArgs, a)
			}
		}

		dirs, files, err := classifyInputs(tfArgs)
		if err != nil {
			return err
		}

		projectCfg := config.LoadProjectConfig(".")

		opts := tf.ParseOptions{
			VarFiles:        flagDiffVarFile,
			Vars:            flagDiffVar,
			MaxResources:    flagDiffMaxResources,
			DownloadModules: flagDiffDownloadModules,
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

		output.InfoLabel(fmt.Sprintf("Found %d resources in %s", len(snapshots), buildProjectLabel(tfArgs)))

		detected := detectProviderRegions(dirs, files, opts)

		region := flagDiffRegion
		if !cmd.Flags().Changed("region") {
			if projectCfg.RegionOverride != nil {
				region = *projectCfg.RegionOverride
			} else if detected.AWS != "" {
				region = detected.AWS
			}
		}

		providerRegions := buildProviderRegions(
			region, flagDiffGCPRegion, flagDiffAzureRegion,
			projectCfg.ProviderRegions, detected,
			cmd.Flags().Changed("gcp-region"), cmd.Flags().Changed("azure-region"),
		)

		output.GravitonEnabled = projectCfg.GravitonForManagedServicesEnabled

		client, apiErr := newSDKClientFn()
		if apiErr != nil {
			return apiErr
		}

		var changes []api.ResourceChange
		if baselineFile != "" {
			baseline, loadErr := loadSnapshotsFromFile(baselineFile)
			if loadErr != nil {
				return fmt.Errorf("failed to load baseline: %w", loadErr)
			}
			changes = diffSnapshots(baseline, snapshots)
		} else {
			targetPath := "."
			if len(tfArgs) > 0 {
				targetPath = tfArgs[0]
			}
			baseline, gitErr := gitBaselineSnapshots(targetPath, flagDiffBase, opts)
			if gitErr != nil {
				output.Info(fmt.Sprintf("Git baseline unavailable (%s), showing all resources as added.", gitErr))
				changes = snapshotsToAddedChanges(snapshots)
			} else {
				baseline = filterExcludedResources(baseline, projectCfg.ExcludedResourceTypes)
				baseline = scopeBaselineToCurrentResources(baseline, snapshots)
				changes = diffSnapshots(baseline, snapshots)
			}
		}

		if len(changes) == 0 {
			output.Info("No resource changes detected.")
			return nil
		}

		usageDir := "."
		if len(tfArgs) > 0 {
			info, _ := os.Stat(tfArgs[0])
			if info != nil && info.IsDir() {
				usageDir = tfArgs[0]
			} else {
				usageDir = filepath.Dir(tfArgs[0])
			}
		}
		usageOverrides := loadUsageOverrides(usageDir)

		data, err := postAnalysis(client, snapshots, changes, region, providerRegions, usageOverrides)
		if err != nil {
			return err
		}

		if flagDiffFormat == formatJSON || output.HasFormattingFlags() {
			return output.PrintResult(data)
		}

		projectLabel := buildProjectLabel(tfArgs)

		if flagDiffFormat == "github-comment" {
			output.RenderMarkdown(output.Stdout, data, true)
			return checkFailAbove(data, flagDiffFailAbove)
		}

		output.RenderDiff(output.Stdout, data, projectLabel)
		return checkFailAbove(data, flagDiffFailAbove)
	},
}

func init() {
	diffCmd.Flags().StringVar(&flagDiffFormat, "format", "table", "Output format: table, json, github-comment")
	diffCmd.Flags().Float64Var(&flagDiffFailAbove, "fail-above", 0, "Exit code 2 if monthly delta exceeds this threshold")
	diffCmd.Flags().StringVar(&flagDiffRegion, "region", "us-east-1", "AWS region for pricing")
	diffCmd.Flags().StringVar(&flagDiffGCPRegion, "gcp-region", "", "GCP region for pricing (default: auto-detect or us-central1)")
	diffCmd.Flags().StringVar(&flagDiffAzureRegion, "azure-region", "", "Azure region for pricing (default: auto-detect or eastus)")
	diffCmd.Flags().StringArrayVar(&flagDiffVarFile, "var-file", nil, "Terraform variable files")
	diffCmd.Flags().StringArrayVar(&flagDiffVar, "var", nil, "Terraform variables (key=value)")
	diffCmd.Flags().IntVar(&flagDiffMaxResources, "max-resources", 500, "Maximum number of resources to include")
	_ = diffCmd.Flags().MarkHidden("max-resources")
	diffCmd.Flags().BoolVar(&flagDiffDownloadModules, "download-modules", true, "Download and resolve remote Terraform modules for accurate estimates")
	diffCmd.Flags().StringVar(&flagDiffBase, "base", "", "Git ref to diff against (default: auto-detect main/master)")
}
