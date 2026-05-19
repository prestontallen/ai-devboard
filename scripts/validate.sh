#!/usr/bin/env bash
# validate.sh — verify structural integrity of a worklog data directory.
#
# Default target: $HOME/.local/share/worklog/
# Override: positional arg, or WORKLOG_DIR env var (positional wins).
#
# Exit codes:
#   0  no violations
#   1  WORK.md missing (hard fail; no other checks run)
#   2  one or more violations
#   64 usage error
#
# Requires: bash 4+, gawk (uses match($0, re, arr) capture form).

set -euo pipefail

usage() {
  cat >&2 <<'EOF'
Usage: validate.sh [WORKLOG_DIR]
       WORKLOG_DIR=/path/to/dir validate.sh

Validates the structural integrity of a worklog data directory.
Default WORKLOG_DIR: $HOME/.local/share/worklog/
EOF
  exit 64
}

case "${1:-}" in
  -h|--help) usage ;;
esac

data_dir="${1:-${WORKLOG_DIR:-$HOME/.local/share/worklog}}"
work_md="$data_dir/WORK.md"
index_md="$data_dir/INDEX.md"
notes_dir="$data_dir/notes"

violations=0
fail() {
  local id="$1"; shift
  echo "VIOLATION [$id] $*" >&2
  violations=$((violations + 1))
}

# --- Check 1: work-md-exists (hard fail) -------------------------------------
if [[ ! -f "$work_md" ]]; then
  fail "work-md-exists" "WORK.md not found at $work_md"
  echo "validate: 1 violation in $data_dir"
  exit 1
fi

# --- Parse WORK.md into block records via gawk -------------------------------
# Each emitted record is TAB-separated:
#   section  state  id  type  parent  notes_ref  has_started  active_children
# section is the most recent `## <Name>` heading (Now, Next, Someday, ...).
# state is the single char between brackets on `- [ ]` / `- [~]` / `- [x]`.
blocks=$(awk '
  function emit_block() {
    if (block_active) {
      printf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", \
             section, state, id, type, parent, notes_ref, has_started, active_children)
    }
    block_active = 0
    state = ""; id = ""; type = ""; parent = ""; notes_ref = ""
    has_started = 0; active_children = ""
  }
  /^## / {
    emit_block()
    if (match($0, /^## ([A-Za-z]+)/, m)) section = m[1]
    next
  }
  /^- \[/ {
    emit_block()
    block_active = 1
    if (match($0, /^- \[([ ~x])\]/, m)) state = m[1]
    next
  }
  block_active && /^  - \*\*ID\*\*: */ {
    if (match($0, /\*\*ID\*\*: *([a-zA-Z0-9_-]+)/, m)) id = tolower(m[1])
  }
  block_active && /^  - \*\*Type\*\*: */ {
    if (match($0, /\*\*Type\*\*: *([a-z]+)/, m)) type = m[1]
  }
  block_active && /^  - \*\*Parent\*\*: */ {
    if (match($0, /\*\*Parent\*\*: *([a-zA-Z0-9_-]+)/, m)) parent = tolower(m[1])
  }
  block_active && /^  - \*\*Notes\*\*: */ {
    if (match($0, /\*\*Notes\*\*: *(.+)$/, m)) notes_ref = m[1]
  }
  block_active && /^  - \*\*Started\*\*: */ {
    has_started = 1
  }
  block_active && /^  - \*\*Active children\*\*: */ {
    if (match($0, /\*\*Active children\*\*: *(.+)$/, m)) active_children = m[1]
  }
  END { emit_block() }
' "$work_md")

# --- Check 2: now-cap --------------------------------------------------------
now_count=$(awk -F'\t' '$1=="Now"{n++} END{print n+0}' <<< "$blocks")
if (( now_count > 5 )); then
  fail "now-cap" "## Now has $now_count tickets, cap is 5"
fi

# --- Check 3: no-top-level-x -------------------------------------------------
while IFS= read -r line; do
  [[ -n "$line" ]] && fail "no-top-level-x" "$line"
done < <(grep -nE '^- \[x\]' "$work_md" || true)

# --- Check 4: started-on-active ----------------------------------------------
while IFS=$'\t' read -r section state id type parent notes_ref has_started active_children; do
  [[ -z "$section" ]] && continue
  if [[ "$section" == "Now" && "$state" == "~" && "$has_started" != "1" ]]; then
    fail "started-on-active" "ticket ${id:-<unknown>} in ## Now is [~] but lacks **Started**:"
  fi
done <<< "$blocks"

# --- Check 6: notes-file-exists ---------------------------------------------
# (Done before three-place-consistency so we get all the surface area.)
while IFS=$'\t' read -r section state id type parent notes_ref has_started active_children; do
  [[ -z "$notes_ref" ]] && continue
  case "$notes_ref" in
    notes/*)
      target="$data_dir/$notes_ref"
      if [[ ! -f "$target" ]]; then
        fail "notes-file-exists" "block ${id:-<unknown>} references $notes_ref but file does not exist"
      fi
      ;;
  esac
done <<< "$blocks"

# --- Check 7: three-place-consistency ---------------------------------------
declare -A epic_active      # epic_id -> active_children string
declare -A child_parent     # child_id -> parent_epic_id

while IFS=$'\t' read -r section state id type parent notes_ref has_started active_children; do
  [[ -z "$id" ]] && continue
  if [[ "$type" == "epic" && "$section" == "Next" ]]; then
    epic_active["$id"]="$active_children"
  fi
  if [[ "$section" == "Now" && -n "$parent" ]]; then
    child_parent["$id"]="$parent"
  fi
done <<< "$blocks"

for child_id in "${!child_parent[@]}"; do
  parent_id="${child_parent[$child_id]}"

  # (a) epic block exists and lists this child in Active children
  if [[ -z "${epic_active[$parent_id]+set}" ]]; then
    fail "three-place-consistency" \
      "child $child_id has Parent:$parent_id but no matching epic block found in ## Next"
  else
    ac="${epic_active[$parent_id]}"
    # case-insensitive word-boundary match for the child id
    if ! grep -qiE "(^|[^a-zA-Z0-9_-])${child_id}([^a-zA-Z0-9_-]|$)" <<< "$ac"; then
      fail "three-place-consistency" \
        "child $child_id not listed in epic $parent_id Active children: '$ac'"
    fi
  fi

  # (b) notes/<parent_id>.md exists and has a `- [ ]` line mentioning child_id
  notes_file="$notes_dir/$parent_id.md"
  if [[ ! -f "$notes_file" ]]; then
    fail "three-place-consistency" \
      "notes/$parent_id.md missing (referenced as parent of $child_id)"
  else
    if ! grep -qiE "^- \[ \].*${child_id}" "$notes_file"; then
      fail "three-place-consistency" \
        "notes/$parent_id.md has no '- [ ]' line mentioning $child_id"
    fi
  fi
done

# --- Check 5: index-refs-exist -----------------------------------------------
if [[ -f "$index_md" ]]; then
  while IFS= read -r ref; do
    [[ -z "$ref" ]] && continue
    target="$data_dir/$ref"
    if [[ ! -e "$target" ]]; then
      fail "index-refs-exist" "INDEX.md references '$ref' but file does not exist"
    fi
  done < <(grep -oE '(archive/[0-9]{4}-[0-9]{2}\.md|notes/[a-zA-Z0-9_-]+\.md)' "$index_md" | sort -u)
else
  echo "INFO: $index_md not present; skipping index-refs-exist check" >&2
fi

# --- Summary -----------------------------------------------------------------
echo "validate: $violations violation(s) in $data_dir"
if (( violations > 0 )); then
  exit 2
fi
exit 0
