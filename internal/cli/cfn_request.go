package cli

import (
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	cfn "github.com/LevelFourAI/levelfour-cli/internal/cloudformation"
)

// cfnFlags are the CloudFormation flags estimate and diff share.
type cfnFlags struct {
	parameters     []string
	parametersFile string
	configEnv      string
	region         string
}

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
func buildCloudFormationInput(root string, head, base cfn.Result, flags cfnFlags, excluded []string) (*api.CloudFormationInput, error) {
	if len(head.Templates) == 0 && len(base.Templates) == 0 {
		return nil, nil
	}
	values, err := parseParameterFlags(flags.parameters)
	if err != nil {
		return nil, err
	}
	input := &api.CloudFormationInput{
		Files:                 api.CloudFormationFiles{Base: nonNilFiles(base.Files), Head: nonNilFiles(head.Files)},
		Parameters:            declaredValues(values, head, base),
		SamconfigEnv:          flags.configEnv,
		ExcludedResourceTypes: excluded,
		Region:                flags.region,
	}
	if flags.parametersFile == "" {
		return input, nil
	}
	file, err := readParametersFile(root, flags.parametersFile)
	if err != nil {
		return nil, err
	}
	input.Files.Head = withFile(input.Files.Head, file)
	if len(base.Templates) > 0 {
		input.Files.Base = withFile(input.Files.Base, file)
	}
	input.ParameterFiles = make(map[string]string)
	for _, template := range allTemplates(head, base) {
		input.ParameterFiles[template] = file.Path
	}
	return input, nil
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
// project root like everything else that is uploaded, reached without a symlink.
func readParametersFile(root, name string) (api.TemplateFile, error) {
	abs, err := filepathAbs(name)
	if err != nil {
		return api.TemplateFile{}, err
	}
	rel := relativeSlash(root, abs)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return api.TemplateFile{}, fmt.Errorf("parameters file %s is outside the project root %s", name, root)
	}
	content, err := cfn.DirTree{Root: root}.Read(rel)
	if err != nil {
		return api.TemplateFile{}, fmt.Errorf("cannot read parameters file %s: %w", name, err)
	}
	if len(content) > cfn.MaxFileSize {
		return api.TemplateFile{}, fmt.Errorf("parameters file %s exceeds the 1MB limit", name)
	}
	return api.TemplateFile{Path: rel, Content: string(content)}, nil
}
