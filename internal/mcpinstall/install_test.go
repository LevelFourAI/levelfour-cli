package mcpinstall

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testOptions() Options {
	return Options{
		Name:     "levelfour",
		Endpoint: "https://mcp.levelfour.ai/mcp",
		APIKey:   "l4_live_secret",
		Binary:   "/opt/homebrew/bin/l4",
	}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return out
}

func TestInstallCreatesAConfigThatDidNotExist(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)

	result, err := Install(context.Background(), cursor, testOptions())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if result.Action != actionAdded {
		t.Errorf("action = %q, want added", result.Action)
	}
	if result.Backup != "" {
		t.Errorf("backup = %q, want none for a file that did not exist", result.Backup)
	}

	servers := readJSON(t, result.Target)["mcpServers"].(map[string]any)
	entry := servers["levelfour"].(map[string]any)
	if entry["url"] != "https://mcp.levelfour.ai/mcp" {
		t.Errorf("entry = %v", entry)
	}

	info, err := os.Stat(result.Target)
	if err != nil {
		t.Fatal(err)
	}
	// The file holds a bearer token.
	if mode := info.Mode().Perm(); mode != configMode {
		t.Errorf("mode = %o, want %o", mode, configMode)
	}
}

func TestInstallMergesWithoutTouchingAnythingElse(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)

	path := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	existing := `{
  "someOtherTopLevelKey": {"keep": true},
  "mcpServers": {
    "github": {"url": "https://api.githubcopilot.com/mcp"},
    "levelfour": {"url": "https://old.example/mcp"}
  }
}`
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Install(context.Background(), cursor, testOptions())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if result.Action != actionReplaced {
		t.Errorf("action = %q, want replaced", result.Action)
	}

	root := readJSON(t, path)
	if root["someOtherTopLevelKey"] == nil {
		t.Error("an unrelated top-level key was dropped")
	}
	servers := root["mcpServers"].(map[string]any)
	if servers["github"] == nil {
		t.Error("another server was dropped")
	}
	if servers["levelfour"].(map[string]any)["url"] != "https://mcp.levelfour.ai/mcp" {
		t.Errorf("the LevelFour entry was not refreshed: %v", servers["levelfour"])
	}

	// The file did not hold a credential before and does now, so a mode that was
	// fine at 0644 is not fine any more.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != configMode {
		t.Errorf("mode = %o, want %o: a token was written into a world-readable file", mode, configMode)
	}

	// The backup has to be the file as it was, byte for byte.
	backup, err := os.ReadFile(filepath.Clean(result.Backup))
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backup) != existing {
		t.Errorf("backup does not match the original:\n%s", backup)
	}
}

func TestInstallUnderASecondNameLeavesTheFirstAlone(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)
	ctx := context.Background()

	if _, err := Install(ctx, cursor, testOptions()); err != nil {
		t.Fatalf("first install: %v", err)
	}
	second := testOptions()
	second.Name = "levelfour-acme"
	second.APIKey = "l4_live_other_org"
	result, err := Install(ctx, cursor, second)
	if err != nil {
		t.Fatalf("second install: %v", err)
	}

	servers := readJSON(t, result.Target)["mcpServers"].(map[string]any)
	if len(servers) != 2 {
		t.Fatalf("servers = %v, want one entry per organization", servers)
	}
	first := servers["levelfour"].(map[string]any)["headers"].(map[string]any)
	if first["Authorization"] != "Bearer l4_live_secret" {
		t.Errorf("the first organization's credential was overwritten: %v", first)
	}
}

func TestInstallRefusesToTouchAFileItCannotParse(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)

	path := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	broken := `{"mcpServers": {` // a half-written file, which happens
	if err := os.WriteFile(path, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Install(context.Background(), cursor, testOptions())
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("err = %v, want a refusal", err)
	}
	after, _ := os.ReadFile(filepath.Clean(path))
	if string(after) != broken {
		t.Error("the unparseable file was modified anyway")
	}
}

func TestInstallRefusesAConfigWhoseSectionIsNotAnObject(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)

	path := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"mcpServers": ["not", "an", "object"]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(context.Background(), cursor, testOptions()); err == nil {
		t.Fatal("expected a refusal")
	}
}

func TestInstallTreatsAnEmptyFileAsAnEmptyConfig(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)

	path := filepath.Join(home, ".cursor", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\n  \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(context.Background(), cursor, testOptions()); err != nil {
		t.Fatalf("install: %v", err)
	}
	if readJSON(t, path)["mcpServers"] == nil {
		t.Error("the entry was not written")
	}
}

func TestInstallReportsAConfigPathItCannotResolve(t *testing.T) {
	withHome(t)
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	cursor, _ := Find(Cursor)

	if _, err := Install(context.Background(), cursor, testOptions()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestInstallReportsAnUnreadableConfig(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)

	// A directory where the config file should be: readable path, unreadable file.
	if err := os.MkdirAll(filepath.Join(home, ".cursor", "mcp.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), cursor, testOptions()); err == nil ||
		!strings.Contains(err.Error(), "cannot read") {
		t.Fatalf("err = %v, want a read failure", err)
	}
}

func TestInstallReportsABackupItCannotWrite(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)

	dir := filepath.Join(home, ".cursor")
	path := filepath.Join(dir, "mcp.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A read-only directory: the config is still readable, the backup is not
	// writable, and the install must stop rather than proceed unprotected.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, err := Install(context.Background(), cursor, testOptions()); err == nil ||
		!strings.Contains(err.Error(), "cannot back up") {
		t.Fatalf("err = %v, want a backup failure", err)
	}
}

func TestInstallReportsADirectoryItCannotCreate(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)

	// Readable but not writable: the config file is genuinely absent, and the
	// directory it belongs in cannot be created.
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })

	if _, err := Install(context.Background(), cursor, testOptions()); err == nil ||
		!strings.Contains(err.Error(), "cannot create") {
		t.Fatalf("err = %v, want a directory failure", err)
	}
}

func TestInstallReportsAConfigItCannotWrite(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)

	dir := filepath.Join(home, ".cursor")
	path := filepath.Join(dir, "mcp.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// Readable, backed up, and then not writable.
	if err := os.WriteFile(path, []byte("{}"), 0o400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	if _, err := Install(context.Background(), cursor, testOptions()); err == nil {
		t.Fatal("expected a write failure")
	}
}

func TestInstallReportsAnEntryItCannotEncode(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")

	broken := Client{
		ID:      "broken",
		Label:   "Broken",
		Section: "mcpServers",
		path:    cursorConfigPath,
		entry:   func(Client, Options) map[string]any { return map[string]any{"stream": make(chan int)} },
	}
	if _, err := Install(context.Background(), broken, testOptions()); err == nil ||
		!strings.Contains(err.Error(), "cannot encode") {
		t.Fatalf("err = %v, want an encode failure", err)
	}
}

func TestBackupNamesAreDatedSoASecondInstallKeepsTheFirstCopy(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)
	ctx := context.Background()

	origNow := now
	stamp := time.Date(2026, 8, 27, 9, 30, 0, 0, time.UTC)
	now = func() time.Time { return stamp }
	t.Cleanup(func() { now = origNow })

	if _, err := Install(ctx, cursor, testOptions()); err != nil {
		t.Fatal(err)
	}
	result, err := Install(ctx, cursor, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(result.Backup, backupSuffix+"20260827-093000") {
		t.Errorf("backup = %q", result.Backup)
	}
}

// stubClaude replaces the vendor CLI with a recorder.
func stubClaude(t *testing.T, respond func(args []string) ([]byte, error)) *[][]string {
	t.Helper()
	var calls [][]string
	orig := runCommand
	runCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		return respond(args)
	}
	t.Cleanup(func() { runCommand = orig })
	return &calls
}

func TestClaudeCodeIsConfiguredThroughItsOwnCLI(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	code, _ := Find(ClaudeCode)

	calls := stubClaude(t, func(args []string) ([]byte, error) {
		if args[1] == "get" {
			return nil, errors.New("no such server")
		}
		return []byte("added"), nil
	})

	result, err := Install(context.Background(), code, testOptions())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if result.Action != actionAdded {
		t.Errorf("action = %q", result.Action)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %v, want a get then an add", *calls)
	}
	// The exact argv, not a set of substrings. --header is variadic in the vendor
	// CLI, so a header placed ahead of the positionals swallows the name and the
	// URL and the command does nothing. Every substring assertion this replaced
	// held just as well for the ordering that never worked.
	want := []string{
		"claude", "mcp", "add",
		"--transport", "http",
		"--scope", "user",
		"levelfour", "https://mcp.levelfour.ai/mcp",
		"--header", "Authorization: Bearer l4_live_secret",
	}
	if !reflect.DeepEqual((*calls)[1], want) {
		t.Errorf("add argv:\n got %q\nwant %q", (*calls)[1], want)
	}
	for i, arg := range (*calls)[1] {
		if arg == "--header" && i < len((*calls)[1])-3 {
			t.Errorf("--header at %d leaves %d args after its value for a variadic flag to eat",
				i, len((*calls)[1])-i-2)
		}
	}
}

func TestClaudeCodeReplacesAnExistingEntry(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	code, _ := Find(ClaudeCode)

	// `claude mcp add` refuses a name that already exists, so a re-run after
	// `l4 auth login` has to remove the old entry to refresh the credential.
	calls := stubClaude(t, func([]string) ([]byte, error) { return nil, nil })

	result, err := Install(context.Background(), code, testOptions())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if result.Action != actionReplaced {
		t.Errorf("action = %q, want replaced", result.Action)
	}
	if len(*calls) != 3 || (*calls)[1][2] != "remove" {
		t.Fatalf("calls = %v, want get, remove, add", *calls)
	}
}

func TestClaudeCodeSurfacesCLIFailures(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	code, _ := Find(ClaudeCode)
	ctx := context.Background()

	stubClaude(t, func(args []string) ([]byte, error) {
		if args[1] == "add" {
			return []byte("boom"), errors.New("exit status 1")
		}
		return nil, errors.New("no such server")
	})
	if _, err := Install(ctx, code, testOptions()); err == nil || !strings.Contains(err.Error(), "mcp add") {
		t.Fatalf("err = %v, want the add failure", err)
	}

	stubClaude(t, func(args []string) ([]byte, error) {
		if args[1] == "remove" {
			return []byte("nope"), errors.New("exit status 1")
		}
		return nil, nil
	})
	if _, err := Install(ctx, code, testOptions()); err == nil || !strings.Contains(err.Error(), "mcp remove") {
		t.Fatalf("err = %v, want the remove failure", err)
	}
}

func TestStatus(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	ctx := context.Background()

	cursor, _ := Find(Cursor)
	state := Status(ctx, cursor, "levelfour")
	if state.Installed || state.Configured {
		t.Errorf("state on an empty machine = %+v", state)
	}

	if _, err := Install(ctx, cursor, testOptions()); err != nil {
		t.Fatal(err)
	}
	state = Status(ctx, cursor, "levelfour")
	if !state.Installed || !state.Configured {
		t.Errorf("state after install = %+v", state)
	}
	if state.Endpoint != "https://mcp.levelfour.ai/mcp" {
		t.Errorf("endpoint = %q", state.Endpoint)
	}

	if Status(ctx, cursor, "levelfour-acme").Configured {
		t.Error("a name that was never installed reported as configured")
	}

	desktop, _ := Find(ClaudeDesktop)
	if _, err := Install(ctx, desktop, testOptions()); err != nil {
		t.Fatal(err)
	}
	state = Status(ctx, desktop, "levelfour")
	if state.Endpoint != "/opt/homebrew/bin/l4 mcp serve" {
		t.Errorf("stdio endpoint = %q", state.Endpoint)
	}

	// A config whose section is the wrong shape reports unconfigured rather
	// than failing: status must never be the thing that breaks.
	windsurf, _ := Find(Windsurf)
	path, _ := windsurf.ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{`{"mcpServers": "wrong"}`, `{"mcpServers": {"levelfour": "wrong"}}`, `{oops`} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if Status(ctx, windsurf, "levelfour").Configured {
			t.Errorf("%s reported as configured", content)
		}
	}

	_ = home
}

func TestStatusOfClaudeCode(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	code, _ := Find(ClaudeCode)
	ctx := context.Background()

	stubClaude(t, func([]string) ([]byte, error) { return nil, errors.New("no such server") })
	if Status(ctx, code, "levelfour").Configured {
		t.Error("reported configured when the CLI does not know the server")
	}

	stubClaude(t, func([]string) ([]byte, error) { return nil, nil })
	state := Status(ctx, code, "levelfour")
	if !state.Configured || state.Endpoint == "" {
		t.Errorf("state = %+v", state)
	}
}

func TestStatusWithNoResolvableHome(t *testing.T) {
	withHome(t)
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	cursor, _ := Find(Cursor)

	if state := Status(context.Background(), cursor, "levelfour"); state.Target != "" {
		t.Errorf("state = %+v, want no target", state)
	}
}

func TestDescribeEntry(t *testing.T) {
	cases := []struct {
		entry map[string]any
		want  string
	}{
		{map[string]any{"url": "https://a/mcp"}, "https://a/mcp"},
		{map[string]any{"serverUrl": "https://b/mcp"}, "https://b/mcp"},
		{map[string]any{"command": "/bin/l4", "args": []any{"mcp", "serve", 7}}, "/bin/l4 mcp serve"},
		{map[string]any{"command": "/bin/l4", "args": "not a list"}, "/bin/l4 "},
		{map[string]any{"something": "else"}, "unrecognized entry"},
	}
	for _, tc := range cases {
		if got := describeEntry(tc.entry); got != tc.want {
			t.Errorf("describeEntry(%v) = %q, want %q", tc.entry, got, tc.want)
		}
	}
}

func TestRunCommandDefaultShellsOut(t *testing.T) {
	// The default seam has to actually run a process, or the tests above are
	// testing a stub that never matched reality.
	out, err := runCommand(context.Background(), "echo", "levelfour")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(string(out)) != "levelfour" {
		t.Errorf("output = %q", out)
	}
}
