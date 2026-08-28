package mcpinstall

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome points every path lookup at a temporary directory, and restores the
// package-level seams afterwards.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	origHome, origGOOS, origLook := userHomeDir, goos, lookPath
	origApps := applicationDirs
	userHomeDir = func() (string, error) { return home, nil }
	// Point the application lookup inside the temp home. Left alone it reads the
	// real /Applications, and whether Cursor happens to be installed on the
	// machine running the suite is not something these tests should depend on.
	applicationDirs = func() []string { return []string{filepath.Join(home, "Applications")} }
	t.Cleanup(func() {
		userHomeDir, goos, lookPath = origHome, origGOOS, origLook
		applicationDirs = origApps
	})
	return home
}

func withGOOS(t *testing.T, value string) {
	t.Helper()
	orig := goos
	goos = value
	t.Cleanup(func() { goos = orig })
}

func TestConfigPathsPerOS(t *testing.T) {
	home := withHome(t)

	cases := []struct {
		os     string
		client string
		want   string
	}{
		{"darwin", ClaudeDesktop, filepath.Join(home, "Library", "Application Support", "Claude", "claude_desktop_config.json")},
		{"linux", ClaudeDesktop, filepath.Join(home, ".config", "Claude", "claude_desktop_config.json")},
		{"darwin", VSCode, filepath.Join(home, "Library", "Application Support", "Code", "User", "mcp.json")},
		{"linux", VSCode, filepath.Join(home, ".config", "Code", "User", "mcp.json")},
		{"darwin", Cursor, filepath.Join(home, ".cursor", "mcp.json")},
		{"darwin", Windsurf, filepath.Join(home, ".codeium", "windsurf", "mcp_config.json")},
		{"darwin", ClaudeCode, filepath.Join(home, ".claude.json")},
	}
	for _, tc := range cases {
		t.Run(tc.os+"/"+tc.client, func(t *testing.T) {
			withGOOS(t, tc.os)
			c, ok := Find(tc.client)
			if !ok {
				t.Fatalf("no client %q", tc.client)
			}
			got, err := c.ConfigPath()
			if err != nil {
				t.Fatalf("config path: %v", err)
			}
			if got != tc.want {
				t.Errorf("path = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWindowsUsesAppData(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "windows")

	t.Setenv("APPDATA", filepath.Join(home, "Roaming"))
	c, _ := Find(ClaudeDesktop)
	got, err := c.ConfigPath()
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	if got != filepath.Join(home, "Roaming", "Claude", "claude_desktop_config.json") {
		t.Errorf("path = %q", got)
	}

	// A Windows session with no APPDATA still has to resolve somewhere sane.
	t.Setenv("APPDATA", "")
	got, err = c.ConfigPath()
	if err != nil {
		t.Fatalf("config path: %v", err)
	}
	if got != filepath.Join(home, "AppData", "Roaming", "Claude", "claude_desktop_config.json") {
		t.Errorf("fallback path = %q", got)
	}
}

func TestConfigPathReportsAHomeDirectoryFailure(t *testing.T) {
	withHome(t)
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	for _, c := range Clients {
		if _, err := c.ConfigPath(); err == nil {
			t.Errorf("%s: expected an error when the home directory cannot be resolved", c.ID)
		}
		if c.Detect() {
			t.Errorf("%s: reported installed with no home directory", c.ID)
		}
	}
}

func TestDetect(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	for _, c := range Clients {
		if c.Presence() != Absent {
			t.Errorf("%s: detected on an empty machine", c.ID)
		}
	}

	// An executable on PATH is proof.
	lookPath = func(name string) (string, error) {
		if name == "claude" {
			return "/usr/local/bin/claude", nil
		}
		return "", errors.New("not found")
	}
	code, _ := Find(ClaudeCode)
	if got := code.Presence(); got != Present {
		t.Errorf("Claude Code presence with its CLI on PATH = %v, want Present", got)
	}

	// A directory the client created once is a hint and nothing more. Treating it
	// as proof would mean creating a config, with a credential in it, for an editor
	// that is not installed.
	if err := os.MkdirAll(filepath.Join(home, ".cursor"), 0o700); err != nil {
		t.Fatal(err)
	}
	cursor, _ := Find(Cursor)
	if got := cursor.Presence(); got != Likely {
		t.Errorf("Cursor presence from a bare directory = %v, want Likely", got)
	}
	if !cursor.Detect() {
		t.Error("a directory should still count as installed for the status table")
	}

	// The app bundle turns the same hint into proof.
	if err := os.MkdirAll(filepath.Join(home, "Applications", "Cursor.app"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := cursor.Presence(); got != Present {
		t.Errorf("Cursor presence with its app installed = %v, want Present", got)
	}

	// So does an existing config file, which only exists once someone configured
	// a server in it.
	windsurf, _ := Find(Windsurf)
	path, _ := windsurf.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := windsurf.Presence(); got != Present {
		t.Errorf("Windsurf presence from its config file = %v, want Present", got)
	}

	detected := Detected()
	if len(detected) != 3 {
		t.Errorf("Detected() = %v, want Claude Code, Cursor and Windsurf", labels(detected))
	}
	if len(Suggested()) != 0 {
		t.Errorf("Suggested() = %v, want none once every hint is confirmed", labels(Suggested()))
	}
}

// TestSuggestedIsNeverInstalledInto is the security property: a client we only
// have a hint of must be reported, never written to.
func TestSuggestedIsNeverInstalledInto(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	lookPath = func(string) (string, error) { return "", errors.New("not found") }

	// VS Code leaves this behind and does not remove it on uninstall.
	if err := os.MkdirAll(filepath.Join(home, "Library", "Application Support", "Code", "User"), 0o700); err != nil {
		t.Fatal(err)
	}
	if len(Detected()) != 0 {
		t.Errorf("Detected() = %v, want none: only a leftover directory is present", labels(Detected()))
	}
	suggested := Suggested()
	if len(suggested) != 1 || suggested[0].ID != VSCode {
		t.Fatalf("Suggested() = %v, want VS Code alone", labels(suggested))
	}
}

func labels(clients []Client) []string {
	out := make([]string, 0, len(clients))
	for _, c := range clients {
		out = append(out, c.ID)
	}
	return out
}

func TestFindAndIDs(t *testing.T) {
	if _, ok := Find("nope"); ok {
		t.Error("Find accepted an unknown id")
	}
	ids := IDs()
	if len(ids) != len(Clients) {
		t.Errorf("IDs() = %v", ids)
	}
	if !strings.Contains(strings.Join(ids, ","), ClaudeCode) {
		t.Errorf("IDs() = %v, want it to include %s", ids, ClaudeCode)
	}
}

func TestEntryShapesMatchEachVendorsSpelling(t *testing.T) {
	opts := Options{
		Name:     "levelfour",
		Endpoint: "https://mcp.levelfour.ai/mcp",
		APIKey:   "l4_live_secret",
		Binary:   "/opt/homebrew/bin/l4",
	}

	cursor, _ := Find(Cursor)
	entry := cursor.Entry(opts)
	if entry["url"] != opts.Endpoint {
		t.Errorf("cursor entry = %v", entry)
	}
	if entry["headers"].(map[string]any)["Authorization"] != "Bearer l4_live_secret" {
		t.Errorf("cursor headers = %v", entry["headers"])
	}

	code, _ := Find(VSCode)
	entry = code.Entry(opts)
	if entry["type"] != "http" || entry["url"] != opts.Endpoint {
		t.Errorf("vscode entry = %v", entry)
	}

	windsurf, _ := Find(Windsurf)
	entry = windsurf.Entry(opts)
	if entry["serverUrl"] != opts.Endpoint {
		t.Errorf("windsurf entry = %v, want serverUrl rather than url", entry)
	}
	if _, present := entry["url"]; present {
		t.Error("windsurf entry carries a url field it does not read")
	}

	desktop, _ := Find(ClaudeDesktop)
	entry = desktop.Entry(opts)
	if entry["command"] != opts.Binary {
		t.Errorf("claude desktop entry = %v", entry)
	}
	args := entry["args"].([]any)
	if len(args) != 2 || args[0] != "mcp" || args[1] != "serve" {
		t.Errorf("claude desktop args = %v", args)
	}
	// The stdio entry must not carry the key: the local server reads it from the
	// keychain, which is the reason Claude Desktop gets stdio at all.
	if strings.Contains(strings.Join(stringList(entry["args"]), " "), opts.APIKey) {
		t.Error("the API key was written into the Claude Desktop config")
	}
	if _, present := entry["headers"]; present {
		t.Error("the stdio entry carries headers")
	}
}
