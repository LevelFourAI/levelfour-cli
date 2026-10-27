package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/mcpinstall"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	tf "github.com/LevelFourAI/levelfour-cli/internal/terraform"
	"gopkg.in/yaml.v3"
)

// The name of the --yes flag and the answer a confirmation prompt accepts are
// the same word, so it is written once.
const wordYes = "yes"

// credentialEnvVar is the variable this CLI reads a key from. It is the same one
// `l4 mcp install --key-source env` writes into a client's config, so a user who
// exports it once has both working.
const credentialEnvVar = mcpinstall.CredentialEnvVar

// detectProviderRegions reads the region of each provider block in the first
// directory target, or in the directory of the first Terraform file target.
func detectProviderRegions(targets []iacTarget, opts tf.ParseOptions) tf.ProviderRegions {
	for _, t := range targets {
		if t.isDir {
			if result := tf.ParseProviderRegions(t.abs, opts); result != (tf.ProviderRegions{}) {
				return result
			}
			break
		}
	}
	for _, t := range targets {
		if isTerraformFile(t.abs) {
			return tf.ParseProviderRegions(filepath.Dir(t.abs), opts)
		}
	}
	return tf.ProviderRegions{}
}

// pricingRegion is the AWS region the user gave: the --region flag, the project
// config, or a Terraform provider block. Empty means none, so the API prices a
// template in the region its samconfig names, or in its default.
func pricingRegion(flag string, flagChanged bool, override *string, detected string) string {
	switch {
	case flagChanged:
		return flag
	case override != nil:
		return *override
	default:
		return detected
	}
}

func buildProviderRegions(
	awsRegion, gcpFlag, azureFlag string,
	configRegions map[string]string,
	detected tf.ProviderRegions,
	gcpFlagChanged, azureFlagChanged bool,
) map[string]string {
	pr := map[string]string{}
	if awsRegion != "" {
		pr["aws"] = awsRegion
	}

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

func postAnalysis(client *api.SDKClient, req *api.AnalyzePrRequest) (*api.AnalyzePrResponse, error) {
	resp, err := client.Raw().AnalyzeIaC(context.Background(), req)
	var respErr *api.ResponseError
	if errors.As(err, &respErr) {
		err = describeAPIError(respErr.Response)
	}
	if err != nil {
		return nil, classifyAPIError(err)
	}
	return resp, nil
}

// A 401 means the credential is not valid, so re-authenticating fixes it. A 403
// means the credential is valid but not allowed to do this, and telling the user
// to log in again would send them down the wrong path.
const (
	authFailedHint = "authentication failed: verify your API key with 'l4 auth status --verify' or re-authenticate with 'l4 auth login'"
	forbiddenHint  = "permission denied: this API key lacks permission for this operation. Mint a read-write key in the dashboard under Settings, API Keys"
)

// classifyStatusError wraps an API error with the hint that matches its HTTP
// status. Any other status is returned untouched.
func classifyStatusError(status int, err error) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("%s: %w", authFailedHint, err)
	case http.StatusForbidden:
		return fmt.Errorf("%s: %w", forbiddenHint, err)
	}
	return err
}

// classifyAPIError is the same mapping for call paths that only hand back an
// error string. The client formats API failures as "API error (<status>): ...",
// so the status is recoverable from the message.
func classifyAPIError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "(403)") || strings.Contains(msg, "Forbidden"):
		return classifyStatusError(http.StatusForbidden, err)
	case strings.Contains(msg, "(401)") || strings.Contains(msg, "Unauthorized"):
		return classifyStatusError(http.StatusUnauthorized, err)
	}
	return err
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
		changes = append(changes, changeOf(s, "added", toAttributesChanged(s.Attributes, "new")))
	}
	return changes
}

func changeOf(s api.ResourceSnapshot, changeType string, attributes map[string]interface{}) api.ResourceChange {
	return api.ResourceChange{
		ResourceType:      s.Type,
		ResourceName:      s.Name,
		ChangeType:        changeType,
		File:              s.File,
		Scope:             s.Scope,
		ModulePath:        s.ModulePath,
		AttributesChanged: attributes,
	}
}

// diffKey identifies a resource across base and head. A snapshot file saved
// before resources carried a scope has none, so then the scope is left out.
func diffKey(s api.ResourceSnapshot, withScope bool) string {
	if withScope {
		return s.Scope + "|" + tf.SnapshotKey(s)
	}
	return tf.SnapshotKey(s)
}

func anyScoped(snapshots []api.ResourceSnapshot) bool {
	for _, s := range snapshots {
		if s.Scope != "" {
			return true
		}
	}
	return false
}

func diffSnapshots(old, new []api.ResourceSnapshot) []api.ResourceChange {
	withScope := anyScoped(old)
	oldMap := make(map[string]api.ResourceSnapshot)
	for _, s := range old {
		oldMap[diffKey(s, withScope)] = s
	}
	newMap := make(map[string]api.ResourceSnapshot)
	for _, s := range new {
		newMap[diffKey(s, withScope)] = s
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
				changes = append(changes, changeOf(newSnap, "modified", attrsChanged))
			}
		} else {
			changes = append(changes, changeOf(newSnap, "added", toAttributesChanged(newSnap.Attributes, "new")))
		}
	}

	for key, oldSnap := range oldMap {
		if _, exists := newMap[key]; !exists {
			changes = append(changes, changeOf(oldSnap, "removed", toAttributesChanged(oldSnap.Attributes, "old")))
		}
	}

	return changes
}

// loadSnapshotFile loads a .json argument that holds a snapshot saved by
// `l4 estimate --out-file`, and reports false for anything else, such as a
// CloudFormation template.
func loadSnapshotFile(p string) ([]api.ResourceSnapshot, bool) {
	if filepath.Ext(p) != ".json" {
		return nil, false
	}
	snapshots, err := loadSnapshotsFromFile(p)
	return snapshots, err == nil
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

// printJSONResult prints the analysis for --format json. --json, --jq and --template
// shape the output when given; --format json on its own prints the whole response.
func printJSONResult(data *api.AnalyzePrResponse) error {
	if output.HasFormattingFlags() {
		return output.PrintResult(data)
	}
	output.PrintJSON(data)
	return nil
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

func buildProjectLabel(args []string) string {
	if len(args) == 0 {
		return "."
	}
	return strings.Join(args, " ")
}
