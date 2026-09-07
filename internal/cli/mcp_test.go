package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/mcp"
	"github.com/LevelFourAI/levelfour-cli/internal/mcpinstall"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
	kr "github.com/zalando/go-keyring"
)

// stubMCP replaces the filesystem-facing seams for one test.
func stubMCP(t *testing.T) {
	t.Helper()
	origInstall, origStatus, origClassify, origServe, origExec :=
		mcpInstall, mcpStatus, mcpClassify, mcpServe, osExecutable
	origUninstall := mcpUninstall
	t.Cleanup(func() {
		mcpUninstall = origUninstall
		mcpInstall, mcpStatus, mcpClassify, mcpServe, osExecutable =
			origInstall, origStatus, origClassify, origServe, origExec
	})
	osExecutable = func() (string, error) { return "/opt/homebrew/bin/l4", nil }
	// Without this the real machine decides the result: whatever leftover client
	// directories the test host happens to have would change the error text.
	mcpClassify = func() (present, hinted []mcpinstall.Client) { return nil, nil }
	mcpStatus = func(context.Context, mcpinstall.Client, string) mcpinstall.State {
		return mcpinstall.State{}
	}
	mcpInstall = func(_ context.Context, c mcpinstall.Client, o mcpinstall.Options) (mcpinstall.Result, error) {
		return mcpinstall.Result{Client: c.ID, Label: c.Label, Target: "/tmp/" + c.ID, Action: "added", Note: c.Note}, nil
	}
	mcpServe = func(context.Context, mcp.Session) error { return nil }
	mcpUninstall = func(_ context.Context, c mcpinstall.Client, o mcpinstall.Options) (mcpinstall.Result, error) {
		return mcpinstall.Result{Client: c.ID, Label: c.Label, Action: "removed"}, nil
	}
}

// configuredAs makes mcpStatus report the given clients as carrying the entry,
// which is what uninstall selects on when no --client is passed.
func configuredAs(ids ...string) {
	mcpStatus = func(_ context.Context, c mcpinstall.Client, _ string) mcpinstall.State {
		for _, id := range ids {
			if c.ID == id {
				return mcpinstall.State{Client: c.ID, Label: c.Label, Status: mcpinstall.StatusInstalled}
			}
		}
		return mcpinstall.State{Client: c.ID, Label: c.Label, Status: mcpinstall.StatusNotInstalled}
	}
}

func TestMCPUninstallCleansEveryClientCarryingTheEntry(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	defer resetFlags()
	configuredAs(mcpinstall.Cursor, mcpinstall.VSCode)

	var cleaned []string
	mcpUninstall = func(_ context.Context, c mcpinstall.Client, _ mcpinstall.Options) (mcpinstall.Result, error) {
		cleaned = append(cleaned, c.ID)
		return mcpinstall.Result{Client: c.ID, Label: c.Label, Action: "removed"}, nil
	}

	out, _, err := executeCommand(t, "mcp", "uninstall")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(cleaned) != 2 {
		t.Errorf("cleaned = %v, want the two carrying the entry", cleaned)
	}
	if !strings.Contains(out.String(), "removed entry") {
		t.Errorf("output = %s", out.String())
	}
}

// Nothing to remove is worth saying plainly rather than reporting success over
// an empty set.
func TestMCPUninstallWhenNothingCarriesTheEntry(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	defer resetFlags()
	configuredAs()

	_, _, err := executeCommand(t, "mcp", "uninstall")
	if err == nil || !strings.Contains(err.Error(), "no client carries an entry") {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPUninstallReportsAFailure(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	defer resetFlags()

	mcpUninstall = func(_ context.Context, c mcpinstall.Client, _ mcpinstall.Options) (mcpinstall.Result, error) {
		if c.ID == mcpinstall.Cursor {
			return mcpinstall.Result{}, errors.New("mcp.json is not valid JSON")
		}
		return mcpinstall.Result{Client: c.ID, Label: c.Label, Action: "removed"}, nil
	}

	_, errOut, err := executeCommand(t, "mcp", "uninstall", "--client", "cursor", "--client", "vscode")
	if err == nil || !strings.Contains(err.Error(), "1 of 2 clients failed") {
		t.Fatalf("err = %v, want a non-zero outcome", err)
	}
	if !strings.Contains(errOut.String(), "not valid JSON") {
		t.Errorf("the failure was not reported: %s", errOut.String())
	}
}

func TestMCPUninstallSaysWhenThereWasNothingToRemove(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	defer resetFlags()

	mcpUninstall = func(_ context.Context, c mcpinstall.Client, _ mcpinstall.Options) (mcpinstall.Result, error) {
		return mcpinstall.Result{Client: c.ID, Label: c.Label, Action: mcpinstall.ActionAbsent}, nil
	}

	out, _, err := executeCommand(t, "mcp", "uninstall", "--client", "cursor")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out.String(), "no entry") {
		t.Errorf("output = %s", out.String())
	}
}

func TestMCPUninstallJSON(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	defer resetFlags()

	out, _, err := executeCommand(t, "mcp", "uninstall", "--client", "windsurf", "--json")
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	for _, want := range []string{`"removed"`, `"backups_purged"`, `"backups_remaining"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %s: %s", want, out.String())
		}
	}
}

func TestMCPInstallWritesTheNamedClient(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	var got mcpinstall.Options
	mcpInstall = func(_ context.Context, c mcpinstall.Client, o mcpinstall.Options) (mcpinstall.Result, error) {
		got = o
		return mcpinstall.Result{Label: c.Label, Target: "/tmp/mcp.json", Action: "added",
			Backup: "/tmp/mcp.json.l4-backup-1", Note: c.Note}, nil
	}

	out, _, err := executeCommand(t, "mcp", "install", "--client", "cursor", "--name", "levelfour-rw")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if got.Name != "levelfour-rw" || got.Endpoint != mcp.Endpoint {
		t.Errorf("options = %+v", got)
	}
	if got.APIKey != "l4_test_testkey123456789a" {
		t.Errorf("the stored credential was not passed through: %q", got.APIKey)
	}

	text := out.String()
	for _, want := range []string{"Cursor", "added", "levelfour-rw", "/tmp/mcp.json", "l4-backup-1",
		"what are we spending this month"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestMCPInstallConfiguresEveryDetectedClient(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	cursor, _ := mcpinstall.Find(mcpinstall.Cursor)
	desktop, _ := mcpinstall.Find(mcpinstall.ClaudeDesktop)
	mcpClassify = func() (present, hinted []mcpinstall.Client) {
		return []mcpinstall.Client{cursor, desktop}, nil
	}

	var configured []string
	mcpInstall = func(_ context.Context, c mcpinstall.Client, _ mcpinstall.Options) (mcpinstall.Result, error) {
		configured = append(configured, c.ID)
		return mcpinstall.Result{Label: c.Label, Action: "added"}, nil
	}

	out, _, err := executeCommand(t, "mcp", "install")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(configured) != 2 {
		t.Errorf("configured = %v", configured)
	}
	// It has to be obvious what it decided to touch.
	if !strings.Contains(out.String(), "Detected Cursor, Claude Desktop") {
		t.Errorf("output does not name what it detected:\n%s", out.String())
	}
}

func TestMCPInstallWithNoClientOnTheMachine(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "mcp", "install")
	if err == nil || !strings.Contains(err.Error(), "no MCP client detected") {
		t.Fatalf("err = %v", err)
	}
}

// A leftover config directory must not be treated as a client to install into,
// and must not be silently ignored either: the user is told which one and how to
// override.
func TestMCPInstallNamesAClientItOnlyHasAHintOf(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	vscode, _ := mcpinstall.Find(mcpinstall.VSCode)
	mcpClassify = func() (present, hinted []mcpinstall.Client) {
		return nil, []mcpinstall.Client{vscode}
	}

	_, _, err := executeCommand(t, "mcp", "install")
	if err == nil {
		t.Fatal("a leftover directory was treated as a client to configure")
	}
	if !strings.Contains(err.Error(), "VS Code") ||
		!strings.Contains(err.Error(), "--client") {
		t.Errorf("err = %v, want it to name the client and the override", err)
	}
}

func TestMCPInstallRejectsAnUnknownClient(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	defer resetFlags()

	_, _, err := executeCommand(t, "mcp", "install", "--client", "emacs")
	if err == nil || !strings.Contains(err.Error(), "unknown client") {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPInstallReportsAFailedClientAndKeepsGoing(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	mcpInstall = func(_ context.Context, c mcpinstall.Client, _ mcpinstall.Options) (mcpinstall.Result, error) {
		if c.ID == mcpinstall.Cursor {
			return mcpinstall.Result{}, errors.New("mcp.json is not valid JSON")
		}
		return mcpinstall.Result{Label: c.Label, Action: "added"}, nil
	}

	out, errOut, err := executeCommand(t, "mcp", "install", "--client", "cursor", "--client", "vscode")
	if !strings.Contains(errOut.String(), "not valid JSON") {
		t.Errorf("the failure was not reported: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "VS Code") {
		t.Errorf("one client failing stopped the rest: %s", out.String())
	}
	// Keeping going is not the same as succeeding. A client that failed has no
	// LevelFour tools, and a caller that only reads the exit code has to learn
	// that from the exit code.
	if err == nil {
		t.Fatal("a client failed and the command still exited 0")
	}
	if !strings.Contains(err.Error(), "1 of 2 clients failed") {
		t.Errorf("err = %v, want it to name how many failed", err)
	}
}

func TestMCPInstallFailsWhenNothingCouldBeConfigured(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	mcpInstall = func(context.Context, mcpinstall.Client, mcpinstall.Options) (mcpinstall.Result, error) {
		return mcpinstall.Result{}, errors.New("nope")
	}

	_, _, err := executeCommand(t, "mcp", "install", "--client", "cursor")
	if err == nil || !strings.Contains(err.Error(), "no client was configured") {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPInstallJSON(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	out, _, err := executeCommand(t, "mcp", "install", "--client", "windsurf", "--json")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !strings.Contains(out.String(), `"installed"`) {
		t.Errorf("output = %s", out.String())
	}
}

func TestMCPInstallNeedsTheBinaryPath(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	osExecutable = func() (string, error) { return "", errors.New("cannot read /proc/self/exe") }

	_, _, err := executeCommand(t, "mcp", "install", "--client", "claude-desktop")
	if err == nil || !strings.Contains(err.Error(), "cannot locate the l4 binary") {
		t.Fatalf("err = %v", err)
	}
}

// withTTY makes isTerminal report true, which is what the interactive login
// path requires. Tests do not run on a terminal.
func withTTY(t *testing.T) {
	t.Helper()
	orig := isTerminal
	isTerminal = func() bool { return true }
	t.Cleanup(func() { isTerminal = orig })
}

func TestMCPInstallLogsInFirstWhenThereIsNoCredential(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	withTTY(t)
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	origLogin := authLoginCmd.RunE
	t.Cleanup(func() { authLoginCmd.RunE = origLogin })

	// The existing device flow is the only way this CLI mints a key, so
	// install has to call it rather than inventing a second path.
	called := false
	authLoginCmd.RunE = func(*cobra.Command, []string) error {
		called = true
		return kr.Set("levelfour-cli", "api-key", "l4_live_minted_by_login")
	}

	var got mcpinstall.Options
	mcpInstall = func(_ context.Context, c mcpinstall.Client, o mcpinstall.Options) (mcpinstall.Result, error) {
		got = o
		return mcpinstall.Result{Label: c.Label, Action: "added"}, nil
	}

	if _, _, err := executeCommand(t, "mcp", "install", "--client", "cursor"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if !called {
		t.Error("install did not run the device login flow")
	}
	if got.APIKey != "l4_live_minted_by_login" {
		t.Errorf("the freshly minted key was not used: %q", got.APIKey)
	}
}

func TestMCPInstallStopsWhenLoginFails(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	withTTY(t)
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	origLogin := authLoginCmd.RunE
	t.Cleanup(func() { authLoginCmd.RunE = origLogin })
	authLoginCmd.RunE = func(*cobra.Command, []string) error { return errors.New("browser never came back") }

	_, _, err := executeCommand(t, "mcp", "install", "--client", "cursor")
	if err == nil || !strings.Contains(err.Error(), "browser never came back") {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPInstallStopsWhenLoginStoresNothing(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	withTTY(t)
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	origLogin := authLoginCmd.RunE
	t.Cleanup(func() { authLoginCmd.RunE = origLogin })
	authLoginCmd.RunE = func(*cobra.Command, []string) error { return nil }

	_, _, err := executeCommand(t, "mcp", "install", "--client", "cursor")
	if err == nil || !strings.Contains(err.Error(), "without storing a key") {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPServeNeedsACredential(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "mcp", "serve")
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPServeRunsTheLocalServer(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	flagAPI = "https://api.levelfour.ai"
	defer resetFlags()

	var session mcp.Session
	mcpServe = func(_ context.Context, s mcp.Session) error {
		session = s
		return nil
	}

	if _, _, err := executeCommand(t, "mcp", "serve"); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if session.Fetcher == nil {
		t.Error("serve was handed no REST fetcher")
	}
	if session.Version != Version {
		t.Errorf("version = %q, want %q", session.Version, Version)
	}
}

func TestMCPStatus(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	mcpStatus = func(_ context.Context, c mcpinstall.Client, name string) mcpinstall.State {
		if name != mcp.ServerName {
			t.Errorf("looked for %q, want the default name", name)
		}
		if c.ID != mcpinstall.Cursor {
			return mcpinstall.State{Client: c.ID, Label: c.Label, Status: mcpinstall.StatusNotInstalled}
		}
		return mcpinstall.State{
			Client: c.ID, Label: c.Label,
			Status:   mcpinstall.StatusInstalled,
			Endpoint: mcp.Endpoint,
		}
	}

	out, _, err := executeCommand(t, "mcp", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Cursor", mcpinstall.StatusInstalled, mcpinstall.StatusNotInstalled, mcp.Endpoint, mcp.Summary()} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q:\n%s", want, text)
		}
	}
}

func TestMCPStatusWithoutACredential(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	out, _, err := executeCommand(t, "mcp", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), "l4 auth login") {
		t.Errorf("output does not say how to fix it:\n%s", out.String())
	}
}

func TestMCPStatusJSON(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	out, _, err := executeCommand(t, "mcp", "status", "--json")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), `"clients"`) {
		t.Errorf("output = %s", out.String())
	}
}

func TestMCPEndpointOverride(t *testing.T) {
	defer resetFlags()
	if mcpEndpoint() != mcp.Endpoint {
		t.Errorf("default endpoint = %q", mcpEndpoint())
	}
	flagMCPEndpoint = "http://localhost:8080/mcp"
	if mcpEndpoint() != "http://localhost:8080/mcp" {
		t.Errorf("override = %q", mcpEndpoint())
	}
}

// A scriptable command must not block on a browser. --json is a documented use,
// and CI has no terminal to press Enter on.
func TestMCPInstallDoesNotLogInWhenThereIsNoTerminal(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	origTerminal := isTerminal
	isTerminal = func() bool { return false }
	t.Cleanup(func() { isTerminal = origTerminal })

	origLogin := authLoginCmd.RunE
	t.Cleanup(func() { authLoginCmd.RunE = origLogin })
	authLoginCmd.RunE = func(*cobra.Command, []string) error {
		t.Error("install opened the browser login with no terminal to drive it")
		return nil
	}

	_, _, err := executeCommand(t, "mcp", "install", "--client", "cursor")
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("err = %v, want it to say it is not authenticated", err)
	}
	if !strings.Contains(err.Error(), credentialEnvVar) {
		t.Errorf("err = %v, want it to name the environment variable", err)
	}
}

// --endpoint writes a URL into a client's entry. Claude Desktop has no URL in
// its entry: it is given a command, and the server that command starts resolves
// its own backend. Installing both together pointed the remote clients at the
// given server and left Claude Desktop on the default, saying nothing, so the
// person testing against a preview server had one client still answering from
// production.
func TestInstallRefusesAnEndpointItCannotAimAtEveryClient(t *testing.T) {
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	cursor, _ := mcpinstall.Find(mcpinstall.Cursor)
	desktop, _ := mcpinstall.Find(mcpinstall.ClaudeDesktop)
	mcpClassify = func() (present, hinted []mcpinstall.Client) {
		return []mcpinstall.Client{cursor, desktop}, nil
	}

	var installed []string
	mcpInstall = func(_ context.Context, c mcpinstall.Client, _ mcpinstall.Options) (mcpinstall.Result, error) {
		installed = append(installed, c.ID)
		return mcpinstall.Result{Client: c.ID}, nil
	}

	_, _, err := executeCommand(t, "mcp", "install", "--endpoint", "https://mcp.example.test/mcp")
	if err == nil {
		t.Fatal("the install went ahead and split the clients across two servers")
	}
	if !strings.Contains(err.Error(), desktop.Label) {
		t.Errorf("error does not name the client that cannot be aimed: %v", err)
	}
	if !strings.Contains(err.Error(), "--client "+mcpinstall.Cursor) {
		t.Errorf("error does not name the clients the flag does apply to: %v", err)
	}
	if len(installed) != 0 {
		t.Errorf("configured %v before refusing, leaving a half-applied endpoint", installed)
	}
}

// Narrowing to the clients a URL reaches is the way through, and it must work.
func TestInstallAcceptsAnEndpointForRemoteClientsAlone(t *testing.T) {
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	var got string
	mcpInstall = func(_ context.Context, c mcpinstall.Client, o mcpinstall.Options) (mcpinstall.Result, error) {
		got = o.Endpoint
		return mcpinstall.Result{Client: c.ID}, nil
	}

	_, _, err := executeCommand(t, "mcp", "install",
		"--client", mcpinstall.Cursor, "--endpoint", "https://mcp.example.test/mcp")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if got != "https://mcp.example.test/mcp" {
		t.Errorf("endpoint written = %q, want the one given", got)
	}
}

// Without the flag, a set containing the stdio client is the ordinary install.
func TestInstallWithoutAnEndpointStillConfiguresTheStdioClient(t *testing.T) {
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	desktop, _ := mcpinstall.Find(mcpinstall.ClaudeDesktop)
	mcpClassify = func() (present, hinted []mcpinstall.Client) {
		return []mcpinstall.Client{desktop}, nil
	}

	var installed []string
	mcpInstall = func(_ context.Context, c mcpinstall.Client, _ mcpinstall.Options) (mcpinstall.Result, error) {
		installed = append(installed, c.ID)
		return mcpinstall.Result{Client: c.ID}, nil
	}

	if _, _, err := executeCommand(t, "mcp", "install"); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(installed) != 1 || installed[0] != mcpinstall.ClaudeDesktop {
		t.Errorf("configured %v, want Claude Desktop", installed)
	}
}

// Naming only the client that cannot be aimed leaves nothing for the flag to
// apply to, so the error says that rather than suggesting an empty --client.
func TestInstallEndpointWithOnlyTheStdioClientNamed(t *testing.T) {
	stubMCP(t)
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "mcp", "install",
		"--client", mcpinstall.ClaudeDesktop, "--endpoint", "https://mcp.example.test/mcp")
	if err == nil {
		t.Fatal("expected a refusal")
	}
	if !strings.Contains(err.Error(), "no other client in this set") {
		t.Errorf("error = %v, want the no-other-client wording", err)
	}
	if strings.Contains(err.Error(), "--client ") {
		t.Errorf("error suggests naming clients when there are none to name: %v", err)
	}
}

func TestResolveKeySource(t *testing.T) {
	orig := flagMCPKeySource
	t.Cleanup(func() { flagMCPKeySource = orig })

	cases := []struct {
		flag string
		want mcpinstall.KeySource
		bad  bool
	}{
		{"inline", mcpinstall.KeyInline, false},
		{"env", mcpinstall.KeyFromEnv, false},
		{"vault", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			flagMCPKeySource = tc.flag
			got, err := resolveKeySource()
			if tc.bad {
				if err == nil {
					t.Fatalf("--key-source %q was accepted, and an unknown value must not fall "+
						"through to writing the credential", tc.flag)
				}
				if !strings.Contains(err.Error(), "inline or env") {
					t.Errorf("error does not name the valid choices: %v", err)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

// A backup taken over an existing install still holds that credential, so the
// count of what is left behind has to be right, and --purge-backups has to
// actually delete them.
func TestHandleBackupsCountsAndPurges(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cursor, _ := mcpinstall.Find(mcpinstall.Cursor)

	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, stamp := range []string{"20260827-093000", "20260827-093100"} {
		name := filepath.Join(dir, "mcp.json.l4-backup-levelfour-"+stamp)
		if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	origName, origPurge := flagMCPName, flagMCPPurge
	t.Cleanup(func() { flagMCPName, flagMCPPurge = origName, origPurge })
	flagMCPName = "levelfour"

	flagMCPPurge = false
	if sweep := handleBackups([]mcpinstall.Client{cursor}); sweep.remaining != 2 || sweep.purged != 0 {
		t.Errorf("without --purge-backups sweep = %+v, want 2 remaining", sweep)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*l4-backup*")); len(left) != 2 {
		t.Errorf("counting the backups deleted %d of them", 2-len(left))
	}

	flagMCPPurge = true
	if sweep := handleBackups([]mcpinstall.Client{cursor}); sweep.purged != 2 || sweep.remaining != 0 {
		t.Errorf("with --purge-backups sweep = %+v, want 2 purged", sweep)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*l4-backup*")); len(left) != 0 {
		t.Errorf("%d backup(s) survived the purge", len(left))
	}
}

// A backup that cannot be removed is counted as left behind, not as purged: the
// message tells the user a credential may still be on disk.
func TestHandleBackupsCountsWhatItCouldNotRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cursor, _ := mcpinstall.Find(mcpinstall.Cursor)

	dir := filepath.Join(home, ".cursor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json.l4-backup-levelfour-20260827-093000"),
		[]byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	origName, origPurge := flagMCPName, flagMCPPurge
	t.Cleanup(func() { flagMCPName, flagMCPPurge = origName, origPurge })
	flagMCPName, flagMCPPurge = "levelfour", true

	if sweep := handleBackups([]mcpinstall.Client{cursor}); sweep.purged != 0 || sweep.remaining != 1 {
		t.Errorf("sweep = %+v, want the undeletable backup counted as remaining", sweep)
	}
}

func TestPrintUninstallResults(t *testing.T) {
	var out, errOut bytes.Buffer
	origOut, origErr := output.Stdout, output.Stderr
	output.Stdout, output.Stderr = &out, &errOut
	t.Cleanup(func() { output.Stdout, output.Stderr = origOut, origErr })

	origName := flagMCPName
	t.Cleanup(func() { flagMCPName = origName })
	flagMCPName = "levelfour"

	printUninstallResults(
		[]mcpinstall.Result{
			{Label: "Cursor", Action: "removed", Backup: "/tmp/mcp.json.l4-backup-levelfour-1"},
			{Label: "VS Code", Action: mcpinstall.ActionAbsent},
		},
		[]string{"Windsurf: permission denied"},
		backupSweep{purged: 2, remaining: 3},
	)

	combined := out.String() + errOut.String()
	for _, want := range []string{
		"Cursor: removed entry",
		"l4-backup-levelfour-1",
		`VS Code: no entry "levelfour" to remove`,
		"Windsurf: permission denied",
		"Deleted 2 backup file(s).",
		"3 backup file(s) left in place",
		"--purge-backups",
	} {
		if !strings.Contains(combined, want) {
			t.Errorf("output is missing %q:\n%s", want, combined)
		}
	}
}

// A client whose directory survives its uninstall is named, never written to.
func TestResolveMCPClientsNamesWhatItSkips(t *testing.T) {
	stubMCP(t)
	defer resetFlags()

	var out bytes.Buffer
	origOut := output.Stdout
	output.Stdout = &out
	t.Cleanup(func() { output.Stdout = origOut })

	cursor, _ := mcpinstall.Find(mcpinstall.Cursor)
	vscode, _ := mcpinstall.Find(mcpinstall.VSCode)
	mcpClassify = func() (present, hinted []mcpinstall.Client) {
		return []mcpinstall.Client{cursor}, []mcpinstall.Client{vscode}
	}

	got, err := resolveMCPClients()
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0].ID != mcpinstall.Cursor {
		t.Fatalf("resolved %v, want Cursor alone", labelsOf(got))
	}
	if !strings.Contains(out.String(), "Skipping "+vscode.Label) {
		t.Errorf("the skipped client was not named:\n%s", out.String())
	}
}
