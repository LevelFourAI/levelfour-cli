// Package mcpinstall writes the LevelFour MCP server into the config of the
// agent clients on this machine.
//
// Each vendor wants the same three facts in a different file under a different
// key, and getting one wrong fails silently: the client starts and lists zero
// tools. The shapes below are transcribed from the documentation cited on each.
package mcpinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Also the values --client accepts.
const (
	ClaudeCode    = "claude-code"
	ClaudeDesktop = "claude-desktop"
	Cursor        = "cursor"
	VSCode        = "vscode"
	Windsurf      = "windsurf"
)

// install.go reads these back to report status, so each spelling is named once.
const (
	sectionMCPServers = "mcpServers"
	sectionServers    = "servers"

	fieldURL       = "url"
	fieldServerURL = "serverUrl"
	fieldHeaders   = "headers"
	fieldType      = "type"
	fieldCommand   = "command"
	fieldArgs      = "args"
)

type KeySource string

const (
	// Default because it is the only shape verified against every vendor. An
	// indirection a client does not resolve fails silently.
	KeyInline KeySource = "inline"
	// Writes a reference the client resolves at launch, spelled per vendor in
	// Client.keyRef, so the credential is never in the file.
	KeyFromEnv KeySource = "env"
)

// The variable `l4` itself reads, so one export serves the CLI and every client.
const CredentialEnvVar = "LEVELFOUR_TOKEN"

// Must match the id in the top-level inputs array, or the header resolves to an
// empty string and every request goes out unauthenticated.
const vsCodeInputID = "levelfour-api-key"

type Options struct {
	Name      string
	Endpoint  string
	APIKey    string
	Binary    string
	KeySource KeySource
}

type Client struct {
	ID    string
	Label string
	// VS Code says "servers"; everyone else says "mcpServers".
	Section string
	Note    string
	// Delegated clients own their own config format and are configured by
	// running their CLI rather than by editing a file.
	Delegated bool

	// Not interchangeable: Claude Code and Windsurf expand ${VAR}, Cursor and VS
	// Code expand ${env:VAR}, and VS Code also offers ${input:id}, which prompts
	// once and stores the value itself.
	keyRef string

	// VS Code and Cursor write no config until someone configures MCP, so without
	// these the only signal left is a directory that outlives an uninstall.
	bins []string
	app  string

	path      func() (string, error)
	entry     func(Client, Options) map[string]any
	rootPatch func(Client, Options, map[string]any)
}

// Seams, so the Windows and Linux paths are testable from a macOS machine.
var (
	goos        = runtime.GOOS
	userHomeDir = os.UserHomeDir
	lookPath    = exec.LookPath
)

var Clients = []Client{
	{
		ID:      ClaudeCode,
		Label:   "Claude Code",
		Note:    "remote HTTP, added with `claude mcp add --scope user` so it loads in every project",
		Section: sectionMCPServers,
		// ~/.claude.json also holds per-project state the vendor CLI owns, so
		// editing it by hand loses someone's project history.
		// https://code.claude.com/docs/en/mcp
		Delegated: true,
		keyRef:    "${" + CredentialEnvVar + "}",
		bins:      []string{"claude"},
		path:      claudeCodeConfigPath,
		entry:     remoteEntry,
	},
	{
		ID:    ClaudeDesktop,
		Label: "Claude Desktop",
		Note: "local stdio, running `l4 mcp serve`, because claude_desktop_config.json only " +
			"starts stdio servers. Your API key stays in the system keychain and is never written to this file",
		Section: sectionMCPServers,
		// https://modelcontextprotocol.io/docs/develop/connect-local-servers
		app:   "Claude.app",
		path:  claudeDesktopConfigPath,
		entry: stdioEntry,
	},
	{
		ID:      Cursor,
		Label:   "Cursor",
		Note:    "remote HTTP with an Authorization header",
		Section: sectionMCPServers,
		// https://cursor.com/docs/context/mcp
		keyRef: "${env:" + CredentialEnvVar + "}",
		bins:   []string{"cursor"},
		app:    "Cursor.app",
		path:   cursorConfigPath,
		entry:  remoteEntry,
	},
	{
		ID:      VSCode,
		Label:   "VS Code",
		Note:    "remote HTTP under the \"servers\" key, which is VS Code's own spelling",
		Section: sectionServers,
		// https://code.visualstudio.com/docs/agents/reference/mcp-configuration
		keyRef:    "${input:" + vsCodeInputID + "}",
		bins:      []string{"code"},
		app:       "Visual Studio Code.app",
		path:      vscodeConfigPath,
		entry:     vscodeEntry,
		rootPatch: vscodeInputs,
	},
	{
		ID:      Windsurf,
		Label:   "Windsurf",
		Note:    "remote HTTP under \"serverUrl\", which is Windsurf's own spelling",
		Section: sectionMCPServers,
		// https://docs.windsurf.com/windsurf/cascade/mcp
		keyRef: "${" + CredentialEnvVar + "}",
		bins:   []string{"windsurf"},
		app:    "Windsurf.app",
		path:   windsurfConfigPath,
		entry:  windsurfEntry,
	},
}

func Find(id string) (Client, bool) {
	for _, c := range Clients {
		if c.ID == id {
			return c, true
		}
	}
	return Client{}, false
}

func IDs() []string {
	ids := make([]string, 0, len(Clients))
	for _, c := range Clients {
		ids = append(ids, c.ID)
	}
	return ids
}

func (c Client) ConfigPath() (string, error) { return c.path() }

type Presence int

const (
	Absent Presence = iota
	// Only the directory a client creates on first run. That outlives an
	// uninstall, so on its own it does not justify writing a credential.
	Likely
	Present
)

// Detect answers "is this client here at all". Writing is gated on Presence
// instead, because a leftover directory is not reason enough to create a config
// with a bearer token in it.
func (c Client) Detect() bool { return c.Presence() >= Likely }

func (c Client) Presence() Presence {
	for _, bin := range c.bins {
		if _, err := lookPath(bin); err == nil {
			return Present
		}
	}
	if c.appIsInstalled() {
		return Present
	}
	path, err := c.path()
	if err != nil {
		return Absent
	}
	if _, err := os.Stat(path); err == nil {
		return Present
	}
	// The containing directory is a hint only when the client created it. For a
	// config that lives directly in the home directory, like Claude Code's
	// ~/.claude.json, the parent is the home directory itself and always exists,
	// which would make every machine look like it has the client.
	dir := filepath.Dir(path)
	if home, err := userHomeDir(); err == nil && filepath.Clean(dir) == filepath.Clean(home) {
		return Absent
	}
	if _, err := os.Stat(dir); err == nil {
		return Likely
	}
	return Absent
}

// macOS only; elsewhere the executable on PATH is the signal.
func (c Client) appIsInstalled() bool {
	if goos != "darwin" || c.app == "" {
		return false
	}
	for _, dir := range applicationDirs() {
		if _, err := os.Stat(filepath.Join(dir, c.app)); err == nil {
			return true
		}
	}
	return false
}

// A seam, or the tests depend on what the machine running them has installed.
var applicationDirs = func() []string {
	dirs := []string{"/Applications"}
	if home, err := userHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

func Detected() []Client {
	var found []Client
	for _, c := range Clients {
		if c.Presence() == Present {
			found = append(found, c)
		}
	}
	return found
}

// A hint but no proof, so the command can name them rather than write to them.
func Suggested() []Client {
	var found []Client
	for _, c := range Clients {
		if c.Presence() == Likely {
			found = append(found, c)
		}
	}
	return found
}

func (c Client) Entry(o Options) map[string]any { return c.entry(c, o) }

// The nil check lives here rather than in five call sites.
func (c Client) PatchRoot(o Options, root map[string]any) {
	if c.rootPatch != nil {
		c.rootPatch(c, o, root)
	}
}

func remoteEntry(c Client, o Options) map[string]any {
	return map[string]any{
		fieldURL:     o.Endpoint,
		fieldHeaders: authHeader(c, o),
	}
}

func vscodeEntry(c Client, o Options) map[string]any {
	entry := remoteEntry(c, o)
	entry[fieldType] = "http"
	return entry
}

func windsurfEntry(c Client, o Options) map[string]any {
	return map[string]any{
		fieldServerURL: o.Endpoint,
		fieldHeaders:   authHeader(c, o),
	}
}

func bearer(c Client, o Options) string {
	if o.KeySource == KeyFromEnv && c.keyRef != "" {
		return "Bearer " + c.keyRef
	}
	return "Bearer " + o.APIKey
}

func authHeader(c Client, o Options) map[string]any {
	return map[string]any{"Authorization": bearer(c, o)}
}

// One string, which is the shape `claude mcp add --header` takes.
func authHeaderLine(c Client, o Options) string {
	return "Authorization: " + bearer(c, o)
}

func (c Client) WritesCredential(o Options) bool {
	if c.entry == nil || c.keyRef == "" {
		return false
	}
	return o.KeySource != KeyFromEnv
}

// password:true keeps the value out of the file and out of settings sync, which
// is why VS Code needs no environment variable. Merged on id, so re-running an
// install does not stack duplicates.
func vscodeInputs(_ Client, o Options, root map[string]any) {
	if o.KeySource != KeyFromEnv {
		return
	}
	input := map[string]any{
		"type":        "promptString",
		"id":          vsCodeInputID,
		"description": "LevelFour API key",
		"password":    true,
	}
	existing, _ := root["inputs"].([]any)
	for i, item := range existing {
		if entry, ok := item.(map[string]any); ok && entry["id"] == vsCodeInputID {
			existing[i] = input
			root["inputs"] = existing
			return
		}
	}
	root["inputs"] = append(existing, input)
}

func stdioEntry(_ Client, o Options) map[string]any {
	return map[string]any{
		fieldCommand: o.Binary,
		fieldArgs:    []any{"mcp", "serve"},
	}
}

func claudeCodeConfigPath() (string, error) { return homePath(".claude.json") }
func cursorConfigPath() (string, error)     { return homePath(".cursor", "mcp.json") }
func windsurfConfigPath() (string, error) {
	return homePath(".codeium", "windsurf", "mcp_config.json")
}

func claudeDesktopConfigPath() (string, error) {
	return appDataPath("Claude", "claude_desktop_config.json")
}

func vscodeConfigPath() (string, error) {
	return appDataPath("Code", "User", "mcp.json")
}

func homePath(parts ...string) (string, error) {
	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, parts...)...), nil
}

func appDataPath(parts ...string) (string, error) {
	switch goos {
	case "darwin":
		return homePath(append([]string{"Library", "Application Support"}, parts...)...)
	case "windows":
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(append([]string{appData}, parts...)...), nil
		}
		return homePath(append([]string{"AppData", "Roaming"}, parts...)...)
	default:
		return homePath(append([]string{".config"}, parts...)...)
	}
}
