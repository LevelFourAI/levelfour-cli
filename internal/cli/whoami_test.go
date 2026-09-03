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

// The API only returns "scope" once the backend ships it, so the row appears
// when the field is there and is silently skipped when it is not.
func TestWhoamiScopeRow(t *testing.T) {
	tests := []struct {
		name      string
		payload   map[string]interface{}
		wantScope bool
	}{
		{
			name: "scope present",
			payload: map[string]interface{}{
				"organization": "Acme Corp",
				"plan":         "enterprise",
				"role":         "api-key",
				"accounts":     []interface{}{},
				"scope":        "read-write",
			},
			wantScope: true,
		},
		{
			name: "scope absent",
			payload: map[string]interface{}{
				"organization": "Acme Corp",
				"plan":         "enterprise",
				"role":         "api-key",
				"accounts":     []interface{}{},
			},
			wantScope: false,
		},
		{
			name: "scope is not a string",
			payload: map[string]interface{}{
				"organization": "Acme Corp",
				"plan":         "enterprise",
				"role":         "api-key",
				"accounts":     []interface{}{},
				"scope":        42,
			},
			wantScope: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				json.NewEncoder(w).Encode(map[string]interface{}{"data": tt.payload})
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
			if strings.Contains(got, "Scope:") != tt.wantScope {
				t.Errorf("Scope row presence = %v, want %v: %q", !tt.wantScope, tt.wantScope, got)
			}
			if tt.wantScope && !strings.Contains(got, "read-write") {
				t.Errorf("output missing scope value: %q", got)
			}
		})
	}
}

func TestExtraString(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]interface{}
		key   string
		want  string
	}{
		{"present", map[string]interface{}{"scope": "read"}, "scope", "read"},
		{"absent", map[string]interface{}{}, "scope", ""},
		{"nil map", nil, "scope", ""},
		{"wrong type", map[string]interface{}{"scope": true}, "scope", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extraString(tt.extra, tt.key); got != tt.want {
				t.Errorf("extraString() = %q, want %q", got, tt.want)
			}
		})
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
	if !strings.Contains(openedURL, "/control-plane") {
		t.Errorf("expected /control-plane URL, got %q", openedURL)
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
