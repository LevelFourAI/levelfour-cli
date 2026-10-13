package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	tf "github.com/LevelFourAI/levelfour-cli/internal/terraform"
)

func TestPostAnalysis_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/iac-analysis/analyze" {
			t.Errorf("path = %q, want /api/v1/iac-analysis/analyze", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"timestamp": "2024-01-01T00:00:00Z",
			"data": map[string]interface{}{
				"cost_summary": map[string]interface{}{
					"total_monthly_difference": 42.0,
					"total_previous_monthly":   0,
					"total_new_monthly":        42.0,
					"total_new_infrastructure": 42.0,
					"formatted":                "+$42.00/mo",
					"estimable_count":          1,
					"total_count":              1,
				},
			},
		})
	}))
	defer srv.Close()

	client, _ := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "0.0.0-test")
	result, err := postAnalysis(client, &api.AnalyzePrRequest{Region: api.StringPtr("us-east-1")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.CostSummary == nil {
		t.Fatal("expected non-nil result with cost_summary")
	}
	if result.CostSummary.TotalMonthlyDifference != 42.0 {
		t.Errorf("TotalMonthlyDifference = %v, want 42", result.CostSummary.TotalMonthlyDifference)
	}
}

func TestPostAnalysis_401Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
	}))
	defer srv.Close()

	client, _ := api.NewSDKClient(srv.URL, "l4_test_badkey1234567890a", "0.0.0-test")
	_, err := postAnalysis(client, &api.AnalyzePrRequest{Region: api.StringPtr("us-east-1")})
	if err == nil {
		t.Fatal("expected error for 401")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error = %q, want 'authentication failed'", err.Error())
	}
	if !strings.Contains(err.Error(), "l4 auth status --verify") {
		t.Errorf("error = %q, want mention of 'l4 auth status --verify'", err.Error())
	}
}

// A 403 means the key is valid but read-only. Telling the user to log in again
// would send them chasing the wrong problem.
func TestPostAnalysis_403Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"error":{"message":"API key scope 'read' is insufficient"}}`))
	}))
	defer srv.Close()

	client, _ := api.NewSDKClient(srv.URL, "l4_test_readonly123456789", "0.0.0-test")
	_, err := postAnalysis(client, &api.AnalyzePrRequest{Region: api.StringPtr("us-east-1")})
	if err == nil {
		t.Fatal("expected error for 403")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error = %q, want 'permission denied'", err.Error())
	}
	if !strings.Contains(err.Error(), "read-write key") {
		t.Errorf("error = %q, want it to mention minting a read-write key", err.Error())
	}
	if strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("403 must not be reported as an auth failure, got %q", err.Error())
	}
	if strings.Contains(err.Error(), "l4 auth login") {
		t.Errorf("403 must not suggest re-authenticating, got %q", err.Error())
	}
}

func TestClassifyAPIError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		want    string
		notWant string
	}{
		{"nil stays nil", nil, "", ""},
		{"403 by status", errors.New("API error (403): forbidden"), "permission denied", "authentication failed"},
		{"403 by phrase", errors.New("Forbidden"), "permission denied", "authentication failed"},
		{"401 by status", errors.New("API error (401): bad key"), "authentication failed", "permission denied"},
		{"401 by phrase", errors.New("Unauthorized"), "authentication failed", "permission denied"},
		{"500 passes through", errors.New("API error (500): boom"), "boom", "permission denied"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyAPIError(tt.err)
			if tt.err == nil {
				if got != nil {
					t.Fatalf("classifyAPIError(nil) = %v, want nil", got)
				}
				return
			}
			if !strings.Contains(got.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", got.Error(), tt.want)
			}
			if strings.Contains(got.Error(), tt.notWant) {
				t.Errorf("error = %q, must not contain %q", got.Error(), tt.notWant)
			}
		})
	}
}

func TestClassifyStatusError(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{"401", 401, "authentication failed"},
		{"403", 403, "permission denied"},
		{"409", 409, "conflict"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyStatusError(tt.status, errors.New("conflict"))
			if !strings.Contains(got.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", got.Error(), tt.want)
			}
		})
	}
}

func TestClassifyStatusErrorPassesOtherStatusesThrough(t *testing.T) {
	// Anything that is not 401 or 403 must come back untouched, so an API message
	// such as a 409 conflict is never dressed up as an auth problem.
	sentinel := errors.New("conflict")
	if got := classifyStatusError(http.StatusConflict, sentinel); got != sentinel {
		t.Errorf("classifyStatusError(409, err) = %v, want the error returned untouched", got)
	}
}

func TestPostAnalysis_Non401Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`internal server error`))
	}))
	defer srv.Close()

	client, _ := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "0.0.0-test")
	_, err := postAnalysis(client, &api.AnalyzePrRequest{Region: api.StringPtr("us-east-1")})
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("500 should not produce auth error, got %q", err.Error())
	}
}

func TestToAttributesChanged(t *testing.T) {
	attrs := map[string]interface{}{
		"instance_type": "t2.micro",
		"ami":           "ami-123",
	}
	result := toAttributesChanged(attrs, "new")
	for k, v := range result {
		m, ok := v.(map[string]interface{})
		if !ok {
			t.Fatalf("value for %q is not a map", k)
		}
		if m["new"] != attrs[k] {
			t.Errorf("result[%q][new] = %v, want %v", k, m["new"], attrs[k])
		}
	}
}

func TestToAttributesChanged_Empty(t *testing.T) {
	result := toAttributesChanged(map[string]interface{}{}, "old")
	if len(result) != 0 {
		t.Errorf("result = %v, want empty", result)
	}
}

func TestSnapshotsToAddedChanges(t *testing.T) {
	snaps := []api.ResourceSnapshot{
		{
			Type:       "aws_instance",
			Name:       "web",
			Attributes: map[string]interface{}{"ami": "ami-123"},
		},
		{
			Type:       "aws_s3_bucket",
			Name:       "data",
			Attributes: map[string]interface{}{"bucket": "my-bucket"},
		},
	}

	changes := snapshotsToAddedChanges(snaps)
	if len(changes) != 2 {
		t.Fatalf("len(changes) = %d, want 2", len(changes))
	}
	for i, c := range changes {
		if c.ChangeType != "added" {
			t.Errorf("changes[%d].ChangeType = %q, want added", i, c.ChangeType)
		}
		if c.ResourceType != snaps[i].Type {
			t.Errorf("changes[%d].ResourceType = %q, want %q", i, c.ResourceType, snaps[i].Type)
		}
		if c.ResourceName != snaps[i].Name {
			t.Errorf("changes[%d].ResourceName = %q, want %q", i, c.ResourceName, snaps[i].Name)
		}
	}
}

func TestSnapshotsToAddedChanges_Empty(t *testing.T) {
	changes := snapshotsToAddedChanges(nil)
	if len(changes) != 0 {
		t.Errorf("len(changes) = %d, want 0", len(changes))
	}
}

func TestDiffSnapshots_Added(t *testing.T) {
	old := []api.ResourceSnapshot{}
	new := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"ami": "ami-123"}},
	}

	changes := diffSnapshots(old, new)
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	if changes[0].ChangeType != "added" {
		t.Errorf("ChangeType = %q, want added", changes[0].ChangeType)
	}
}

func TestDiffSnapshots_Removed(t *testing.T) {
	old := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"ami": "ami-123"}},
	}
	new := []api.ResourceSnapshot{}

	changes := diffSnapshots(old, new)
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	if changes[0].ChangeType != "removed" {
		t.Errorf("ChangeType = %q, want removed", changes[0].ChangeType)
	}
}

func TestDiffSnapshots_Modified(t *testing.T) {
	old := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"instance_type": "t2.micro"}},
	}
	new := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"instance_type": "t2.large"}},
	}

	changes := diffSnapshots(old, new)
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	if changes[0].ChangeType != "modified" {
		t.Errorf("ChangeType = %q, want modified", changes[0].ChangeType)
	}
	ac := changes[0].AttributesChanged["instance_type"].(map[string]interface{})
	if ac["old"] != "t2.micro" || ac["new"] != "t2.large" {
		t.Errorf("attributes = %v, want old=t2.micro new=t2.large", ac)
	}
}

func TestDiffSnapshots_Unchanged(t *testing.T) {
	snap := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"instance_type": "t2.micro"}},
	}

	changes := diffSnapshots(snap, snap)
	if len(changes) != 0 {
		t.Errorf("len(changes) = %d, want 0 for unchanged", len(changes))
	}
}

func TestDiffSnapshots_NewAttrAdded(t *testing.T) {
	old := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"ami": "ami-123"}},
	}
	new := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"ami": "ami-123", "instance_type": "t2.micro"}},
	}

	changes := diffSnapshots(old, new)
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	ac := changes[0].AttributesChanged["instance_type"].(map[string]interface{})
	if _, ok := ac["old"]; ok {
		t.Error("old key should not exist for newly added attr")
	}
	if ac["new"] != "t2.micro" {
		t.Errorf("new = %v, want t2.micro", ac["new"])
	}
}

func TestDiffSnapshots_AttrRemoved(t *testing.T) {
	old := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"ami": "ami-123", "instance_type": "t2.micro"}},
	}
	new := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"ami": "ami-123"}},
	}

	changes := diffSnapshots(old, new)
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	ac := changes[0].AttributesChanged["instance_type"].(map[string]interface{})
	if ac["old"] != "t2.micro" {
		t.Errorf("old = %v, want t2.micro", ac["old"])
	}
	if _, ok := ac["new"]; ok {
		t.Error("new key should not exist for removed attr")
	}
}

func TestDiffSnapshots_Empty(t *testing.T) {
	changes := diffSnapshots(nil, nil)
	if len(changes) != 0 {
		t.Errorf("len(changes) = %d, want 0", len(changes))
	}
}

func TestLoadSnapshotsFromFile_ArrayFormat(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "snaps.json")
	data := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"ami": "ami-123"}},
	}
	b, _ := json.Marshal(data)
	os.WriteFile(f, b, 0o600)

	snaps, err := loadSnapshotsFromFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("len = %d, want 1", len(snaps))
	}
	if snaps[0].Type != "aws_instance" {
		t.Errorf("type = %q, want aws_instance", snaps[0].Type)
	}
}

func TestLoadSnapshotsFromFile_EnvelopeFormat(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "snaps.json")
	envelope := map[string]interface{}{
		"head_resources": []api.ResourceSnapshot{
			{Type: "aws_s3_bucket", Name: "b", Attributes: map[string]interface{}{"bucket": "test"}},
		},
	}
	b, _ := json.Marshal(envelope)
	os.WriteFile(f, b, 0o600)

	snaps, err := loadSnapshotsFromFile(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("len = %d, want 1", len(snaps))
	}
	if snaps[0].Type != "aws_s3_bucket" {
		t.Errorf("type = %q, want aws_s3_bucket", snaps[0].Type)
	}
}

func TestLoadSnapshotsFromFile_UnrecognizedFormat(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "bad.json")
	os.WriteFile(f, []byte(`{"something_else": true}`), 0o600)

	_, err := loadSnapshotsFromFile(f)
	if err == nil {
		t.Fatal("expected error for unrecognized format")
	}
	if !strings.Contains(err.Error(), "unrecognized file format") {
		t.Errorf("error = %q, want 'unrecognized file format'", err.Error())
	}
}

func TestLoadSnapshotsFromFile_InvalidJSON(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "bad.json")
	os.WriteFile(f, []byte(`not json at all`), 0o600)

	_, err := loadSnapshotsFromFile(f)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unrecognized file format") {
		t.Errorf("error = %q, want 'unrecognized file format'", err.Error())
	}
}

func TestLoadSnapshotsFromFile_FileNotFound(t *testing.T) {
	_, err := loadSnapshotsFromFile("/nonexistent/path/xyz.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoadSnapshotsFromFile_ReadError(t *testing.T) {
	origReadFile := osReadFile
	osReadFile = func(name string) ([]byte, error) {
		return nil, errors.New("simulated read error")
	}
	defer func() { osReadFile = origReadFile }()

	_, err := loadSnapshotsFromFile("any.json")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "simulated read error") {
		t.Errorf("error = %q, want 'simulated read error'", err.Error())
	}
}

func TestLoadSnapshotsFromFile_EnvelopeWithBadResources(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "bad_envelope.json")
	os.WriteFile(f, []byte(`{"head_resources": "not-an-array"}`), 0o600)

	_, err := loadSnapshotsFromFile(f)
	if err == nil {
		t.Fatal("expected error for bad head_resources type")
	}
	if !strings.Contains(err.Error(), "unrecognized file format") {
		t.Errorf("error = %q, want 'unrecognized file format'", err.Error())
	}
}

func TestSaveSnapshots(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "out.json")
	snaps := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"ami": "ami-123"}},
	}

	err := saveSnapshots(f, snaps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(f)
	var loaded []api.ResourceSnapshot
	if jsonErr := json.Unmarshal(data, &loaded); jsonErr != nil {
		t.Fatalf("failed to unmarshal saved file: %v", jsonErr)
	}
	if len(loaded) != 1 {
		t.Fatalf("len = %d, want 1", len(loaded))
	}
	if loaded[0].Type != "aws_instance" {
		t.Errorf("type = %q, want aws_instance", loaded[0].Type)
	}
}

func TestSaveSnapshots_MarshalError(t *testing.T) {
	snaps := []api.ResourceSnapshot{
		{Type: "x", Name: "y", Attributes: map[string]interface{}{"bad": make(chan int)}},
	}
	err := saveSnapshots("/tmp/should-not-write.json", snaps)
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestSaveSnapshots_WriteError(t *testing.T) {
	origWriteFile := osWriteFile
	osWriteFile = func(name string, data []byte, perm os.FileMode) error {
		return errors.New("simulated write error")
	}
	defer func() { osWriteFile = origWriteFile }()

	err := saveSnapshots("/any/path.json", []api.ResourceSnapshot{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "simulated write error") {
		t.Errorf("error = %q, want 'simulated write error'", err.Error())
	}
}

func TestSaveSnapshots_Roundtrip(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "roundtrip.json")
	snaps := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "a", Attributes: map[string]interface{}{"x": "1"}},
		{Type: "aws_s3_bucket", Name: "b", Attributes: map[string]interface{}{"bucket": "bkt"}},
	}

	if err := saveSnapshots(f, snaps); err != nil {
		t.Fatalf("save error: %v", err)
	}

	loaded, err := loadSnapshotsFromFile(f)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("len = %d, want 2", len(loaded))
	}
}

func TestCheckFailAbove_BelowThreshold(t *testing.T) {
	data := &api.AnalyzePrResponse{
		CostSummary: &api.CostSummary{TotalMonthlyDifference: 50.0},
	}
	err := checkFailAbove(data, 100.0)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCheckFailAbove_ExceedsThreshold(t *testing.T) {
	data := &api.AnalyzePrResponse{
		CostSummary: &api.CostSummary{TotalMonthlyDifference: 150.0},
	}
	err := checkFailAbove(data, 100.0)
	if !errors.Is(err, ErrIssuesFound) {
		t.Errorf("err = %v, want ErrIssuesFound", err)
	}
}

func TestCheckFailAbove_ExactThreshold(t *testing.T) {
	data := &api.AnalyzePrResponse{
		CostSummary: &api.CostSummary{TotalMonthlyDifference: 100.0},
	}
	err := checkFailAbove(data, 100.0)
	if err != nil {
		t.Errorf("unexpected error: %v, delta == threshold should not fail", err)
	}
}

func TestCheckFailAbove_ZeroThreshold(t *testing.T) {
	data := &api.AnalyzePrResponse{
		CostSummary: &api.CostSummary{TotalMonthlyDifference: 999.0},
	}
	err := checkFailAbove(data, 0)
	if err != nil {
		t.Errorf("unexpected error: %v, zero threshold should never fail", err)
	}
}

func TestCheckFailAbove_NilCostSummary(t *testing.T) {
	data := &api.AnalyzePrResponse{CostSummary: nil}
	err := checkFailAbove(data, 100.0)
	if err != nil {
		t.Errorf("unexpected error: %v, nil CostSummary should use delta=0", err)
	}
}

func TestCheckFailAbove_NegativeDelta(t *testing.T) {
	data := &api.AnalyzePrResponse{
		CostSummary: &api.CostSummary{TotalMonthlyDifference: -50.0},
	}
	err := checkFailAbove(data, 10.0)
	if err != nil {
		t.Errorf("unexpected error: %v, negative delta should not exceed positive threshold", err)
	}
}

func TestBuildProjectLabel_Empty(t *testing.T) {
	got := buildProjectLabel(nil)
	if got != "." {
		t.Errorf("got %q, want '.'", got)
	}
}

func TestBuildProjectLabel_EmptySlice(t *testing.T) {
	got := buildProjectLabel([]string{})
	if got != "." {
		t.Errorf("got %q, want '.'", got)
	}
}

func TestBuildProjectLabel_SingleArg(t *testing.T) {
	got := buildProjectLabel([]string{"infra"})
	if got != "infra" {
		t.Errorf("got %q, want infra", got)
	}
}

func TestBuildProjectLabel_MultipleArgs(t *testing.T) {
	got := buildProjectLabel([]string{"infra", "modules/vpc"})
	if got != "infra modules/vpc" {
		t.Errorf("got %q, want 'infra modules/vpc'", got)
	}
}

func TestIsExcluded_ExactMatch(t *testing.T) {
	patterns := []string{"aws_iam_role", "aws_s3_bucket"}
	if !isExcluded("aws_iam_role", patterns) {
		t.Error("expected aws_iam_role to be excluded")
	}
	if isExcluded("aws_iam_policy", patterns) {
		t.Error("expected aws_iam_policy not to be excluded")
	}
}

func TestIsExcluded_GlobPattern(t *testing.T) {
	patterns := []string{"aws_cloudwatch_*"}
	if !isExcluded("aws_cloudwatch_log_group", patterns) {
		t.Error("expected aws_cloudwatch_log_group to be excluded")
	}
	if !isExcluded("aws_cloudwatch_metric_alarm", patterns) {
		t.Error("expected aws_cloudwatch_metric_alarm to be excluded")
	}
	if isExcluded("aws_instance", patterns) {
		t.Error("expected aws_instance not to be excluded")
	}
}

func TestIsExcluded_MultipleWildcards(t *testing.T) {
	patterns := []string{"aws_*_log_*"}
	if !isExcluded("aws_cloudwatch_log_group", patterns) {
		t.Error("expected aws_cloudwatch_log_group to match aws_*_log_*")
	}
	if isExcluded("aws_instance", patterns) {
		t.Error("expected aws_instance not to match aws_*_log_*")
	}
}

func TestIsExcluded_NoWildcardNoPartialMatch(t *testing.T) {
	patterns := []string{"aws_s3_bucket"}
	if isExcluded("aws_s3_bucket_policy", patterns) {
		t.Error("exact match pattern should not match aws_s3_bucket_policy")
	}
}

func TestIsExcluded_EmptyPatterns(t *testing.T) {
	if isExcluded("aws_instance", nil) {
		t.Error("nil patterns should never exclude")
	}
	if isExcluded("aws_instance", []string{}) {
		t.Error("empty patterns should never exclude")
	}
}

func TestFilterExcludedResources(t *testing.T) {
	snaps := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web"},
		{Type: "aws_cloudwatch_log_group", Name: "logs"},
		{Type: "aws_cloudwatch_metric_alarm", Name: "alarm"},
		{Type: "aws_s3_bucket", Name: "data"},
	}

	filtered := filterExcludedResources(snaps, []string{"aws_cloudwatch_*"})
	if len(filtered) != 2 {
		t.Fatalf("len = %d, want 2", len(filtered))
	}
	if filtered[0].Type != "aws_instance" {
		t.Errorf("filtered[0].Type = %q, want aws_instance", filtered[0].Type)
	}
	if filtered[1].Type != "aws_s3_bucket" {
		t.Errorf("filtered[1].Type = %q, want aws_s3_bucket", filtered[1].Type)
	}
}

func TestFilterExcludedResources_NoPatterns(t *testing.T) {
	snaps := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web"},
	}
	filtered := filterExcludedResources(snaps, nil)
	if len(filtered) != 1 {
		t.Fatalf("len = %d, want 1", len(filtered))
	}
}

func TestDiffSnapshots_ModulePath(t *testing.T) {
	old := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", ModulePath: "module.app", Attributes: map[string]interface{}{"ami": "ami-old"}},
	}
	new := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", ModulePath: "module.app", Attributes: map[string]interface{}{"ami": "ami-new"}},
	}

	changes := diffSnapshots(old, new)
	if len(changes) != 1 {
		t.Fatalf("len(changes) = %d, want 1", len(changes))
	}
	if changes[0].ChangeType != "modified" {
		t.Errorf("ChangeType = %q, want modified", changes[0].ChangeType)
	}
}

func TestDiffSnapshots_DifferentModulePaths(t *testing.T) {
	old := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", ModulePath: "module.a", Attributes: map[string]interface{}{"ami": "ami-123"}},
	}
	new := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "web", ModulePath: "module.b", Attributes: map[string]interface{}{"ami": "ami-123"}},
	}

	changes := diffSnapshots(old, new)
	if len(changes) != 2 {
		t.Fatalf("len(changes) = %d, want 2 (1 removed + 1 added)", len(changes))
	}
}

func TestSnapshotsToAddedChanges_WrapsAttributes(t *testing.T) {
	snaps := []api.ResourceSnapshot{
		{Type: "aws_instance", Name: "w", Attributes: map[string]interface{}{"k": "v"}},
	}
	changes := snapshotsToAddedChanges(snaps)
	ac := changes[0].AttributesChanged["k"].(map[string]interface{})
	if ac["new"] != "v" {
		t.Errorf("expected new=v, got %v", ac["new"])
	}
	if _, ok := ac["old"]; ok {
		t.Error("old key should not exist in added change")
	}
}

func TestPostAnalysis_SendsPayloadFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]interface{}
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["region"] != "us-east-1" {
			t.Errorf("region = %v, want us-east-1", payload["region"])
		}
		if _, ok := payload["head_resources"]; !ok {
			t.Error("missing head_resources")
		}
		if _, ok := payload["resource_changes"]; !ok {
			t.Error("missing resource_changes")
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"timestamp": "2024-01-01T00:00:00Z",
			"data":      map[string]interface{}{},
		})
	}))
	defer srv.Close()

	client, _ := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "0.0.0-test")
	snaps := []api.ResourceSnapshot{{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"ami": "ami-123"}}}
	changes := []api.ResourceChange{{ResourceType: "aws_instance", ResourceName: "web", ChangeType: "added"}}
	_, err := postAnalysis(client, (&analysisInputs{terraform: snaps, region: "us-east-1"}).request(changes, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "init")
	gitRun(t, dir, "config", "user.email", "tester@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	gitRun(t, dir, "config", "commit.gpgSign", "false")
	gitRun(t, dir, "config", "tag.gpgSign", "false")
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestGitRepoRoot(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	root, err := gitRepoRoot(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(root)
	if got != expected {
		t.Errorf("root = %q, want %q", got, expected)
	}
}

func TestGitRepoRoot_FilePath(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	filePath := filepath.Join(dir, "main.tf")
	os.WriteFile(filePath, []byte("resource \"null\" \"x\" {}"), 0o600)

	root, err := gitRepoRoot(filePath)
	if err != nil {
		t.Fatalf("unexpected error for file path: %v", err)
	}
	expected, _ := filepath.EvalSymlinks(dir)
	got, _ := filepath.EvalSymlinks(root)
	if got != expected {
		t.Errorf("root = %q, want %q", got, expected)
	}
}

func TestGitRepoRoot_NotARepo(t *testing.T) {
	dir := t.TempDir()
	_, err := gitRepoRoot(dir)
	if err == nil {
		t.Fatal("expected error for non-git dir")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error = %q, want 'not a git repository'", err.Error())
	}
}

func TestGitDefaultBranch_Main(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "init")

	branch := gitDefaultBranch(dir)
	if branch != "main" && branch != "master" {
		t.Errorf("branch = %q, want main or master", branch)
	}
}

func TestGitDefaultBranch_Master(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	gitRun(t, dir, "checkout", "-b", "master")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "init")

	branch := gitDefaultBranch(dir)
	if branch != "master" {
		t.Errorf("branch = %q, want master", branch)
	}
}

func TestGitMergeBase(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "init")

	defaultBranch := gitDefaultBranch(dir)
	sha, err := gitMergeBase(dir, defaultBranch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sha) < 7 {
		t.Errorf("sha = %q, expected a commit hash", sha)
	}
}

func TestGitMergeBase_InvalidRef(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o600)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "init")

	_, err := gitMergeBase(dir, "nonexistent-branch-xyz")
	if err == nil {
		t.Fatal("expected error for invalid ref")
	}
	if !strings.Contains(err.Error(), "merge-base failed") {
		t.Errorf("error = %q, want 'merge-base failed'", err.Error())
	}
}

func TestGitDefaultBranch_NoBranch(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	branch := gitDefaultBranch(dir)
	if branch != "" {
		t.Errorf("branch = %q, want empty for repo with no commits", branch)
	}
}

func TestBuildProviderRegions_FlagOverride(t *testing.T) {
	result := buildProviderRegions(
		"us-east-1", "asia-east1", "westeurope",
		nil, tf.ProviderRegions{}, true, true,
	)
	if result["aws"] != "us-east-1" {
		t.Errorf("aws = %q, want us-east-1", result["aws"])
	}
	if result["gcp"] != "asia-east1" {
		t.Errorf("gcp = %q, want asia-east1", result["gcp"])
	}
	if result["azure"] != "westeurope" {
		t.Errorf("azure = %q, want westeurope", result["azure"])
	}
}

func TestBuildProviderRegions_ConfigFallback(t *testing.T) {
	cfg := map[string]string{"gcp": "europe-west1", "azure": "northeurope"}
	result := buildProviderRegions(
		"us-east-1", "", "",
		cfg, tf.ProviderRegions{}, false, false,
	)
	if result["gcp"] != "europe-west1" {
		t.Errorf("gcp = %q, want europe-west1", result["gcp"])
	}
	if result["azure"] != "northeurope" {
		t.Errorf("azure = %q, want northeurope", result["azure"])
	}
}

func TestBuildProviderRegions_AutoDetectFallback(t *testing.T) {
	detected := tf.ProviderRegions{GCP: "us-west1", Azure: "eastus2"}
	result := buildProviderRegions(
		"us-east-1", "", "",
		nil, detected, false, false,
	)
	if result["gcp"] != "us-west1" {
		t.Errorf("gcp = %q, want us-west1", result["gcp"])
	}
	if result["azure"] != "eastus2" {
		t.Errorf("azure = %q, want eastus2", result["azure"])
	}
}

func TestBuildProviderRegions_Defaults(t *testing.T) {
	result := buildProviderRegions(
		"us-east-1", "", "",
		nil, tf.ProviderRegions{}, false, false,
	)
	if result["gcp"] != "us-central1" {
		t.Errorf("gcp = %q, want us-central1", result["gcp"])
	}
	if result["azure"] != "eastus" {
		t.Errorf("azure = %q, want eastus", result["azure"])
	}
}

func TestLoadUsageOverrides_FromFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "usage.yml"), []byte("lambda_invocations: 5000000\n"), 0o600)
	result := loadUsageOverrides(dir)
	if result == nil {
		t.Fatal("expected overrides, got nil")
	}
	if result["lambda_invocations"] != 5000000 {
		t.Errorf("lambda_invocations = %v, want 5000000", result["lambda_invocations"])
	}
}

func TestLoadUsageOverrides_FromDotLevelfour(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".levelfour"), 0o755)
	os.WriteFile(filepath.Join(dir, ".levelfour", "usage.yml"), []byte("s3_storage_gb: 500\n"), 0o600)
	result := loadUsageOverrides(dir)
	if result == nil {
		t.Fatal("expected overrides, got nil")
	}
	if result["s3_storage_gb"] != 500 {
		t.Errorf("s3_storage_gb = %v, want 500", result["s3_storage_gb"])
	}
}

func TestLoadUsageOverrides_NoFile(t *testing.T) {
	dir := t.TempDir()
	result := loadUsageOverrides(dir)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestLoadUsageOverrides_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "usage.yml"), []byte(""), 0o600)
	result := loadUsageOverrides(dir)
	if result != nil {
		t.Errorf("expected nil for empty file, got %v", result)
	}
}

func TestPostAnalysis_ProviderRegionsInPayload(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"timestamp": "2024-01-01T00:00:00Z",
			"data":      map[string]interface{}{},
		})
	}))
	defer srv.Close()

	client, _ := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "0.0.0-test")
	pr := map[string]string{"aws": "us-east-1", "gcp": "us-central1", "azure": "eastus"}
	_, err := postAnalysis(client, (&analysisInputs{region: "us-east-1", providerRegions: pr}).request(nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received["provider_regions"] == nil {
		t.Error("expected provider_regions in payload")
	}
}

func TestPostAnalysis_UsageOverridesInPayload(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"timestamp": "2024-01-01T00:00:00Z",
			"data":      map[string]interface{}{},
		})
	}))
	defer srv.Close()

	client, _ := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "0.0.0-test")
	overrides := map[string]interface{}{"lambda_invocations": 5000000, "hours_per_month": 730.0}
	_, err := postAnalysis(client, (&analysisInputs{region: "us-east-1", usage: overrides}).request(nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received["usage_overrides"] == nil {
		t.Error("expected usage_overrides in payload")
	}
}
