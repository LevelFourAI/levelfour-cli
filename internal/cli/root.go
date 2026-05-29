package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/keyring"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/LevelFourAI/levelfour-cli/internal/sentryx"
	"github.com/LevelFourAI/levelfour-cli/internal/version"
	"github.com/spf13/cobra"
)

var (
	Version = "dev"
	Commit  = "none"
)

var (
	flagToken    string
	flagAPI      string
	flagJSON     bool
	flagJQ       string
	flagTemplate string
	flagWeb      bool
	flagCSV      bool
	flagQuiet    bool
	flagNoColor  bool
)

var rootCmd = &cobra.Command{
	Use:           "l4",
	Short:         "LevelFour: cloud cost optimization from the terminal",
	Version:       Version,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		output.JSONMode = flagJSON || flagJQ != ""
		output.JQExpression = flagJQ
		output.TemplateFmt = flagTemplate
		output.CSVMode = flagCSV
		output.QuietMode = flagQuiet
		output.NoColor = flagNoColor || os.Getenv("NO_COLOR") != ""

		if flagQuiet && (output.JSONMode || flagCSV || flagTemplate != "") {
			return fmt.Errorf("--quiet is mutually exclusive with --json, --csv, --jq, --template")
		}

		if flagCSV {
			output.Warning("--csv is only supported by export commands. Use 'l4 export <subcommand> --format csv'.")
		}

		if cfg, err := config.Load(); err == nil && cfg.Telemetry {
			_, _ = sentryx.Init(sentryx.InitOptions{
				Enabled:     true,
				Version:     Version,
				Environment: "cli",
			})
		}

		return nil
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		if msg := version.CheckForUpdate(Version); msg != "" {
			fmt.Fprint(os.Stderr, msg)
		}
	},
	Example: `- Estimate a Terraform file

  $ l4 estimate main.tf

- See what changed compared to your main branch

  $ l4 diff main.tf`,
}

func Execute() error {
	return rootCmd.Execute()
}

func resolveToken() (string, string) {
	if flagToken != "" {
		return flagToken, "--token flag"
	}
	if key := os.Getenv("LEVELFOUR_TOKEN"); key != "" {
		return key, "LEVELFOUR_TOKEN env var"
	}
	key, err := keyring.Get()
	if err == nil && key != "" {
		return key, "system keychain"
	}
	return "", ""
}

func newAPIClient() (*api.Client, error) {
	key, _ := resolveToken()
	if key == "" {
		return nil, fmt.Errorf("not authenticated: run 'l4 auth login' or set LEVELFOUR_TOKEN")
	}
	baseURL := config.ResolveAPI(flagAPI)
	return api.NewClient(baseURL, key, Version)
}

func newUnauthenticatedClient() *api.Client {
	return api.NewUnauthenticatedClient(config.ResolveAPI(flagAPI), Version)
}

var newSDKClientFn = newSDKClient

func newSDKClient() (*api.SDKClient, error) {
	key, _ := resolveToken()
	if key == "" {
		return nil, fmt.Errorf("not authenticated: run 'l4 auth login' or set LEVELFOUR_TOKEN")
	}
	baseURL := config.ResolveAPI(flagAPI)
	return api.NewSDKClient(baseURL, key, Version)
}

func dashboardURL(path string) string {
	apiURL := config.ResolveAPI(flagAPI)
	host := strings.Replace(apiURL, "api.", "dashboard.", 1)
	host = strings.TrimSuffix(host, "/")
	if strings.HasPrefix(path, "/") {
		return host + path
	}
	return host + "/" + path
}

func openWeb(path string) error {
	url := dashboardURL(path)
	return openBrowser(url)
}

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate via browser (shortcut for 'auth login')",
	RunE:  authLoginCmd.RunE,
}

const (
	groupCore   = "core"
	groupAuth   = "auth"
	groupConfig = "config"
)

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: groupCore, Title: "Core Commands:"},
		&cobra.Group{ID: groupAuth, Title: "Authentication:"},
		&cobra.Group{ID: groupConfig, Title: "Configuration:"},
	)

	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().StringVar(&flagJQ, "jq", "", "Filter JSON output using jq syntax")
	rootCmd.PersistentFlags().StringVar(&flagTemplate, "template", "", "Format output using a Go template")
	rootCmd.PersistentFlags().StringVarP(&flagToken, "token", "t", "", "API token override (for CI/scripting)")
	rootCmd.PersistentFlags().StringVar(&flagAPI, "api", "", "API base URL")
	rootCmd.PersistentFlags().BoolVarP(&flagWeb, "web", "w", false, "Open in browser instead of terminal output")
	rootCmd.PersistentFlags().BoolVar(&flagCSV, "csv", false, "Output in CSV format")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress all output, communicate via exit code only")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable colored output")

	costsCmd.GroupID = groupCore
	recommendationsCmd.GroupID = groupCore
	integrationsCmd.GroupID = groupCore
	statusCmd.GroupID = groupCore
	whoamiCmd.GroupID = groupCore
	estimateNewCmd.GroupID = groupCore
	diffCmd.GroupID = groupCore
	exportCmd.GroupID = groupCore
	apiCmd.GroupID = groupCore

	authCmd.GroupID = groupAuth
	loginCmd.GroupID = groupAuth

	configureCmd.GroupID = groupConfig
	completionCmd.GroupID = groupConfig

	rootCmd.AddCommand(costsCmd)
	rootCmd.AddCommand(recommendationsCmd)
	rootCmd.AddCommand(integrationsCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(whoamiCmd)
	rootCmd.AddCommand(estimateNewCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(exportCmd)
	rootCmd.AddCommand(apiCmd)
	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(configureCmd)
	rootCmd.AddCommand(completionCmd)
	loginCmd.Flags().BoolVar(&flagForce, "force", false, "Force re-authentication even if already logged in")
	rootCmd.AddCommand(loginCmd)
	rootCmd.AddCommand(telemetryCmd)

	rootCmd.SetVersionTemplate(fmt.Sprintf("l4 version %s (commit: %s)\n", Version, Commit))

	initHelp()
}
