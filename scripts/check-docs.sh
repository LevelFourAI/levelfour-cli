#!/bin/sh
# Keeps internal working documents out of the repository.
#
# Handover notes, session scratch and checkpoint files are produced while work happens
# and are meaningless once it lands. They belong on the machine that produced them.
# Plans belong in plan mode, which stores them outside any repository.
#
# Two rules:
#   1. A markdown file at the repository root must appear in ALLOWED_ROOT_DOCS.
#   2. No markdown file anywhere may carry a working-document name.
# ALLOWED_DOC_PATHS exempts specific full paths from both rules.
#
# Usage:
#   scripts/check-docs.sh            audit every tracked file (CI)
#   scripts/check-docs.sh --staged   audit files staged for commit (pre-commit hook)
#
# POSIX sh on purpose: this runs under macOS bash 3.2, Alpine, and ubuntu-latest alike.
set -eu

# Paths are compared against the repository root, so behave identically no matter where
# the caller invoked us from. Without this, `git ls-files` returns paths relative to the
# current directory and every nested file looks like a root file.
cd "$(git rev-parse --show-toplevel)"

# Permanent documents allowed at the repository root. Compared case-insensitively.
ALLOWED_ROOT_DOCS=" agents.md architecture.md authors.md changelog.md claude.md code_of_conduct.md contributing.md design.md features.md governance.md license.md maintainers.md migration.md notice.md readme.md reference.md roadmap.md security.md support.md tech_debt.md troubleshooting.md upgrading.md api.md "

# Repo-specific exemptions, as full paths from the repository root. Use this for a real
# document that one of the rules would otherwise reject, including generated files whose
# name the generator owns. One path per line.
ALLOWED_DOC_PATHS="
"

# Matched case-insensitively against the basename, on a word boundary.
#
# Deliberately narrow. Words that carry product meaning in this codebase are NOT here:
# "plan" and "plans" (Savings Plans, terraform plan), "remediation" (the product),
# "session", "notes", "summary", "implementation" and "walkthrough" (ordinary docs
# vocabulary). A root file using any of those is still caught by the allowlist above,
# which is the rule that does the real work.
WORKING_DOC_RE='(^|[-_ ])(handover|handoff|scratch|todo|wip|writeup|checkpoint)([-_ .]|$)'

report=$(mktemp)
trap 'rm -f "$report"' EXIT

# core.quotePath=false so non-ASCII paths arrive verbatim rather than C-quoted, which
# would otherwise slip past the extension test entirely.
if [ "${1:-}" = "--staged" ]; then
  git -c core.quotePath=false diff --cached --name-only --diff-filter=AMR
else
  git -c core.quotePath=false ls-files
fi | while IFS= read -r f; do
  # Extension test, case-insensitively: .md and .mdx only.
  case "$(printf '%s' "$f" | tr '[:upper:]' '[:lower:]')" in
    *.md|*.mdx) ;;
    *) continue ;;
  esac

  # Agent assets (skills, commands, prompts) are committed on purpose.
  case "$f" in
    .claude/*|*/.claude/*) continue ;;
  esac

  # Explicit per-repo exemption wins over both rules.
  if printf '%s\n' "$ALLOWED_DOC_PATHS" | grep -qxF "$f"; then
    continue
  fi

  case "$f" in
    */*) ;;
    *)
      lower=$(printf '%s' "$f" | tr '[:upper:]' '[:lower:]')
      case "$ALLOWED_ROOT_DOCS" in
        *" $lower "*) ;;
        *)
          printf '%s|only allowlisted markdown may sit at the repository root\n' "$f" >>"$report"
          continue
          ;;
      esac
      ;;
  esac

  base=${f##*/}
  if printf '%s' "$base" | grep -qiE "$WORKING_DOC_RE"; then
    printf '%s|reads as an internal working document\n' "$f" >>"$report"
  fi
done

if [ -s "$report" ]; then
  {
    echo "Blocked: internal working documents must not be committed."
    echo
    while IFS='|' read -r path reason; do
      printf '  %-58s %s\n' "$path" "$reason"
    done <"$report"
    echo
    echo "Keep this material off the repository:"
    echo "  plans    write them in plan mode, which stores them outside any repo"
    echo "  notes    keep them in a scratch directory on your own machine"
    echo
    echo "If one of these really is a permanent repository document, exempt it in"
    echo "scripts/check-docs.sh in the same pull request, so the exception is reviewed:"
    echo "  at the repository root  add its name to ALLOWED_ROOT_DOCS"
    echo "  anywhere else           add its full path to ALLOWED_DOC_PATHS"
  } >&2
  exit 1
fi
