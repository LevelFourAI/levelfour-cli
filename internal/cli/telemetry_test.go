package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/config"
)

func TestTelemetryStatusDefaultDisabled(t *testing.T) {
	setupConfigDir(t)
	outBuf, _, err := executeCommand(t, "telemetry", "status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "disabled") {
		t.Errorf("expected status to be disabled, got %q", outBuf.String())
	}
}

func TestTelemetryEnableThenStatus(t *testing.T) {
	setupConfigDir(t)

	if _, _, err := executeCommand(t, "telemetry", "enable"); err != nil {
		t.Fatalf("enable error: %v", err)
	}
	cfg, _ := config.Load()
	if !cfg.Telemetry {
		t.Errorf("Telemetry should be true after enable")
	}
	if !cfg.TelemetryPromptShown {
		t.Errorf("TelemetryPromptShown should be true after enable")
	}

	outBuf, _, err := executeCommand(t, "telemetry", "status")
	if err != nil {
		t.Fatalf("status error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "enabled") {
		t.Errorf("expected enabled, got %q", outBuf.String())
	}
}

func TestTelemetryDisable(t *testing.T) {
	setupConfigDir(t)
	if err := config.Save(&config.Config{Telemetry: true, TelemetryPromptShown: true}); err != nil {
		t.Fatalf("setup save: %v", err)
	}
	outBuf, _, err := executeCommand(t, "telemetry", "disable")
	if err != nil {
		t.Fatalf("disable error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Telemetry disabled") {
		t.Errorf("expected disable confirmation, got %q", outBuf.String())
	}
	cfg, _ := config.Load()
	if cfg.Telemetry {
		t.Errorf("Telemetry should be false after disable")
	}
}

func TestTelemetryEnableConfirmationOutput(t *testing.T) {
	setupConfigDir(t)
	outBuf, _, err := executeCommand(t, "telemetry", "enable")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Crash reports will be sent") {
		t.Errorf("expected enable confirmation, got %q", outBuf.String())
	}
}

func TestTelemetryStatusLoadError(t *testing.T) {
	orig := config.ExportConfigDir()
	dir := t.TempDir()
	config.SetConfigDir(dir)
	defer config.SetConfigDir(orig)

	os.MkdirAll(config.Dir(), 0o700)
	os.WriteFile(config.Path(), []byte("{not valid json"), 0o600)

	_, _, err := executeCommand(t, "telemetry", "status")
	if err == nil {
		t.Error("expected error for unreadable config")
	}
}

func TestTelemetryEnableLoadError(t *testing.T) {
	orig := config.ExportConfigDir()
	dir := t.TempDir()
	config.SetConfigDir(dir)
	defer config.SetConfigDir(orig)

	os.MkdirAll(config.Dir(), 0o700)
	os.WriteFile(config.Path(), []byte("{broken"), 0o600)

	_, _, err := executeCommand(t, "telemetry", "enable")
	if err == nil {
		t.Error("expected error when config is corrupt")
	}
}

func TestTelemetrySaveError(t *testing.T) {
	orig := config.ExportConfigDir()
	tmp := t.TempDir()
	config.SetConfigDir(tmp)
	defer config.SetConfigDir(orig)

	os.MkdirAll(config.Dir(), 0o700)
	os.WriteFile(config.Path(), []byte(`{}`), 0o400)
	defer os.Chmod(config.Path(), 0o600)

	_, _, err := executeCommand(t, "telemetry", "enable")
	if err == nil {
		t.Skip("filesystem allowed write despite read-only mode; skipping")
	}
}

func TestRootTelemetryInitNoOpWhenDisabled(t *testing.T) {
	setupConfigDir(t)
	if _, _, err := executeCommand(t, "telemetry", "status"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRootTelemetryInitWhenEnabled(t *testing.T) {
	setupConfigDir(t)
	if err := config.Save(&config.Config{Telemetry: true, TelemetryPromptShown: true}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, _, err := executeCommand(t, "telemetry", "status"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
