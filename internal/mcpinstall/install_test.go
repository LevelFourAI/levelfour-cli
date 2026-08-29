package mcpinstall

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	second.Name = "levelfour-rw"
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
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The directory, not the file. The config is replaced by writing a temp file
	// beside it and renaming over, so what has to be writable is the directory
	// that holds both. A read-only file in a writable directory is replaceable,
	// which is the same rule rename has always followed.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

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
	if !strings.HasSuffix(result.Backup, backupSuffix+"levelfour-20260827-093000") {
		t.Errorf("backup = %q", result.Backup)
	}
}

func TestStatus(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	ctx := context.Background()

	cursor, _ := Find(Cursor)
	state := Status(ctx, cursor, "levelfour")
	if state.Status != StatusClientNotFound {
		t.Errorf("status on an empty machine = %q, want %q", state.Status, StatusClientNotFound)
	}

	if _, err := Install(ctx, cursor, testOptions()); err != nil {
		t.Fatal(err)
	}
	state = Status(ctx, cursor, "levelfour")
	// Installing wrote the config file, which is itself evidence the client is here.
	if state.Status != StatusInstalled {
		t.Errorf("status after install = %q, want %q", state.Status, StatusInstalled)
	}
	if state.Endpoint != "https://mcp.levelfour.ai/mcp" {
		t.Errorf("endpoint = %q", state.Endpoint)
	}

	if got := Status(ctx, cursor, "levelfour-rw").Status; got == StatusInstalled {
		t.Errorf("a name that was never installed reported as %q", got)
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
		if got := Status(ctx, windsurf, "levelfour").Status; got == StatusInstalled {
			t.Errorf("%s reported as %q", content, got)
		}
	}

	_ = home
}

// Status reports the scope Install manages, which is user scope. A server at
// local or project scope belongs to whoever put it there, and reporting it as
// configured here would promise that `l4 mcp install` maintains it.
func TestStatusOfClaudeCode(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	code, _ := Find(ClaudeCode)
	ctx := context.Background()
	config := filepath.Join(home, ".claude.json")

	if got := Status(ctx, code, "levelfour").Status; got == StatusInstalled {
		t.Errorf("reported %q with no config file at all", got)
	}

	local := `{"projects":{"/some/repo":{"mcpServers":{"levelfour":{"url":"https://example/mcp"}}}}}`
	if err := os.WriteFile(config, []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Status(ctx, code, "levelfour").Status; got == StatusInstalled {
		t.Errorf("reported a local-scope entry as %q; install does not manage it", got)
	}

	user := `{"mcpServers":{"levelfour":{"url":"https://mcp.levelfour.ai/mcp"}}}`
	if err := os.WriteFile(config, []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}
	state := Status(ctx, code, "levelfour")
	if state.Status != StatusInstalled {
		t.Fatalf("status = %q, want an installed variant", state.Status)
	}
	if state.Endpoint != "https://mcp.levelfour.ai/mcp" {
		t.Errorf("endpoint = %q, want the url read back from the file", state.Endpoint)
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

func TestUninstallRemovesOnlyTheNamedEntry(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)
	ctx := context.Background()

	if _, err := Install(ctx, cursor, testOptions()); err != nil {
		t.Fatal(err)
	}
	other := testOptions()
	other.Name = "levelfour-rw"
	if _, err := Install(ctx, cursor, other); err != nil {
		t.Fatal(err)
	}

	result, err := Uninstall(ctx, cursor, testOptions())
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if result.Action != actionRemoved {
		t.Errorf("action = %q, want removed", result.Action)
	}
	if result.Backup == "" {
		t.Error("no backup was taken before removing an entry")
	}

	servers := readJSON(t, result.Target)["mcpServers"].(map[string]any)
	if _, present := servers["levelfour"]; present {
		t.Error("the named entry survived")
	}
	if _, present := servers["levelfour-rw"]; !present {
		t.Error("uninstall removed an entry it was not asked about")
	}
}

// Removing something that is not there is how a caller reaches a known state,
// so it reports rather than fails.
func TestUninstallOfSomethingAbsentIsNotAnError(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)
	ctx := context.Background()

	result, err := Uninstall(ctx, cursor, testOptions())
	if err != nil {
		t.Fatalf("uninstall with no config at all: %v", err)
	}
	if result.Action != ActionAbsent {
		t.Errorf("action = %q, want %q", result.Action, ActionAbsent)
	}

	if _, err := Install(ctx, cursor, testOptions()); err != nil {
		t.Fatal(err)
	}
	absent := testOptions()
	absent.Name = "never-installed"
	if result, err = Uninstall(ctx, cursor, absent); err != nil || result.Action != ActionAbsent {
		t.Errorf("result = %+v, err = %v", result, err)
	}
}

// A client whose application is gone still has its config cleaned. That is the
// case uninstall exists for, and a detection-gated removal would skip it.
func TestUninstallCleansAClientThatIsNoLongerInstalled(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	windsurf, _ := Find(Windsurf)
	ctx := context.Background()

	if _, err := Install(ctx, windsurf, testOptions()); err != nil {
		t.Fatal(err)
	}
	result, err := Uninstall(ctx, windsurf, testOptions())
	if err != nil || result.Action != actionRemoved {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
}

func TestBackupsListsTheDatedCopies(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)
	ctx := context.Background()

	if got := Backups(cursor, "levelfour"); len(got) != 0 {
		t.Errorf("Backups on an empty machine = %v", got)
	}

	origNow := now
	t.Cleanup(func() { now = origNow })

	// Three installs at distinct times. The first finds no file so takes no
	// backup; the next two each back up what the previous one left.
	for _, stamp := range []time.Time{
		time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 29, 11, 0, 0, 0, time.UTC),
	} {
		now = func() time.Time { return stamp }
		if _, err := Install(ctx, cursor, testOptions()); err != nil {
			t.Fatal(err)
		}
	}

	if got := Backups(cursor, "levelfour"); len(got) != 2 {
		t.Errorf("Backups() = %v, want 2", got)
	}
}

// Claude Code used to be configured by running `claude mcp add --header
// "Authorization: Bearer <key>"`, which put a live credential in a process
// listing. It is written the same way every other client is now, so there is no
// subprocess to leak it and no vendor binary to depend on.
func TestClaudeCodeIsConfiguredWithoutASubprocess(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	// Nothing on PATH: an install that still shelled out could not succeed.
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	code, _ := Find(ClaudeCode)

	result, err := Install(context.Background(), code, testOptions())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if result.Action != actionAdded {
		t.Errorf("action = %q, want added", result.Action)
	}

	entry := (entryRef{filepath.Join(home, ".claude.json"), sectionMCPServers, "levelfour"}).read()
	if entry == nil {
		t.Fatal("no entry was written to ~/.claude.json")
	}
	if entry[fieldURL] != "https://mcp.levelfour.ai/mcp" {
		t.Errorf("url = %v", entry[fieldURL])
	}
}

// ~/.claude.json is the vendor's file and holds the user's project history. A
// rewrite that dropped it would cost them that history, which is why the entry
// is merged rather than the file replaced.
func TestClaudeCodeKeepsEverythingElseInTheFile(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	code, _ := Find(ClaudeCode)
	config := filepath.Join(home, ".claude.json")

	// A large integer alongside the history: decoded through float64 it would
	// come back reformatted, in a file this command does not own.
	existing := `{"projects":{"/some/repo":{"lastCost":1.5}},"installMethod":"brew","firstStartTime":9007199254740993}`
	if err := os.WriteFile(config, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), code, testOptions()); err != nil {
		t.Fatalf("install: %v", err)
	}

	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"/some/repo"`, `"installMethod": "brew"`, "9007199254740993"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s is gone from the config after an install:\n%s", want, raw)
		}
	}
}

// The vendor creates this file 0644. Once a credential is in it that is a live
// bearer token any local user can read, and the command tells the user their key
// is readable only by them.
func TestClaudeCodeRestrictsTheFileItPutACredentialIn(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	code, _ := Find(ClaudeCode)
	config := filepath.Join(home, ".claude.json")

	if err := os.WriteFile(config, []byte(`{"mcpServers":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(context.Background(), code, testOptions()); err != nil {
		t.Fatalf("install: %v", err)
	}

	info, err := os.Stat(config)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != configMode {
		t.Errorf("mode = %o, want %o: a bearer token is in this file", mode, configMode)
	}
}

func TestClaudeCodeReplacesAnExistingEntry(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	code, _ := Find(ClaudeCode)

	user := `{"mcpServers":{"levelfour":{"url":"https://old.example/mcp"}}}`
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Install(context.Background(), code, testOptions())
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if result.Action != actionReplaced {
		t.Errorf("action = %q, want replaced", result.Action)
	}
	entry := (entryRef{filepath.Join(home, ".claude.json"), sectionMCPServers, "levelfour"}).read()
	if entry[fieldURL] != "https://mcp.levelfour.ai/mcp" {
		t.Errorf("url = %v, want the new endpoint", entry[fieldURL])
	}
}

func TestUninstallOfClaudeCodeEditsTheFile(t *testing.T) {
	home := withHome(t)
	withGOOS(t, "darwin")
	code, _ := Find(ClaudeCode)
	config := filepath.Join(home, ".claude.json")

	user := `{"projects":{"/repo":{}},"mcpServers":{"levelfour":{"url":"https://mcp.levelfour.ai/mcp"}}}`
	if err := os.WriteFile(config, []byte(user), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Uninstall(context.Background(), code, testOptions())
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if result.Action != actionRemoved {
		t.Errorf("action = %q, want removed", result.Action)
	}
	if (entryRef{config, sectionMCPServers, "levelfour"}).read() != nil {
		t.Error("the entry is still there")
	}
	raw, _ := os.ReadFile(config)
	if !strings.Contains(string(raw), "/repo") {
		t.Error("uninstall took the project history with it")
	}
}

// Every client is configured with nothing else on the machine: no executable on
// PATH and no application bundle. Each still reports installed, because the
// config file carrying the entry is itself proof the client is here. That is why
// there is no separate "installed but the client is gone" status to report.
func TestConfiguredAlwaysMeansFound(t *testing.T) {
	for _, c := range Clients {
		t.Run(c.ID, func(t *testing.T) {
			withHome(t)
			withGOOS(t, "darwin")
			lookPath = func(string) (string, error) { return "", errors.New("not found") }
			applicationDirs = func() []string { return nil }

			ctx := context.Background()
			if _, err := Install(ctx, c, testOptions()); err != nil {
				t.Fatalf("install: %v", err)
			}
			if got := Status(ctx, c, "levelfour").Status; got != StatusInstalled {
				t.Errorf("status = %q, want %q", got, StatusInstalled)
			}
		})
	}
}

// --purge-backups must not take the rollback copy for an entry it was told to
// leave alone. Installing a second server backs up a file that already holds the
// first, so an unscoped sweep deleted the only copy protecting it.
//
// The names overlap on purpose: "levelfour" is a prefix of "levelfour-rw", which
// a trailing wildcard would happily match.
func TestBackupsAreScopedToTheEntry(t *testing.T) {
	withHome(t)
	withGOOS(t, "darwin")
	cursor, _ := Find(Cursor)
	ctx := context.Background()

	stamps := []time.Time{
		time.Date(2026, 8, 27, 9, 30, 0, 0, time.UTC),
		time.Date(2026, 8, 27, 9, 31, 0, 0, time.UTC),
		time.Date(2026, 8, 27, 9, 32, 0, 0, time.UTC),
	}
	origNow := now
	t.Cleanup(func() { now = origNow })
	at := func(i int) { now = func() time.Time { return stamps[i] } }

	rw := testOptions()
	rw.Name = "levelfour-rw"

	at(0)
	if _, err := Install(ctx, cursor, testOptions()); err != nil {
		t.Fatal(err)
	}
	at(1)
	if _, err := Install(ctx, cursor, rw); err != nil {
		t.Fatal(err)
	}
	at(2)
	if _, err := Install(ctx, cursor, testOptions()); err != nil {
		t.Fatal(err)
	}

	base := Backups(cursor, "levelfour")
	if len(base) != 1 {
		t.Fatalf("backups for levelfour = %v, want the one taken by its own second install", base)
	}
	for _, path := range base {
		if strings.Contains(filepath.Base(path), "levelfour-rw") {
			t.Errorf("purging levelfour would take %s, which belongs to levelfour-rw", path)
		}
	}
	if got := Backups(cursor, "levelfour-rw"); len(got) != 1 {
		t.Errorf("backups for levelfour-rw = %v, want its own", got)
	}
}
