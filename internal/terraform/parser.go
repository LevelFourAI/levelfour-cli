package terraform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

const (
	MaxFiles       = 50
	MaxResources   = 500
	MaxFileSize    = 1 << 20
	MaxDepth       = 10
	blockModule    = "module"
	providerAWS    = "aws"
	providerGoogle = "google"
	providerAzure  = "azurerm"
)

// ProviderRegions holds the auto-detected region for each cloud provider.
type ProviderRegions struct {
	AWS   string
	GCP   string
	Azure string
}

func ParseDirectory(dir string) ([]api.ResourceSnapshot, []string, error) {
	return ParseDirectoryWithOpts(dir, ParseOptions{})
}

func ParseDirectoryWithOpts(dir string, opts ParseOptions) ([]api.ResourceSnapshot, []string, error) {
	var evalCtx *hcl.EvalContext

	absDir, _ := filepath.Abs(dir)

	vars := ParseVariableDefaults(absDir)
	fileVars := ParseVarFiles(absDir, opts.VarFiles)
	for k, v := range fileVars {
		vars[k] = v
	}
	flagVars := ParseVarFlags(opts.Vars)
	for k, v := range flagVars {
		vars[k] = v
	}
	evalCtx = BuildEvalContext(vars)

	locals := ParseLocals(absDir, evalCtx)
	if len(locals) > 0 {
		evalCtx = BuildEvalContextWithLocals(vars, locals)
	}

	var downloader *ModuleDownloader
	if opts.DownloadModules {
		projectDir := opts.ProjectDir
		if projectDir == "" {
			projectDir = absDir
		}
		downloader = NewModuleDownloader(projectDir)
	}

	visited := make(map[string]bool)
	absDir2, _ := filepath.Abs(dir)
	visited[absDir2] = true
	return parseDirectoryWithVisited(dir, visited, 0, evalCtx, opts, downloader)
}

func parseDirectoryWithVisited(dir string, visited map[string]bool, depth int, evalCtx *hcl.EvalContext, opts ParseOptions, downloader *ModuleDownloader) ([]api.ResourceSnapshot, []string, error) {
	maxFiles := MaxFiles
	if opts.MaxFiles > 0 {
		maxFiles = opts.MaxFiles
	}
	maxResources := MaxResources
	if opts.MaxResources > 0 {
		maxResources = opts.MaxResources
	}
	maxFileSize := int64(MaxFileSize)
	if opts.MaxFileSize > 0 {
		maxFileSize = opts.MaxFileSize
	}

	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return nil, nil, err
	}

	subMatches, _ := filepath.Glob(filepath.Join(dir, "**", "*.tf"))
	seen := make(map[string]bool)
	var allFiles []string
	for _, f := range append(matches, subMatches...) {
		abs, _ := filepath.Abs(f)
		if !seen[abs] {
			seen[abs] = true
			allFiles = append(allFiles, abs)
		}
	}

	if len(allFiles) == 0 {
		return nil, nil, fmt.Errorf("no .tf files found in %s", dir)
	}

	var warnings []string

	if len(allFiles) > maxFiles {
		warnings = append(warnings, fmt.Sprintf("Found %d .tf files, processing first %d", len(allFiles), maxFiles))
		allFiles = allFiles[:maxFiles]
	}

	var snapshots []api.ResourceSnapshot

	for _, f := range allFiles {
		info, statErr := os.Stat(f)
		if statErr != nil {
			continue
		}
		if info.Size() > maxFileSize {
			warnings = append(warnings, fmt.Sprintf("Skipping %s: file exceeds 1MB limit", filepath.Base(f)))
			continue
		}

		content, readErr := os.ReadFile(filepath.Clean(f))
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("Failed to read %s: %v", filepath.Base(f), readErr))
			continue
		}

		fileSnapshots, parseWarnings := parseHCLContent(content, f, visited, depth, evalCtx, opts, downloader)
		snapshots = append(snapshots, fileSnapshots...)
		warnings = append(warnings, parseWarnings...)
	}

	if len(snapshots) > maxResources {
		warnings = append(warnings, fmt.Sprintf("Found %d resources, truncating to %d", len(snapshots), maxResources))
		snapshots = snapshots[:maxResources]
	}

	return snapshots, warnings, nil
}

type fileGroup struct {
	dir   string
	files []string
}

var absPath = filepath.Abs
var readFile = os.ReadFile

func groupFilesByDir(files []string) ([]fileGroup, error) {
	dirMap := make(map[string]*fileGroup)
	var dirOrder []string

	for _, f := range files {
		abs, err := absPath(f)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve path %s: %w", f, err)
		}
		if filepath.Ext(abs) != ".tf" {
			return nil, fmt.Errorf("not a .tf file: %s", f)
		}
		dir := filepath.Dir(abs)
		if _, ok := dirMap[dir]; !ok {
			dirMap[dir] = &fileGroup{dir: dir}
			dirOrder = append(dirOrder, dir)
		}
		dirMap[dir].files = append(dirMap[dir].files, abs)
	}

	var groups []fileGroup
	for _, dir := range dirOrder {
		groups = append(groups, *dirMap[dir])
	}
	return groups, nil
}

func parseFileGroup(group fileGroup, opts ParseOptions, visited map[string]bool, maxFileSize int64, downloader *ModuleDownloader) ([]api.ResourceSnapshot, []string) {
	vars := ParseVariableDefaults(group.dir)
	fileVars := ParseVarFiles(group.dir, opts.VarFiles)
	for k, v := range fileVars {
		vars[k] = v
	}
	flagVars := ParseVarFlags(opts.Vars)
	for k, v := range flagVars {
		vars[k] = v
	}
	evalCtx := BuildEvalContext(vars)

	locals := ParseLocals(group.dir, evalCtx)
	if len(locals) > 0 {
		evalCtx = BuildEvalContextWithLocals(vars, locals)
	}

	var snapshots []api.ResourceSnapshot
	var warnings []string

	for _, f := range group.files {
		info, statErr := os.Stat(f)
		if statErr != nil {
			warnings = append(warnings, fmt.Sprintf("cannot stat %s: %v", filepath.Base(f), statErr))
			continue
		}
		if info.Size() > maxFileSize {
			warnings = append(warnings, fmt.Sprintf("Skipping %s: file exceeds 1MB limit", filepath.Base(f)))
			continue
		}
		content, readErr := readFile(filepath.Clean(f))
		if readErr != nil {
			warnings = append(warnings, fmt.Sprintf("Failed to read %s: %v", filepath.Base(f), readErr))
			continue
		}
		fileSnaps, parseWarnings := parseHCLContent(content, f, visited, 0, evalCtx, opts, downloader)
		snapshots = append(snapshots, fileSnaps...)
		warnings = append(warnings, parseWarnings...)
	}

	return snapshots, warnings
}

func ParseFilesWithOpts(files []string, opts ParseOptions) ([]api.ResourceSnapshot, []string, error) {
	groups, err := groupFilesByDir(files)
	if err != nil {
		return nil, nil, err
	}
	if len(groups) == 0 {
		return nil, nil, fmt.Errorf("no .tf files provided")
	}

	maxResources := MaxResources
	if opts.MaxResources > 0 {
		maxResources = opts.MaxResources
	}
	maxFileSize := int64(MaxFileSize)
	if opts.MaxFileSize > 0 {
		maxFileSize = opts.MaxFileSize
	}

	var downloader *ModuleDownloader
	if opts.DownloadModules {
		projectDir := opts.ProjectDir
		if projectDir == "" && len(groups) > 0 {
			projectDir = groups[0].dir
		}
		downloader = NewModuleDownloader(projectDir)
	}

	var snapshots []api.ResourceSnapshot
	var warnings []string
	visited := make(map[string]bool)

	for _, group := range groups {
		groupSnaps, groupWarns := parseFileGroup(group, opts, visited, maxFileSize, downloader)
		snapshots = append(snapshots, groupSnaps...)
		warnings = append(warnings, groupWarns...)
	}

	if len(snapshots) > maxResources {
		warnings = append(warnings, fmt.Sprintf("Found %d resources, truncating to %d", len(snapshots), maxResources))
		snapshots = snapshots[:maxResources]
	}

	return snapshots, warnings, nil
}

func ParseGitRef(ref, path, tmpDir string) ([]api.ResourceSnapshot, []string, error) {
	return ParseGitRefWithOpts(ref, path, tmpDir, ParseOptions{})
}

func ParseGitRefWithOpts(ref, path, tmpDir string, opts ParseOptions) ([]api.ResourceSnapshot, []string, error) {
	cmd := exec.CommandContext(context.Background(), "git", "archive", "--format=tar", ref, "--", path)
	archive, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("git archive failed for %s: %w", ref, err)
	}

	extractDir := filepath.Join(tmpDir, ref)
	if mkErr := os.MkdirAll(extractDir, 0o750); mkErr != nil {
		return nil, nil, mkErr
	}

	tarCmd := exec.CommandContext(context.Background(), "tar", "xf", "-", "-C", extractDir)
	tarCmd.Stdin = strings.NewReader(string(archive))
	if tarErr := tarCmd.Run(); tarErr != nil {
		return nil, nil, fmt.Errorf("tar extract failed: %w", tarErr)
	}

	return ParseDirectoryWithOpts(filepath.Join(extractDir, path), opts)
}

func ParseProviderRegion(dir string, opts ...ParseOptions) string {
	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return ""
	}

	absDir, _ := filepath.Abs(dir)
	evalCtx := buildEvalCtxFromOpts(absDir, opts)

	for _, f := range matches {
		if region := extractProviderRegionFromFile(f, evalCtx); region != "" {
			return region
		}
	}

	return ""
}

func buildEvalCtxFromOpts(absDir string, opts []ParseOptions) *hcl.EvalContext {
	vars := ParseVariableDefaults(absDir)
	if len(opts) > 0 {
		fileVars := ParseVarFiles(absDir, opts[0].VarFiles)
		for k, v := range fileVars {
			vars[k] = v
		}
		flagVars := ParseVarFlags(opts[0].Vars)
		for k, v := range flagVars {
			vars[k] = v
		}
	}
	return BuildEvalContext(vars)
}

func extractProviderRegionFromFile(f string, evalCtx *hcl.EvalContext) string {
	content, readErr := os.ReadFile(filepath.Clean(f))
	if readErr != nil {
		return ""
	}
	file, diags := hclsyntax.ParseConfig(content, f, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return ""
	}
	body := file.Body.(*hclsyntax.Body)
	for _, block := range body.Blocks {
		if block.Type != "provider" || len(block.Labels) < 1 || block.Labels[0] != providerAWS {
			continue
		}
		if region := extractRegionFromBlock(block, evalCtx, content); region != "" {
			return region
		}
	}
	return ""
}

func extractRegionFromBlock(block *hclsyntax.Block, evalCtx *hcl.EvalContext, content []byte) string {
	regionAttr, ok := block.Body.Attributes["region"]
	if !ok {
		return ""
	}
	val, valDiags := regionAttr.Expr.Value(evalCtx)
	if valDiags.HasErrors() {
		rng := regionAttr.Expr.Range()
		raw := strings.TrimSpace(string(content[rng.Start.Byte:rng.End.Byte]))
		if raw != "" && !strings.Contains(raw, "var.") && !strings.Contains(raw, "local.") {
			return raw
		}
		return ""
	}
	if val.Type() == cty.String {
		return val.AsString()
	}
	return ""
}

// ParseProviderRegions auto-detects regions for AWS, GCP, and Azure providers.
func ParseProviderRegions(dir string, opts ...ParseOptions) ProviderRegions {
	matches, err := filepath.Glob(filepath.Join(dir, "*.tf"))
	if err != nil {
		return ProviderRegions{}
	}

	absDir, _ := filepath.Abs(dir)
	evalCtx := buildEvalCtxFromOpts(absDir, opts)

	var result ProviderRegions
	for _, f := range matches {
		extractProviderRegionsFromFile(f, evalCtx, &result)
	}
	return result
}

func extractProviderRegionsFromFile(f string, evalCtx *hcl.EvalContext, result *ProviderRegions) {
	content, readErr := os.ReadFile(filepath.Clean(f))
	if readErr != nil {
		return
	}
	file, diags := hclsyntax.ParseConfig(content, f, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return
	}
	body := file.Body.(*hclsyntax.Body)
	for _, block := range body.Blocks {
		if block.Type != "provider" || len(block.Labels) < 1 {
			continue
		}
		switch block.Labels[0] {
		case providerAWS:
			if result.AWS == "" {
				result.AWS = extractRegionFromBlock(block, evalCtx, content)
			}
		case providerGoogle:
			if result.GCP == "" {
				result.GCP = extractRegionFromBlock(block, evalCtx, content)
			}
		case providerAzure:
			if result.Azure == "" {
				result.Azure = extractLocationFromBlock(block, evalCtx, content)
			}
		}
	}
}

func extractLocationFromBlock(block *hclsyntax.Block, evalCtx *hcl.EvalContext, content []byte) string {
	// Azure uses "location" instead of "region" in its provider blocks
	for _, attrName := range []string{"location", "region"} {
		attr, ok := block.Body.Attributes[attrName]
		if !ok {
			continue
		}
		val, valDiags := attr.Expr.Value(evalCtx)
		if valDiags.HasErrors() {
			rng := attr.Expr.Range()
			raw := strings.TrimSpace(string(content[rng.Start.Byte:rng.End.Byte]))
			if raw != "" && !strings.Contains(raw, "var.") && !strings.Contains(raw, "local.") {
				return raw
			}
			continue
		}
		if val.Type() == cty.String {
			return val.AsString()
		}
	}
	return ""
}

func parseHCLContent(content []byte, filename string, visited map[string]bool, depth int, evalCtx *hcl.EvalContext, opts ParseOptions, downloader *ModuleDownloader) ([]api.ResourceSnapshot, []string) {
	var warnings []string

	file, diags := hclsyntax.ParseConfig(content, filename, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		for _, d := range diags.Errs() {
			warnings = append(warnings, fmt.Sprintf("%s: %s", filepath.Base(filename), d.Error()))
		}
		return nil, warnings
	}

	body := file.Body.(*hclsyntax.Body)
	resources, modules := classifyBlocks(body, content, evalCtx)

	parsed := make(map[string]interface{})
	if len(resources) > 0 {
		parsed["resource"] = resources
	}
	if len(modules) > 0 {
		parsed[blockModule] = modules
	}

	snapshots := extractResources(parsed)
	modSnapshots, modWarnings := extractModules(parsed, filename, visited, depth, evalCtx, opts, downloader)
	snapshots = append(snapshots, modSnapshots...)
	warnings = append(warnings, modWarnings...)

	return snapshots, warnings
}

func classifyBlocks(body *hclsyntax.Body, content []byte, evalCtx *hcl.EvalContext) (map[string]interface{}, map[string]interface{}) {
	resources := make(map[string]interface{})
	modules := make(map[string]interface{})

	for _, block := range body.Blocks {
		switch block.Type {
		case "resource":
			if len(block.Labels) >= 2 {
				resType := block.Labels[0]
				resName := block.Labels[1]
				if _, ok := resources[resType]; !ok {
					resources[resType] = make(map[string]interface{})
				}
				resources[resType].(map[string]interface{})[resName] = []interface{}{bodyToMap(block.Body, content, evalCtx)}
			}
		case blockModule:
			if len(block.Labels) >= 1 {
				modules[block.Labels[0]] = []interface{}{bodyToMap(block.Body, content, evalCtx)}
			}
		}
	}

	return resources, modules
}

var unresolvedPrefixes = []string{"var.", "local.", "data.", "module.", "each.", "self."}

func isUnresolvedRef(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	for _, prefix := range unresolvedPrefixes {
		if strings.HasPrefix(trimmed, prefix) || strings.Contains(trimmed, "${"+prefix) {
			return true
		}
	}
	return false
}

func bodyToMap(body *hclsyntax.Body, src []byte, evalCtx *hcl.EvalContext) map[string]interface{} {
	result := make(map[string]interface{})

	for name, attr := range body.Attributes {
		val, diags := attr.Expr.Value(evalCtx)
		if !diags.HasErrors() {
			result[name] = ctyToGo(val)
		} else {
			rng := attr.Expr.Range()
			raw := string(src[rng.Start.Byte:rng.End.Byte])
			if isUnresolvedRef(raw) {
				result[name] = nil
			} else {
				result[name] = raw
			}
		}
	}

	for _, block := range body.Blocks {
		existing, _ := result[block.Type].([]interface{})
		result[block.Type] = append(existing, bodyToMap(block.Body, src, evalCtx))
	}

	return result
}

func ctyToGo(val cty.Value) interface{} {
	if val.IsNull() {
		return nil
	}
	if !val.IsKnown() {
		return nil
	}

	ty := val.Type()
	switch {
	case ty == cty.String:
		return val.AsString()
	case ty == cty.Number:
		f, _ := val.AsBigFloat().Float64()
		return f
	case ty == cty.Bool:
		return val.True()
	case ty.IsListType() || ty.IsTupleType() || ty.IsSetType():
		var items []interface{}
		it := val.ElementIterator()
		for it.Next() {
			_, v := it.Element()
			items = append(items, ctyToGo(v))
		}
		return items
	case ty.IsMapType() || ty.IsObjectType():
		m := make(map[string]interface{})
		it := val.ElementIterator()
		for it.Next() {
			k, v := it.Element()
			m[k.AsString()] = ctyToGo(v)
		}
		return m
	default:
		return val.GoString()
	}
}

func shouldSkipByCount(attrs map[string]interface{}) bool {
	countVal, ok := attrs["count"]
	if !ok {
		return false
	}
	switch v := countVal.(type) {
	case float64:
		return v == 0
	case int:
		return v == 0
	case int64:
		return v == 0
	case bool:
		return !v
	}
	return false
}

func extractResources(parsed map[string]interface{}) []api.ResourceSnapshot {
	resources, ok := parsed["resource"].(map[string]interface{})
	if !ok {
		return nil
	}

	var snapshots []api.ResourceSnapshot
	for resType, instances := range resources {
		instanceMap, ok := instances.(map[string]interface{})
		if !ok {
			continue
		}
		for name, attrs := range instanceMap {
			attrSlice, ok := attrs.([]interface{})
			if !ok {
				continue
			}
			for _, a := range attrSlice {
				attrMap, ok := a.(map[string]interface{})
				if !ok {
					continue
				}
				if shouldSkipByCount(attrMap) {
					continue
				}
				cleaned := make(map[string]interface{})
				for k, v := range attrMap {
					if v != nil && k != "count" {
						cleaned[k] = v
					}
				}
				snapshots = append(snapshots, api.ResourceSnapshot{
					Type:       resType,
					Name:       name,
					Attributes: cleaned,
				})
			}
		}
	}
	return snapshots
}

func SnapshotKey(s api.ResourceSnapshot) string {
	if s.ModulePath != "" {
		return s.ModulePath + "." + s.Type + "." + s.Name
	}
	return s.Type + "." + s.Name
}

func extractModules(parsed map[string]interface{}, filename string, visited map[string]bool, depth int, evalCtx *hcl.EvalContext, opts ParseOptions, downloader *ModuleDownloader) ([]api.ResourceSnapshot, []string) {
	modules, ok := parsed[blockModule].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	var snapshots []api.ResourceSnapshot
	var warnings []string

	for name, config := range modules {
		configSlice, ok := config.([]interface{})
		if !ok {
			continue
		}
		for _, c := range configSlice {
			configMap, ok := c.(map[string]interface{})
			if !ok {
				continue
			}

			if shouldSkipByCount(configMap) {
				continue
			}

			source, _ := configMap["source"].(string)
			if isRemoteSource(source) {
				if downloader != nil {
					version, _ := configMap["version"].(string)
					s, w := resolveRemoteModule(name, source, version, configMap, filename, visited, depth, evalCtx, opts, "", downloader)
					snapshots = append(snapshots, s...)
					warnings = append(warnings, w...)
				} else {
					snapshots = append(snapshots, api.ResourceSnapshot{
						Type:       blockModule,
						Name:       name,
						Attributes: configMap,
					})
				}
				continue
			}

			if isLocalSource(source) {
				s, w := resolveLocalModule(name, source, filename, visited, depth, evalCtx, opts, "", downloader)
				snapshots = append(snapshots, s...)
				warnings = append(warnings, w...)
			}
		}
	}

	return snapshots, warnings
}

func isRemoteSource(source string) bool {
	return source != "" && !strings.HasPrefix(source, ".") && !strings.HasPrefix(source, "/")
}

func isLocalSource(source string) bool {
	return source != "" && (strings.HasPrefix(source, ".") || strings.HasPrefix(source, "/"))
}

func resolveLocalModule(name, source, filename string, visited map[string]bool, depth int, evalCtx *hcl.EvalContext, opts ParseOptions, parentModulePath string, downloader *ModuleDownloader) ([]api.ResourceSnapshot, []string) {
	moduleDir := filepath.Join(filepath.Dir(filename), source)
	absModuleDir, _ := filepath.Abs(moduleDir)

	if visited[absModuleDir] {
		return nil, nil
	}

	if depth >= MaxDepth {
		return nil, []string{fmt.Sprintf("module.%s: max depth %d reached, skipping %s", name, MaxDepth, source)}
	}

	visited[absModuleDir] = true
	modSnapshots, modWarnings, modErr := parseDirectoryWithVisited(moduleDir, visited, depth+1, evalCtx, opts, downloader)
	if modErr != nil {
		return nil, []string{fmt.Sprintf("module.%s: failed to parse local module %s: %v", name, source, modErr)}
	}

	modulePath := "module." + name
	if parentModulePath != "" {
		modulePath = parentModulePath + ".module." + name
	}

	for i := range modSnapshots {
		if modSnapshots[i].ModulePath == "" {
			modSnapshots[i].ModulePath = modulePath
		} else {
			modSnapshots[i].ModulePath = modulePath + "." + modSnapshots[i].ModulePath
		}
	}

	return modSnapshots, modWarnings
}
