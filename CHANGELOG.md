# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
