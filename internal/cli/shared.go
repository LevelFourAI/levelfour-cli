package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	tf "github.com/LevelFourAI/levelfour-cli/internal/terraform"
	"gopkg.in/yaml.v3"
)

func classifyInputs(args []string) (dirs []string, files []string, err error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	for _, a := range args {
		info, statErr := os.Stat(a)
		if statErr != nil {
			return nil, nil, fmt.Errorf("path not found: %s", a)
		}
		if info.IsDir() {
			dirs = append(dirs, a)
		} else {
			files = append(files, a)
		}
	}
	return dirs, files, nil
}

func parseSnapshots(dirs, files []string, opts tf.ParseOptions) ([]api.ResourceSnapshot, []string, error) {
	var all []api.ResourceSnapshot
	var warnings []string

	for _, dir := range dirs {
		snaps, warns, err := tf.ParseDirectoryWithOpts(dir, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse %s: %w", dir, err)
		}
		all = append(all, snaps...)
		warnings = append(warnings, warns...)
	}

	if len(files) > 0 {
		snaps, warns, err := tf.ParseFilesWithOpts(files, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse files: %w", err)
		}
		all = append(all, snaps...)
		warnings = append(warnings, warns...)
	}

	return all, warnings, nil
}

func detectProviderRegions(dirs []string, files []string, opts tf.ParseOptions) tf.ProviderRegions {
	if len(dirs) > 0 {
		result := tf.ParseProviderRegions(dirs[0], opts)
		if result.AWS != "" || result.GCP != "" || result.Azure != "" {
			return result
		}
	}
	if len(files) > 0 {
		return tf.ParseProviderRegions(filepath.Dir(files[0]), opts)
	}
	return tf.ProviderRegions{}
}

func detectRegion(dirs []string, files []string) string {
	return detectProviderRegions(dirs, files, tf.ParseOptions{}).AWS
}

func buildProviderRegions(
	awsRegion, gcpFlag, azureFlag string,
	configRegions map[string]string,
	detected tf.ProviderRegions,
	gcpFlagChanged, azureFlagChanged bool,
) map[string]string {
	pr := map[string]string{"aws": awsRegion}

	// GCP: explicit flag > config > auto-detect > default
	switch {
	case gcpFlagChanged && gcpFlag != "":
		pr["gcp"] = gcpFlag
	case configRegions["gcp"] != "":
		pr["gcp"] = configRegions["gcp"]
	case detected.GCP != "":
		pr["gcp"] = detected.GCP
	default:
		pr["gcp"] = "us-central1"
	}

	// Azure: explicit flag > config > auto-detect > default
	switch {
	case azureFlagChanged && azureFlag != "":
		pr["azure"] = azureFlag
	case configRegions["azure"] != "":
		pr["azure"] = configRegions["azure"]
	case detected.Azure != "":
		pr["azure"] = detected.Azure
	default:
		pr["azure"] = "eastus"
	}

	return pr
}

func loadUsageOverrides(dir string) map[string]interface{} {
	candidates := []string{
		filepath.Join(dir, "usage.yml"),
		filepath.Join(dir, ".levelfour", "usage.yml"),
	}
	for _, path := range candidates {
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			continue
		}
		var overrides map[string]interface{}
		if yaml.Unmarshal(data, &overrides) == nil && len(overrides) > 0 {
			return overrides
		}
	}
	return nil
}

func postAnalysis(client *api.SDKClient, snapshots []api.ResourceSnapshot, changes []api.ResourceChange, region string, providerRegions map[string]string, usageOverrides map[string]interface{}) (*api.AnalyzePrResponse, error) {
	sdkRegions := make(map[string]*string, len(providerRegions))
	for k, v := range providerRegions {
		sdkRegions[k] = api.StringPtr(v)
	}

	req := &api.AnalyzePrRequest{
		HeadResources:   snapshots,
		ResourceChanges: changes,
		Region:          api.StringPtr(region),
		ProviderRegions: sdkRegions,
		UsageOverrides:  usageOverrides,
	}
	resp, err := client.Raw().AnalyzeIaC(context.Background(), req)
	if err != nil {
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "Unauthorized") {
			return nil, fmt.Errorf("authentication failed: verify your API key with 'l4 auth status --verify' or re-authenticate with 'l4 auth login': %w", err)
		}
		return nil, err
	}
	return resp, nil
}

func toAttributesChanged(attrs map[string]interface{}, side string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range attrs {
		result[k] = map[string]interface{}{side: v}
	}
	return result
}

func snapshotsToAddedChanges(snapshots []api.ResourceSnapshot) []api.ResourceChange {
	changes := make([]api.ResourceChange, 0, len(snapshots))
	for _, s := range snapshots {
		changes = append(changes, api.ResourceChange{
			ResourceType:      s.Type,
			ResourceName:      s.Name,
			ChangeType:        "added",
			AttributesChanged: toAttributesChanged(s.Attributes, "new"),
		})
	}
	return changes
}

func diffSnapshots(old, new []api.ResourceSnapshot) []api.ResourceChange {
	oldMap := make(map[string]api.ResourceSnapshot)
	for _, s := range old {
		oldMap[tf.SnapshotKey(s)] = s
	}
	newMap := make(map[string]api.ResourceSnapshot)
	for _, s := range new {
		newMap[tf.SnapshotKey(s)] = s
	}

	var changes []api.ResourceChange

	for key, newSnap := range newMap {
		if oldSnap, exists := oldMap[key]; exists {
			attrsChanged := make(map[string]interface{})
			allKeys := make(map[string]bool)
			for k := range oldSnap.Attributes {
				allKeys[k] = true
			}
			for k := range newSnap.Attributes {
				allKeys[k] = true
			}
			for k := range allKeys {
				oldVal, oldOK := oldSnap.Attributes[k]
				newVal, newOK := newSnap.Attributes[k]
				switch {
				case oldOK && newOK:
					if fmt.Sprintf("%v", oldVal) != fmt.Sprintf("%v", newVal) {
						attrsChanged[k] = map[string]interface{}{"old": oldVal, "new": newVal}
					}
				case newOK:
					attrsChanged[k] = map[string]interface{}{"new": newVal}
				default:
					attrsChanged[k] = map[string]interface{}{"old": oldVal}
				}
			}
			if len(attrsChanged) > 0 {
				changes = append(changes, api.ResourceChange{
					ResourceType:      newSnap.Type,
					ResourceName:      newSnap.Name,
					ChangeType:        "modified",
					AttributesChanged: attrsChanged,
				})
			}
		} else {
			changes = append(changes, api.ResourceChange{
				ResourceType:      newSnap.Type,
				ResourceName:      newSnap.Name,
				ChangeType:        "added",
				AttributesChanged: toAttributesChanged(newSnap.Attributes, "new"),
			})
		}
	}

	for key, oldSnap := range oldMap {
		if _, exists := newMap[key]; !exists {
			changes = append(changes, api.ResourceChange{
				ResourceType:      oldSnap.Type,
				ResourceName:      oldSnap.Name,
				ChangeType:        "removed",
				AttributesChanged: toAttributesChanged(oldSnap.Attributes, "old"),
			})
		}
	}

	return changes
}

func isJSONFile(path string) bool {
	if path == "" {
		return false
	}
	return len(path) > 5 && path[len(path)-5:] == ".json"
}

var osReadFile = os.ReadFile

func loadSnapshotsFromFile(path string) ([]api.ResourceSnapshot, error) {
	data, err := osReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	var snapshots []api.ResourceSnapshot
	if json.Unmarshal(data, &snapshots) == nil {
		return snapshots, nil
	}

	var envelope map[string]interface{}
	if json.Unmarshal(data, &envelope) == nil {
		if resources, ok := envelope["head_resources"]; ok {
			b, _ := json.Marshal(resources)
			if json.Unmarshal(b, &snapshots) == nil {
				return snapshots, nil
			}
		}
	}

	return nil, fmt.Errorf("unrecognized file format")
}

var osWriteFile = os.WriteFile

var filepathAbs = filepath.Abs
var filepathRel = filepath.Rel
var osChdir = os.Chdir
var osMkdirTemp = os.MkdirTemp

func saveSnapshots(path string, snapshots []api.ResourceSnapshot) error {
	data, err := json.MarshalIndent(snapshots, "", "  ")
	if err != nil {
		return err
	}
	return osWriteFile(path, data, 0o600)
}

func isExcluded(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if !strings.Contains(pattern, "*") {
			if value == pattern {
				return true
			}
			continue
		}
		if globMatch(pattern, value) {
			return true
		}
	}
	return false
}

func globMatch(pattern, value string) bool {
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(value, parts[0]) {
		return false
	}

	pos := len(parts[0])
	for _, segment := range parts[1 : len(parts)-1] {
		idx := strings.Index(value[pos:], segment)
		if idx == -1 {
			return false
		}
		pos += idx + len(segment)
	}

	last := parts[len(parts)-1]
	return strings.HasSuffix(value, last) && pos <= len(value)-len(last)
}

func filterExcludedResources(snapshots []api.ResourceSnapshot, patterns []string) []api.ResourceSnapshot {
	if len(patterns) == 0 {
		return snapshots
	}
	var filtered []api.ResourceSnapshot
	for _, s := range snapshots {
		if !isExcluded(s.Type, patterns) {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func checkFailAbove(data *api.AnalyzePrResponse, threshold float64) error {
	delta := float64(0)
	if data.CostSummary != nil {
		delta = data.CostSummary.TotalMonthlyDifference
	}
	if threshold > 0 && delta > threshold {
		fmt.Fprintf(os.Stderr, "Cost delta $%.2f exceeds threshold $%.2f\n",
			delta, threshold)
		return ErrIssuesFound
	}
	return nil
}

func gitRepoRoot(path string) (string, error) {
	dir := path
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		dir = filepath.Dir(path)
	}
	out, err := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not a git repository: %s", path)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitDefaultBranch(repoRoot string) string {
	for _, name := range []string{"main", "master"} {
		if err := exec.CommandContext(context.Background(), "git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", "refs/heads/"+name).Run(); err == nil {
			return name
		}
	}
	return ""
}

func gitMergeBase(repoRoot, ref string) (string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "-C", repoRoot, "merge-base", ref, "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("merge-base failed for %s: %w", ref, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func gitBaselineSnapshots(targetPath, baseRef string, opts tf.ParseOptions) ([]api.ResourceSnapshot, error) {
	abs, err := filepathAbs(targetPath)
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}

	repoRoot, err := gitRepoRoot(abs)
	if err != nil {
		return nil, err
	}

	if baseRef == "" {
		baseRef = gitDefaultBranch(repoRoot)
		if baseRef == "" {
			return nil, fmt.Errorf("no main or master branch found")
		}
	}

	sha, err := gitMergeBase(repoRoot, baseRef)
	if err != nil {
		return nil, err
	}

	dirAbs := abs
	if info, statErr := os.Stat(abs); statErr == nil && !info.IsDir() {
		dirAbs = filepath.Dir(abs)
	}

	relDirPath, err := filepathRel(repoRoot, dirAbs)
	if err != nil {
		return nil, err
	}

	opts.ProjectDir = dirAbs

	origDir, _ := os.Getwd()
	if chErr := osChdir(repoRoot); chErr != nil {
		return nil, chErr
	}

	tmpDir, tmpErr := osMkdirTemp("", "l4-git-baseline-*")
	if tmpErr != nil {
		_ = osChdir(origDir)
		return nil, tmpErr
	}

	snapshots, _, parseErr := tf.ParseGitRefWithOpts(sha, relDirPath, tmpDir, opts)

	_ = osChdir(origDir)
	_ = os.RemoveAll(tmpDir)

	if parseErr != nil {
		return nil, parseErr
	}

	return snapshots, nil
}

func scopeBaselineToCurrentResources(baseline, current []api.ResourceSnapshot) []api.ResourceSnapshot {
	keys := make(map[string]bool, len(current))
	for _, s := range current {
		keys[s.Type+"."+s.Name] = true
	}
	var scoped []api.ResourceSnapshot
	for _, s := range baseline {
		if keys[s.Type+"."+s.Name] {
			scoped = append(scoped, s)
		}
	}
	return scoped
}

func buildProjectLabel(args []string) string {
	if len(args) == 0 {
		return "."
	}
	return strings.Join(args, " ")
}
