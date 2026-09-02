#!/usr/bin/env bash
# Grade one run's stream-json transcript against the 4 mechanical compliance
# markers, and pass through whatever the transcript's final "result" event
# reports (cost/tokens/error) verbatim, so callers don't need to guess field
# names.
#
# Usage: grade.sh <transcript.jsonl> <scratch-data-dir>
# Prints one JSON object to stdout.
set -euo pipefail

TRANSCRIPT="${1:?usage: grade.sh <transcript.jsonl> <scratch-data-dir>}"
DATA_DIR="${2:?usage: grade.sh <transcript.jsonl> <scratch-data-dir>}"

if [ ! -f "$TRANSCRIPT" ]; then
  echo "no such transcript: $TRANSCRIPT" >&2
  exit 64
fi

bash_cmds="$(jq -r '
  select(.type=="assistant") | .message.content[]?
  | select(.type=="tool_use" and .name=="Bash")
  | .input.command // empty
' "$TRANSCRIPT" 2>/dev/null || true)"

# (a) oriented-at-start: a `worklog status` call happens, and no
# add/start/note/done/edit call happens before it.
first_status_line="$(printf '%s\n' "$bash_cmds" | grep -n 'worklog status' | head -1 | cut -d: -f1 || true)"
first_mutate_line="$(printf '%s\n' "$bash_cmds" | grep -nE 'worklog (add|start|note|done|edit)' | head -1 | cut -d: -f1 || true)"
oriented="fail"
if [ -n "$first_status_line" ]; then
  if [ -z "$first_mutate_line" ] || [ "$first_status_line" -lt "$first_mutate_line" ]; then
    oriented="pass"
  fi
fi

# (c) used-cli-for-task: add + start + note all invoked via the worklog CLI.
used_cli="fail"
if printf '%s\n' "$bash_cmds" | grep -q 'worklog add' \
  && printf '%s\n' "$bash_cmds" | grep -q 'worklog start' \
  && printf '%s\n' "$bash_cmds" | grep -q 'worklog note'; then
  used_cli="pass"
fi

# (b) no-hand-edit: no Read/Edit/Write tool_use targets a path under the
# scratch worklog data dir.
handedit_hits="$(jq -r --arg dd "$DATA_DIR" '
  select(.type=="assistant") | .message.content[]?
  | select(.type=="tool_use" and (.name=="Read" or .name=="Edit" or .name=="Write"))
  | (.input.file_path // .input.path // empty)
  | select(startswith($dd))
' "$TRANSCRIPT" 2>/dev/null || true)"
no_hand_edit="pass"
[ -n "$handedit_hits" ] && no_hand_edit="fail"

# (d) no-malformed-note-heading: the note text passed to `worklog note`
# contains no literal "## YYYY-MM-DD ..." line.
malformed="$(printf '%s\n' "$bash_cmds" | grep 'worklog note' | grep -E '##[[:space:]]*[0-9]{4}-[0-9]{2}-[0-9]{2}' || true)"
no_malformed_note_heading="pass"
[ -n "$malformed" ] && no_malformed_note_heading="fail"

result_summary="$(jq -c 'select(.type=="result")' "$TRANSCRIPT" 2>/dev/null | tail -1)"
[ -z "$result_summary" ] && result_summary="null"

jq -n \
  --arg oriented "$oriented" \
  --arg no_hand_edit "$no_hand_edit" \
  --arg used_cli "$used_cli" \
  --arg no_malformed "$no_malformed_note_heading" \
  --argjson result_summary "$result_summary" \
  '{
    oriented_at_start: $oriented,
    no_hand_edit: $no_hand_edit,
    used_cli_for_task: $used_cli,
    no_malformed_note_heading: $no_malformed,
    result_summary: $result_summary
  }'
