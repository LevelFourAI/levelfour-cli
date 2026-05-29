package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func estimateAPIHandler() http.HandlerFunc {
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

func writeTFFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const basicTF = `resource "aws_instance" "test" {
  ami           = "ami-123"
  instance_type = "t3.micro"
}
`

const providerTF = `provider "aws" {
  region = "eu-west-1"
}
`

func TestEstimateDefaultCurrentDir(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	origDir, _ := os.Getwd()
	os.Chdir(tfDir)
	defer os.Chdir(origDir)

	out, _, err := executeCommand(t, "estimate", "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "aws_instance") {
		t.Errorf("output missing resource, got: %s", out.String())
	}
}

func TestEstimateSingleFileArg(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	tfFile := writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "estimate", tfFile, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "aws_instance") {
		t.Errorf("output missing resource, got: %s", out.String())
	}
}

func TestEstimateDirectoryArg(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "aws_instance") {
		t.Errorf("output missing resource, got: %s", out.String())
	}
}

func TestEstimateNoResourcesFound(t *testing.T) {
	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "empty.tf", "# nothing\n")

	out, _, err := executeCommand(t, "estimate", tfDir, "--api", "http://localhost:9999", "--token", "l4_test_testkey123456789a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No Terraform resources found.") {
		t.Errorf("expected no-resources message, got: %s", out.String())
	}
}

func TestEstimate401Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte("Invalid API key"))
	}))
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error = %q, want authentication failed", err.Error())
	}
}

func TestEstimateOutFile(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	outFile := filepath.Join(t.TempDir(), "snapshot.json")

	_, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--out-file", outFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, readErr := os.ReadFile(outFile)
	if readErr != nil {
		t.Fatalf("failed to read out-file: %v", readErr)
	}

	var snapshots []map[string]interface{}
	if json.Unmarshal(data, &snapshots) != nil {
		t.Fatalf("out-file is not valid JSON: %s", string(data))
	}
	if len(snapshots) == 0 {
		t.Error("expected at least one snapshot")
	}
}

func TestEstimateFormatJSON(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--format", "json", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "resource_cost_estimates") {
		t.Errorf("expected JSON output with resource_cost_estimates, got: %s", got)
	}
}

func TestEstimateFormatJSONWithoutGlobalFlag(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--format", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEstimateFormatGithubComment(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--format", "github-comment")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "## Cost Estimate") {
		t.Errorf("expected markdown output, got: %s", got)
	}
}

func TestEstimateFailAboveExceeded(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--fail-above", "50")
	if err == nil {
		t.Fatal("expected ErrIssuesFound")
	}
	if err != ErrIssuesFound {
		t.Errorf("err = %v, want ErrIssuesFound", err)
	}
}

func TestEstimateFailAboveBelowThreshold(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--fail-above", "500")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEstimateRegionAutoDetect(t *testing.T) {
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

	_, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a")
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

func TestEstimatePathNotFound(t *testing.T) {
	_, _, err := executeCommand(t, "estimate", "/nonexistent/path/xyz", "--api", "http://localhost:9999", "--token", "l4_test_testkey123456789a")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
	if !strings.Contains(err.Error(), "path not found") {
		t.Errorf("err = %q, want 'path not found'", err.Error())
	}
}

func TestEstimateFailAboveWithGithubComment(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	_, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--format", "github-comment", "--fail-above", "50")
	if err != ErrIssuesFound {
		t.Errorf("err = %v, want ErrIssuesFound", err)
	}
}

func TestEstimateGlobalJSONFlag(t *testing.T) {
	srv := httptest.NewServer(estimateAPIHandler())
	defer srv.Close()

	tfDir := t.TempDir()
	writeTFFile(t, tfDir, "main.tf", basicTF)

	out, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "resource_cost_estimates") {
		t.Errorf("expected JSON output, got: %s", got)
	}
}

func TestEstimateRegionExplicitOverridesAutoDetect(t *testing.T) {
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

	_, _, err := executeCommand(t, "estimate", tfDir, "--api", srv.URL, "--token", "l4_test_testkey123456789a", "--region", "ap-southeast-1")
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
