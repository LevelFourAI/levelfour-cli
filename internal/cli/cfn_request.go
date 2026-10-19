package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	cfn "github.com/LevelFourAI/levelfour-cli/internal/cloudformation"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
)

// cfnOptions are what estimate and diff send with the templates: the
// CloudFormation flags they share and the project config that applies.
type cfnOptions struct {
	parameters     []string
	parametersFile string
	configEnv      string
	region         string
	excluded       []string
	// renamed maps a template's path at the base to its path at the head.
	renamed map[string]string
}

// The API's limits on the fields below, refused or trimmed here so a run gets
// a clear message instead of a 422.
const (
	maxConfigEnv       = 128
	maxExcludedTypes   = 100
	maxExcludedPattern = 256
)

func parseParameterFlags(values []string) (map[string]string, error) {
	parsed := make(map[string]string, len(values))
	for _, v := range values {
		key, value, ok := strings.Cut(v, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid --parameter %q: want Key=Value", v)
		}
		parsed[key] = value
	}
	return parsed, nil
}

// buildCloudFormationInput assembles the templates as they are at base and at
// head, or returns nil when neither side has one.
func buildCloudFormationInput(root string, head, base cfn.Result, opts cfnOptions) (*api.CloudFormationInput, error) {
	if len(head.Templates) == 0 && len(base.Templates) == 0 {
		return nil, nil
	}
	if len(opts.configEnv) > maxConfigEnv {
		return nil, fmt.Errorf("--config-env is %d characters, longer than the %d the API accepts", len(opts.configEnv), maxConfigEnv)
	}
	values, err := parseParameterFlags(opts.parameters)
	if err != nil {
		return nil, err
	}
	input := &api.CloudFormationInput{
		Files: api.CloudFormationFiles{
			Base:    nonNilFiles(base.Files),
			Head:    nonNilFiles(head.Files),
			Renamed: uploadedRenames(opts.renamed, base, head),
		},
		Parameters:            declaredValues(values, head, base),
		SamconfigEnv:          opts.configEnv,
		ExcludedResourceTypes: limitExclusions(opts.excluded),
		Region:                opts.region,
	}
	if opts.parametersFile == "" {
		return input, nil
	}
	return input, attachParametersFile(input, root, opts.parametersFile, allTemplates(head, base))
}

// uploadedRenames keeps the renames whose old path is uploaded at the base and
// whose new path is uploaded at the head.
func uploadedRenames(renamed map[string]string, base, head cfn.Result) map[string]string {
	atBase, atHead := uploadedPaths(base), uploadedPaths(head)
	kept := make(map[string]string)
	for from, to := range renamed {
		if atBase[from] && atHead[to] {
			kept[from] = to
		}
	}
	return kept
}

func uploadedPaths(side cfn.Result) map[string]bool {
	paths := make(map[string]bool, len(side.Files))
	for _, f := range side.Files {
		paths[f.Path] = true
	}
	return paths
}

// attachParametersFile uploads the --parameters-file on each side that has
// templates and names it for every template.
func attachParametersFile(input *api.CloudFormationInput, root, name string, templates []string) error {
	file, err := readParametersFile(root, name)
	if err != nil {
		return err
	}
	input.Files.Head = withFile(input.Files.Head, file)
	if len(input.Files.Base) > 0 {
		input.Files.Base = withFile(input.Files.Base, file)
	}
	if len(input.Files.Head) > cfn.MaxFiles || len(input.Files.Base) > cfn.MaxFiles {
		return fmt.Errorf("the parameters file takes the upload past %d files on one side: pass fewer paths", cfn.MaxFiles)
	}
	input.ParameterFiles = make(map[string]string)
	for _, template := range templates {
		input.ParameterFiles[template] = file.Path
	}
	return nil
}

// limitExclusions keeps the excluded_resource_types the API accepts: the first
// 100 patterns of at most 256 characters. It warns naming each one it drops.
func limitExclusions(patterns []string) []string {
	var kept []string
	var extra []string
	for _, p := range patterns {
		switch {
		case len(p) > maxExcludedPattern:
			output.Warning(fmt.Sprintf("Ignoring excluded_resource_types entry %q: longer than %d characters", p, maxExcludedPattern))
		case len(kept) == maxExcludedTypes:
			extra = append(extra, p)
		default:
			kept = append(kept, p)
		}
	}
	if len(extra) > 0 {
		output.Warning(fmt.Sprintf("Ignoring excluded_resource_types after the first %d: %s", maxExcludedTypes, strings.Join(extra, ", ")))
	}
	return kept
}

func nonNilFiles(files []api.TemplateFile) []api.TemplateFile {
	if files == nil {
		return []api.TemplateFile{}
	}
	return files
}

func withFile(files []api.TemplateFile, file api.TemplateFile) []api.TemplateFile {
	for _, f := range files {
		if f.Path == file.Path {
			return files
		}
	}
	return append(files, file)
}

func allTemplates(head, base cfn.Result) []string {
	seen := make(map[string]bool)
	var templates []string
	for _, t := range append(append([]string{}, head.Templates...), base.Templates...) {
		if !seen[t] {
			seen[t] = true
			templates = append(templates, t)
		}
	}
	return templates
}

// declaredValues gives each template the --parameter values it declares, on
// whichever side it exists. A key no template declares goes nowhere.
func declaredValues(values map[string]string, head, base cfn.Result) map[string]map[string]string {
	if len(values) == 0 {
		return nil
	}
	contents := make(map[string]string)
	for _, f := range append(append([]api.TemplateFile{}, base.Files...), head.Files...) {
		contents[f.Path] = f.Content
	}
	parameters := make(map[string]map[string]string)
	for _, template := range allTemplates(head, base) {
		declared := cfn.DeclaredParameters([]byte(contents[template]))
		for key, value := range values {
			if !declared[key] {
				continue
			}
			if parameters[template] == nil {
				parameters[template] = make(map[string]string)
			}
			parameters[template][key] = value
		}
	}
	if len(parameters) == 0 {
		return nil
	}
	return parameters
}

// readParametersFile reads the --parameters-file, which has to sit inside the
// project root like everything else that is uploaded. Its directory is
// resolved so a working directory behind a symlink (/tmp, /var on macOS)
// matches the resolved root; the file itself must not be a symlink.
func readParametersFile(root, name string) (api.TemplateFile, error) {
	abs, err := filepathAbs(name)
	if err != nil {
		return api.TemplateFile{}, err
	}
	rel := relativeSlash(root, filepath.Join(resolvedPath(filepath.Dir(abs)), filepath.Base(abs)))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return api.TemplateFile{}, fmt.Errorf("parameters file %s is outside the project root %s", name, root)
	}
	content, err := cfn.DirTree{Root: root}.Read(rel)
	if errors.Is(err, cfn.ErrTooLarge) {
		return api.TemplateFile{}, fmt.Errorf("parameters file %s exceeds the 1MB limit", name)
	}
	if err != nil {
		return api.TemplateFile{}, fmt.Errorf("cannot read parameters file %s: %w", name, err)
	}
	return api.TemplateFile{Path: rel, Content: string(content)}, nil
}
