package cli

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	cfn "github.com/LevelFourAI/levelfour-cli/internal/cloudformation"
	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	tf "github.com/LevelFourAI/levelfour-cli/internal/terraform"
	"github.com/spf13/cobra"
)

// regionFlags are the region flags estimate and diff share.
type regionFlags struct {
	aws, gcp, azure                      string
	awsChanged, gcpChanged, azureChanged bool
}

func regionFlagsOf(cmd *cobra.Command, aws, gcp, azure string) regionFlags {
	return regionFlags{
		aws: aws, gcp: gcp, azure: azure,
		awsChanged:   cmd.Flags().Changed("region"),
		gcpChanged:   cmd.Flags().Changed("gcp-region"),
		azureChanged: cmd.Flags().Changed("azure-region"),
	}
}

// collectTemplates is a variable so a test can make reading templates fail.
var collectTemplates = cfn.Collect

// givenRegion is the --region flag when the user passed it. Unlike the
// Terraform region it never falls back to the project config or a provider
// block, so a template is priced in its samconfig region unless told otherwise.
func givenRegion(cmd *cobra.Command, value string) string {
	if cmd.Flags().Changed("region") {
		return value
	}
	return ""
}

// analysisInputs is what estimate and diff read before they ask the API.
type analysisInputs struct {
	root            string
	targets         []iacTarget
	cfnTargets      []string
	terraform       []api.ResourceSnapshot
	templates       cfn.Result
	region          string
	providerRegions map[string]string
	usage           map[string]interface{}
	config          config.ProjectConfig
}

func readAnalysisInputs(args []string, opts tf.ParseOptions, regions regionFlags) (*analysisInputs, error) {
	root, targets, err := resolveTargets(args)
	if err != nil {
		return nil, err
	}
	if err := checkFileTargets(targets); err != nil {
		return nil, err
	}
	projectCfg := config.LoadProjectConfig(".")

	snapshots, warnings, err := collectTerraform(root, targets, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Terraform files: %w", err)
	}
	snapshots = filterExcludedResources(snapshots, projectCfg.ExcludedResourceTypes)

	cfnTargets, skipped := cloudFormationTargets(targets)
	templates, err := collectTemplates(newProjectTree(root), cfnTargets)
	if err != nil {
		return nil, fmt.Errorf("failed to read CloudFormation templates: %w", err)
	}
	for _, w := range append(append(warnings, skipped...), templates.Warnings...) {
		output.Warning(w)
	}

	detected := detectProviderRegions(targets, opts)
	region := pricingRegion(regions.aws, regions.awsChanged, projectCfg.RegionOverride, detected.AWS)
	output.GravitonEnabled = projectCfg.GravitonForManagedServicesEnabled

	return &analysisInputs{
		root:       root,
		targets:    targets,
		cfnTargets: cfnTargets,
		terraform:  snapshots,
		templates:  templates,
		region:     region,
		providerRegions: buildProviderRegions(
			region, regions.gcp, regions.azure, projectCfg.ProviderRegions, detected,
			regions.gcpChanged, regions.azureChanged,
		),
		usage:  loadUsageOverrides(usageDir(targets)),
		config: projectCfg,
	}, nil
}

// usageDir is where usage.yml is read: the first directory target, or the
// directory of the first file target.
func usageDir(targets []iacTarget) string {
	for _, t := range targets {
		if t.isDir {
			return t.abs
		}
	}
	return filepath.Dir(targets[0].abs)
}

func (in *analysisInputs) empty() bool {
	return len(in.terraform) == 0 && len(in.templates.Templates) == 0
}

func (in *analysisInputs) foundLabel(projectLabel string) string {
	templates := len(in.templates.Templates)
	switch {
	case templates == 0:
		return fmt.Sprintf("Found %d resources in %s", len(in.terraform), projectLabel)
	case len(in.terraform) == 0:
		return fmt.Sprintf("Found %d CloudFormation templates in %s", templates, projectLabel)
	default:
		return fmt.Sprintf("Found %d resources and %d CloudFormation templates in %s", len(in.terraform), templates, projectLabel)
	}
}

func (in *analysisInputs) request(changes []api.ResourceChange, cfnInput *api.CloudFormationInput) *api.AnalyzePrRequest {
	regions := make(map[string]*string, len(in.providerRegions))
	for k, v := range in.providerRegions {
		regions[k] = api.StringPtr(v)
	}
	return &api.AnalyzePrRequest{
		HeadResources:   in.terraform,
		ResourceChanges: changes,
		Region:          api.StringPtr(in.region),
		ProviderRegions: regions,
		UsageOverrides:  in.usage,
		CloudFormation:  cfnInput,
	}
}

// announceUpload lists on stderr every file about to be sent to the API.
func announceUpload(input *api.CloudFormationInput) {
	if input == nil {
		return
	}
	seen := make(map[string]bool)
	var paths []string
	for _, f := range append(append([]api.TemplateFile{}, input.Files.Base...), input.Files.Head...) {
		if !seen[f.Path] {
			seen[f.Path] = true
			paths = append(paths, f.Path)
		}
	}
	sort.Strings(paths)
	output.ProgressList(fmt.Sprintf("Uploading %d files to the LevelFour API:", len(paths)), paths)
}

// renderOptions are the output flags estimate and diff share.
type renderOptions struct {
	format       string
	projectLabel string
	isDiff       bool
	failAbove    float64
}

func renderAnalysis(data *api.AnalyzePrResponse, opts renderOptions) error {
	switch {
	case opts.format == formatJSON || output.HasFormattingFlags():
		if err := printJSONResult(data); err != nil {
			return err
		}
	case opts.format == "github-comment":
		output.RenderMarkdown(output.Stdout, data, opts.isDiff)
	case opts.isDiff:
		output.RenderDiff(output.Stdout, data, opts.projectLabel)
	default:
		output.RenderBreakdown(output.Stdout, data, opts.projectLabel)
	}
	return checkFailAbove(data, opts.failAbove)
}
