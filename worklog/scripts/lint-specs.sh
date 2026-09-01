#!/usr/bin/env bash
# lint-specs.sh — detect drift between the rule blocks across spec files.
#
# Modes:
#   (default)  diff each pair; exit non-zero if any differ
#   --print    print each rule block under a header
#
# A rule block is the content between `<!-- rules:start -->` and
# `<!-- rules:end -->` markers (both on their own lines). Both markers must
# exist in each spec file or this script errors out.
#
# Exit codes:
#   0   all rule blocks identical (or --print succeeded)
#   1   one or more pairs differ, or a marker is missing, or a file missing
#   64  usage error

set -euo pipefail

SCRIPT_DIR="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &>/dev/null && pwd )"
REPO_ROOT="$( cd -- "$SCRIPT_DIR/.." &>/dev/null && pwd )"

mode="diff"
case "${1:-}" in
  -h|--help)
    cat <<'EOF'
Usage: lint-specs.sh [--print | --help]

Detects drift between rule blocks across spec files.
  (default) diff pairwise; exit non-zero if any differ
  --print   print each rule block under its filename header
EOF
    exit 0
    ;;
  --print) mode="print" ;;
  "")      ;;
  *) echo "unknown arg: $1 (try --help)" >&2; exit 64 ;;
esac

declare -a files=(
  "$REPO_ROOT/README.md"
  "$REPO_ROOT/skill/SKILL.md"
  "$REPO_ROOT/skill/claude/command.md"
)

# Extract rule block from $1: lines between rules:start and rules:end markers,
# with leading and trailing blank lines stripped.
extract_rules() {
  local file="$1"
  awk '
    /^<!-- rules:start -->$/ { capture=1; next }
    /^<!-- rules:end -->$/   { capture=0; next }
    capture { lines[++n] = $0 }
    END {
      start = 1
      while (start <= n && lines[start] ~ /^[[:space:]]*$/) start++
      end = n
      while (end >= start && lines[end] ~ /^[[:space:]]*$/) end--
      for (i = start; i <= end; i++) print lines[i]
    }
  ' "$file"
}

# Pre-check files exist and have both markers.
for f in "${files[@]}"; do
  if [[ ! -f "$f" ]]; then
    echo "ERROR: spec file missing: $f" >&2
    exit 1
  fi
  if ! grep -q '^<!-- rules:start -->$' "$f"; then
    echo "ERROR: $f is missing <!-- rules:start --> marker" >&2
    exit 1
  fi
  if ! grep -q '^<!-- rules:end -->$' "$f"; then
    echo "ERROR: $f is missing <!-- rules:end --> marker" >&2
    exit 1
  fi
done

# Extract each block into a temp file.
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
declare -a tmps=()
for i in "${!files[@]}"; do
  tmp="$tmpdir/block-$i"
  extract_rules "${files[$i]}" > "$tmp"
  tmps+=("$tmp")
done

if [[ "$mode" == "print" ]]; then
  for i in "${!files[@]}"; do
    echo "=== ${files[$i]} ==="
    cat "${tmps[$i]}"
    echo
  done
  exit 0
fi

# Diff pairwise.
differ=0
for i in "${!files[@]}"; do
  for ((j=i+1; j<${#files[@]}; j++)); do
    if ! diff -q "${tmps[$i]}" "${tmps[$j]}" >/dev/null 2>&1; then
      echo "=== DRIFT: ${files[$i]}  vs  ${files[$j]} ===" >&2
      diff -u --label "${files[$i]}" --label "${files[$j]}" "${tmps[$i]}" "${tmps[$j]}" >&2 || true
      differ=1
    fi
  done
done

if (( differ )); then
  echo "lint-specs: rule blocks differ across spec files (see diffs above)" >&2
  exit 1
fi
echo "lint-specs: rule blocks in sync"
exit 0
