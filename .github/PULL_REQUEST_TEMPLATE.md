## Summary
<!-- What does this PR do? -->

## Checklist
- [ ] Tests pass (`go test -race ./...`) at 100% total coverage on `./internal/...`
- [ ] Lint passes (`golangci-lint run`)
- [ ] [README.md](../README.md) updated if commands, flags, or install path changed
- [ ] New or renamed commands and flags follow [Adding a command](../CONTRIBUTING.md#adding-a-command)
- [ ] Summary names the commands that changed, so a maintainer can update the hosted reference (docs.levelfour.ai/cli)
- [ ] No `nolint` pragmas (CI fails on them)
- [ ] Title is a conventional commit; it becomes the squashed commit subject and drives the changelog
