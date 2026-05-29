package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/config"
	kr "github.com/zalando/go-keyring"
)

func setupConfigDir(t *testing.T) {
	t.Helper()
	orig := config.ExportConfigDir()
	config.SetConfigDir(t.TempDir())
	t.Cleanup(func() { config.SetConfigDir(orig) })
}

func TestConfigGet(t *testing.T) {
	setupConfigDir(t)

	t.Run("api default", func(t *testing.T) {
		outBuf, _, err := executeCommand(t, "config", "get", "api")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "https://api.levelfour.ai") {
			t.Errorf("expected default URL, got %q", got)
		}
	})

	t.Run("api json", func(t *testing.T) {
		outBuf, _, err := executeCommand(t, "config", "get", "api", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "api") {
			t.Errorf("expected JSON with api, got %q", got)
		}
	})

	t.Run("unknown key", func(t *testing.T) {
		_, _, err := executeCommand(t, "config", "get", "unknown_key")
		if err == nil {
			t.Error("expected error for unknown key")
		}
	})
}

func TestConfigGetLoadError(t *testing.T) {
	orig := config.ExportConfigDir()
	config.SetConfigDir("/dev/null/impossible")
	defer config.SetConfigDir(orig)

	outBuf, _, err := executeCommand(t, "config", "get", "api")
	if err != nil {
		if !strings.Contains(err.Error(), "failed to load config") {
			t.Errorf("unexpected error: %v", err)
		}
	} else {
		if !strings.Contains(outBuf.String(), "api.levelfour.ai") {
			t.Logf("got default URL: %s", outBuf.String())
		}
	}
}

func TestConfigSet(t *testing.T) {
	setupConfigDir(t)

	t.Run("set api", func(t *testing.T) {
		outBuf, _, err := executeCommand(t, "config", "set", "api", "https://custom.api.com")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "Set api") {
			t.Errorf("expected success message, got %q", outBuf.String())
		}

		cfg, _ := config.Load()
		if cfg.API != "https://custom.api.com" {
			t.Errorf("config not saved: BaseURL = %q", cfg.API)
		}
	})

	t.Run("unknown key", func(t *testing.T) {
		_, _, err := executeCommand(t, "config", "set", "unknown_key", "value")
		if err == nil {
			t.Error("expected error for unknown key")
		}
	})
}

func TestConfigSetLoadError(t *testing.T) {
	orig := config.ExportConfigDir()
	config.SetConfigDir("/dev/null/impossible")
	defer config.SetConfigDir(orig)

	_, _, err := executeCommand(t, "config", "set", "api", "https://test.com")
	if err == nil {
		t.Error("expected error when config dir is invalid")
	}
}

func TestConfigSetSaveError(t *testing.T) {
	orig := config.ExportConfigDir()
	tmp := t.TempDir()
	config.SetConfigDir(tmp)
	defer config.SetConfigDir(orig)

	os.MkdirAll(config.Dir(), 0o700)
	os.WriteFile(config.Path(), []byte(`{}`), 0o400)
	defer os.Chmod(config.Path(), 0o600)

	_, _, err := executeCommand(t, "config", "set", "api", "https://test.com")
	if err == nil {
		t.Error("expected error when save fails")
	}
}

func TestConfigList(t *testing.T) {
	setupConfigDir(t)
	kr.MockInit()

	t.Run("table output", func(t *testing.T) {
		flagToken = "l4_live_testkey12345678"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "config", "list")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "api") {
			t.Errorf("output missing api key: %q", got)
		}
		if !strings.Contains(got, "token") {
			t.Errorf("output missing token key: %q", got)
		}
		if !strings.Contains(got, "version") {
			t.Errorf("output missing version key: %q", got)
		}
	})

	t.Run("json output", func(t *testing.T) {
		flagToken = "l4_live_testkey12345678"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "config", "list", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "api") {
			t.Errorf("JSON output missing api: %q", got)
		}
		if !strings.Contains(got, "token") {
			t.Errorf("JSON output missing token: %q", got)
		}
		if !strings.Contains(got, "source") {
			t.Errorf("JSON output missing source: %q", got)
		}
	})
}

func TestConfigListAPIFlag(t *testing.T) {
	setupConfigDir(t)
	kr.MockInit()

	flagAPI = "https://custom.api.com"
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "config", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "--api flag") {
		t.Errorf("expected --api flag source, got %q", got)
	}
}

func TestConfigListEnvAPI(t *testing.T) {
	setupConfigDir(t)
	kr.MockInit()

	t.Setenv("LEVELFOUR_API", "https://env.api.com")
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "config", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "LEVELFOUR_API env var") {
		t.Errorf("expected env var source, got %q", got)
	}
}

func TestConfigListConfigFileAPI(t *testing.T) {
	setupConfigDir(t)
	kr.MockInit()

	cfg := &config.Config{API: "https://config.file.api.com"}
	config.Save(cfg)

	t.Setenv("LEVELFOUR_API", "")
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "config", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, config.Path()) {
		t.Errorf("expected config file source, got %q", got)
	}
}

func TestConfigListNoToken(t *testing.T) {
	setupConfigDir(t)
	kr.MockInit()
	t.Setenv("LEVELFOUR_TOKEN", "")

	defer resetFlags()

	outBuf, _, err := executeCommand(t, "config", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "not set") {
		t.Errorf("expected 'not set' for token, got %q", got)
	}
}

func TestConfigListNoColor(t *testing.T) {
	setupConfigDir(t)
	kr.MockInit()

	t.Setenv("NO_COLOR", "1")
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "config", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "disabled") {
		t.Errorf("expected color disabled, got %q", got)
	}
	if !strings.Contains(got, "NO_COLOR env var") {
		t.Errorf("expected NO_COLOR source, got %q", got)
	}
}

func TestConfigListNoColorFlag(t *testing.T) {
	setupConfigDir(t)
	kr.MockInit()

	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "config", "list", "--no-color")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "disabled") {
		t.Errorf("expected color disabled, got %q", got)
	}
	if !strings.Contains(got, "--no-color flag") {
		t.Errorf("expected --no-color flag source, got %q", got)
	}
}
