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

type Result struct {
	Client string `json:"client"`
	Label  string `json:"label"`
	Target string `json:"target"`
	Action string `json:"action"` // "added" or "replaced"
	Backup string `json:"backup,omitempty"`
	Note   string `json:"transport"`
}

// Status uses the words the commands use, so a row answers the question the
// command asks.
type State struct {
	Client   string `json:"client"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Target   string `json:"config_path"`
	Endpoint string `json:"endpoint,omitempty"` // url, serverUrl or the command a stdio entry runs
}

const (
	StatusInstalled      = "installed"
	StatusNotInstalled   = "not installed"
	StatusClientNotFound = "client not found"
	// Reachable only for the delegated client, which is found by its executable
	// rather than by the config file that carries the entry.
	StatusOrphaned = "installed (client not found)"
)

// Configured wins the phrasing, because that is what install changed.
func describeStatus(found, configured bool) string {
	switch {
	case configured && found:
		return StatusInstalled
	case configured:
		return StatusOrphaned
	case found:
		return StatusNotInstalled
	default:
		return StatusClientNotFound
	}
}

const (
	actionAdded    = "added"
	actionReplaced = "replaced"
	actionRemoved  = "removed"

	// Nothing to remove is a success: uninstall exists to reach a known state.
	ActionAbsent = "not configured"

	// Dated, so a second install does not overwrite the evidence from the first.
	backupSuffix = ".l4-backup-"

	// These files hold a bearer token, including a rewrite of one that was looser.
	configMode = 0o600
	dirMode    = 0o700
)

var (
	now        = time.Now
	runCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
)

// Install never overwrites a config blind: a file that is not valid JSON stops
// the install rather than being replaced.
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

func Status(_ context.Context, c Client, name string) State {
	state := State{Client: c.ID, Label: c.Label, Status: describeStatus(c.Detect(), false)}

	path, err := c.ConfigPath()
	if err != nil {
		return state
	}
	state.Target = path

	entry := (entryRef{path, c.Section, name}).read()
	if entry == nil {
		return state
	}
	state.Status = describeStatus(c.Detect(), true)
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
	path := ref.path
	previous := ref.read()

	// Read the config rather than asking `claude mcp get`, which searches every
	// scope. This writes user scope, so a name that exists at local or project
	// scope would otherwise be removed from a scope it is not in, and the remove
	// fails with "No MCP server named ... in user scope".
	action := actionAdded
	if previous != nil {
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
		// The vendor CLI creates this file 0644, and it now holds a bearer token.
		// Only when we put one there: a missing file means nothing to protect.
		if c.WritesCredential(o) {
			if chmodErr := os.Chmod(path, configMode); chmodErr != nil && !os.IsNotExist(chmodErr) {
				return action, fmt.Errorf(
					"%s now holds a credential but could not be restricted to %o: %w", path, configMode, chmodErr)
			}
		}
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

// Re-reads, so a change the vendor CLI made in between survives.
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

// Uninstall removes the entry under o.Name from one client. It is the inverse of
// Install: the same backup is taken, the same single key is touched, and the
// delegated client goes through its own CLI.
//
// Removing an entry that is not there is not an error. Uninstall exists to reach
// a known state, and a client that was never configured is already in it.
func Uninstall(ctx context.Context, c Client, o Options) (Result, error) {
	result := Result{Client: c.ID, Label: c.Label, Action: ActionAbsent}

	path, err := c.ConfigPath()
	if err != nil {
		return result, err
	}
	result.Target = path

	original, existed, err := readConfig(path)
	if err != nil || !existed {
		return result, err
	}
	if (entryRef{path, c.Section, o.Name}).read() == nil {
		return result, nil
	}

	result.Backup, err = writeBackup(path, original)
	if err != nil {
		return result, err
	}
	result.Action = actionRemoved

	if c.Delegated {
		return result, c.removeViaCLI(ctx, o)
	}
	return result, removeEntry(path, original, c.Section, o.Name)
}

func (c Client) removeViaCLI(ctx context.Context, o Options) error {
	out, err := runCommand(ctx, "claude", "mcp", "remove", o.Name, "--scope", "user")
	if err != nil {
		return fmt.Errorf("claude mcp remove %s failed: %w: %s", o.Name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeEntry(path string, original []byte, section, name string) error {
	root, err := decodeConfig(path, original)
	if err != nil {
		return err
	}
	entries, ok := root[section].(map[string]any)
	if !ok {
		return nil
	}
	delete(entries, name)
	root[section] = entries
	return writeConfig(path, root)
}

// Backups lists the dated copies this command has left beside a client's config.
// Nothing prunes them, and each one is a full copy of a file that may hold a
// credential, so `uninstall --purge-backups` needs to find them.
func Backups(c Client) []string {
	path, err := c.ConfigPath()
	if err != nil {
		return nil
	}
	matches, err := filepath.Glob(path + backupSuffix + "*")
	if err != nil {
		return nil
	}
	return matches
}
