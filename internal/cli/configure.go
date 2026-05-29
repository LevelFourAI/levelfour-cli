package cli

import (
	"fmt"
	"os"

	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var validConfigKeys = map[string]bool{
	"api": true,
}

var configureCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration",
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a configuration value",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		if !validConfigKeys[key] {
			return fmt.Errorf("unknown config key: %s (valid keys: api)", key)
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		var value string
		switch key {
		case "api":
			value = cfg.API
			if value == "" {
				value = defaultAPI
			}
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(map[string]string{key: value})
		}

		fmt.Fprintln(os.Stdout, value)
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration value",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		if !validConfigKeys[key] {
			return fmt.Errorf("unknown config key: %s (valid keys: api)", key)
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		switch key {
		case "api":
			cfg.API = value
		}

		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		output.Success(fmt.Sprintf("Set %s = %s", key, value))
		return nil
	},
}

const sourceDefault = "default"

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show all configuration values and their sources",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = &config.Config{}
		}

		apiURL := config.ResolveAPI(flagAPI)
		apiSource := sourceDefault
		if flagAPI != "" {
			apiSource = "--api flag"
		} else if os.Getenv("LEVELFOUR_API") != "" {
			apiSource = "LEVELFOUR_API env var"
		} else if cfg.API != "" {
			apiSource = config.Path()
		}

		tokenMasked := ""
		tokenSource := "not set"
		if key, source := resolveToken(); key != "" {
			tokenMasked = maskKey(key)
			tokenSource = source
		}

		colorStatus := "enabled"
		colorSource := sourceDefault
		if os.Getenv("NO_COLOR") != "" {
			colorStatus = "disabled"
			colorSource = "NO_COLOR env var"
		}
		if flagNoColor {
			colorStatus = "disabled"
			colorSource = "--no-color flag"
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(map[string]interface{}{
				"api":     map[string]string{"value": apiURL, "source": apiSource},
				"token":   map[string]string{"value": tokenMasked, "source": tokenSource},
				"color":   map[string]string{"value": colorStatus, "source": colorSource},
				"version": fmt.Sprintf("%s (commit: %s)", Version, Commit),
			})
		}

		output.KeyValue("api", fmt.Sprintf("%-40s (source: %s)", apiURL, apiSource))
		output.KeyValue("token", fmt.Sprintf("%-40s (source: %s)", tokenMasked, tokenSource))
		output.KeyValue("color", fmt.Sprintf("%-40s (source: %s)", colorStatus, colorSource))
		output.KeyValue("version", fmt.Sprintf("%s (commit: %s)", Version, Commit))
		return nil
	},
}

func init() {
	configureCmd.AddCommand(configGetCmd)
	configureCmd.AddCommand(configSetCmd)
	configureCmd.AddCommand(configListCmd)
}
