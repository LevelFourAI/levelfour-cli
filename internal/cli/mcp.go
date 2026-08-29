package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/mcp"
	"github.com/LevelFourAI/levelfour-cli/internal/mcpinstall"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var (
	flagMCPClients   []string
	flagMCPName      string
	flagMCPEndpoint  string
	flagMCPKeySource string
	flagMCPPurge     bool
	flagMCPYes       bool
)

// Seams. Everything here reaches the filesystem, another process or a browser,
// so each one is a variable the tests can stand in for.
var (
	mcpInstall   = mcpinstall.Install
	mcpUninstall = mcpinstall.Uninstall
	mcpStatus    = mcpinstall.Status
	mcpDetected  = mcpinstall.Detected
	mcpSuggested = mcpinstall.Suggested
	mcpServe     = mcp.Serve
	osExecutable = os.Executable
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Connect coding agents to LevelFour over MCP",
	Long: "Connect coding agents to LevelFour over the Model Context Protocol.\n\n" +
		"`l4 mcp install` writes the server into the agent clients on this machine. " +
		"`l4 mcp uninstall` takes it back out. `l4 mcp serve` runs the same tools locally over stdio.",
}

var mcpInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Add the LevelFour MCP server to your agent clients",
	Long: "Adds the LevelFour MCP server to Claude Code, Claude Desktop, Cursor, VS Code or Windsurf.\n\n" +
		"With no --client, every client detected on this machine is configured. Existing config " +
		"files are parsed and merged, never replaced, and a dated backup is taken first.\n\n" +
		"--name exists because one API key belongs to exactly one organization: a user in several " +
		"organizations runs this once per organization, with a different name each time.",
	Example: `- Configure every detected client

  $ l4 mcp install

- Configure one client

  $ l4 mcp install --client cursor

- Add a second entry, for another organization or another key scope

  $ l4 mcp install --client cursor --name levelfour-rw --token $READ_WRITE_KEY

- Write a reference to $LEVELFOUR_TOKEN instead of the key itself

  $ l4 mcp install --key-source env`,
	RunE: func(cmd *cobra.Command, args []string) error {
		clients, err := resolveMCPClients()
		if err != nil {
			return err
		}

		apiKey, err := ensureAuthenticated(cmd, args)
		if err != nil {
			return err
		}

		binary, err := osExecutable()
		if err != nil {
			return fmt.Errorf("cannot locate the l4 binary, which Claude Desktop needs to launch it: %w", err)
		}

		keySource, err := resolveKeySource()
		if err != nil {
			return err
		}

		opts := mcpinstall.Options{
			Name:      flagMCPName,
			Endpoint:  mcpEndpoint(),
			APIKey:    apiKey,
			Binary:    binary,
			KeySource: keySource,
		}

		results := make([]mcpinstall.Result, 0, len(clients))
		var failures []string
		for _, c := range clients {
			result, installErr := mcpInstall(cmd.Context(), c, opts)
			if installErr != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", c.Label, installErr))
				continue
			}
			results = append(results, result)
		}

		if output.HasFormattingFlags() {
			if err := output.PrintResult(map[string]any{"installed": results, "failed": failures}); err != nil {
				return err
			}
		} else {
			printInstallResults(results, failures, opts)
		}
		return installOutcome(results, failures)
	},
}

// installOutcome decides the exit code. A machine reading --json and a person
// reading the table have to agree on whether this worked, so the verdict is
// computed once for both rather than inside the branch that prints. A client
// that failed is a client with no LevelFour tools, which is a failure whatever
// else succeeded.
func installOutcome(results []mcpinstall.Result, failures []string) error {
	if len(results) == 0 {
		return fmt.Errorf("no client was configured: %s", strings.Join(failures, "; "))
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d of %d clients failed: %s",
			len(failures), len(results)+len(failures), strings.Join(failures, "; "))
	}
	return nil
}

// resolveKeySource maps the flag onto the package's own enum, so an unknown
// value is refused here rather than silently falling through to writing the
// credential.
func resolveKeySource() (mcpinstall.KeySource, error) {
	switch flagMCPKeySource {
	case string(mcpinstall.KeyInline):
		return mcpinstall.KeyInline, nil
	case string(mcpinstall.KeyFromEnv):
		return mcpinstall.KeyFromEnv, nil
	default:
		return "", fmt.Errorf("unknown --key-source %q: choose inline or env", flagMCPKeySource)
	}
}

var mcpUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove the LevelFour MCP server from your agent clients",
	Long: "Removes the entry `l4 mcp install` wrote, from every client that carries it.\n\n" +
		"Only the entry under --name is touched, and a dated backup is taken first, so other " +
		"servers in the same file are left alone.\n\n" +
		"Backups are kept unless you pass --purge-backups. They are worth a thought: the second " +
		"install of a client backs up a file that already holds the first credential.",
	Example: `- Remove it everywhere

  $ l4 mcp uninstall

- Remove one client, or one named entry

  $ l4 mcp uninstall --client cursor
  $ l4 mcp uninstall --name levelfour-rw

- Leave nothing behind, including the backups

  $ l4 mcp uninstall --purge-backups`,
	RunE: func(cmd *cobra.Command, args []string) error {
		clients, err := resolveUninstallClients(cmd.Context())
		if err != nil {
			return err
		}
		if !flagMCPYes && !confirmAction(fmt.Sprintf("Remove entry %q from %s?",
			flagMCPName, strings.Join(labelsOf(clients), ", "))) {
			output.Info("Aborted.")
			return nil
		}

		opts := mcpinstall.Options{Name: flagMCPName}
		results := make([]mcpinstall.Result, 0, len(clients))
		var failures []string
		for _, c := range clients {
			result, removeErr := mcpUninstall(cmd.Context(), c, opts)
			if removeErr != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", c.Label, removeErr))
				continue
			}
			results = append(results, result)
		}

		purged, remaining := handleBackups(clients)

		if output.HasFormattingFlags() {
			if err := output.PrintResult(map[string]any{
				"removed": results, "failed": failures,
				"backups_purged": purged, "backups_remaining": remaining,
			}); err != nil {
				return err
			}
		} else {
			printUninstallResults(results, failures, purged, remaining)
		}
		return uninstallOutcome(results, failures)
	},
}

// resolveUninstallClients returns the clients to clean. With no --client that is
// every client carrying the entry, not every client detected, which is the
// opposite of install on purpose: a config left behind by an application that is
// gone is the case worth reaching.
func resolveUninstallClients(ctx context.Context) ([]mcpinstall.Client, error) {
	if len(flagMCPClients) > 0 {
		return namedClients()
	}
	var carrying []mcpinstall.Client
	for _, c := range mcpinstall.Clients {
		switch mcpStatus(ctx, c, flagMCPName).Status {
		case mcpinstall.StatusInstalled, mcpinstall.StatusOrphaned:
			carrying = append(carrying, c)
		}
	}
	if len(carrying) == 0 {
		return nil, fmt.Errorf("no client carries an entry named %q. Nothing to remove", flagMCPName)
	}
	return carrying, nil
}

// handleBackups deletes the dated copies when asked, and otherwise counts them so
// the command can say what it left behind.
func handleBackups(clients []mcpinstall.Client) (purged int, remaining int) {
	for _, c := range clients {
		for _, path := range mcpinstall.Backups(c) {
			if !flagMCPPurge {
				remaining++
				continue
			}
			if err := os.Remove(path); err != nil {
				output.Warning(fmt.Sprintf("could not remove %s: %v", path, err))
				remaining++
				continue
			}
			purged++
		}
	}
	return purged, remaining
}

func uninstallOutcome(results []mcpinstall.Result, failures []string) error {
	if len(failures) == 0 {
		return nil
	}
	return fmt.Errorf("%d of %d clients failed: %s",
		len(failures), len(results)+len(failures), strings.Join(failures, "; "))
}

func printUninstallResults(results []mcpinstall.Result, failures []string, purged, remaining int) {
	for _, r := range results {
		if r.Action == mcpinstall.ActionAbsent {
			output.Info(fmt.Sprintf("%s: no entry %q to remove", r.Label, flagMCPName))
			continue
		}
		output.Success(fmt.Sprintf("%s: removed entry %q", r.Label, flagMCPName))
		if r.Backup != "" {
			output.KeyValue("  Backup", r.Backup)
		}
	}
	for _, f := range failures {
		output.Error(f)
	}
	if purged > 0 {
		output.Info(fmt.Sprintf("Deleted %d backup file(s).", purged))
	}
	if remaining > 0 {
		output.Info(fmt.Sprintf("%d backup file(s) left in place. A backup taken over an existing "+
			"install still holds that credential; --purge-backups deletes them.", remaining))
	}
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the LevelFour MCP server locally over stdio",
	Long: "Runs the LevelFour MCP tools on this machine, speaking MCP over stdin and stdout and " +
		"reading your cloud data through the LevelFour API with the credential in your keychain.\n\n" +
		"This is what `l4 mcp install` points Claude Desktop at, because its config file starts " +
		"stdio servers and cannot send an Authorization header to a remote one. Run it by hand " +
		"only to debug: on its own it waits for a client that will never speak.",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient()
		if err != nil {
			return err
		}
		// stdout is the protocol stream from here on. Anything the shared output
		// helpers would print to it would be read as a malformed JSON-RPC frame
		// and drop the session, so send them where the client keeps its log.
		output.Stdout = os.Stderr
		return mcpServe(cmd.Context(), mcp.Session{
			Fetcher: mcp.NewRESTFetcher(client),
			Version: Version,
			In:      os.Stdin,
			Out:     os.Stdout,
			Notices: os.Stderr,
		})
	},
}

var mcpStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show which agent clients are wired up to LevelFour",
	Long: "Reports, per client, whether it is installed on this machine and whether it carries a " +
		"LevelFour entry. Run this first when an agent says it cannot see any LevelFour tools.",
	RunE: func(cmd *cobra.Command, args []string) error {
		states := make([]mcpinstall.State, 0, len(mcpinstall.Clients))
		for _, c := range mcpinstall.Clients {
			states = append(states, mcpStatus(cmd.Context(), c, flagMCPName))
		}

		key, source := resolveToken()
		if output.HasFormattingFlags() {
			return output.PrintResult(map[string]any{
				"name":         flagMCPName,
				"endpoint":     mcpEndpoint(),
				"surface":      mcp.Summary(),
				"credential":   source,
				"clients":      states,
				"api_base_url": config.ResolveAPI(flagAPI),
			})
		}

		output.Header("LevelFour MCP")
		output.KeyValue("Entry name", flagMCPName)
		output.KeyValue("Hosted endpoint", mcpEndpoint())
		output.KeyValue("Local surface", mcp.Summary())
		if key == "" {
			output.KeyValue("Credential", "none. Run 'l4 auth login'")
		} else {
			output.KeyValue("Credential", fmt.Sprintf("%s (%s)", source, maskKey(key)))
		}

		fmt.Fprintln(output.Stdout)
		rows := make([][]string, 0, len(states))
		for _, s := range states {
			rows = append(rows, []string{s.Label, s.Status, s.Endpoint})
		}
		output.Table([]string{"Client", "Status", "Endpoint"}, rows)
		return nil
	},
}

// labelsOf is the human-readable side of a client list, which is all the
// messages need.
func labelsOf(clients []mcpinstall.Client) []string {
	labels := make([]string, 0, len(clients))
	for _, c := range clients {
		labels = append(labels, c.Label)
	}
	return labels
}

func mcpEndpoint() string {
	if flagMCPEndpoint != "" {
		return flagMCPEndpoint
	}
	return mcp.Endpoint
}

// resolveMCPClients turns --client, or an empty --client, into the set to write.
func resolveMCPClients() ([]mcpinstall.Client, error) {
	if len(flagMCPClients) == 0 {
		detected := mcpDetected()
		// Clients there is only a hint of are named, never written to. The hint is
		// a directory the client created once, and those outlive an uninstall, so
		// acting on one means creating a config file with a credential in it for
		// software that is not here any more.
		suggested := labelsOf(mcpSuggested())

		if len(detected) == 0 {
			if len(suggested) > 0 {
				return nil, fmt.Errorf(
					"no MCP client was confirmed on this machine. %s left configuration behind but "+
						"could not be found: name one with --client if you still use it",
					strings.Join(suggested, " and "))
			}
			return nil, fmt.Errorf(
				"no MCP client detected on this machine. Install one of %s, or name it with --client",
				strings.Join(mcpinstall.IDs(), ", "))
		}

		output.Info(fmt.Sprintf("Detected %s. Configuring all of them; use --client to narrow.",
			strings.Join(labelsOf(detected), ", ")))
		if len(suggested) > 0 {
			output.Info(fmt.Sprintf("Skipping %s: configuration was found but the application was not. "+
				"Add --client to configure it anyway.", strings.Join(suggested, ", ")))
		}
		return detected, nil
	}

	return namedClients()
}

// namedClients turns --client into the set it names.
func namedClients() ([]mcpinstall.Client, error) {
	clients := make([]mcpinstall.Client, 0, len(flagMCPClients))
	for _, id := range flagMCPClients {
		c, ok := mcpinstall.Find(id)
		if !ok {
			return nil, fmt.Errorf("unknown client %q: choose from %s", id, strings.Join(mcpinstall.IDs(), ", "))
		}
		clients = append(clients, c)
	}
	return clients, nil
}

// ensureAuthenticated returns the stored API key, running the existing browser
// device flow first when there is none. There is one credential path in this
// CLI and this command reuses it rather than minting a second kind of key.
func ensureAuthenticated(cmd *cobra.Command, args []string) (string, error) {
	if key, _ := resolveToken(); key != "" {
		return key, nil
	}
	// The browser flow prints a code and waits on Enter. That is fine at a
	// prompt and wrong in CI, which is a documented use of --json, so a
	// non-interactive run is told what to do instead of being blocked on input
	// that will never arrive.
	if !isTerminal() {
		return "", fmt.Errorf("not authenticated: run 'l4 auth login' first, or pass --token or %s",
			credentialEnvVar)
	}
	output.Info("Not authenticated yet. Opening the browser to mint a read-scoped API key.")
	if err := authLoginCmd.RunE(cmd, args); err != nil {
		return "", err
	}
	key, _ := resolveToken()
	if key == "" {
		return "", fmt.Errorf("authentication finished without storing a key: run 'l4 auth login' and try again")
	}
	return key, nil
}

func printInstallResults(results []mcpinstall.Result, failures []string, opts mcpinstall.Options) {
	wroteCredential := false
	for _, r := range results {
		output.Success(fmt.Sprintf("%s: %s entry %q", r.Label, r.Action, flagMCPName))
		output.KeyValue("  Wrote", r.Target)
		if r.Backup != "" {
			output.KeyValue("  Backup", r.Backup)
		}
		output.KeyValue("  Transport", r.Note)
		if c, ok := mcpinstall.Find(r.Client); ok && c.WritesCredential(opts) {
			wroteCredential = true
		}
	}
	for _, f := range failures {
		output.Error(f)
	}
	if len(results) == 0 {
		return
	}

	fmt.Fprintln(output.Stdout)
	// What landed on disk is not something to leave a user to infer from a
	// transport note. Either the key is in a file they can read, or it is not and
	// something has to supply it before the client will authenticate.
	if wroteCredential {
		output.Info("Your API key was written into the files above, readable only by you. " +
			"Use --key-source env to keep it out of them.")
	} else {
		output.Info(fmt.Sprintf("No key was written. Export %s in the environment the client "+
			"starts from; VS Code prompts for it instead and stores it itself.", mcpinstall.CredentialEnvVar))
	}
	output.Info("Restart the client, then ask it: what are we spending this month")
}

func init() {
	mcpInstallCmd.Flags().StringSliceVar(&flagMCPClients, "client", nil,
		"Client to configure ("+strings.Join(mcpinstall.IDs(), ", ")+"); repeatable, defaults to every one detected")
	mcpInstallCmd.Flags().StringVar(&flagMCPName, "name", mcp.ServerName, "Name for the server entry")
	mcpInstallCmd.Flags().StringVar(&flagMCPEndpoint, "endpoint", "", "MCP endpoint to point clients at")
	mcpInstallCmd.Flags().StringVar(&flagMCPKeySource, "key-source", string(mcpinstall.KeyInline),
		"Where clients read the credential: inline (written into the config, 0600) or env (a reference to $"+
			mcpinstall.CredentialEnvVar+", so no key is stored)")
	mcpUninstallCmd.Flags().StringSliceVar(&flagMCPClients, "client", nil,
		"Client to clean ("+strings.Join(mcpinstall.IDs(), ", ")+"); repeatable, defaults to every one carrying the entry")
	mcpUninstallCmd.Flags().StringVar(&flagMCPName, "name", mcp.ServerName, "Name of the server entry to remove")
	mcpUninstallCmd.Flags().BoolVar(&flagMCPPurge, "purge-backups", false,
		"Also delete the dated .l4-backup-* copies, which can hold a previous credential")
	mcpUninstallCmd.Flags().BoolVarP(&flagMCPYes, wordYes, "y", false, "Skip the confirmation prompt")

	mcpStatusCmd.Flags().StringVar(&flagMCPName, "name", mcp.ServerName, "Name of the server entry to look for")
	mcpStatusCmd.Flags().StringVar(&flagMCPEndpoint, "endpoint", "", "MCP endpoint to report as the hosted default")

	mcpCmd.AddCommand(mcpInstallCmd)
	mcpCmd.AddCommand(mcpUninstallCmd)
	mcpCmd.AddCommand(mcpServeCmd)
	mcpCmd.AddCommand(mcpStatusCmd)
}
