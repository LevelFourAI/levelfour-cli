package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/browser"
	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/keyring"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const defaultAPI = "https://api.levelfour.ai"

var openBrowser = browser.OpenURL

var runField = func(f huh.Field) error {
	if isTerminal() {
		return f.Run()
	}
	return f.RunAccessible(os.Stdout, os.Stdin)
}

var promptForAPI = func() (string, error) {
	var choice string
	err := runField(huh.NewSelect[string]().
		Title("Which LevelFour API do you want to use?").
		Options(
			huh.NewOption("api.levelfour.ai", defaultAPI),
			huh.NewOption("Other", "other"),
		).
		Value(&choice).
		WithTheme(output.L4Theme()))
	if err != nil {
		return "", err
	}

	if choice != "other" {
		return choice, nil
	}

	var customURL string
	err = runField(huh.NewInput().
		Title("API base URL:").
		Placeholder("api-preview.levelfour.ai").
		Value(&customURL).
		WithTheme(output.L4Theme()))
	if err != nil {
		return "", err
	}

	return normalizeURL(customURL), nil
}

func normalizeURL(u string) string {
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "https://" + u
	}
	return u
}

var flagForce bool

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate via browser",
	RunE: func(cmd *cobra.Command, args []string) error {
		apiURL, err := resolveOrPromptAPI()
		if err != nil {
			return err
		}

		if !flagForce {
			if existingKey, source := resolveToken(); existingKey != "" {
				raw, _ := api.NewRawClient(apiURL, existingKey, Version)
				if raw != nil {
					if vErr := raw.VerifyAuth(context.Background()); vErr == nil {
						output.Success(fmt.Sprintf("Already authenticated (source: %s). Use --force to re-authenticate.", source))
						return nil
					}
				}
			}
		}

		unauthClient := api.NewUnauthSDKClient(apiURL, Version)

		resp, err := unauthClient.Raw().CreateDeviceCode(context.Background())
		if err != nil {
			return fmt.Errorf("failed to start device authorization: %w", err)
		}

		codeData := resp.Data

		interval := 5
		if codeData.Interval != nil {
			interval = *codeData.Interval
		}
		if interval <= 0 {
			interval = 5
		}

		expiresIn := 300
		if codeData.ExpiresIn != nil {
			expiresIn = *codeData.ExpiresIn
		}

		fmt.Fprintf(os.Stdout, "\n! First copy your one-time code: %s\n\n", output.Bold(codeData.UserCode))
		fmt.Fprintf(os.Stdout, "  Press Enter to open %s in your browser...", codeData.VerificationURI)

		fmt.Fscanln(os.Stdin)

		if err := openBrowser(codeData.VerificationURI); err != nil {
			fmt.Fprintf(os.Stdout, "  Could not open browser. Please visit: %s\n", codeData.VerificationURI)
		}

		apiKey, pollErr := pollDeviceAuth(unauthClient, codeData.DeviceCode, interval, expiresIn)
		if pollErr != nil {
			return pollErr
		}

		if err := keyring.Store(apiKey); err != nil {
			return fmt.Errorf("failed to store credentials in system keychain: %w", err)
		}
		cfg, _ := config.Load()
		if cfg == nil {
			cfg = &config.Config{}
		}
		cfg.API = apiURL
		if err := config.Save(cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}
		fmt.Fprintln(os.Stdout)
		output.Success("Authenticated. API key stored in system keychain.")
		return nil
	},
}

var flagVerify bool

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current authentication status",
	RunE: func(cmd *cobra.Command, args []string) error {
		key, source := resolveToken()
		if key == "" {
			output.Error("Not authenticated. Run 'l4 auth login' or set LEVELFOUR_TOKEN.")
			return nil
		}

		masked := maskKey(key)

		baseURL := config.ResolveAPI(flagAPI)

		validity := ""
		if flagVerify {
			client, clientErr := newSDKClientFn()
			if clientErr != nil {
				validity = "invalid"
			} else {
				_, verifyErr := client.SDK().Auth.GetWhoami(context.Background())
				if verifyErr != nil {
					validity = "invalid"
				} else {
					validity = "valid"
				}
			}
		}

		if output.HasFormattingFlags() {
			result := map[string]string{
				"key":    masked,
				"source": source,
				"api":    baseURL,
			}
			if validity != "" {
				result["valid"] = validity
			}
			return output.PrintResult(result)
		}

		output.Header("Authenticated")
		output.KeyValue("Key", masked)
		output.KeyValue("Source", source)
		output.KeyValue("API", baseURL)
		if validity != "" {
			output.KeyValue("Token", validity)
		}
		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := keyring.Delete(); err != nil {
			return fmt.Errorf("failed to remove credentials: %w", err)
		}
		output.Success("Credentials removed from system keychain.")
		return nil
	},
}

func resolveOrPromptAPI() (string, error) {
	if flagAPI != "" {
		return flagAPI, nil
	}
	if url := os.Getenv("LEVELFOUR_API"); url != "" {
		return url, nil
	}
	return promptForAPI()
}

func maskKey(key string) string {
	if len(key) <= 12 {
		return strings.Repeat("*", len(key))
	}
	return key[:8] + strings.Repeat("*", len(key)-12) + key[len(key)-4:]
}

func doPoll(client *api.SDKClient, deviceCode string, interval, expiresIn int) (string, error) {
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Duration(interval) * time.Second)

		pollResp, err := client.Raw().PollDeviceCode(context.Background(), deviceCode)
		if err != nil {
			continue
		}

		pollData := pollResp.Data
		if pollData == nil {
			continue
		}
		switch pollData.Status {
		case "complete":
			if pollData.APIKey == nil || *pollData.APIKey == "" {
				return "", fmt.Errorf("authorization completed but no API key received")
			}
			return *pollData.APIKey, nil
		case "expired":
			return "", fmt.Errorf("device code expired; run 'l4 auth login' to try again")
		}
	}
	return "", fmt.Errorf("device code expired; run 'l4 auth login' to try again")
}

var isTerminal = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

var pollDeviceAuth = func(client *api.SDKClient, deviceCode string, interval, expiresIn int) (string, error) {
	var apiKey string
	err := tuicommon.RunWithSpinner("Waiting for authorization...", output.L4SpinnerTheme(), func(_ context.Context) error {
		var pollErr error
		apiKey, pollErr = doPoll(client, deviceCode, interval, expiresIn)
		return pollErr
	})
	if err != nil {
		return "", err
	}
	return apiKey, nil
}

func init() {
	authLoginCmd.Flags().BoolVar(&flagForce, "force", false, "Force re-authentication even if already logged in")
	authStatusCmd.Flags().BoolVar(&flagVerify, "verify", false, "Validate the token against the API")

	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authLogoutCmd)
}
