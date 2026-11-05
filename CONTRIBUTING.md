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
- Update `README.md` when public behavior changes. Release Please writes [CHANGELOG.md](CHANGELOG.md), so leave that file alone.

## Architecture

- `cmd/levelfour/main.go`: entry point; both `levelfour` and `l4` binaries build from here.
- `internal/cli/`: Cobra command definitions (one file per command group).
- `internal/api/`: HTTP escape hatches for endpoints not yet in the public Go SDK. See `internal/api/types.go` for the transitional note.
- `internal/output/`: shared rendering primitives (table, JSON, CSV, markdown).
- `internal/terraform/`: HCL parser for `l4 estimate` and `l4 diff`.
- `internal/cloudformation/`: finds the CloudFormation templates `l4 estimate` and `l4 diff` upload, and the files they reference. It never evaluates a template.
- `internal/mcp/`: the stdio bridge behind `l4 mcp serve`, which relays the hosted MCP server.
- `internal/mcpinstall/`: writes and removes MCP entries in agent client configs.
- `internal/sentryx/`: opt-in crash-telemetry wrapper.
- `internal/version/`: update check against the latest published release.
- `internal/browser/`, `internal/context/`, `internal/cli/tuicommon/`: small shared helpers.
- `internal/config/`, `internal/keyring/`: persistent settings and credential storage.

Authentication and most API surface goes through the [LevelFour Go SDK](https://github.com/LevelFourAI/levelfour-go). New endpoints should land in the SDK first, then the CLI consumes them.

## Adding a command

Commands read the same way across the CLI, so anyone who knows `l4 recommendations list` can guess the next one. These rules apply whether you write the code or a coding agent writes it for you. Read a neighbouring command before you start: `internal/cli/recommendations.go` for a list and a `view`, `internal/cli/alerts_write.go` for writes.

If a command needs a verb or a flag this section does not list, open an issue before you write it.

### Where it goes

Take the first row that fits.

| What the command is about | Where it goes | Example |
|---|---|---|
| Something any provider can have: costs, savings, commitments, tags, alerts | The group that owns it, with `--provider` to narrow it | `l4 commitments list --provider gcp` |
| Something only one provider has | Under `l4 providers <id>`, where `<id>` is what `l4 providers list` prints | `l4 providers datadog hosts list` |
| A new concept across providers | A new top-level group. Open an issue first | |

`l4 --help` lists every top-level command, so never add `l4 <provider>`. A new provider adds one line to `l4 providers --help` and none to `l4 --help`.

### Shape

A command reads noun, then verb: `l4 <group> <verb>`, or `l4 providers <id> <noun> <verb>` for one provider's items.

| Verb | Use it for | Example |
|---|---|---|
| `list` | Many items | `l4 recommendations list` |
| `view <id>` | One item in full | `l4 commitments view <id>` |
| A noun that names the answer | A report that is neither a list nor one item | `l4 costs summary`, `l4 commitments coverage` |
| `create`, `update <id>`, `delete <id>` | Writes | `l4 alerts create` |
| A verb for the change of state | A write that moves one item on | `l4 alerts pause <id>`, `l4 rec accept <id>` |

- A group only holds subcommands. It has no `RunE`, so `l4 alerts` prints its help.
- Write positional arguments into `Use`, `<id>` when required and `[product]` when optional, and enforce them with `Args`: `cobra.ExactArgs(1)`, `cobra.MaximumNArgs(1)`, `cobra.NoArgs`.
- Add an alias for a shorter name (`rec`) or to keep a renamed command working (`integrations` for `providers`).

### Flags

| Need | Flag | How |
|---|---|---|
| Pages | `--page`, `--page-size` | Defaults 1 and 20. The help text names the cap |
| Order | `--sort-by`, `--sort-order` | The help text names each accepted value |
| Search | `--search` | |
| Filter | One named flag per filter, such as `--service` or `--cloud` | Repeatable: `StringArray`, or `BoolSlice` and `IntSlice` when the API takes a bool or a number. The flag is kebab-case and the query parameter snake_case |
| One provider | `--provider` | |
| Dates | `--start`, `--end` | ISO 8601 |
| A spreadsheet | `--format csv` | |
| An input file | `-f`, `--file` | |
| Skip a confirmation | `-y`, `--yes` | Writes only |

`--json`, `--jq`, `--template`, `--web` and `--quiet` are global, so do not declare them again.

| Do not write | Write |
|---|---|
| `l4 datadog hosts list` | `l4 providers datadog hosts list` |
| `l4 providers datadog list hosts` | `l4 providers datadog hosts list` |
| `--list` | A `list` subcommand |
| `--sort`, `--order` | `--sort-by`, `--sort-order` |
| `--filter cloud=aws` | `--cloud aws` |
| `--product infra_host` for the one item a command reads | A positional `[product]` |

Older names stay so existing scripts keep working, and new commands do not copy them: `l4 alerts get`, `l4 tags show`, and `--limit` with `--offset` on `l4 alerts events`.

### Code

A list, start to finish, with `--web` first, then `--json`, then the output for a person:

```go
var widgetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List widgets, the largest first",
	Args:  cobra.NoArgs,
	Example: `  l4 widgets list
  l4 widgets list --service RDS --sort-by cost --sort-order desc
  l4 widgets list --page 2 --page-size 50`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		client, err := newSDKClientFn()
		if err != nil {
			return err
		}
		if flagWeb {
			return openWeb("/widgets")
		}
		resp, err := client.SDK().Widgets.List(context.Background(), widgetsRequest(cmd))
		if err != nil {
			return err
		}
		if output.HasFormattingFlags() {
			return output.PrintResult(resp)
		}
		output.Table(widgetHeaders, widgetRows(resp))
		return nil
	},
}
```

- **Files.** A group lives in `internal/cli/<group>.go` and splits by verb as it grows: `commitments_list.go`, `commitments_plan.go`. Name commands `<group>Cmd` and `<group><Verb>Cmd`, and add them in `init()`. A new top-level group also needs its `GroupID` and `rootCmd.AddCommand` in `root.go`. A provider's group goes on `providersCmd` in `providers.go`.
- **API.** Call the SDK through `client.SDK()`. If the route is missing, it lands in [levelfour-go](https://github.com/LevelFourAI/levelfour-go) first, and the CLI moves to that release. Use `client.Raw()` only for what the SDK cannot decode, such as a CSV body. Some older commands call routes by hand: do not copy that for a route the SDK has.
- **Writes.** Send them through `postWrite`, `putWrite`, `patchWrite` or `deleteWrite`. The first three carry the idempotency key the API deduplicates on. A write nobody can undo asks first through `requireApproval`.
- **Output.** A person gets a table or a key-value view, and `--json` opts into the full response. Use `output.Table`, `output.KeyValue`, `output.Header` and `output.PaginationFooter`. An empty result prints one line through `output.Info` and exits 0.
- **Text from the API.** A name from a customer's account can hold control characters that move the cursor or plant a link. Pass it through `output.Sanitize` before `output.Table`, `output.KeyValue` or `output.Header`, which print what they receive because some cells carry a styled status badge. `output.Info` and `output.Warning` sanitize on their own, and `--json` escapes.
- **Help.** `Short` says what the command returns, with no trailing period. Every command that reads data has an `Example` with real flags.
- **Errors.** Return them from `RunE`. A message says what went wrong and names the command that helps: `"%q is not a connected provider: 'l4 providers list' shows them"`.
- **Many similar lists.** Lists that differ only in columns and filters can share one table. `internal/cli/datadog_list.go` does this, and a test checks each column against the fields the SDK returns.

`l4 mcp serve` relays the hosted MCP server, so a new MCP tool needs no change here.

### Tests

Drive a command through `executeCommand` against an `httptest` server: `internal/cli/providers_test.go` shows the setup. Cover the output for a person, `--json`, `--web`, an API error and a missing credential. Reset any new package-level flag variable in `resetFlags` in `internal/cli/root_test.go`, because Cobra keeps flag values between runs in one test process.

### Before you open the pull request

- Add the command to "Command reference" in `README.md`.
- Name new or changed commands in the summary. The hosted reference at [docs.levelfour.ai/cli](https://docs.levelfour.ai/cli) lives in a private repository, so a maintainer updates it.
- Keep customer names, account IDs and internal design out of code, test data, commit messages and pull request bodies. This repository is public.

## Releases

Releases are automated with [Release Please](https://github.com/googleapis/release-please).

Conventional commits merged to `main` open a release pull request titled `chore(main): release X.Y.Z`, which updates [CHANGELOG.md](CHANGELOG.md) and `.release-please-manifest.json`. It merges itself once the required checks pass, so a `feat` or `fix` on `main` ships without anyone cutting a release. That merge tags the commit, and goreleaser then publishes the archives and updates the Homebrew cask.

Do not push version tags by hand. A hand-pushed tag bypasses release-please's bookkeeping and strands the release it had already prepared.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). Report unacceptable behaviour to conduct@levelfour.ai.
