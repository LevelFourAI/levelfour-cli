package mcpinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Result describes what one client install did, so the command can print it
// rather than claiming success in the abstract.
type Result struct {
	Client string `json:"client"`
	Label  string `json:"label"`
	Target string `json:"target"`
	Action string `json:"action"` // "added" or "replaced"
	Backup string `json:"backup,omitempty"`
	Note   string `json:"transport"`
}

// State is what `l4 mcp status` reports per client.
type State struct {
	Client     string `json:"client"`
	Label      string `json:"label"`
	Installed  bool   `json:"installed"`
	Configured bool   `json:"configured"`
	Target     string `json:"config_path"`
	Endpoint   string `json:"endpoint,omitempty"` // url, serverUrl or the command a stdio entry runs
}

const (
	actionAdded    = "added"
	actionReplaced = "replaced"

	// backupSuffix keeps our copies recognizable and greppable, and dated so a
	// second install does not overwrite the evidence from the first.
	backupSuffix = ".l4-backup-"

	// configMode: these files hold a bearer token. 0600 on every write, including
	// a rewrite of a file that was looser before.
	configMode = 0o600
	dirMode    = 0o700
)

var (
	now        = time.Now
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
)

// Install writes the entry for one client. It never overwrites a config file
// blind: the file is parsed first (a file that is not valid JSON stops the
// install rather than being replaced), copied to a dated backup, and only the
// one entry under o.Name is touched.
func Install(ctx context.Context, c Client, o Options) (Result, error) {
	result := Result{Client: c.ID, Label: c.Label, Note: c.Note}

	path, err := c.ConfigPath()
	if err != nil {
		return result, err
	}
	result.Target = path

	original, existed, err := readConfig(path)
	if err != nil {
		return result, err
	}
	if existed {
		backup, err := writeBackup(path, original)
		if err != nil {
			return result, err
		}
		result.Backup = backup
	}

	if c.Delegated {
		result.Target = fmt.Sprintf("claude mcp add %s (user scope, %s)", o.Name, path)
		result.Action, err = c.installViaCLI(ctx, o, entryRef{path, c.Section, o.Name})
		return result, err
	}

	root, err := decodeConfig(path, original)
	if err != nil {
		return result, err
	}
	section, err := sectionOf(root, c.Section, path)
	if err != nil {
		return result, err
	}

	result.Action = actionAdded
	if _, exists := section[o.Name]; exists {
		result.Action = actionReplaced
	}
	section[o.Name] = c.Entry(o)
	root[c.Section] = section
	c.PatchRoot(o, root)

	return result, writeConfig(path, root)
}

// Status reports whether each client is installed and whether it already carries
// an entry under this name.
func Status(ctx context.Context, c Client, name string) State {
	state := State{Client: c.ID, Label: c.Label, Installed: c.Detect()}

	path, err := c.ConfigPath()
	if err != nil {
		return state
	}
	state.Target = path

	if c.Delegated {
		state.Configured = claudeCLIHasServer(ctx, name)
		if state.Configured {
			state.Endpoint = "configured in Claude Code"
		}
		return state
	}

	original, existed, err := readConfig(path)
	if err != nil || !existed {
		return state
	}
	root, err := decodeConfig(path, original)
	if err != nil {
		return state
	}
	section, ok := root[c.Section].(map[string]any)
	if !ok {
		return state
	}
	entry, ok := section[name].(map[string]any)
	if !ok {
		return state
	}
	state.Configured = true
	state.Endpoint = describeEntry(entry)
	return state
}

func describeEntry(entry map[string]any) string {
	for _, key := range []string{fieldURL, fieldServerURL} {
		if value, ok := entry[key].(string); ok {
			return value
		}
	}
	if command, ok := entry[fieldCommand].(string); ok {
		return command + " " + strings.Join(stringList(entry[fieldArgs]), " ")
	}
	return "unrecognized entry"
}

func stringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func readConfig(path string) (data []byte, existed bool, err error) {
	data, err = os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("cannot read %s: %w", path, err)
	}
	return data, true, nil
}

func decodeConfig(path string, data []byte) (map[string]any, error) {
	root := map[string]any{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return root, nil
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf(
			"%s is not valid JSON, so it was left alone: %w. Fix or move the file, then run this again", path, err)
	}
	return root, nil
}

func sectionOf(root map[string]any, name, path string) (map[string]any, error) {
	value, exists := root[name]
	if !exists || value == nil {
		return map[string]any{}, nil
	}
	section, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s has a %q that is not an object, so it was left alone", path, name)
	}
	return section, nil
}

func writeBackup(path string, original []byte) (string, error) {
	backup := path + backupSuffix + now().UTC().Format("20060102-150405")
	if err := os.WriteFile(backup, original, configMode); err != nil {
		return "", fmt.Errorf("cannot back up %s: %w", path, err)
	}
	return backup, nil
}

func writeConfig(path string, root map[string]any) error {
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot encode %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("cannot create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), configMode); err != nil {
		return err
	}
	// WriteFile only applies its mode when it creates the file, so a config that
	// was already 0644 would stay world-readable with a bearer token now in it.
	return os.Chmod(path, configMode)
}

// `claude mcp add` refuses a name that already exists, so an existing entry is
// removed first and restored if the add then fails.
//
// The positionals must precede --header: -H is variadic, so a header placed
// first consumes <name> and <commandOrUrl> and the command exits with "missing
// required argument 'name'".
func (c Client) installViaCLI(ctx context.Context, o Options, ref entryRef) (string, error) {
	previous := ref.read()

	action := actionAdded
	if claudeCLIHasServer(ctx, o.Name) {
		action = actionReplaced
		if out, err := runCommand(ctx, "claude", "mcp", "remove", o.Name, "--scope", "user"); err != nil {
			return action, fmt.Errorf("claude mcp remove %s failed: %w: %s", o.Name, err, strings.TrimSpace(string(out)))
		}
	}

	out, err := runCommand(ctx, "claude", "mcp", "add",
		"--transport", "http",
		"--scope", "user",
		o.Name, o.Endpoint,
		"--header", authHeaderLine(c, o))
	if err == nil {
		return action, nil
	}

	failed := fmt.Errorf("claude mcp add %s failed: %w: %s", o.Name, err, strings.TrimSpace(string(out)))
	if previous == nil {
		return action, failed
	}
	if restoreErr := ref.restore(previous); restoreErr != nil {
		return action, fmt.Errorf("%w. Restoring the previous entry also failed: %v", failed, restoreErr)
	}
	return action, fmt.Errorf("%w. The previous entry was put back", failed)
}

// Anything unreadable returns nil: refusing to install because a rollback might
// not be possible would trade a rare failure for a certain one.
// entryRef locates one server entry inside a client-owned config file.
type entryRef struct {
	path    string
	section string
	name    string
}

func (r entryRef) read() map[string]any {
	data, existed, err := readConfig(r.path)
	if err != nil || !existed {
		return nil
	}
	root, err := decodeConfig(r.path, data)
	if err != nil {
		return nil
	}
	entries, ok := root[r.section].(map[string]any)
	if !ok {
		return nil
	}
	entry, ok := entries[r.name].(map[string]any)
	if !ok {
		return nil
	}
	return entry
}

// Re-reads rather than reusing the earlier decode, so a change the vendor CLI
// made between the remove and the failure survives.
func (r entryRef) restore(entry map[string]any) error {
	data, _, err := readConfig(r.path)
	if err != nil {
		return err
	}
	root, err := decodeConfig(r.path, data)
	if err != nil {
		return err
	}
	entries, err := sectionOf(root, r.section, r.path)
	if err != nil {
		return err
	}
	entries[r.name] = entry
	root[r.section] = entries
	return writeConfig(r.path, root)
}

func claudeCLIHasServer(ctx context.Context, name string) bool {
	_, err := runCommand(ctx, "claude", "mcp", "get", name)
	return err == nil
}
