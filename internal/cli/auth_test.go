package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/keyring"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	kr "github.com/zalando/go-keyring"
)

func useTempConfig(t *testing.T) {
	t.Helper()
	orig := config.ExportConfigDir()
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir(orig) })
}

func TestMaskKey(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want string
	}{
		{"short key", "abc", "***"},
		{"exactly 12", "123456789012", "************"},
		{"long key", "l4_live_abcdefghijklmnop", "l4_live_************mnop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskKey(tt.key)
			if got != tt.want {
				t.Errorf("maskKey(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

func TestAuthLogin(t *testing.T) {
	useTempConfig(t)

	t.Run("complete flow", func(t *testing.T) {
		pollCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "TEST-CODE",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				pollCount++
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_newkey1234567890",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "Authenticated") {
			t.Errorf("expected authenticated message, got %q", got)
		}
		cfg, _ := config.Load()
		if cfg.API != srv.URL {
			t.Errorf("config.BaseURL = %q, want %q", cfg.API, srv.URL)
		}
	})

	t.Run("expired flow", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "EXP-CODE",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status": "expired",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		defer resetFlags()

		_, _, err := executeCommand(t, "auth", "login")
		if err == nil {
			t.Error("expected error for expired device code")
		}
	})

	t.Run("empty api key", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "EMPTY-CODE",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		defer resetFlags()

		_, _, err := executeCommand(t, "auth", "login")
		if err == nil {
			t.Error("expected error for empty api key")
		}
	})

	t.Run("browser open fails", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "BROWSER-FAIL",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_key_browser_fail",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return os.ErrNotExist }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "Could not open browser") {
			t.Errorf("expected browser fallback message, got %q", got)
		}
	})

	t.Run("keyring store error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "STORE-FAIL",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_key_store_fail",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origStoreFunc := keyring.StoreFunc
		keyring.StoreFunc = func(_ string) error { return fmt.Errorf("keyring locked") }
		defer func() { keyring.StoreFunc = origStoreFunc }()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		defer resetFlags()

		_, _, err := executeCommand(t, "auth", "login")
		if err == nil {
			t.Error("expected error when keyring store fails")
		}
	})

	t.Run("device post error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			w.Write([]byte("server error"))
		}))
		defer srv.Close()

		flagAPI = srv.URL
		defer resetFlags()

		_, _, err := executeCommand(t, "auth", "login")
		if err == nil {
			t.Error("expected error when device auth fails")
		}
	})

	t.Run("poll error then success", func(t *testing.T) {
		pollCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "POLL-ERR",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				pollCount++
				if pollCount == 1 {
					w.WriteHeader(500)
					w.Write([]byte("temporary error"))
					return
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_key_after_retry",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		defer resetFlags()

		_, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("custom interval", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "INTERVAL-CODE",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_interval_key_ok",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		defer resetFlags()

		_, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestAuthStatus(t *testing.T) {
	kr.MockInit()

	t.Run("with token", func(t *testing.T) {
		flagToken = "l4_live_testkey12345678"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "status")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if got == "" {
			t.Error("expected output for authenticated status")
		}
	})

	t.Run("with token json", func(t *testing.T) {
		flagToken = "l4_live_testkey12345678"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "status", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "key") || !strings.Contains(got, "source") {
			t.Errorf("expected JSON with key and source, got %q", got)
		}
	})

	t.Run("without token", func(t *testing.T) {
		flagToken = ""
		t.Setenv("LEVELFOUR_TOKEN", "")
		kr.MockInit()
		defer resetFlags()

		_, errBuf, err := executeCommand(t, "auth", "status")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := errBuf.String()
		if got == "" {
			t.Error("expected error output for unauthenticated status")
		}
	})
}

func TestAuthStatusVerify(t *testing.T) {
	t.Run("valid token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"organization": "Acme",
					"plan":         "enterprise",
					"role":         "admin",
				},
			})
		}))
		defer srv.Close()

		flagAPI = srv.URL
		flagToken = "l4_live_testkey12345678"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "status", "--verify")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "valid") {
			t.Errorf("expected 'valid' in output, got %q", got)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			w.Write([]byte("unauthorized"))
		}))
		defer srv.Close()

		flagAPI = srv.URL
		flagToken = "l4_live_badkey123456789"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "status", "--verify")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "invalid") {
			t.Errorf("expected 'invalid' in output, got %q", got)
		}
	})

	t.Run("no verify flag", func(t *testing.T) {
		flagToken = "l4_live_testkey12345678"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "status")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if strings.Contains(got, "valid") || strings.Contains(got, "invalid") {
			t.Errorf("should not show validity without --verify, got %q", got)
		}
	})
}

func TestAuthLogout(t *testing.T) {
	useTempConfig(t)
	kr.MockInit()
	kr.Set("levelfour-cli", "api-key", "l4_test_testkey123456789a")
	defer resetFlags()

	cfg, _ := config.Load()
	cfg.API = "https://api.custom.example.com"
	config.Save(cfg)

	outBuf, _, err := executeCommand(t, "auth", "logout")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if got == "" {
		t.Error("expected success message")
	}

	cfgAfter, _ := config.Load()
	if cfgAfter.API != "https://api.custom.example.com" {
		t.Errorf("logout should preserve BaseURL, got %q", cfgAfter.API)
	}
}

func TestAuthLogoutError(t *testing.T) {
	useTempConfig(t)
	kr.MockInit()
	defer resetFlags()

	_, _, err := executeCommand(t, "auth", "logout")
	if err == nil {
		t.Error("expected error when deleting non-existent key")
	}
}

func TestAuthLoginSessionReuse(t *testing.T) {
	useTempConfig(t)

	t.Run("already authenticated skips device flow", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/auth/device/verify" {
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{"items": []interface{}{}},
				})
				return
			}
			t.Errorf("unexpected request to %s; device flow should not start", r.URL.Path)
		}))
		defer srv.Close()

		kr.MockInit()
		kr.Set("levelfour-cli", "api-key", "l4_test_validkey123456")

		flagAPI = srv.URL
		flagForce = false
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "Already authenticated") {
			t.Errorf("expected 'Already authenticated' message, got %q", got)
		}
	})

	t.Run("stale key proceeds with device flow", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device/verify":
				w.WriteHeader(401)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]interface{}{"message": "unauthorized"},
				})
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "STALE-KEY",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_newkey_after_stale",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()
		kr.Set("levelfour-cli", "api-key", "l4_test_stalekey123456")

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		flagForce = false
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "Authenticated") {
			t.Errorf("expected authenticated message, got %q", got)
		}
	})

	t.Run("force flag bypasses check", func(t *testing.T) {
		deviceFlowStarted := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				deviceFlowStarted = true
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "FORCE-CODE",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_force_key_12345",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()
		kr.Set("levelfour-cli", "api-key", "l4_test_validkey123456")

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		flagForce = true
		defer resetFlags()

		_, _, err := executeCommand(t, "auth", "login", "--force")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !deviceFlowStarted {
			t.Error("expected device flow to start with --force flag")
		}
	})

	t.Run("no key proceeds with device flow", func(t *testing.T) {
		deviceFlowStarted := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				deviceFlowStarted = true
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "NOKEY-CODE",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_fresh_key_12345",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		flagForce = false
		defer resetFlags()

		_, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !deviceFlowStarted {
			t.Error("expected device flow to start when no key exists")
		}
	})
}

func TestResolveOrPromptAPI(t *testing.T) {
	useTempConfig(t)

	t.Run("flag takes priority", func(t *testing.T) {
		flagAPI = "https://flag.example.com"
		defer resetFlags()

		got, err := resolveOrPromptAPI()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://flag.example.com" {
			t.Errorf("got %q, want flag URL", got)
		}
	})

	t.Run("env takes priority over config", func(t *testing.T) {
		t.Setenv("LEVELFOUR_API", "https://env.example.com")
		defer resetFlags()

		got, err := resolveOrPromptAPI()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://env.example.com" {
			t.Errorf("got %q, want env URL", got)
		}
	})

	t.Run("config present still calls prompt", func(t *testing.T) {
		t.Setenv("LEVELFOUR_API", "")
		config.Save(&config.Config{API: "https://config.example.com"})
		defer resetFlags()

		promptCalled := false
		origPrompt := promptForAPI
		promptForAPI = func() (string, error) {
			promptCalled = true
			return "https://prompted.example.com", nil
		}
		defer func() { promptForAPI = origPrompt }()

		got, err := resolveOrPromptAPI()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !promptCalled {
			t.Error("expected prompt to be called when config is present")
		}
		if got != "https://prompted.example.com" {
			t.Errorf("got %q, want prompted URL", got)
		}
	})

	t.Run("falls back to prompt", func(t *testing.T) {
		t.Setenv("LEVELFOUR_API", "")
		os.Remove(config.Path())
		defer resetFlags()

		origPrompt := promptForAPI
		promptForAPI = func() (string, error) {
			return "https://prompted.example.com", nil
		}
		defer func() { promptForAPI = origPrompt }()

		got, err := resolveOrPromptAPI()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "https://prompted.example.com" {
			t.Errorf("got %q, want prompted URL", got)
		}
	})

	t.Run("prompt error propagates", func(t *testing.T) {
		t.Setenv("LEVELFOUR_API", "")
		os.Remove(config.Path())
		defer resetFlags()

		origPrompt := promptForAPI
		promptForAPI = func() (string, error) {
			return "", fmt.Errorf("user cancelled")
		}
		defer func() { promptForAPI = origPrompt }()

		_, err := resolveOrPromptAPI()
		if err == nil {
			t.Error("expected error from prompt")
		}
	})

	t.Run("non-tty success path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "NONTTY-CODE",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_nontty_key_12345",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origTerminal := isTerminal
		isTerminal = func() bool { return false }
		defer func() { isTerminal = origTerminal }()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "Authenticated") {
			t.Errorf("expected authenticated message, got %q", outBuf.String())
		}
	})

	t.Run("decode error then success", func(t *testing.T) {
		pollCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "DECODE-ERR",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				pollCount++
				if pollCount == 1 {
					json.NewEncoder(w).Encode(map[string]interface{}{
						"no_data_key": "missing data wrapper",
					})
					return
				}
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_after_decode_err",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		flagAPI = srv.URL
		defer resetFlags()

		_, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("prompt selects default API via mock", func(t *testing.T) {
		t.Setenv("LEVELFOUR_API", "")
		os.Remove(config.Path())

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"device_code":      "test-device-code",
						"user_code":        "PROMPT-CODE",
						"verification_uri": "https://example.com/verify",
						"expires_in":       float64(900),
						"interval":         float64(0),
					},
				})
			case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"status":  "complete",
						"api_key": "l4_test_prompted_key_123",
					},
				})
			}
		}))
		defer srv.Close()

		kr.MockInit()

		origPrompt := promptForAPI
		promptForAPI = func() (string, error) { return srv.URL, nil }
		defer func() { promptForAPI = origPrompt }()

		origStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		w.Write([]byte("\n"))
		w.Close()
		defer func() { os.Stdin = origStdin }()

		origBrowser := openBrowser
		openBrowser = func(_ string) error { return nil }
		defer func() { openBrowser = origBrowser }()

		defer resetFlags()

		outBuf, _, err := executeCommand(t, "auth", "login")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "Authenticated") {
			t.Errorf("expected authenticated message, got %q", outBuf.String())
		}
		cfg, _ := config.Load()
		if cfg.API != srv.URL {
			t.Errorf("config.API = %q, want %q", cfg.API, srv.URL)
		}
	})
}

func TestDoPollDeadlineExpiry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status": "pending",
			},
		})
	}))
	defer srv.Close()

	client := api.NewUnauthSDKClient(srv.URL, "test")
	_, err := doPoll(client, "test-code", 0, 1)
	if err == nil {
		t.Error("expected error on deadline expiry")
	}
}

func TestDoPollCompleteNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"status": "complete",
			},
		})
	}))
	defer srv.Close()

	client := api.NewUnauthSDKClient(srv.URL, "test")
	_, err := doPoll(client, "test-code", 0, 2)
	if err == nil || !strings.Contains(err.Error(), "no API key received") {
		t.Errorf("expected 'no API key received' error, got: %v", err)
	}
}

func TestIsTerminalDefault(t *testing.T) {
	got := isTerminal()
	if got {
		t.Log("running in terminal")
	} else {
		t.Log("not running in terminal")
	}
}

func TestRunFieldAccessible(t *testing.T) {
	origTerminal := isTerminal
	isTerminal = func() bool { return false }
	defer func() { isTerminal = origTerminal }()

	origStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Write([]byte("1\n"))
	w.Close()
	defer func() { os.Stdin = origStdin }()

	var choice string
	field := huh.NewSelect[string]().
		Title("Pick:").
		Options(
			huh.NewOption("A", "a"),
			huh.NewOption("B", "b"),
		).
		Value(&choice).
		WithTheme(output.L4Theme())

	err := runField(field)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if choice != "a" {
		t.Errorf("choice = %q, want %q", choice, "a")
	}
}

func TestRunFieldTTY(t *testing.T) {
	origTerminal := isTerminal
	isTerminal = func() bool { return true }
	defer func() { isTerminal = origTerminal }()

	origStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Close()
	defer func() { os.Stdin = origStdin }()

	var choice string
	field := huh.NewSelect[string]().
		Title("Pick:").
		Options(
			huh.NewOption("A", "a"),
		).
		Value(&choice)

	err := runField(field)
	if err == nil {
		t.Log("field.Run() succeeded in TTY mode")
	}
	_ = err
}

func runFieldWithInput(f huh.Field, input string) error {
	r, w, _ := os.Pipe()
	w.Write([]byte(input))
	w.Close()
	return f.RunAccessible(os.Stdout, r)
}

func TestPromptForAPIDefaultSelection(t *testing.T) {
	origRunField := runField
	runField = func(f huh.Field) error {
		return runFieldWithInput(f, "1\n")
	}
	defer func() { runField = origRunField }()

	result, err := promptForAPI()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != defaultAPI {
		t.Errorf("got %q, want %q", result, defaultAPI)
	}
}

func TestPromptForAPICustomURL(t *testing.T) {
	callCount := 0
	origRunField := runField
	runField = func(f huh.Field) error {
		callCount++
		if callCount == 1 {
			return runFieldWithInput(f, "2\n")
		}
		return runFieldWithInput(f, "https://custom.example.com\n")
	}
	defer func() { runField = origRunField }()

	result, err := promptForAPI()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "https://custom.example.com" {
		t.Errorf("got %q, want custom URL", result)
	}
}

func TestPromptForAPINormalization(t *testing.T) {
	callCount := 0
	origRunField := runField
	runField = func(f huh.Field) error {
		callCount++
		if callCount == 1 {
			return runFieldWithInput(f, "2\n")
		}
		return runFieldWithInput(f, "api-preview.levelfour.ai\n")
	}
	defer func() { runField = origRunField }()

	result, err := promptForAPI()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "https://api-preview.levelfour.ai" {
		t.Errorf("got %q, want normalized URL", result)
	}
}

func TestPromptForAPISelectError(t *testing.T) {
	origRunField := runField
	runField = func(f huh.Field) error {
		return fmt.Errorf("user cancelled")
	}
	defer func() { runField = origRunField }()

	_, err := promptForAPI()
	if err == nil {
		t.Error("expected error when select fails")
	}
}

func TestPromptForAPIInputError(t *testing.T) {
	callCount := 0
	origRunField := runField
	runField = func(f huh.Field) error {
		callCount++
		if callCount == 1 {
			return runFieldWithInput(f, "2\n")
		}
		return fmt.Errorf("input cancelled")
	}
	defer func() { runField = origRunField }()

	_, err := promptForAPI()
	if err == nil {
		t.Error("expected error when input fails")
	}
}

func TestAuthLoginDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/device" && r.Method == "POST" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": "not-a-map",
			})
			return
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = ""
	defer resetFlags()

	_, _, err := executeCommand(t, "auth", "login", "--force")
	if err == nil {
		t.Error("expected error for bad device response format")
	}
}

func TestAuthLoginConfigSaveError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/auth/device" && r.Method == "POST":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"device_code":      "test-device-code",
					"user_code":        "SAVE-FAIL",
					"verification_uri": "https://example.com/verify",
					"expires_in":       float64(900),
					"interval":         float64(0),
				},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"status":  "complete",
					"api_key": "l4_test_save_fail_key_1",
				},
			})
		}
	}))
	defer srv.Close()

	kr.MockInit()

	config.SetConfigDir("/dev/null/impossible")
	defer config.SetConfigDir(t.TempDir())

	origStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	w.Write([]byte("\n"))
	w.Close()
	defer func() { os.Stdin = origStdin }()

	origBrowser := openBrowser
	openBrowser = func(_ string) error { return nil }
	defer func() { openBrowser = origBrowser }()

	flagAPI = srv.URL
	defer resetFlags()

	_, _, err := executeCommand(t, "auth", "login")
	if err == nil {
		t.Error("expected error when config save fails")
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare domain", "api-preview.levelfour.ai", "https://api-preview.levelfour.ai"},
		{"https prefix", "https://api.levelfour.ai", "https://api.levelfour.ai"},
		{"http prefix", "http://localhost:8000", "http://localhost:8000"},
		{"empty string", "", "https://"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeURL(tt.input)
			if got != tt.want {
				t.Errorf("normalizeURL(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestAuthLoginResolveError(t *testing.T) {
	useTempConfig(t)
	t.Setenv("LEVELFOUR_API", "")
	os.Remove(config.Path())

	origPrompt := promptForAPI
	promptForAPI = func() (string, error) {
		return "", fmt.Errorf("user cancelled")
	}
	defer func() { promptForAPI = origPrompt }()

	defer resetFlags()

	_, _, err := executeCommand(t, "auth", "login")
	if err == nil {
		t.Error("expected error when prompt fails")
	}
}
