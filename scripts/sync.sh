#!/usr/bin/env bash
# sync.sh — deploy skill files from this repo to both agents' expected locations.
#
# Modes:
#   (default)   copy + verify with diff
#   --check     diff only; exit 0 if all match, 1 if any differ
#   --dry-run   print what would happen; do nothing
#
# Exit codes:
#   0  success (or --check with all matching)
#   1  source missing, --check with mismatches, or other I/O failure
#   2  post-copy diff failed (unexpected)
#   3  target exists and is a directory (refused)
#   64 usage error

set -euo pipefail

SCRIPT_DIR="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &>/dev/null && pwd )"
REPO_ROOT="$( cd -- "$SCRIPT_DIR/.." &>/dev/null && pwd )"

mode="sync"
case "${1:-}" in
  -h|--help)
    cat <<'EOF'
Usage: sync.sh [--check | --dry-run | --help]

Deploys skill files from this repo to:
  skill/SKILL.md         → ~/.cursor/skills/worklog/SKILL.md
  skill/SKILL.md         → ~/.claude/skills/worklog/SKILL.md
  skill/claude/command.md → ~/.claude/commands/worklog.md

Modes:
  (default)   copy + verify with diff
  --check     diff only; exit 0 if all match, 1 if any differ
  --dry-run   print what would happen; do nothing
EOF
    exit 0
    ;;
  --check)   mode="check" ;;
  --dry-run) mode="dryrun" ;;
  "")        ;;
  *) echo "unknown arg: $1 (try --help)" >&2; exit 64 ;;
esac

# Deployment table.
# TODO(phase-1.x): add Cursor .mdc once skill/cursor/rule.mdc exists:
#   "$REPO_ROOT/skill/cursor/rule.mdc" → "$HOME/.cursor/rules/worklog.mdc"
# TODO(phase-1.x): add CLAUDE.md snippet once skill/claude/claudemd-snippet.md exists.
declare -a srcs=(
  "$REPO_ROOT/skill/SKILL.md"
  "$REPO_ROOT/skill/SKILL.md"
  "$REPO_ROOT/skill/claude/command.md"
)
declare -a dsts=(
  "$HOME/.cursor/skills/worklog/SKILL.md"
  "$HOME/.claude/skills/worklog/SKILL.md"
  "$HOME/.claude/commands/worklog.md"
)

# Verify all sources exist before doing anything.
for src in "${srcs[@]}"; do
  if [[ ! -f "$src" ]]; then
    echo "ERROR: source missing: $src" >&2
    exit 1
  fi
done

mismatch=0
for i in "${!srcs[@]}"; do
  src="${srcs[$i]}"
  dst="${dsts[$i]}"
  target_dir="$(dirname "$dst")"

  case "$mode" in
    dryrun)
      echo "would: mkdir -p \"$target_dir\""
      echo "would: cp \"$src\" \"$dst\""
      ;;

    check)
      if [[ -d "$dst" ]]; then
        echo "ERROR: $dst exists and is a directory" >&2
        exit 3
      elif [[ ! -e "$dst" ]]; then
        echo "differ: $src → $dst (target missing)"
        mismatch=1
      elif diff -q "$src" "$dst" >/dev/null 2>&1; then
        echo "match:  $src → $dst"
      else
        echo "differ: $src → $dst"
        mismatch=1
      fi
      ;;

    sync)
      if ! mkdir -p "$target_dir"; then
        echo "ERROR: cannot create $target_dir (permissions?)" >&2
        exit 1
      fi
      if [[ -d "$dst" ]]; then
        echo "ERROR: $dst exists and is a directory; refusing to overwrite" >&2
        exit 3
      fi
      if [[ -e "$dst" ]] && diff -q "$src" "$dst" >/dev/null 2>&1; then
        echo "unchanged: $src → $dst"
        continue
      fi
      if ! cp "$src" "$dst"; then
        echo "ERROR: cp failed for $dst" >&2
        exit 1
      fi
      if diff -q "$src" "$dst" >/dev/null 2>&1; then
        echo "synced:    $src → $dst"
      else
        echo "ERROR: post-copy diff failed for $dst (target on different filesystem?)" >&2
        exit 2
      fi
      ;;
  esac
done

if [[ "$mode" == "check" && $mismatch -ne 0 ]]; then
  exit 1
fi
exit 0
