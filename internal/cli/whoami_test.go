package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"
)

func TestWhoamiUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "whoami")
	if err == nil {
		t.Error("expected error when not authenticated")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error = %q, want 'not authenticated'", err.Error())
	}
}

func TestWhoamiTableOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"organization": "Acme Corp",
				"plan":         "enterprise",
				"role":         "admin",
				"accounts": []interface{}{
					map[string]interface{}{
						"name":       "production",
						"provider":   "AWS",
						"account_id": "123456789012",
					},
				},
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "whoami")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "Acme Corp") {
		t.Errorf("output missing organization: %q", got)
	}
	if !strings.Contains(got, "enterprise") {
		t.Errorf("output missing plan: %q", got)
	}
	if !strings.Contains(got, "admin") {
		t.Errorf("output missing role: %q", got)
	}
	if !strings.Contains(got, "production") {
		t.Errorf("output missing account name: %q", got)
	}
	if !strings.Contains(got, "123456789012") {
		t.Errorf("output missing account id: %q", got)
	}
}

func TestWhoamiJSONOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"organization": "Acme Corp",
				"plan":         "enterprise",
				"role":         "admin",
				"accounts":     []interface{}{},
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "whoami", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "organization") {
		t.Errorf("JSON output missing organization key: %q", got)
	}
	if !strings.Contains(got, "Acme Corp") {
		t.Errorf("JSON output missing organization value: %q", got)
	}
}

func TestWhoamiWebFlag(t *testing.T) {
	origBrowser := openBrowser
	var openedURL string
	openBrowser = func(url string) error {
		openedURL = url
		return nil
	}
	defer func() { openBrowser = origBrowser }()

	flagAPI = "https://api.levelfour.ai"
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "whoami", "--web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(openedURL, "/settings") {
		t.Errorf("expected /settings URL, got %q", openedURL)
	}
}

func TestWhoamiEmptyOrganization(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"organization": "",
				"plan":         "free",
				"role":         "owner",
				"accounts":     []interface{}{},
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "whoami")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "N/A") {
		t.Errorf("expected 'N/A' for empty org, got %q", got)
	}
}

func TestWhoamiNoAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"organization": "Solo Corp",
				"plan":         "free",
				"role":         "owner",
				"accounts":     []interface{}{},
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "whoami")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "Solo Corp") {
		t.Errorf("output missing organization: %q", got)
	}
	if strings.Contains(got, "connected") {
		t.Errorf("should not show accounts section when empty: %q", got)
	}
}
