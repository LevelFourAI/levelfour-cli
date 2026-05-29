package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func diffAPIHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"resource_cost_estimates": []map[string]interface{}{
					{
						"resource_type":           "aws_instance",
						"resource_name":           "test",
						"change_type":             "added",
						"new_monthly_cost":        100.0,
						"monthly_cost_difference": 100.0,
						"components":              []interface{}{},
					},
				},
				"cost_summary": map[string]interface{}{
					"total_new_monthly":        100.0,
					"total_monthly_difference": 100.0,
					"total_previous_monthly":   0.0,
					"estimable_count":          1,
					"total_count":              1,
				},
				"upgrade_suggestions": []interface{}{},
			},
		})
	}
}

func TestDiffWithBaseline(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	baseline := []map[string]interface{}{
		{
			"type":       "aws_instance",
			"name":       "test",
			"attributes": map[string]interface{}{"instance_type": "t3.small"},
		},
	}
	baselineData, _ := json.Marshal(baseline)
	baselineFile := filepath.Join(t.TempDir(), "baseline.json")
	os.WriteFile(baselineFile, baselineData, 0o600)

	out, _, err := executeCommand(t, "diff", baselineFile, tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "aws_instance") {
		t.Errorf("output missing resource, got: %s", out.String())
	}
}

func TestDiffWithoutBaseline(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "aws_instance") {
		t.Errorf("output missing resource, got: %s", out.String())
	}
}

func TestDiffMultipleJSONFilesError(t *testing.T) {
	tmpDir := t.TempDir()
	f1 := filepath.Join(tmpDir, "a.json")
	f2 := filepath.Join(tmpDir, "b.json")
	os.WriteFile(f1, []byte("[]"), 0o600)
	os.WriteFile(f2, []byte("[]"), 0o600)

	_, _, err := executeCommand(t, "diff", f1, f2, "--api", "http://localhost:9999", "--token", "l4_test_testkey123456789a")
	if err == nil {
		t.Fatal("expected error for multiple JSON files")
	}
	if !strings.Contains(err.Error(), "only one baseline") {
		t.Errorf("err = %q, want 'only one baseline'", err.Error())
	}
}

func TestDiffNoResourcesFound(t *testing.T) {
	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "empty.tf", "# nothing\n")

	out, _, err := executeCommand(t, "diff", tfDir, "--api", "http://localhost:9999", "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No Terraform resources found.") {
		t.Errorf("expected no-resources message, got: %s", out.String())
	}
}

func TestDiffNoChangesDetected(t *testing.T) {
	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	baseline := []map[string]interface{}{
		{
			"type":       "aws_instance",
			"name":       "test",
			"attributes": map[string]interface{}{"ami": "ami-123", "instance_type": "t3.micro"},
		},
	}
	baselineData, _ := json.Marshal(baseline)
	baselineFile := filepath.Join(t.TempDir(), "baseline.json")
	os.WriteFile(baselineFile, baselineData, 0o600)

	out, _, err := executeCommand(t, "diff", baselineFile, tfDir, "--api", "http://localhost:9999", "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No resource changes detected.") {
		t.Errorf("expected no-changes message, got: %s", out.String())
	}
}

func TestDiff401Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte("Invalid API key"))
	}))
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("err = %q, want authentication failed", err.Error())
	}
}

func TestDiffFormatJSON(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--format", "json", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "resource_cost_estimates") {
		t.Errorf("expected JSON output, got: %s", got)
	}
}

func TestDiffFormatJSONWithoutGlobalFlag(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--format", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiffFormatGithubComment(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--format", "github-comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "## Cost Estimate (diff)") {
		t.Errorf("expected markdown diff output, got: %s", got)
	}
}

func TestDiffFailAboveExceeded(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--fail-above", "50")
	if err == nil {
		t.Fatal("expected ErrIssuesFound")
	}
	if err != ErrIssuesFound {
		t.Errorf("err = %v, want ErrIssuesFound", err)
	}
}

func TestDiffFailAboveBelowThreshold(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--fail-above", "500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDiffPathNotFound(t *testing.T) {
	_, _, err := executeCommand(t, "diff", "/nonexistent/path/xyz", "--api", "http://localhost:9999", "--token", "l4_test_testkey123456789a")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "path not found") {
		t.Errorf("err = %q, want 'path not found'", err.Error())
	}
}

func TestDiffRegionAutoDetect(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"resource_cost_estimates": []map[string]interface{}{
					{
						"resource_type":           "aws_instance",
						"resource_name":           "test",
						"change_type":             "added",
						"new_monthly_cost":        100.0,
						"monthly_cost_difference": 100.0,
						"components":              []interface{}{},
					},
				},
				"cost_summary": map[string]interface{}{
					"total_new_monthly":        100.0,
					"total_monthly_difference": 100.0,
					"total_previous_monthly":   0.0,
					"estimable_count":          1,
					"total_count":              1,
				},
				"upgrade_suggestions": []interface{}{},
			},
		})
	}))
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)
	writeTFFile(t, tfDir, "provider.tf", providerTF)

	_, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody == nil {
		t.Fatal("no request body captured")
	}
	if region, ok := capturedBody["region"].(string); !ok || region != "eu-west-1" {
		t.Errorf("region = %v, want eu-west-1", capturedBody["region"])
	}
}

func TestDiffFailAboveWithGithubComment(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--format", "github-comment", "--fail-above", "50")
	if err != ErrIssuesFound {
		t.Errorf("err = %v, want ErrIssuesFound", err)
	}
}

func TestDiffGlobalJSONFlag(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "resource_cost_estimates") {
		t.Errorf("expected JSON output, got: %s", got)
	}
}

func TestDiffBaselineLoadError(t *testing.T) {
	nonexistent := filepath.Join(t.TempDir(), "missing.json")
	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "diff", nonexistent, tfDir, "--api", "http://localhost:9999", "--token", "l4_test_testkey123456789a")
	if err == nil {
		t.Fatal("expected error for missing baseline")
	}
	if !strings.Contains(err.Error(), "failed to load baseline") {
		t.Errorf("err = %q, want 'failed to load baseline'", err.Error())
	}
}

func TestDiffDefaultCurrentDir(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	origDir, _ := os.Getwd()
	os.Chdir(tfDir)
	defer os.Chdir(origDir)

	out, _, err := executeCommand(t, "diff", "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "aws_instance") {
		t.Errorf("output missing resource, got: %s", out.String())
	}
}

func TestDiffRegionExplicitOverridesAutoDetect(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"resource_cost_estimates": []interface{}{},
				"cost_summary": map[string]interface{}{
					"total_new_monthly":        0.0,
					"total_monthly_difference": 0.0,
					"total_previous_monthly":   0.0,
					"estimable_count":          0,
					"total_count":              0,
				},
				"upgrade_suggestions": []interface{}{},
			},
		})
	}))
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)
	writeTFFile(t, tfDir, "provider.tf", providerTF)

	_, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--region", "ap-southeast-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody == nil {
		t.Fatal("no request body captured")
	}
	if region, ok := capturedBody["region"].(string); !ok || region != "ap-southeast-1" {
		t.Errorf("region = %v, want ap-southeast-1", capturedBody["region"])
	}
}

func diffInitGitRepo(t *testing.T, dir string) {
	t.Helper()
	diffGitRun(t, dir, "init")
	diffGitRun(t, dir, "config", "user.email", "tester@example.com")
	diffGitRun(t, dir, "config", "user.name", "Test")
}

func diffGitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestDiffGitBaselineModifiedResource(t *testing.T) {
	var capturedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"resource_cost_estimates": []map[string]interface{}{
					{
						"resource_type":           "aws_instance",
						"resource_name":           "test",
						"change_type":             "modified",
						"new_monthly_cost":        50.0,
						"monthly_cost_difference": -50.0,
						"components":              []interface{}{},
					},
				},
				"cost_summary": map[string]interface{}{
					"total_new_monthly":        50.0,
					"total_monthly_difference": -50.0,
					"total_previous_monthly":   100.0,
					"estimable_count":          1,
					"total_count":              1,
				},
				"upgrade_suggestions": []interface{}{},
			},
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	diffInitGitRepo(t, dir)

	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`resource "aws_instance" "test" {
  ami           = "ami-123"
  instance_type = "t3.small"
}
`), 0o600)
	diffGitRun(t, dir, "add", "-A")
	diffGitRun(t, dir, "commit", "-m", "init")

	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`resource "aws_instance" "test" {
  ami           = "ami-123"
  instance_type = "t3.micro"
}
`), 0o600)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	out, _, err := executeCommand(t, "diff", ".", "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "aws_instance") {
		t.Errorf("output missing resource, got: %s", got)
	}
	if capturedBody == nil {
		t.Fatal("no request body captured")
	}
	changes, ok := capturedBody["resource_changes"].([]interface{})
	if !ok || len(changes) == 0 {
		t.Fatal("expected resource_changes in request body")
	}
	change := changes[0].(map[string]interface{})
	if change["change_type"] != "modified" {
		t.Errorf("change_type = %v, want modified", change["change_type"])
	}
}

func TestDiffGitBaselineExplicitBase(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	dir := t.TempDir()
	diffInitGitRepo(t, dir)

	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`resource "aws_instance" "test" {
  ami           = "ami-123"
  instance_type = "t3.small"
}
`), 0o600)
	diffGitRun(t, dir, "add", "-A")
	diffGitRun(t, dir, "commit", "-m", "init")

	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`resource "aws_instance" "test" {
  ami           = "ami-123"
  instance_type = "t3.micro"
}
`), 0o600)

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	defaultBranch := gitDefaultBranch(dir)
	out, _, err := executeCommand(t, "diff", "--base", defaultBranch, ".", "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "aws_instance") {
		t.Errorf("output missing resource, got: %s", out.String())
	}
}

func TestDiffGitBaselineNotARepo(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "diff", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Git baseline unavailable") {
		t.Errorf("expected fallback info message, got: %s", got)
	}
}

func TestDiffGitBaselineNoChanges(t *testing.T) {
	dir := t.TempDir()
	diffInitGitRepo(t, dir)

	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(basicTF), 0o600)
	diffGitRun(t, dir, "add", "-A")
	diffGitRun(t, dir, "commit", "-m", "init")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	out, _, err := executeCommand(t, "diff", ".", "--api", "http://localhost:9999", "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No resource changes detected.") {
		t.Errorf("expected no-changes message, got: %s", out.String())
	}
}

func TestDiffGitBaselineFileOverridesGit(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	dir := t.TempDir()
	diffInitGitRepo(t, dir)

	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(`resource "aws_instance" "test" {
  ami           = "ami-123"
  instance_type = "t3.small"
}
`), 0o600)
	diffGitRun(t, dir, "add", "-A")
	diffGitRun(t, dir, "commit", "-m", "init")

	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(basicTF), 0o600)

	baseline := []map[string]interface{}{
		{
			"type":       "aws_instance",
			"name":       "test",
			"attributes": map[string]interface{}{"instance_type": "t3.small"},
		},
	}
	baselineData, _ := json.Marshal(baseline)
	baselineFile := filepath.Join(t.TempDir(), "baseline.json")
	os.WriteFile(baselineFile, baselineData, 0o600)

	out, _, err := executeCommand(t, "diff", baselineFile, dir, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "aws_instance") {
		t.Errorf("output missing resource, got: %s", out.String())
	}
}

func TestDiffGitBaselineInvalidRef(t *testing.T) {
	srv := httptest.NewServer(diffAPIHandler())
	defer srv.Close()

	dir := t.TempDir()
	diffInitGitRepo(t, dir)

	os.WriteFile(filepath.Join(dir, "main.tf"), []byte(basicTF), 0o600)
	diffGitRun(t, dir, "add", "-A")
	diffGitRun(t, dir, "commit", "-m", "init")

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	out, _, err := executeCommand(t, "diff", "--base", "nonexistent-branch-xyz", ".", "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Git baseline unavailable") {
		t.Errorf("expected fallback info message, got: %s", got)
	}
}
