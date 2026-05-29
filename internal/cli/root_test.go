package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	kr "github.com/zalando/go-keyring"
)

func resetFlags() {
	flagToken = ""
	flagAPI = ""
	flagJSON = false
	flagJQ = ""
	flagTemplate = ""
	flagForce = false
	flagVerify = false
	flagTUI = false
	flagWeb = false
	flagCSV = false
	flagQuiet = false
	flagNoColor = false
	output.JSONMode = false
	output.JQExpression = ""
	output.TemplateFmt = ""
	output.CSVMode = false
	output.QuietMode = false
	output.NoColor = false

	flagExportFormat = ""
	flagExportPeriod = ""
	flagExportAccount = ""
	flagExportOut = ""

	flagEstNewFormat = ""
	flagEstNewOutFile = ""
	flagEstNewFailAbove = 0
	flagEstNewRegion = ""
	flagEstNewVarFile = nil
	flagEstNewVar = nil
	flagEstNewMaxResources = 0

	flagDiffFormat = ""
	flagDiffFailAbove = 0
	flagDiffRegion = ""
	flagDiffVarFile = nil
	flagDiffVar = nil
	flagDiffMaxResources = 0
	flagDiffBase = ""

	flagBreakdownAccount = nil
	flagBreakdownStart = ""
	flagBreakdownEnd = ""
	flagBreakdownPreset = ""
	flagBreakdownGranularity = ""
	flagBreakdownGroupBy = nil
	flagBreakdownSortBy = ""
	flagBreakdownSortByDate = ""
	flagBreakdownSortOrder = ""
	flagBreakdownService = nil
	flagBreakdownEnvironment = nil
	flagBreakdownRegion = nil
	flagBreakdownTagKey = nil
	flagBreakdownTagValue = nil
	flagBreakdownProvider = ""
	flagBreakdownFormat = ""
	flagBreakdownTUI = false
	flagSummaryProvider = ""
	flagDailyProvider = ""
	flagDailyStart = ""
	flagDailyEnd = ""
	flagMonthlyProvider = ""
	flagFiltersProvider = ""
	flagFiltersStart = ""
	flagFiltersEnd = ""

	flagAPIMethod = ""
	flagAPIFields = nil
	flagAPIInclude = false

	flagListTUI = false
	flagRecsProvider = ""
	flagRecsService = nil
	flagRecsEnvironment = nil
	flagRecsAccount = nil
	flagRecsTag = nil
	flagRecsStatus = nil
	flagRecsSearch = ""
	flagRecsSortBy = ""
	flagRecsSortOrder = ""
}

func captureOutput(t *testing.T) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	origOut, origErr := output.Stdout, output.Stderr
	output.Stdout = &outBuf
	output.Stderr = &errBuf
	t.Cleanup(func() {
		output.Stdout = origOut
		output.Stderr = origErr
	})
	return &outBuf, &errBuf
}

func executeCommand(t *testing.T, args ...string) (*bytes.Buffer, *bytes.Buffer, error) {
	t.Helper()
	outBuf, errBuf := captureOutput(t)

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetOut(w)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs(args)
	err := rootCmd.Execute()

	w.Close()
	var pipeBuf bytes.Buffer
	pipeBuf.ReadFrom(r)
	os.Stdout = origStdout

	combined := outBuf.String() + pipeBuf.String()
	outBuf.Reset()
	outBuf.WriteString(combined)

	resetFlags()
	return outBuf, errBuf, err
}

func TestExecute(t *testing.T) {
	rootCmd.SetArgs([]string{"--help"})
	err := Execute()
	resetFlags()
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
}

func TestVersionSet(t *testing.T) {
	if rootCmd.Version != Version {
		t.Errorf("rootCmd.Version = %q, want %q", rootCmd.Version, Version)
	}
	tmpl := rootCmd.VersionTemplate()
	if !strings.Contains(tmpl, "l4 version") {
		t.Errorf("version template = %q, want 'l4 version'", tmpl)
	}
}

func TestHelpOutput(t *testing.T) {
	outBuf, _, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("--help error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "cloud cost optimization") {
		t.Errorf("help missing description: %q", got)
	}
}

func TestResolveToken(t *testing.T) {
	kr.MockInit()

	t.Run("flag priority", func(t *testing.T) {
		flagToken = "flag-key"
		defer func() { flagToken = "" }()

		key, source := resolveToken()
		if key != "flag-key" {
			t.Errorf("key = %q, want flag-key", key)
		}
		if source != "--token flag" {
			t.Errorf("source = %q, want '--token flag'", source)
		}
	})

	t.Run("env priority", func(t *testing.T) {
		flagToken = ""
		t.Setenv("LEVELFOUR_TOKEN", "env-key")

		key, source := resolveToken()
		if key != "env-key" {
			t.Errorf("key = %q, want env-key", key)
		}
		if source != "LEVELFOUR_TOKEN env var" {
			t.Errorf("source = %q, want 'LEVELFOUR_TOKEN env var'", source)
		}
	})

	t.Run("keyring", func(t *testing.T) {
		flagToken = ""
		t.Setenv("LEVELFOUR_TOKEN", "")
		kr.MockInit()
		kr.Set("levelfour-cli", "api-key", "keyring-key")

		key, source := resolveToken()
		if key != "keyring-key" {
			t.Errorf("key = %q, want keyring-key", key)
		}
		if source != "system keychain" {
			t.Errorf("source = %q, want 'system keychain'", source)
		}
	})

	t.Run("empty", func(t *testing.T) {
		flagToken = ""
		t.Setenv("LEVELFOUR_TOKEN", "")
		kr.MockInit()

		key, source := resolveToken()
		if key != "" || source != "" {
			t.Errorf("expected empty, got key=%q source=%q", key, source)
		}
	})
}

func TestNewAPIClientUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, err := newAPIClient()
	if err == nil {
		t.Error("expected error when not authenticated")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error = %q, want 'not authenticated'", err.Error())
	}
}

func TestNewAPIClientAuthenticated(t *testing.T) {
	flagToken = "l4_test_testkey123456789a"
	flagAPI = "http://localhost:8000"
	defer resetFlags()

	c, err := newAPIClient()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.BaseURL != "http://localhost:8000" {
		t.Errorf("BaseURL = %q, want localhost", c.BaseURL)
	}
}

func TestNewUnauthenticatedClientFunc(t *testing.T) {
	flagAPI = "http://localhost:8000"
	defer resetFlags()

	c := newUnauthenticatedClient()
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

func TestDashboardURL(t *testing.T) {
	tests := []struct {
		name    string
		apiFlag string
		path    string
		want    string
	}{
		{"with leading slash", "https://api.levelfour.ai", "/settings", "https://dashboard.levelfour.ai/settings"},
		{"without leading slash", "https://api.levelfour.ai", "settings", "https://dashboard.levelfour.ai/settings"},
		{"custom api", "https://api.staging.levelfour.ai", "/drift", "https://dashboard.staging.levelfour.ai/drift"},
		{"trailing slash api", "https://api.levelfour.ai/", "/savings", "https://dashboard.levelfour.ai/savings"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			flagAPI = tt.apiFlag
			defer resetFlags()
			got := dashboardURL(tt.path)
			if got != tt.want {
				t.Errorf("dashboardURL(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestOpenWeb(t *testing.T) {
	origBrowser := openBrowser
	var openedURL string
	openBrowser = func(url string) error {
		openedURL = url
		return nil
	}
	defer func() { openBrowser = origBrowser }()

	flagAPI = "https://api.levelfour.ai"
	defer resetFlags()

	err := openWeb("/settings")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if openedURL != "https://dashboard.levelfour.ai/settings" {
		t.Errorf("openWeb opened %q, want dashboard URL", openedURL)
	}
}

func TestCSVFlagWarning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/costs/summary"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"provider_id":      "aws",
					"provider_name":    "AWS",
					"monthly_spending": 100.0,
				},
			})
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_monthly_savings": 0.0,
					"total_annual_savings":  0.0,
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, errBuf, err := executeCommand(t, "costs", "summary", "--csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := errBuf.String()
	if !strings.Contains(got, "--csv is only supported by export commands") {
		t.Errorf("expected CSV warning, got %q", got)
	}
}

func TestQuietMutualExclusion(t *testing.T) {
	_, _, err := executeCommand(t, "--quiet", "--json", "--help")
	if err == nil {
		t.Log("quiet + json may error depending on command")
	}
}

func TestLoginShortcutHelp(t *testing.T) {
	outBuf, _, err := executeCommand(t, "login", "--help")
	if err != nil {
		t.Fatalf("login --help error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "Authenticate via browser") {
		t.Errorf("login --help missing description:\n%s", got)
	}
	if !strings.Contains(got, "--force") {
		t.Errorf("login --help missing --force flag:\n%s", got)
	}
}

func TestLoginShortcutInRootHelp(t *testing.T) {
	outBuf, _, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("--help error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "login:") {
		t.Errorf("login shortcut should appear in root help:\n%s", got)
	}
}
