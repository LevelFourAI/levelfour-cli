# Instructions for coding agents

Follow [CONTRIBUTING.md](CONTRIBUTING.md). Its rules apply to you the same way they apply to a person, and CI enforces most of them.

Before you add or rename a command, a subcommand or a flag, read "Adding a command" in that file, then read a neighbouring command in `internal/cli/`. Reuse its verbs and flags. If none fits, stop and raise it in an issue, or with whoever asked you for the change.

Before you commit, run:

```bash
go test -race -coverprofile=coverage.out ./internal/...
go tool cover -func=coverage.out | tail -1
golangci-lint run --timeout=5m
```

The coverage line must read 100.0%.

- Fix lint findings. CI fails on any `nolint` pragma.
- Keep the pre-commit hooks on.
- Leave `CHANGELOG.md` and version tags to Release Please.
- Write pull request titles as Conventional Commits, because a squash merge turns the title into the commit subject.
- This repository is public. Keep customer names, account IDs and internal design out of code, test data, commit messages and pull request bodies.
