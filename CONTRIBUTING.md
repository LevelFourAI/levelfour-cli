# Contributing

Thank you for your interest in contributing to the LevelFour CLI.

## Development setup

```bash
git clone https://github.com/LevelFourAI/levelfour-cli.git
cd levelfour-cli
go mod download
go build ./...
```

Run the CLI against staging:

```bash
go run ./cmd/levelfour --api https://api.staging.levelfour.ai whoami
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

Pre-commit hooks (gofmt, gosec, gitleaks, actionlint, zizmor) run automatically on commit. Install once with:

```bash
pre-commit install
```

## Commit messages

We use [Conventional Commits](https://www.conventionalcommits.org). Prefix every commit with `feat:`, `fix:`, `refactor:`, `chore:`, `docs:`, `test:`, `ci:`, or `perf:`. The changelog is generated from these.

Examples:

```
feat(cli): add l4 costs summary command
fix(api): retry on 408 timeouts
docs(readme): document --jq output format
```

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
- `internal/sentryx/`: opt-in crash-telemetry wrapper.
- `internal/config/`, `internal/keyring/`: persistent settings and credential storage.

Authentication and most API surface goes through the [LevelFour Go SDK](https://github.com/LevelFourAI/levelfour-go). New endpoints should land in the SDK first, then the CLI consumes them.

## Releases

Releases are tag-driven. Push a `vX.Y.Z` tag on `main` and goreleaser publishes archives + the Homebrew formula automatically.

## Code of conduct

Be respectful. Assume good intent. Disagreement is fine; rudeness is not.
