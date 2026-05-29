package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type ProjectConfig struct {
	ExcludedResourceTypes             []string          `yaml:"excluded_resource_types"`
	RegionOverride                    *string           `yaml:"region_override"`
	GravitonForManagedServicesEnabled bool              `yaml:"graviton_for_managed_services_enabled"`
	ProviderRegions                   map[string]string `yaml:"provider_regions"`
}

var absPath = filepath.Abs

func LoadProjectConfig(startDir string) ProjectConfig {
	dir, err := absPath(startDir)
	if err != nil {
		return ProjectConfig{}
	}

	for {
		candidate := filepath.Join(dir, ".levelfour", "config.yml")
		data, readErr := os.ReadFile(filepath.Clean(candidate))
		if readErr == nil {
			var cfg ProjectConfig
			if yaml.Unmarshal(data, &cfg) == nil {
				return cfg
			}
			return ProjectConfig{}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return ProjectConfig{}
}
