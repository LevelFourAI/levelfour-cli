package version

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clearCIEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"CI", "BUILD_NUMBER", "RUN_ID", "GITHUB_ACTIONS"} {
		t.Setenv(key, "")
	}
}

func TestCheckForUpdateDevVersion(t *testing.T) {
	msg := CheckForUpdate("dev")
	if msg != "" {
		t.Errorf("expected empty for dev version, got %q", msg)
	}
}

func TestCheckForUpdateCIEnvVars(t *testing.T) {
	for _, env := range []string{"CI", "BUILD_NUMBER", "RUN_ID", "GITHUB_ACTIONS"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, "true")
			msg := CheckForUpdate("1.0.0")
			if msg != "" {
				t.Errorf("expected empty with %s set, got %q", env, msg)
			}
		})
	}
}

func TestCheckForUpdateNewVersionAvailable(t *testing.T) {
	clearCIEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name": "v2.0.0",
		})
	}))
	defer srv.Close()

	origHTTPGet := httpGet
	httpGet = func(_ string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
		return http.DefaultClient.Do(req)
	}
	defer func() { httpGet = origHTTPGet }()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cacheDir := filepath.Join(tmpHome, ".levelfour")
	os.MkdirAll(cacheDir, 0o700)

	msg := CheckForUpdate("1.0.0")
	if !strings.Contains(msg, "2.0.0") {
		t.Errorf("expected update message with 2.0.0, got %q", msg)
	}
}

func TestCheckForUpdateSameVersion(t *testing.T) {
	clearCIEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name": "v1.0.0",
		})
	}))
	defer srv.Close()

	origHTTPGet := httpGet
	httpGet = func(_ string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
		return http.DefaultClient.Do(req)
	}
	defer func() { httpGet = origHTTPGet }()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	msg := CheckForUpdate("1.0.0")
	if msg != "" {
		t.Errorf("expected empty for same version, got %q", msg)
	}
}

func TestCheckForUpdateCachedNewerVersion(t *testing.T) {
	clearCIEnv(t)
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cacheDir := filepath.Join(tmpHome, ".levelfour")
	os.MkdirAll(cacheDir, 0o700)

	c := cachedCheck{LastCheck: time.Now(), Latest: "2.0.0"}
	b, _ := json.Marshal(c)
	os.WriteFile(filepath.Join(cacheDir, "update-check"), b, 0o600)

	msg := CheckForUpdate("1.0.0")
	if !strings.Contains(msg, "2.0.0") {
		t.Errorf("expected cached update message with 2.0.0, got %q", msg)
	}
}

func TestCheckForUpdateCachedSameVersion(t *testing.T) {
	clearCIEnv(t)
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cacheDir := filepath.Join(tmpHome, ".levelfour")
	os.MkdirAll(cacheDir, 0o700)

	c := cachedCheck{LastCheck: time.Now(), Latest: "1.0.0"}
	b, _ := json.Marshal(c)
	os.WriteFile(filepath.Join(cacheDir, "update-check"), b, 0o600)

	msg := CheckForUpdate("1.0.0")
	if msg != "" {
		t.Errorf("expected empty for cached same version, got %q", msg)
	}
}

func TestCheckForUpdateCachedEmptyLatest(t *testing.T) {
	clearCIEnv(t)
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	cacheDir := filepath.Join(tmpHome, ".levelfour")
	os.MkdirAll(cacheDir, 0o700)

	c := cachedCheck{LastCheck: time.Now(), Latest: ""}
	b, _ := json.Marshal(c)
	os.WriteFile(filepath.Join(cacheDir, "update-check"), b, 0o600)

	msg := CheckForUpdate("1.0.0")
	if msg != "" {
		t.Errorf("expected empty for cached empty latest, got %q", msg)
	}
}

func TestCheckForUpdateFetchFails(t *testing.T) {
	clearCIEnv(t)
	origHTTPGet := httpGet
	httpGet = func(_ string) (*http.Response, error) {
		return nil, errors.New("network error")
	}
	defer func() { httpGet = origHTTPGet }()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	msg := CheckForUpdate("1.0.0")
	if msg != "" {
		t.Errorf("expected empty when fetch fails, got %q", msg)
	}
}

func TestFetchLatestSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name": "v3.1.0",
		})
	}))
	defer srv.Close()

	origHTTPGet := httpGet
	httpGet = func(_ string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
		return http.DefaultClient.Do(req)
	}
	defer func() { httpGet = origHTTPGet }()

	got := fetchLatest()
	if got != "3.1.0" {
		t.Errorf("fetchLatest() = %q, want %q", got, "3.1.0")
	}
}

func TestFetchLatestHTTPError(t *testing.T) {
	origHTTPGet := httpGet
	httpGet = func(_ string) (*http.Response, error) {
		return nil, errors.New("connection refused")
	}
	defer func() { httpGet = origHTTPGet }()

	got := fetchLatest()
	if got != "" {
		t.Errorf("fetchLatest() = %q, want empty", got)
	}
}

func TestFetchLatestNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	origHTTPGet := httpGet
	httpGet = func(_ string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
		return http.DefaultClient.Do(req)
	}
	defer func() { httpGet = origHTTPGet }()

	got := fetchLatest()
	if got != "" {
		t.Errorf("fetchLatest() = %q, want empty for non-200", got)
	}
}

func TestFetchLatestInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "not json")
	}))
	defer srv.Close()

	origHTTPGet := httpGet
	httpGet = func(_ string) (*http.Response, error) {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL, nil)
		return http.DefaultClient.Do(req)
	}
	defer func() { httpGet = origHTTPGet }()

	got := fetchLatest()
	if got != "" {
		t.Errorf("fetchLatest() = %q, want empty for invalid JSON", got)
	}
}

func TestCachePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".levelfour", "update-check")
	got := cachePath()
	if got != expected {
		t.Errorf("cachePath() = %q, want %q", got, expected)
	}
}

func TestDefaultHttpGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "ok")
	}))
	defer srv.Close()

	resp, err := httpGet(srv.URL)
	if err != nil {
		t.Fatalf("httpGet returned error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("httpGet status = %d, want 200", resp.StatusCode)
	}
}

func TestDefaultHttpGetInvalidURL(t *testing.T) {
	resp, err := httpGet("://bad-url")
	if err == nil {
		resp.Body.Close()
		t.Error("expected error for invalid URL")
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		name    string
		latest  string
		current string
		want    bool
	}{
		{"major bump", "2.0.0", "1.0.0", true},
		{"minor bump", "1.1.0", "1.0.0", true},
		{"patch bump", "1.0.1", "1.0.0", true},
		{"same version", "1.0.0", "1.0.0", false},
		{"older version", "1.0.0", "2.0.0", false},
		{"longer version", "1.0.0.1", "1.0.0", true},
		{"shorter version", "1.0", "1.0.0", false},
		{"two-digit minor", "1.10.0", "1.9.0", true},
		{"two-digit patch", "1.0.10", "1.0.9", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNewer(tt.latest, tt.current)
			if got != tt.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
			}
		})
	}
}

func TestFormatMessage(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	msg := formatMessage("1.0.0", "2.0.0")
	if !strings.Contains(msg, "1.0.0") {
		t.Errorf("message missing current version: %q", msg)
	}
	if !strings.Contains(msg, "2.0.0") {
		t.Errorf("message missing latest version: %q", msg)
	}
	if !strings.Contains(msg, "brew upgrade") {
		t.Errorf("message missing upgrade instructions: %q", msg)
	}
}

func TestFormatMessageColored(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	msg := formatMessage("1.0.0", "2.0.0")
	if !strings.Contains(msg, "1.0.0") {
		t.Errorf("message missing current version: %q", msg)
	}
	if !strings.Contains(msg, "2.0.0") {
		t.Errorf("message missing latest version: %q", msg)
	}
	if !strings.Contains(msg, "\033[") {
		t.Errorf("expected ANSI escape sequences in colored message: %q", msg)
	}
}

func TestNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	if noColor() {
		t.Error("expected noColor() = false when NO_COLOR is empty")
	}
	t.Setenv("NO_COLOR", "1")
	if !noColor() {
		t.Error("expected noColor() = true when NO_COLOR is set")
	}
}
