package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/mcp"
	"github.com/LevelFourAI/levelfour-cli/internal/mcpinstall"
	"github.com/spf13/cobra"
	kr "github.com/zalando/go-keyring"
)

// stubMCP replaces the filesystem-facing seams for one test.
func stubMCP(t *testing.T) {
	t.Helper()
	origInstall, origStatus, origDetected, origServe, origExec :=
		mcpInstall, mcpStatus, mcpDetected, mcpServe, osExecutable
	origSuggested := mcpSuggested
	t.Cleanup(func() {
		mcpInstall, mcpStatus, mcpDetected, mcpServe, osExecutable =
			origInstall, origStatus, origDetected, origServe, origExec
		mcpSuggested = origSuggested
	})
	osExecutable = func() (string, error) { return "/opt/homebrew/bin/l4", nil }
	mcpDetected = func() []mcpinstall.Client { return nil }
	// Without this the real machine decides the result: whatever leftover client
	// directories the test host happens to have would change the error text.
	mcpSuggested = func() []mcpinstall.Client { return nil }
	mcpStatus = func(context.Context, mcpinstall.Client, string) mcpinstall.State {
		return mcpinstall.State{}
	}
	mcpInstall = func(_ context.Context, c mcpinstall.Client, o mcpinstall.Options) (mcpinstall.Result, error) {
		return mcpinstall.Result{Client: c.ID, Label: c.Label, Target: "/tmp/" + c.ID, Action: "added", Note: c.Note}, nil
	}
	mcpServe = func(context.Context, mcp.Session) error { return nil }
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

	out, _, err := executeCommand(t, "mcp", "install", "--client", "cursor", "--name", "levelfour-acme")
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if got.Name != "levelfour-acme" || got.Endpoint != mcp.Endpoint {
		t.Errorf("options = %+v", got)
	}
	if got.APIKey != "l4_test_testkey123456789a" {
		t.Errorf("the stored credential was not passed through: %q", got.APIKey)
	}

	text := out.String()
	for _, want := range []string{"Cursor", "added", "levelfour-acme", "/tmp/mcp.json", "l4-backup-1",
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
	mcpDetected = func() []mcpinstall.Client { return []mcpinstall.Client{cursor, desktop} }

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
	mcpSuggested = func() []mcpinstall.Client { return []mcpinstall.Client{vscode} }

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

func TestMCPInstallLogsInFirstWhenThereIsNoCredential(t *testing.T) {
	kr.MockInit()
	stubMCP(t)
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
		return mcpinstall.State{
			Client: c.ID, Label: c.Label,
			Installed:  c.ID == mcpinstall.Cursor,
			Configured: c.ID == mcpinstall.Cursor,
			Endpoint:   mcp.Endpoint,
		}
	}

	out, _, err := executeCommand(t, "mcp", "status")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	text := out.String()
	for _, want := range []string{"Cursor", "yes", "no", mcp.Endpoint, mcp.Summary()} {
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

func TestYesNo(t *testing.T) {
	if yesNo(true) != "yes" || yesNo(false) != "no" {
		t.Error("yesNo")
	}
}
