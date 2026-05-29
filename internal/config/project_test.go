package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectConfig_FullConfig(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, ".levelfour")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(`
excluded_resource_types:
  - aws_cloudwatch_*
  - aws_iam_role
region_override: eu-west-1
graviton_for_managed_services_enabled: true
`), 0o600)

	cfg := LoadProjectConfig(tmp)

	if len(cfg.ExcludedResourceTypes) != 2 {
		t.Fatalf("ExcludedResourceTypes len = %d, want 2", len(cfg.ExcludedResourceTypes))
	}
	if cfg.ExcludedResourceTypes[0] != "aws_cloudwatch_*" {
		t.Errorf("ExcludedResourceTypes[0] = %q, want aws_cloudwatch_*", cfg.ExcludedResourceTypes[0])
	}
	if cfg.RegionOverride == nil || *cfg.RegionOverride != "eu-west-1" {
		t.Errorf("RegionOverride = %v, want eu-west-1", cfg.RegionOverride)
	}
	if !cfg.GravitonForManagedServicesEnabled {
		t.Error("GravitonForManagedServicesEnabled = false, want true")
	}
}

func TestLoadProjectConfig_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	cfg := LoadProjectConfig(tmp)

	if len(cfg.ExcludedResourceTypes) != 0 {
		t.Errorf("ExcludedResourceTypes = %v, want empty", cfg.ExcludedResourceTypes)
	}
	if cfg.RegionOverride != nil {
		t.Errorf("RegionOverride = %v, want nil", cfg.RegionOverride)
	}
	if cfg.GravitonForManagedServicesEnabled {
		t.Error("GravitonForManagedServicesEnabled = true, want false")
	}
}

func TestLoadProjectConfig_PartialConfig(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, ".levelfour")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(`
excluded_resource_types:
  - aws_iam_*
`), 0o600)

	cfg := LoadProjectConfig(tmp)

	if len(cfg.ExcludedResourceTypes) != 1 {
		t.Fatalf("ExcludedResourceTypes len = %d, want 1", len(cfg.ExcludedResourceTypes))
	}
	if cfg.RegionOverride != nil {
		t.Errorf("RegionOverride = %v, want nil", cfg.RegionOverride)
	}
	if cfg.GravitonForManagedServicesEnabled {
		t.Error("GravitonForManagedServicesEnabled = true, want false")
	}
}

func TestLoadProjectConfig_WalksUpDirectories(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, ".levelfour")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(`
graviton_for_managed_services_enabled: true
`), 0o600)

	nested := filepath.Join(tmp, "modules", "vpc")
	os.MkdirAll(nested, 0o755)

	cfg := LoadProjectConfig(nested)

	if !cfg.GravitonForManagedServicesEnabled {
		t.Error("GravitonForManagedServicesEnabled = false, want true (found via walk-up)")
	}
}

func TestLoadProjectConfig_AbsPathError(t *testing.T) {
	orig := absPath
	absPath = func(path string) (string, error) {
		return "", os.ErrNotExist
	}
	defer func() { absPath = orig }()

	cfg := LoadProjectConfig("anything")
	if len(cfg.ExcludedResourceTypes) != 0 {
		t.Errorf("ExcludedResourceTypes = %v, want empty on abs path error", cfg.ExcludedResourceTypes)
	}
}

func TestLoadProjectConfig_InvalidYAML(t *testing.T) {
	tmp := t.TempDir()
	cfgDir := filepath.Join(tmp, ".levelfour")
	os.MkdirAll(cfgDir, 0o755)
	os.WriteFile(filepath.Join(cfgDir, "config.yml"), []byte(`{{{broken`), 0o600)

	cfg := LoadProjectConfig(tmp)

	if len(cfg.ExcludedResourceTypes) != 0 {
		t.Errorf("ExcludedResourceTypes = %v, want empty on bad YAML", cfg.ExcludedResourceTypes)
	}
}
