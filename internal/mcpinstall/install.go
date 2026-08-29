package mcpinstall

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
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
)

// Configured wins the phrasing, because that is what install changed.
//
// There is no "configured but the client is gone" state. An entry can only be
// read back out of a config file, and that file existing is itself what Presence
// counts as the client being here, so configured implies found for every client.
// TestConfiguredAlwaysMeansFound pins that, since a fourth status string that
// cannot occur is what the two booleans this replaced were carrying.
func describeStatus(found, configured bool) string {
	switch {
	case configured:
		return StatusInstalled
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

	// The date, as a glob. Matching on its shape is what keeps a purge of
	// "levelfour" from also taking the backups of "levelfour-rw", which a
	// trailing wildcard after the name would.
	backupStampGlob = "-[0-9][0-9][0-9][0-9][0-9][0-9][0-9][0-9]-[0-9][0-9][0-9][0-9][0-9][0-9]"

	// These files hold a bearer token, including a rewrite of one that was looser.
	configMode = 0o600
	dirMode    = 0o700
)

var now = time.Now

// Install never overwrites a config blind: a file that is not valid JSON stops
// the install rather than being replaced.
func Install(_ context.Context, c Client, o Options) (Result, error) {
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
		backup, err := writeBackup(path, o.Name, original)
		if err != nil {
			return result, err
		}
		result.Backup = backup
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
	decoder := json.NewDecoder(bytes.NewReader(data))
	// Numbers in a vendor's file are kept as their literal text. Decoding them
	// through float64 and re-encoding would reformat anything past 2^53.
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
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

func writeBackup(path, name string, original []byte) (string, error) {
	backup := path + backupSuffix + name + "-" + now().UTC().Format("20060102-150405")
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
	return replaceFile(path, append(encoded, '\n'))
}

// replaceFile writes a temporary file beside the target and renames it over.
//
// Two properties come from that, and both matter for a file holding a bearer
// token: os.CreateTemp opens at 0600, so the credential is never on disk under a
// mode the vendor chose, where a plain write to an existing 0644 file would be
// world-readable until a following chmod landed. And rename is atomic, so an
// interrupted write cannot truncate a config that also holds the user's own
// state.
func replaceFile(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".l4-tmp-")
	if err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	// Removing the temp file is a no-op once the rename has consumed it.
	defer func() { _ = os.Remove(temp.Name()) }()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return os.Rename(temp.Name(), path)
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

// Uninstall removes the entry under o.Name from one client. It is the inverse of
// Install: the same backup is taken and the same single key is touched.
//
// Removing an entry that is not there is not an error. Uninstall exists to reach
// a known state, and a client that was never configured is already in it.
func Uninstall(_ context.Context, c Client, o Options) (Result, error) {
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

	result.Backup, err = writeBackup(path, o.Name, original)
	if err != nil {
		return result, err
	}
	result.Action = actionRemoved
	return result, removeEntry(path, original, c.Section, o.Name)
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

// Backups lists the dated copies taken while operating on this entry. Nothing
// prunes them, and each is a full copy of a file that may hold a credential, so
// `uninstall --purge-backups` needs to find them.
//
// Scoped to the name, because a purge must not take the rollback copy for an
// entry the command was told to leave alone. Copies written before the name was
// part of the filename do not match, which keeps them.
func Backups(c Client, name string) []string {
	path, err := c.ConfigPath()
	if err != nil {
		return nil
	}
	matches, err := filepath.Glob(path + backupSuffix + name + backupStampGlob)
	if err != nil {
		return nil
	}
	return matches
}
