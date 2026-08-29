# LevelFour CLI

[![CI](https://github.com/LevelFourAI/levelfour-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/LevelFourAI/levelfour-cli/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)

The official command-line tool for [LevelFour](https://levelfour.ai). Surfaces cloud cost recommendations, integrations, and live spending from the terminal, plus terminal-only features like local Terraform cost estimation and an interactive TUI for browsing recommendations.

> **Status: v0.x.** The CLI surface is still stabilizing. Minor versions may include breaking changes until v1.0. Pin to an exact version in CI and review the [release notes](https://github.com/LevelFourAI/levelfour-cli/releases) before upgrading.

## Installation

### Homebrew (macOS, Linux)

```bash
brew install LevelFourAI/tap/levelfour
```

### Go install

```bash
go install github.com/LevelFourAI/levelfour-cli/cmd/levelfour@latest
```

### Direct download

Binaries for Linux, macOS, and Windows are attached to every [release](https://github.com/LevelFourAI/levelfour-cli/releases).

## Quick start

```bash
l4 auth login                                  # browser-based authentication
l4 whoami                                      # confirm identity
l4 costs summary                               # KPI overview
l4 recommendations list --status available     # pending savings opportunities
l4 estimate ./infra/                           # estimate Terraform costs locally
```

The package installs two interchangeable binaries: `levelfour` (long form) and `l4` (short form, recommended for everyday use).

## Connect your coding agent (MCP)

Give Claude Code, Claude Desktop, Cursor, VS Code or Windsurf access to your cloud spend and savings recommendations:

```bash
l4 mcp install
```

It logs you in if you are not already, detects the agent clients on this machine, and writes an entry into each one. Then restart the client and ask it:

> what are we spending this month

Narrow it, or install one entry per organization (an API key belongs to exactly one):

```bash
l4 mcp install --client cursor
l4 mcp install --client cursor --name levelfour-rw     # a second entry, e.g. a read-write key
l4 mcp status                                          # what is wired up, and what is not
```

Existing config files are parsed and merged rather than replaced, only the entry under `--name` is touched, and a dated `.l4-backup-<timestamp>` copy is taken first.

| Client | Written to | Transport |
|---|---|---|
| Claude Code | `claude mcp add --scope user` | Remote HTTP |
| Claude Desktop | `claude_desktop_config.json` | Local stdio (`l4 mcp serve`) |
| Cursor | `~/.cursor/mcp.json` | Remote HTTP |
| VS Code | user-profile `mcp.json` | Remote HTTP |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` | Remote HTTP |

Remote clients talk to `https://mcp.levelfour.ai/mcp` and carry your API key in an `Authorization` header. By default the key is written into the config file. Files this command writes itself are created `0600` on macOS and Linux; the Claude Code entry is written by `claude mcp add`, which owns that file and its permissions. Pass `--key-source env` to write a reference instead, and the key never lands in the file:

```bash
l4 mcp install --key-source env    # then export LEVELFOUR_TOKEN where the client starts
```

VS Code needs no environment variable either way: with `--key-source env` it is given an `inputs` prompt and stores the key in its own secret storage. Claude Desktop's config only starts stdio servers, so it runs `l4 mcp serve` instead and never receives a key at all.

`l4 mcp serve` runs the read-only tools locally over stdin and stdout, reading your data through the LevelFour API with the stored credential. The hosted catalog depends on your key: a `read` key is shown 16 tools, a `read-write` key is shown those plus 2 that record an accept or reject decision. `l4 mcp serve` carries the same 16 under the same names, so an agent that learned to route against the hosted server gets the same answers here. To accept or reject from the terminal, use `l4 rec accept` and `l4 rec reject`. At startup it prints its version and tool count on stderr, which is where your client keeps its log.

## Authentication

`l4` resolves credentials in a fixed order:

1. `--token` / `-t` flag
2. `LEVELFOUR_TOKEN` environment variable
3. OS keychain (populated by `l4 auth login`)

For CI:

```yaml
- env:
    LEVELFOUR_TOKEN: ${{ secrets.LEVELFOUR_TOKEN }}
  run: l4 recommendations list --status available --jq '.data.items[].recommendation_id'
```

## Output formats

Every command supports machine-readable output:

```bash
l4 costs summary --json                                       # raw JSON
l4 recommendations list --jq '.data.items[].monthly_savings'  # filter with jq
l4 costs breakdown --format csv                               # CSV for spreadsheets
```

See [output formats](https://docs.levelfour.ai/cli/output-formats) for the full matrix.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error |
| `2` | Issues found (`l4 estimate --fail-above` triggered, recommendations exceed threshold) |
| `4` | Authentication required (no token, expired, or invalid) |
| `130` | Interrupted (Ctrl+C) |

Stable; script against them.

## Crash telemetry

Crash telemetry is **opt-in** and **off by default**. Enable with:

```bash
l4 telemetry enable
```

What it sends: panic stack traces and the failing command name. Home paths are rewritten to `~`, AWS access keys and known token env vars are redacted, and HTTP headers/cookies are stripped before transport. See `l4 telemetry --help`.

## Documentation

- [docs.levelfour.ai/cli](https://docs.levelfour.ai/cli): full command reference and recipes
- [docs.levelfour.ai/sdks/go](https://docs.levelfour.ai/sdks/go): the Go SDK that powers the CLI
- [api.md](https://github.com/LevelFourAI/levelfour-go/blob/main/api.md): underlying API methods

## Reporting issues

File CLI bugs at [github.com/LevelFourAI/levelfour-cli/issues](https://github.com/LevelFourAI/levelfour-cli/issues). Include `l4 --version` output and the failing command.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md) for the responsible-disclosure policy.

## License

Apache-2.0; see [LICENSE](LICENSE).
