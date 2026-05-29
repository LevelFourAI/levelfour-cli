package cli

import (
	"fmt"
	"io"

	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/spf13/cobra"
)

var telemetryCmd = &cobra.Command{
	Use:   "telemetry",
	Short: "Manage CLI crash telemetry",
	Long: `Crash telemetry sends panic stacktraces and command names to LevelFour
so we can fix CLI bugs quickly. It is OFF by default.

Run 'l4 telemetry enable' to opt in. Crash reports are scrubbed: home
directory paths are replaced with '~', AWS access keys and known token env
vars are redacted, and request headers/cookies are stripped before transport.`,
	GroupID: groupConfig,
}

var telemetryEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable crash telemetry",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return setTelemetry(cmd.OutOrStdout(), true)
	},
}

var telemetryDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable crash telemetry",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return setTelemetry(cmd.OutOrStdout(), false)
	},
}

const (
	telemetryStateEnabled  = "enabled"
	telemetryStateDisabled = "disabled"
)

var telemetryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show telemetry status",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		state := telemetryStateDisabled
		if cfg.Telemetry {
			state = telemetryStateEnabled
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Telemetry is %s\n", state)
		return nil
	},
}

func setTelemetry(w io.Writer, enabled bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Telemetry = enabled
	cfg.TelemetryPromptShown = true
	if err := config.Save(cfg); err != nil {
		return err
	}
	if enabled {
		fmt.Fprintln(w, "Telemetry enabled. Crash reports will be sent to LevelFour.")
	} else {
		fmt.Fprintln(w, "Telemetry disabled.")
	}
	return nil
}

func init() {
	telemetryCmd.AddCommand(telemetryEnableCmd, telemetryDisableCmd, telemetryStatusCmd)
}
