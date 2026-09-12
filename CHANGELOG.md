# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.4](https://github.com/LevelFourAI/levelfour-cli/compare/v0.1.3...v0.1.4) (2026-09-12)


### Build

* **deps:** bump the go-modules group with 4 updates ([#32](https://github.com/LevelFourAI/levelfour-cli/issues/32)) ([c23019a](https://github.com/LevelFourAI/levelfour-cli/commit/c23019aee98698e50a3fd410cd17e1f066f7766d))

## [0.1.3](https://github.com/LevelFourAI/levelfour-cli/compare/v0.1.2...v0.1.3) (2026-09-08)


### Features

* **recommendations:** print the status a person reads ([#23](https://github.com/LevelFourAI/levelfour-cli/issues/23)) ([9566390](https://github.com/LevelFourAI/levelfour-cli/commit/956639082b0e9c70c5b0ec2490be0f68ef2a9447))


### Bug Fixes

* **web:** open the view that exists behind --web ([#24](https://github.com/LevelFourAI/levelfour-cli/issues/24)) ([9e1bc1d](https://github.com/LevelFourAI/levelfour-cli/commit/9e1bc1d1933a0e73aefd1285ecc562537754a8a0))


### Documentation

* **readme:** add a command and global flag reference ([#29](https://github.com/LevelFourAI/levelfour-cli/issues/29)) ([dfd278e](https://github.com/LevelFourAI/levelfour-cli/commit/dfd278e413c53556991985c710507d757ec3f78d))


### Build

* **deps:** bump the github-actions group across 1 directory with 7 updates ([#15](https://github.com/LevelFourAI/levelfour-cli/issues/15)) ([b508a44](https://github.com/LevelFourAI/levelfour-cli/commit/b508a44705e10dd3d2e300c01a297dbb730dc94a))
* **deps:** bump the go-modules group across 1 directory with 6 updates ([#21](https://github.com/LevelFourAI/levelfour-cli/issues/21)) ([7aafa8a](https://github.com/LevelFourAI/levelfour-cli/commit/7aafa8a24e6cdc6926c924c87d6df12c73c7dcc3))

## [0.1.2](https://github.com/LevelFourAI/levelfour-cli/compare/v0.1.1...v0.1.2) (2026-09-07)


### Features

* **cli:** accept, reject and execute recommendations from the command line ([59ef679](https://github.com/LevelFourAI/levelfour-cli/commit/59ef679bc4f4fb9ec9173da80309cede13f38e56))
* **cli:** connect coding agents to LevelFour over MCP ([#17](https://github.com/LevelFourAI/levelfour-cli/issues/17)) ([471bfbb](https://github.com/LevelFourAI/levelfour-cli/commit/471bfbb6640eb3ed60b6d5a6950e84a0d89bf40f))


### Bug Fixes

* **cli:** derive flag help from the enum lists, and test what the tests claimed ([3e9eb35](https://github.com/LevelFourAI/levelfour-cli/commit/3e9eb3553e84657990530a0b5dbb579c7135a510))
* **cli:** honour --fail-above when output is machine readable ([#26](https://github.com/LevelFourAI/levelfour-cli/issues/26)) ([91fd28d](https://github.com/LevelFourAI/levelfour-cli/commit/91fd28d7334e2abcbf9f0fbb70d37fc883e7ef19))
* **cli:** refuse an --endpoint that cannot reach every client ([#25](https://github.com/LevelFourAI/levelfour-cli/issues/25)) ([5f5b9fa](https://github.com/LevelFourAI/levelfour-cli/commit/5f5b9fa103cb61b81078b1bbd807af25af9c3202))


### Documentation

* **cli:** use a synthetic recommendation id in examples and fixtures ([5ba43a3](https://github.com/LevelFourAI/levelfour-cli/commit/5ba43a3a624b1b01101694380991f3453a5d0d72))
* correct the public-facing docs and close the fixture leak path ([#27](https://github.com/LevelFourAI/levelfour-cli/issues/27)) ([0957baf](https://github.com/LevelFourAI/levelfour-cli/commit/0957baf575df740281e8b96c461a5980ddf1f3b6))


### Continuous Integration

* raise go and deps past advisories, pin action tags exactly ([4cf4821](https://github.com/LevelFourAI/levelfour-cli/commit/4cf482114b1d6b8a9bccc510e2f1726ffc01740e))

## [0.1.1](https://github.com/LevelFourAI/levelfour-cli/compare/v0.1.0...v0.1.1) (2026-05-30)


### Features

* **ci:** add Release Please for zero-touch versioned releases ([#3](https://github.com/LevelFourAI/levelfour-cli/issues/3)) ([deda00d](https://github.com/LevelFourAI/levelfour-cli/commit/deda00dc56e71ccf7485e93667ce47f071dd4471))


### Build

* **deps:** bump googleapis/release-please-action ([#6](https://github.com/LevelFourAI/levelfour-cli/issues/6)) ([249f07f](https://github.com/LevelFourAI/levelfour-cli/commit/249f07fdea2e468438b1369c25009996daabb2d7))
* **deps:** bump the github-actions group with 5 updates ([#1](https://github.com/LevelFourAI/levelfour-cli/issues/1)) ([7ebe3ea](https://github.com/LevelFourAI/levelfour-cli/commit/7ebe3ea915569417fc4fb8de1d9bbc353db57059))
* **deps:** bump the go-modules group with 9 updates ([#2](https://github.com/LevelFourAI/levelfour-cli/issues/2)) ([bce1b4a](https://github.com/LevelFourAI/levelfour-cli/commit/bce1b4a1e4c2f2e3bf9890bc7f1080466634012d))

## [0.1.0](https://github.com/LevelFourAI/levelfour-cli/releases/tag/v0.1.0) (2026-05-27)

Initial public release of the LevelFour CLI.

### Commands

* `l4 costs`: summary, breakdown, daily, monthly aggregates, and filter discovery.
* `l4 recommendations`: paginated list and full-detail view of cost-optimization recommendations, with a 3-tab interactive TUI.
* `l4 integrations`: list connected cloud providers.
* `l4 status`: API health and account evaluation status.
* `l4 whoami`: identity and organization context.
* `l4 estimate`: parse Terraform files locally and estimate monthly cloud costs before resources are created.
* `l4 diff`: cost delta between current Terraform and a git baseline (or saved snapshot).
* `l4 export`: structured exports (CSV, JSON) of costs and recommendations.
* `l4 api`: authenticated raw API access for scripting.
* `l4 auth login | logout | status`: browser-based device-code authentication; OS keychain storage.
* `l4 config get | set | list`: persistent settings (default provider, output format, etc.).
* `l4 completion bash | zsh | fish | powershell`: shell completion.
* `l4 telemetry enable | disable | status`: opt-in Sentry crash telemetry with stack-trace scrubbing.

### Features

* Two interchangeable binaries shipped per release: `levelfour` (long form) and `l4` (short).
* Output formats: table, JSON (`--json`), jq filter (`--jq`), Go template (`--template`), CSV (`--csv`).
* Stable exit codes for scripting (`0` success, `2` issues found, `4` auth required, `130` interrupted).
* Powered by the official [LevelFour Go SDK](https://github.com/LevelFourAI/levelfour-go) `v0.1.0`.
* Authentication via the `LEVELFOUR_TOKEN` env var, `--token` flag, or OS keychain.
* Opt-in crash telemetry with PII scrubbing (home paths, AWS keys, token env vars, HTTP headers).
* CI guardrail patterns: `l4 estimate --fail-above` and `l4 diff --fail-above` for cost-threshold gates.
