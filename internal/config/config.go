package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	API                  string `json:"api,omitempty"`
	Telemetry            bool   `json:"telemetry,omitempty"`
	TelemetryPromptShown bool   `json:"telemetry_prompt_shown,omitempty"`
}

var configDir string

func SetConfigDir(dir string) { configDir = dir }

func ExportConfigDir() string { return configDir }

func Dir() string {
	if configDir != "" {
		return configDir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".levelfour")
}

func Path() string {
	return filepath.Join(Dir(), "config.json")
}

func Load() (*Config, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(Path(), data, 0o600)
}

func ResolveAPI(flagURL string) string {
	if flagURL != "" {
		return flagURL
	}
	if url := os.Getenv("LEVELFOUR_API"); url != "" {
		return url
	}
	cfg, err := Load()
	if err == nil && cfg.API != "" {
		return cfg.API
	}
	return "https://api.levelfour.ai"
}
