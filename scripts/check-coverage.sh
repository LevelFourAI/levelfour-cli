#!/usr/bin/env bash
set -euo pipefail

export GOTOOLCHAIN=auto

# Unset git env vars so tests that create temp repos don't inherit the
# hook runner's git context (breaks in worktrees where GIT_DIR points to
# .git/worktrees/<name> instead of .git/).
unset GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE

go test -race -coverprofile=coverage.out ./internal/...

TOTAL=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | tr -d '%')
echo "Coverage: ${TOTAL}%"
rm coverage.out

if (( $(echo "$TOTAL < 100.0" | bc -l) )); then
  echo "FAIL: Coverage ${TOTAL}% is below 100% threshold"
  exit 1
fi
