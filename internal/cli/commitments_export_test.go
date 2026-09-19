package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportCommitmentsCSVLeavesAPlanCoverageEmpty(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "export", "commitments", "--format", "csv")
	if err != nil {
		t.Fatalf("export error: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected a header and two rows, got:\n%s", out.String())
	}
	if !strings.HasPrefix(lines[0], "id,service,kind,") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1],
		"ri-a1b2,ec2,reserved_instance,111122223333,prod,2026-10-07T00:00:00Z,98.2,61.4,12400,,active") {
		t.Errorf("reservation row = %q", lines[1])
	}
	// An empty cell is a spreadsheet's null. A zero would claim the plan covers
	// none of the eligible usage, which nothing measured.
	if !strings.Contains(lines[2], "sp-9f3e,compute,savings_plan,111122223333,payer,,100,,,,active") {
		t.Errorf("plan row = %q", lines[2])
	}
}

func TestExportCommitmentsJSON(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "export", "commitments", "--format", "json")
	if err != nil {
		t.Fatalf("export error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if _, ok := payload["data"]; !ok {
		t.Errorf("JSON export should carry the payload: %v", payload)
	}
}

func TestExportCommitmentsGoogleCloud(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{"": gcpListBody}))

	out, _, err := executeCommand(t, "export", "commitments", "--format", "csv")
	if err != nil {
		t.Fatalf("export error: %v", err)
	}
	if !strings.Contains(out.String(), "cud-1,gcp,compute,committed_use_discount") {
		t.Errorf("Google Cloud rows should export from the list route:\n%s", out.String())
	}
}

func TestExportCommitmentsGoogleCloudJSON(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{"": gcpListBody}))

	out, _, err := executeCommand(t, "export", "commitments", "--format", "json")
	if err != nil {
		t.Fatalf("export error: %v", err)
	}
	if !strings.Contains(out.String(), "cud-1") {
		t.Errorf("JSON export should carry the payload:\n%s", out.String())
	}
}

func TestExportCommitmentsToAFile(t *testing.T) {
	awsPortfolioServer(t)
	path := filepath.Join(t.TempDir(), "commitments.csv")

	if _, _, err := executeCommand(t, "export", "commitments", "--format", "csv", "--out", path); err != nil {
		t.Fatalf("export error: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the export: %v", err)
	}
	if !strings.Contains(string(contents), "ri-a1b2") {
		t.Errorf("file = %q", contents)
	}
}

func TestExportCommitmentsReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "export", "commitments", "--format", "csv")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}

	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, nil))
	if _, _, err = executeCommand(t, "export", "commitments", "--format", "csv"); err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
}

func TestExportCommitmentsUnauthenticated(t *testing.T) {
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "export", "commitments"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}
