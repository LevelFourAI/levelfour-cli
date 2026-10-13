package cli

import (
	cfn "github.com/LevelFourAI/levelfour-cli/internal/cloudformation"
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
	flagEstNewParameter       []string
	flagEstNewParametersFile  string
	flagEstNewConfigEnv       string
)

var estimateNewCmd = &cobra.Command{
	Use:   "estimate [path|file ...]",
	Short: "Estimate infrastructure costs and show optimization opportunities",
	Long: `Estimate the monthly cloud cost of Terraform and CloudFormation before
resources are created or modified.

Accepts directories, individual .tf files, CloudFormation templates
(.yaml, .yml, .json, .template), or a mix. A directory is read recursively:
every Terraform root module in it is priced once, and every CloudFormation
template in it is found.

Terraform is parsed locally. CloudFormation templates are uploaded to the
LevelFour API to be evaluated and priced, together with the files they need:
nested stack templates, included snippets, samconfig files and the
--parameters-file. Nothing outside the project (the git repository, or the
current directory) is uploaded.

Use 'l4 diff' to compare costs against your git branch; no snapshot needed.
Use --out-file only when you need a Terraform baseline outside of git (e.g.
standalone files not in a repo, or saving a CI artifact to diff in a later
pipeline step).`,
	Example: `- Estimate current directory

  $ l4 estimate

- Estimate a specific directory or file

  $ l4 estimate ./infra/
  $ l4 estimate main.tf
  $ l4 estimate template.yaml

- Price a template with a parameter value

  $ l4 estimate template.yaml --parameter InstanceType=m6i.large

- Save a snapshot for non-git comparisons

  $ l4 estimate --out-file baseline.json
  $ l4 diff baseline.json

- Fail in CI if monthly cost exceeds threshold

  $ l4 estimate --fail-above 500 -q`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := tf.ParseOptions{
			VarFiles:        flagEstNewVarFile,
			Vars:            flagEstNewVar,
			MaxResources:    flagEstNewMaxResources,
			DownloadModules: flagEstNewDownloadModules,
		}
		in, err := readAnalysisInputs(args, opts, regionFlagsOf(cmd, flagEstNewRegion, flagEstNewGCPRegion, flagEstNewAzureRegion))
		if err != nil {
			return err
		}
		if in.empty() {
			output.Info("No Terraform or CloudFormation resources found.")
			return nil
		}
		projectLabel := buildProjectLabel(args)
		output.InfoLabel(in.foundLabel(projectLabel))

		cfnInput, err := buildCloudFormationInput(in.root, in.templates, cfn.Result{}, cfnFlags{
			parameters:     flagEstNewParameter,
			parametersFile: flagEstNewParametersFile,
			configEnv:      flagEstNewConfigEnv,
			region:         givenRegion(cmd, flagEstNewRegion),
		}, in.config.ExcludedResourceTypes)
		if err != nil {
			return err
		}

		announceUpload(cfnInput)
		client, apiErr := newSDKClientFn()
		if apiErr != nil {
			return apiErr
		}
		data, err := postAnalysis(client, in.request(snapshotsToAddedChanges(in.terraform), cfnInput))
		if err != nil {
			return err
		}

		if flagEstNewOutFile != "" {
			if writeErr := saveSnapshots(flagEstNewOutFile, in.terraform); writeErr != nil {
				return writeErr
			}
		}

		return renderAnalysis(data, renderOptions{format: flagEstNewFormat, projectLabel: projectLabel, failAbove: flagEstNewFailAbove})
	},
}

func init() {
	estimateNewCmd.Flags().StringVar(&flagEstNewFormat, "format", "table", "Output format: table, json, github-comment")
	estimateNewCmd.Flags().StringVar(&flagEstNewOutFile, "out-file", "", "Save the Terraform resource snapshot for later use with 'l4 diff'")
	estimateNewCmd.Flags().Float64Var(&flagEstNewFailAbove, "fail-above", 0, "Exit code 2 if monthly cost exceeds this threshold")
	estimateNewCmd.Flags().StringVar(&flagEstNewRegion, "region", "", "AWS region for pricing (default: Terraform reads the project config or a provider block, CloudFormation a template's samconfig, then us-east-1)")
	estimateNewCmd.Flags().StringVar(&flagEstNewGCPRegion, "gcp-region", "", "GCP region for pricing (default: auto-detect or us-central1)")
	estimateNewCmd.Flags().StringVar(&flagEstNewAzureRegion, "azure-region", "", "Azure region for pricing (default: auto-detect or eastus)")
	estimateNewCmd.Flags().StringArrayVar(&flagEstNewVarFile, "var-file", nil, "Terraform variable files")
	estimateNewCmd.Flags().StringArrayVar(&flagEstNewVar, "var", nil, "Terraform variables (key=value)")
	estimateNewCmd.Flags().IntVar(&flagEstNewMaxResources, "max-resources", 500, "Maximum number of resources to include")
	_ = estimateNewCmd.Flags().MarkHidden("max-resources")
	estimateNewCmd.Flags().BoolVar(&flagEstNewDownloadModules, "download-modules", true, "Download and resolve remote Terraform modules for accurate estimates")
	estimateNewCmd.Flags().StringArrayVar(&flagEstNewParameter, "parameter", nil, "CloudFormation parameter value (Key=Value), for every template that declares Key")
	estimateNewCmd.Flags().StringVar(&flagEstNewParametersFile, "parameters-file", "", "CloudFormation parameters file (CLI JSON or CodePipeline format), uploaded with the templates")
	estimateNewCmd.Flags().StringVar(&flagEstNewConfigEnv, "config-env", "default", "samconfig environment whose parameters and region apply")
}
