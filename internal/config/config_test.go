package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestDir(t *testing.T) func() {
	t.Helper()
	orig := ExportConfigDir()
	SetConfigDir(t.TempDir())
	return func() { SetConfigDir(orig) }
}

func TestSetAndExportConfigDir(t *testing.T) {
	orig := ExportConfigDir()
	defer SetConfigDir(orig)

	SetConfigDir("/tmp/test-dir")
	if got := ExportConfigDir(); got != "/tmp/test-dir" {
		t.Errorf("ExportConfigDir() = %q, want %q", got, "/tmp/test-dir")
	}
}

func TestDir(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	got := Dir()
	if got != ExportConfigDir() {
		t.Errorf("Dir() = %q, want %q", got, ExportConfigDir())
	}
}

func TestDirDefault(t *testing.T) {
	orig := configDir
	configDir = ""
	defer func() { configDir = orig }()

	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".levelfour")
	got := Dir()
	if got != want {
		t.Errorf("Dir() = %q, want %q", got, want)
	}
}

func TestPath(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	want := filepath.Join(configDir, "config.json")
	got := Path()
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.API != "" {
		t.Errorf("expected empty BaseURL, got %q", cfg.API)
	}
}

func TestLoadValidJSON(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	data, _ := json.Marshal(&Config{API: "https://custom.api.com"})
	os.MkdirAll(Dir(), 0o700)
	os.WriteFile(Path(), data, 0o600)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.API != "https://custom.api.com" {
		t.Errorf("BaseURL = %q, want %q", cfg.API, "https://custom.api.com")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	os.MkdirAll(Dir(), 0o700)
	os.WriteFile(Path(), []byte("{broken"), 0o600)

	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSave(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	cfg := &Config{API: "https://test.levelfour.ai"}
	if err := Save(cfg); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	info, err := os.Stat(Path())
	if err != nil {
		t.Fatalf("config file not created: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("file permissions = %o, want 600", info.Mode().Perm())
	}

	data, _ := os.ReadFile(Path())
	var loaded Config
	json.Unmarshal(data, &loaded)
	if loaded.API != "https://test.levelfour.ai" {
		t.Errorf("saved BaseURL = %q, want %q", loaded.API, "https://test.levelfour.ai")
	}
}

func TestLoadReadError(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	dir := Dir()
	os.MkdirAll(dir, 0o700)
	os.WriteFile(Path(), []byte("data"), 0o600)
	os.Chmod(Path(), 0o000)
	defer os.Chmod(Path(), 0o600)

	_, err := Load()
	if err == nil {
		t.Error("expected error for unreadable file")
	}
}

func TestSaveMkdirError(t *testing.T) {
	orig := ExportConfigDir()
	SetConfigDir("/dev/null/impossible")
	defer SetConfigDir(orig)

	err := Save(&Config{API: "https://test.com"})
	if err == nil {
		t.Error("expected error when dir creation fails")
	}
}

func TestResolveAPI(t *testing.T) {
	cleanup := setupTestDir(t)
	defer cleanup()

	t.Run("flag priority", func(t *testing.T) {
		got := ResolveAPI("https://flag.url")
		if got != "https://flag.url" {
			t.Errorf("got %q, want flag URL", got)
		}
	})

	t.Run("env priority", func(t *testing.T) {
		t.Setenv("LEVELFOUR_API", "https://env.url")
		got := ResolveAPI("")
		if got != "https://env.url" {
			t.Errorf("got %q, want env URL", got)
		}
	})

	t.Run("config file", func(t *testing.T) {
		t.Setenv("LEVELFOUR_API", "")
		Save(&Config{API: "https://config.url"})
		got := ResolveAPI("")
		if got != "https://config.url" {
			t.Errorf("got %q, want config URL", got)
		}
	})

	t.Run("default", func(t *testing.T) {
		t.Setenv("LEVELFOUR_API", "")
		os.Remove(Path())
		got := ResolveAPI("")
		if got != "https://api.levelfour.ai" {
			t.Errorf("got %q, want default URL", got)
		}
	})
}
