package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestExportCostsCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "csv" {
			t.Errorf("expected format=csv query param, got %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "text/csv")
		w.Write([]byte("Service,Cost\nEC2,500.00\nRDS,300.00\n"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "export", "costs", "--format", "csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "EC2") {
		t.Errorf("CSV output missing EC2: %q", got)
	}
	if !strings.Contains(got, "RDS") {
		t.Errorf("CSV output missing RDS: %q", got)
	}
}

func TestExportRecommendationsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{
						"recommendation_id":  "rec-001",
						"service":            "EC2",
						"environment":        "production",
						"monthly_savings":    150.00,
						"annual_savings":     1800.00,
						"savings_percentage": 30.0,
					},
				},
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "export", "recommendations", "--format", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "rec-001") {
		t.Errorf("JSON output missing recommendation: %q", got)
	}
	if !strings.Contains(got, "items") {
		t.Errorf("JSON output missing items key: %q", got)
	}
}

func TestCsvEscape(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple", "hello", "hello"},
		{"with comma", "hello,world", `"hello,world"`},
		{"with quote", `say "hi"`, `"say ""hi"""`},
		{"with newline", "line1\nline2", "\"line1\nline2\""},
		{"empty", "", ""},
		{"no special chars", "abc123", "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := csvEscape(tt.input)
			if got != tt.want {
				t.Errorf("csvEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAppendCSV(t *testing.T) {
	headers := []string{"Name", "Cost", "Region"}
	rows := [][]string{
		{"EC2", "500.00", "us-east-1"},
		{"RDS", "300.00", "eu-west-1"},
	}

	var buf []byte
	buf = appendCSV(buf, headers, rows)
	got := string(buf)

	if !strings.HasPrefix(got, "Name,Cost,Region\n") {
		t.Errorf("CSV missing header row: %q", got)
	}
	if !strings.Contains(got, "EC2,500.00,us-east-1\n") {
		t.Errorf("CSV missing first data row: %q", got)
	}
	if !strings.Contains(got, "RDS,300.00,eu-west-1\n") {
		t.Errorf("CSV missing second data row: %q", got)
	}
}

func TestAppendCSVEmpty(t *testing.T) {
	headers := []string{"A", "B"}
	var rows [][]string

	var buf []byte
	buf = appendCSV(buf, headers, rows)
	got := string(buf)

	if got != "A,B\n" {
		t.Errorf("CSV with no rows = %q, want header only", got)
	}
}

func TestAppendCSVEscaping(t *testing.T) {
	headers := []string{"Name", "Description"}
	rows := [][]string{
		{"test", "has,comma"},
		{"test2", `has "quotes"`},
	}

	var buf []byte
	buf = appendCSV(buf, headers, rows)
	got := string(buf)

	if !strings.Contains(got, `"has,comma"`) {
		t.Errorf("CSV missing escaped comma: %q", got)
	}
	if !strings.Contains(got, `"has ""quotes"""`) {
		t.Errorf("CSV missing escaped quotes: %q", got)
	}
}

func TestWriteOutputToFile(t *testing.T) {
	tmpFile := t.TempDir() + "/output.csv"
	flagExportOut = tmpFile
	defer func() { flagExportOut = "" }()

	err := writeOutput([]byte("test data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, readErr := os.ReadFile(tmpFile)
	if readErr != nil {
		t.Fatalf("failed to read output file: %v", readErr)
	}
	if string(data) != "test data" {
		t.Errorf("file content = %q, want 'test data'", string(data))
	}
}

func TestWriteOutputToStdout(t *testing.T) {
	outBuf, _ := captureOutput(t)
	flagExportOut = ""

	err := writeOutput([]byte("stdout data"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(outBuf.String(), "stdout data") {
		t.Errorf("stdout = %q, want 'stdout data'", outBuf.String())
	}
}

func TestExportRecommendationsCSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_size") != "100" {
			t.Errorf("expected page_size=100, got %s", r.URL.Query().Get("page_size"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{
						"recommendation_id":  "rec-001",
						"service":            "EC2",
						"environment":        "production",
						"monthly_savings":    150.00,
						"annual_savings":     1800.00,
						"savings_percentage": 30.0,
					},
				},
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "export", "recommendations", "--format", "csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "ID,Service,Environment,Monthly Savings,Annual Savings,Savings %") {
		t.Errorf("CSV missing headers: %q", got)
	}
	if !strings.Contains(got, "rec-001") {
		t.Errorf("CSV missing recommendation ID: %q", got)
	}
}

func TestExportCostsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{"name": "EC2", "cost": 500},
				},
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "export", "costs", "--format", "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "EC2") {
		t.Errorf("JSON output missing EC2: %q", got)
	}
}

func TestExportCostsCSVError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "export", "costs", "--format", "csv")
	if err == nil {
		t.Error("expected error for API error on CSV export")
	}
}

func TestExportCostsWithOutFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"items": []interface{}{}},
		})
	}))
	defer srv.Close()

	tmpFile := t.TempDir() + "/export.json"
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "export", "costs", "--format", "json", "--out", tmpFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, readErr := os.ReadFile(tmpFile)
	if readErr != nil {
		t.Fatalf("failed to read file: %v", readErr)
	}
	if len(data) == 0 {
		t.Error("output file is empty")
	}
}

func TestExportRecommendationsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "export", "recommendations", "--format", "csv")
	if err == nil {
		t.Error("expected error for API error on recommendations export")
	}
}

func TestExportRecommendationsWithAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("account_id") != "prod" {
			t.Errorf("expected account_id=prod, got %s", r.URL.Query().Get("account_id"))
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"items": []interface{}{
					map[string]interface{}{
						"recommendation_id":  "rec-001",
						"service":            "EC2",
						"environment":        "production",
						"monthly_savings":    100.00,
						"annual_savings":     1200.00,
						"savings_percentage": 20.0,
					},
				},
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "export", "recommendations", "--format", "csv", "--account", "prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "rec-001") {
		t.Errorf("CSV missing recommendation: %q", outBuf.String())
	}
}

func TestExportRecommendationsPagination(t *testing.T) {
	page1Items := make([]interface{}, 100)
	for i := range page1Items {
		page1Items[i] = map[string]interface{}{
			"recommendation_id":  fmt.Sprintf("rec-%03d", i),
			"service":            "EC2",
			"environment":        "production",
			"monthly_savings":    10.00,
			"annual_savings":     120.00,
			"savings_percentage": 5.0,
		}
	}
	page2Items := []interface{}{
		map[string]interface{}{
			"recommendation_id":  "rec-100",
			"service":            "RDS",
			"environment":        "staging",
			"monthly_savings":    20.00,
			"annual_savings":     240.00,
			"savings_percentage": 8.0,
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "2" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"items": page2Items},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"items": page1Items},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "export", "recommendations", "--format", "csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "rec-000") {
		t.Errorf("CSV missing first page item: %q", got[:200])
	}
	if !strings.Contains(got, "rec-100") {
		t.Errorf("CSV missing second page item: %q", got[len(got)-200:])
	}
	lines := strings.Count(got, "\n")
	if lines != 102 {
		t.Errorf("expected 102 lines (1 header + 101 items), got %d", lines)
	}
}

func TestExportCostsWithPeriod(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.String()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"items": []interface{}{}},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "export", "costs", "--format", "json", "--period", "30d")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(gotPath, "period=30d") {
		t.Errorf("request path missing period param: %q", gotPath)
	}
}
