// Package mcpinstall writes the LevelFour MCP server into the config of the
// agent clients installed on this machine.
//
// Every client wants the same three facts (a name, an endpoint, a credential)
// in a different file under a different key with a different field name for the
// URL, and getting one of them wrong fails silently: the client starts, lists
// zero tools, and says nothing. The shapes below are transcribed from each
// vendor's current documentation, cited on the client, and are the whole reason
// this package exists rather than a paragraph in the README.
package mcpinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Client ids, also the values --client accepts.
const (
	ClaudeCode    = "claude-code"
	ClaudeDesktop = "claude-desktop"
	Cursor        = "cursor"
	VSCode        = "vscode"
	Windsurf      = "windsurf"
)

// The keys inside a server entry. Each vendor reads a different subset under a
// different spelling, and install.go reads them back to report status, so they
// are named here rather than repeated as literals on both sides.
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

// KeySource decides how the credential reaches a client that talks to the
// hosted server over HTTP.
type KeySource string

const (
	// KeyInline writes the credential itself, at 0600. One command and done, at
	// the cost of a bearer token sitting in a file the client owns. This is the
	// default because it is the only shape verified against every vendor: an
	// indirection a client does not resolve fails the way this whole package
	// exists to prevent, with the client starting, listing zero tools, and
	// saying nothing.
	KeyInline KeySource = "inline"
	// KeyFromEnv writes a reference the client resolves at launch, so the
	// credential is never in the file. Each vendor spells the reference
	// differently, which is what Client.keyRef carries.
	KeyFromEnv KeySource = "env"
)

// CredentialEnvVar is the variable the indirected entries point at. It is the
// one `l4` itself reads, so a user who exports it once has the CLI and every
// client working off a single value.
const CredentialEnvVar = "LEVELFOUR_TOKEN"

// vsCodeInputID ties the ${input:...} reference in the entry to the id declared
// in the top-level inputs array. The two have to agree or the header resolves to
// an empty string and every request is unauthenticated.
const vsCodeInputID = "levelfour-api-key"

// Options carry what an entry is built from.
type Options struct {
	// Name of the entry. One API key belongs to one organization, so a user in
	// several organizations installs one entry per organization.
	Name string
	// Endpoint is the hosted streamable HTTP server.
	Endpoint string
	// APIKey is sent as an Authorization header by the remote clients. It is not
	// written at all for a client configured to run the local stdio server, nor
	// for any client when KeySource is KeyFromEnv.
	APIKey string
	// Binary is the absolute path to this l4 binary, for the stdio clients.
	Binary string
	// KeySource decides between the credential and a reference to it. The zero
	// value is KeyInline.
	KeySource KeySource
}

// Client is one agent client this command knows how to configure.
type Client struct {
	ID    string
	Label string
	// Section is the top-level key entries live under. VS Code says "servers";
	// everyone else says "mcpServers".
	Section string
	// Note explains the transport chosen, and is printed after an install.
	Note string
	// Delegated clients own their own config format and are configured by
	// running their CLI rather than by editing a file.
	Delegated bool

	// keyRef is how this client spells "read the credential from somewhere other
	// than this file", used when KeySource is KeyFromEnv. The spellings are not
	// interchangeable: Claude Code and Windsurf expand ${VAR}, Cursor and VS Code
	// expand ${env:VAR}, and VS Code additionally offers ${input:id}, which
	// prompts once and stores the value in the editor's own secret storage
	// rather than in the environment.
	keyRef string

	// bins and app are how Presence proves the client is really here. A config
	// file proves it too, but VS Code and Cursor do not write one until someone
	// configures MCP, so without these the only remaining signal is a directory
	// that outlives an uninstall.
	bins []string
	app  string

	path  func() (string, error)
	entry func(Client, Options) map[string]any
	// rootPatch mutates the config root beyond this client's own entry. Only VS
	// Code needs it, for the inputs array that sits beside "servers".
	rootPatch func(Client, Options, map[string]any)
}

// Test seams. The OS and the filesystem are the only things this package
// touches, and both have to be substitutable to test the Windows and Linux
// paths from a macOS machine.
var (
	goos        = runtime.GOOS
	userHomeDir = os.UserHomeDir
	lookPath    = exec.LookPath
)

// Clients is the supported set, in the order results are printed.
var Clients = []Client{
	{
		ID:      ClaudeCode,
		Label:   "Claude Code",
		Note:    "remote HTTP, added with `claude mcp add --scope user` so it loads in every project",
		Section: sectionMCPServers,
		// Claude Code stores user-scope servers inside ~/.claude.json, which also
		// holds per-project state the CLI owns. Writing that file by hand is how
		// you lose someone's project history, so the vendor CLI does it.
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

// Find returns the client with this id.
func Find(id string) (Client, bool) {
	for _, c := range Clients {
		if c.ID == id {
			return c, true
		}
	}
	return Client{}, false
}

// IDs lists the accepted --client values.
func IDs() []string {
	ids := make([]string, 0, len(Clients))
	for _, c := range Clients {
		ids = append(ids, c.ID)
	}
	return ids
}

// ConfigPath is the file this client's entry is written to on this OS.
func (c Client) ConfigPath() (string, error) { return c.path() }

// Presence is how much evidence there is that a client is on this machine.
type Presence int

const (
	// Absent: nothing on disk suggests this client.
	Absent Presence = iota
	// Likely: the directory a client creates on first run is there, but nothing
	// else is. That directory outlives an uninstall, so on its own it is not
	// enough to write a credential into a file that does not exist yet.
	Likely
	// Present: the client's own MCP config is there, or its executable is.
	Present
)

// Detect reports whether this client looks installed at all, which is what
// `l4 mcp status` shows in its Installed column. Writing to a client is gated on
// Presence rather than on this, because the two questions are different: a
// leftover ~/Library/Application Support/Code/User is enough to report VS Code
// as maybe-there and not enough to justify creating an mcp.json with a bearer
// token in it for an editor that was uninstalled a year ago.
func (c Client) Detect() bool { return c.Presence() >= Likely }

// Presence weighs the evidence. An executable on PATH or an existing MCP config
// is proof; a directory the client once created is a hint.
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

// appIsInstalled looks for a macOS application bundle. There is no equivalent
// check on the other platforms, where the executable on PATH is the signal.
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

// applicationDirs is where a macOS app can be installed. It is a seam because
// otherwise these tests would pass or fail depending on what the machine running
// them happens to have in /Applications.
var applicationDirs = func() []string {
	dirs := []string{"/Applications"}
	if home, err := userHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

// Detected returns the clients a bare `l4 mcp install` should configure. Only
// proof counts: this writes credentials, and doing that for a client the user
// does not have is a leak rather than a convenience.
func Detected() []Client {
	var found []Client
	for _, c := range Clients {
		if c.Presence() == Present {
			found = append(found, c)
		}
	}
	return found
}

// Suggested returns the clients there is a hint of but no proof of, so the
// command can name them instead of silently writing to them or silently
// ignoring them.
func Suggested() []Client {
	var found []Client
	for _, c := range Clients {
		if c.Presence() == Likely {
			found = append(found, c)
		}
	}
	return found
}

// Entry builds the config block for this client.
func (c Client) Entry(o Options) map[string]any { return c.entry(c, o) }

// PatchRoot applies whatever this client needs beyond its own entry. Most
// clients need nothing, so the nil check lives here rather than in five places.
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

// bearer is the Authorization value: either the credential, or this client's own
// way of naming where to find it.
func bearer(c Client, o Options) string {
	if o.KeySource == KeyFromEnv && c.keyRef != "" {
		return "Bearer " + c.keyRef
	}
	return "Bearer " + o.APIKey
}

func authHeader(c Client, o Options) map[string]any {
	return map[string]any{"Authorization": bearer(c, o)}
}

// authHeaderLine is the same header as one "Name: value" string, which is the
// shape `claude mcp add --header` takes.
func authHeaderLine(c Client, o Options) string {
	return "Authorization: " + bearer(c, o)
}

// WritesCredential reports whether installing this client puts the credential
// itself on disk, so the command can say so rather than leaving the user to
// infer it from a transport note.
func (c Client) WritesCredential(o Options) bool {
	if c.entry == nil || c.keyRef == "" {
		return false
	}
	return o.KeySource != KeyFromEnv
}

// vscodeInputs declares the prompt VS Code shows the first time the server is
// used. password:true keeps the value out of the file and out of settings sync,
// which is why VS Code is the one client that needs no environment variable.
// The array is merged on id: re-running an install must not stack duplicates.
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

// appDataPath resolves the per-user application data directory each desktop
// client stores its config in.
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
