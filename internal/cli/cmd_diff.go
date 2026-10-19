package cli

import (
	"fmt"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	cfn "github.com/LevelFourAI/levelfour-cli/internal/cloudformation"
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
	flagDiffParameter       []string
	flagDiffParametersFile  string
	flagDiffConfigEnv       string
)

var diffCmd = &cobra.Command{
	Use:   "diff [baseline.json] [path|file ...]",
	Short: "Show cost difference between current and baseline state",
	Long: `Show how your Terraform and CloudFormation changes affect monthly cloud costs.

By default, compares the current branch against the git merge-base
(main or master); just point it at your .tf files or templates, no setup
needed. Use --base to compare against a different git ref.

CloudFormation templates are uploaded to the LevelFour API to be evaluated
and priced, as they are at the merge-base and as they are now, together with
the files they need: nested stack templates, included snippets, samconfig
files and the --parameters-file. Nothing outside the git repository is
uploaded.

You can also pass a .json snapshot file created by 'l4 estimate --out-file'
for Terraform comparisons outside of git (e.g. standalone files or CI
artifacts). Against a snapshot file, CloudFormation templates are priced as
added.`,
	Example: `- See cost impact of your changes

  $ l4 diff main.tf
  $ l4 diff ./infra/
  $ l4 diff template.yaml

- Compare against a specific branch

  $ l4 diff --base main ./infra/

- Compare against a saved snapshot (non-git workflow)

  $ l4 diff baseline.json`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		output.DocumentOnStdout = writesDocument(flagDiffFormat)
		baseline, paths, err := splitBaselineArg(args)
		if err != nil {
			return err
		}
		opts := tf.ParseOptions{
			VarFiles:        flagDiffVarFile,
			Vars:            flagDiffVar,
			MaxResources:    flagDiffMaxResources,
			DownloadModules: flagDiffDownloadModules,
		}
		in, err := readAnalysisInputs(paths, opts, regionFlagsOf(cmd, flagDiffRegion, flagDiffGCPRegion, flagDiffAzureRegion))
		if err != nil {
			return err
		}
		// The baseline is read before deciding there is nothing to price: a
		// branch that deletes every resource has an empty head and a full base.
		base, err := diffAgainstBaseline(in, baseline, opts)
		if err != nil {
			return err
		}
		changes := base.changes
		if in.empty() && len(changes) == 0 && len(base.templates.Templates) == 0 {
			output.Info("No Terraform or CloudFormation resources found.")
			return nil
		}
		projectLabel := buildProjectLabel(paths)
		output.InfoLabel(in.foundLabel(projectLabel))
		cfnInput, err := buildCloudFormationInput(in.root, in.templates, base.templates, cfnOptions{
			parameters:     flagDiffParameter,
			parametersFile: flagDiffParametersFile,
			configEnv:      flagDiffConfigEnv,
			region:         templateRegion(cmd, flagDiffRegion, in.config.RegionOverride),
			excluded:       in.config.ExcludedResourceTypes,
			renamed:        base.renamed,
		})
		if err != nil {
			return err
		}
		if len(changes) == 0 && cfnInput == nil {
			output.Info("No resource changes detected.")
			return nil
		}

		announceUpload(cfnInput)
		client, apiErr := newSDKClientFn()
		if apiErr != nil {
			return apiErr
		}
		data, err := postAnalysis(client, in.request(changes, cfnInput))
		if err != nil {
			return err
		}
		return renderAnalysis(data, renderOptions{format: flagDiffFormat, projectLabel: projectLabel, isDiff: true, failAbove: flagDiffFailAbove})
	},
}

// splitBaselineArg separates a snapshot saved by 'l4 estimate --out-file' from
// the paths to compare, and loads it. A .json argument that is not one is a
// template. The snapshot is nil when there is none.
func splitBaselineArg(args []string) (*[]api.ResourceSnapshot, []string, error) {
	var baseline *[]api.ResourceSnapshot
	var paths []string
	for _, a := range args {
		snapshots, ok := loadSnapshotFile(a)
		if !ok {
			paths = append(paths, a)
			continue
		}
		if baseline != nil {
			return nil, nil, fmt.Errorf("only one baseline .json file is supported")
		}
		baseline = &snapshots
	}
	return baseline, paths, nil
}

// comparisonBase is what the working tree is compared against.
type comparisonBase struct {
	changes   []api.ResourceChange
	templates cfn.Result
	// renamed maps a template's path at the baseline commit to its path now.
	renamed map[string]string
}

// diffAgainstBaseline returns the Terraform changes and the CloudFormation
// templates as they were at the baseline: the snapshot when one is given, the
// git merge-base otherwise. Without a baseline everything reads as added.
func diffAgainstBaseline(in *analysisInputs, snapshot *[]api.ResourceSnapshot, opts tf.ParseOptions) (comparisonBase, error) {
	if snapshot != nil {
		return comparisonBase{changes: diffSnapshots(*snapshot, in.terraform)}, nil
	}

	sha, err := resolveBaseCommit(in.root, flagDiffBase)
	if err != nil {
		return allAdded(in, err), nil
	}
	baseline, err := gitBaseline(in.root, sha, in.targets, opts)
	if err != nil {
		return allAdded(in, err), nil
	}
	templates, err := cfn.Collect(baseTree(in.root, sha), in.cfnTargets)
	if err != nil {
		return comparisonBase{}, fmt.Errorf("failed to read CloudFormation templates at %s: %w", sha, err)
	}
	renamed, err := templateRenames(in.root, sha, in.cfnTargets)
	if err != nil {
		return comparisonBase{}, fmt.Errorf("failed to find renamed templates at %s: %w", sha, err)
	}
	baseline = filterExcludedResources(baseline, in.config.ExcludedResourceTypes)
	return comparisonBase{changes: diffSnapshots(baseline, in.terraform), templates: templates, renamed: renamed}, nil
}

// templateRenames asks git for renames only where templates are read.
func templateRenames(root, sha string, cfnTargets []string) (map[string]string, error) {
	if len(cfnTargets) == 0 {
		return nil, nil
	}
	return gitRenames(root, sha, cfnTargets)
}

// baseTree reads the project at the baseline commit. A variable so a test can
// make that read fail.
var baseTree = func(root, sha string) cfn.Tree { return gitTree{repoRoot: root, sha: sha} }

func allAdded(in *analysisInputs, cause error) comparisonBase {
	output.Info(fmt.Sprintf("Git baseline unavailable (%s), showing all resources as added.", cause))
	return comparisonBase{changes: snapshotsToAddedChanges(in.terraform)}
}

func init() {
	diffCmd.Flags().StringVar(&flagDiffFormat, "format", formatTable, "Output format: table, json, github-comment")
	diffCmd.Flags().Float64Var(&flagDiffFailAbove, "fail-above", 0, "Exit code 2 if monthly delta exceeds this threshold")
	diffCmd.Flags().StringVar(&flagDiffRegion, "region", "", "AWS region for pricing (default: the project config's region_override, then a Terraform provider block or a template's samconfig, then us-east-1)")
	diffCmd.Flags().StringVar(&flagDiffGCPRegion, "gcp-region", "", "GCP region for pricing (default: auto-detect or us-central1)")
	diffCmd.Flags().StringVar(&flagDiffAzureRegion, "azure-region", "", "Azure region for pricing (default: auto-detect or eastus)")
	diffCmd.Flags().StringArrayVar(&flagDiffVarFile, "var-file", nil, "Terraform variable files")
	diffCmd.Flags().StringArrayVar(&flagDiffVar, "var", nil, "Terraform variables (key=value)")
	diffCmd.Flags().IntVar(&flagDiffMaxResources, "max-resources", 500, "Maximum number of resources to include")
	_ = diffCmd.Flags().MarkHidden("max-resources")
	diffCmd.Flags().BoolVar(&flagDiffDownloadModules, "download-modules", true, "Download and resolve remote Terraform modules for accurate estimates")
	diffCmd.Flags().StringVar(&flagDiffBase, "base", "", "Git ref to diff against (default: auto-detect main/master)")
	diffCmd.Flags().StringArrayVar(&flagDiffParameter, "parameter", nil, "CloudFormation parameter value (Key=Value) for the root templates that declare Key; a nested stack gets what its parent passes")
	diffCmd.Flags().StringVar(&flagDiffParametersFile, "parameters-file", "", "CloudFormation parameters file (CLI JSON or CodePipeline format) for the root templates, uploaded with them")
	diffCmd.Flags().StringVar(&flagDiffConfigEnv, "config-env", "default", "samconfig environment whose parameters and region apply")
}
