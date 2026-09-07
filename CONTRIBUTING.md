# Contributing

Thank you for your interest in contributing to the LevelFour CLI.

## Development setup

```bash
git clone https://github.com/LevelFourAI/levelfour-cli.git
cd levelfour-cli
go mod download
go build ./...
```

Run the CLI from source:

```bash
go run ./cmd/levelfour whoami
```

## Tests

```bash
go test -race ./...
go test -race -coverprofile=coverage.out ./internal/...
go tool cover -func=coverage.out | tail -1
```

CI enforces 100% statement coverage on `./internal/...`. Add tests for every new branch.

## Linting

```bash
golangci-lint run --timeout=5m
```

Pre-commit hooks (golangci-lint, gofmt, gosec, actionlint, zizmor, and the test suite) run automatically on commit. Install once with:

```bash
pre-commit install
```

The hooks run these tools from your PATH rather than installing them, so you need `golangci-lint`, `gosec`, `actionlint`, `zizmor` and `bc` available. Note that the test hook runs the full race-enabled suite and the coverage gate, so a commit takes minutes rather than seconds.

CI also fails on any `nolint` pragma. Fix the finding instead of suppressing it.

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org). The changelog is generated from these, so the prefix decides where a change lands.

Types with a changelog section, as configured in `release-please-config.json`: `feat`, `fix`, `perf`, `refactor`, `docs`, `chore`, `build`, `ci`. Other valid types such as `test` and `style` are accepted but produce no changelog entry.

```
feat(cli): add l4 costs summary command
fix(api): retry on 408 timeouts
docs(readme): document --jq output format
```

Signal a breaking change with `!` after the type, or a `BREAKING CHANGE:` footer. Pull requests are squash-merged, so the pull request title becomes the commit subject: put the `!` in the title.

```
feat(cli)!: rename --format to --output
```

Do not use a bare `Docs:`, `Fix:` or `Feat:` trailer in a commit body. Release Please reads a trailer whose key matches a commit type as another commit and emits a changelog entry for it, with any `#123` resolved against this repository. Use `Refs:` or `Docs-PR:` instead.

## Pull requests

- Open against `main`. CI must pass before merge.
- Keep PRs focused. One conceptual change per PR.
- Update [CHANGELOG.md](CHANGELOG.md) and `README.md` when public behavior changes.

## Architecture

- `cmd/levelfour/main.go`: entry point; both `levelfour` and `l4` binaries build from here.
- `internal/cli/`: Cobra command definitions (one file per command group).
- `internal/api/`: HTTP escape hatches for endpoints not yet in the public Go SDK. See `internal/api/types.go` for the transitional note.
- `internal/output/`: shared rendering primitives (table, JSON, CSV, markdown).
- `internal/terraform/`: HCL parser for `l4 estimate` and `l4 diff`.
- `internal/mcp/`: the MCP server surface behind `l4 mcp serve`.
- `internal/mcpinstall/`: writes and removes MCP entries in agent client configs.
- `internal/sentryx/`: opt-in crash-telemetry wrapper.
- `internal/version/`: update check against the latest published release.
- `internal/browser/`, `internal/context/`, `internal/cli/tuicommon/`: small shared helpers.
- `internal/config/`, `internal/keyring/`: persistent settings and credential storage.

Authentication and most API surface goes through the [LevelFour Go SDK](https://github.com/LevelFourAI/levelfour-go). New endpoints should land in the SDK first, then the CLI consumes them.

## Releases

Releases are automated with [Release Please](https://github.com/googleapis/release-please).

Conventional commits merged to `main` accumulate into a release pull request titled `chore(main): release X.Y.Z`, which updates [CHANGELOG.md](CHANGELOG.md) and `.release-please-manifest.json`. Merging that pull request tags the commit, and goreleaser then publishes the archives and updates the Homebrew cask.

Do not push version tags by hand. A hand-pushed tag bypasses release-please's bookkeeping and strands the release it had already prepared.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). Report unacceptable behaviour to conduct@levelfour.ai.
