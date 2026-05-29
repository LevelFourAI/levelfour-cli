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
