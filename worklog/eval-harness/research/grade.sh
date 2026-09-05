#!/usr/bin/env bash
# Mechanically grade one research-eval transcript against the rubric rules that
# are objectively observable from the tool stream. The judgment rules (R1, R2,
# R5, R6) are scored separately by judge.sh, blind to the arm.
#
# Usage: grade.sh <transcript.jsonl> <meta.json>
# Prints one JSON object to stdout.
set -euo pipefail

TRANSCRIPT="${1:?usage: grade.sh <transcript.jsonl> <meta.json>}"
META="${2:?usage: grade.sh <transcript.jsonl> <meta.json>}"

[ -f "$TRANSCRIPT" ] || { echo "no such transcript: $TRANSCRIPT" >&2; exit 64; }

# One tool name per line, in call order.
tools="$(jq -r 'select(.type=="assistant") | .message.content[]?
  | select(.type=="tool_use") | .name' "$TRANSCRIPT" 2>/dev/null || true)"

bash_cmds="$(jq -r 'select(.type=="assistant") | .message.content[]?
  | select(.type=="tool_use" and .name=="Bash") | .input.command // empty' \
  "$TRANSCRIPT" 2>/dev/null || true)"

# Whole text of the LAST result event (not the last line of it).
final_text="$(jq -rs 'map(select(.type=="result")) | last | .result // empty' "$TRANSCRIPT" 2>/dev/null || true)"
notes="$(jq -r '.notes_written // ""' "$META")"

count() { grep -cE "$1" || true; }

# --- R3: evidence discipline -------------------------------------------------
# file:line citations in the deliverable (final report + persisted notes).
# `|| true` inside the pipeline: grep exits 1 on no match, and pipefail would
# otherwise abort the whole script on a run that cited nothing.
cite_count="$(printf '%s\n%s\n' "$final_text" "$notes" \
  | { grep -oE '[A-Za-z0-9_./-]+\.(go|md|html|py|sh|ya?ml|js|ts):[0-9]+' || true; } | wc -l | tr -d ' ')"
# Bare file references with no line number, for comparison with the above.
file_refs="$(printf '%s\n%s\n' "$final_text" "$notes" \
  | { grep -oE '[A-Za-z0-9_/-]+\.(go|md|html|py|sh|ya?ml|js|ts)\b' || true; } | wc -l | tr -d ' ')"
r3="fail"; if [ "$cite_count" -ge 3 ]; then r3="pass"; fi

# --- R4: findings persisted, not left only in conversation -------------------
note_calls="$(printf '%s\n' "$bash_cmds" | count 'worklog note')"
r4="fail"; if [ "$note_calls" -ge 1 ]; then r4="pass"; fi

# --- R7: work advanced while subagents ran -----------------------------------
# Subagents surface as Agent (or Task, depending on harness build).
agent_calls="$(printf '%s\n' "$tools" | count '^(Agent|Task)$')"
if [ "$agent_calls" -eq 0 ]; then
  r7="n/a"
else
  first="$(printf '%s\n' "$tools" | grep -nE '^(Agent|Task)$' | head -1 | cut -d: -f1)"
  last="$(printf '%s\n' "$tools" | grep -nE '^(Agent|Task)$' | tail -1 | cut -d: -f1)"
  between=0
  if [ "$last" -gt "$first" ]; then
    between="$(printf '%s\n' "$tools" | sed -n "$((first+1)),${last}p" | grep -vcE '^(Agent|Task)$' || true)"
  fi
  r7="fail"; if [ "$between" -ge 1 ]; then r7="pass"; fi
fi

# --- descriptive counters (not pass/fail) ------------------------------------
read_calls="$(printf '%s\n' "$tools" | count '^(Read|Grep|Glob)$')"
status_calls="$(printf '%s\n' "$bash_cmds" | count 'worklog status')"
# Questions put back to the human at the end of the report.
trailing_questions="$(printf '%s\n' "$final_text" | count '\?[[:space:]]*$')"
report_chars="${#final_text}"

result_summary="$(jq -c 'select(.type=="result")' "$TRANSCRIPT" 2>/dev/null | tail -1)"
if [ -z "$result_summary" ]; then result_summary="null"; fi

jq -n \
  --argjson meta "$(cat "$META")" \
  --arg r3 "$r3" --arg r4 "$r4" --arg r7 "$r7" \
  --argjson cites "$cite_count" --argjson file_refs "$file_refs" --argjson notes_n "$note_calls" \
  --argjson agents "$agent_calls" --argjson reads "$read_calls" \
  --argjson statuses "$status_calls" --argjson tq "$trailing_questions" \
  --argjson report_chars "$report_chars" \
  --argjson result_summary "$result_summary" \
  '{
     arm: $meta.arm, label: $meta.label, model: $meta.model,
     skills_installed: $meta.skills_installed,
     mechanical: {
       R3_evidence_discipline: $r3,
       R4_findings_persisted: $r4,
       R7_no_idle_blocking: $r7
     },
     counters: {
       file_line_citations: $cites, bare_file_refs: $file_refs, worklog_note_calls: $notes_n,
       subagents_spawned: $agents, read_search_calls: $reads,
       worklog_status_calls: $statuses, trailing_questions: $tq,
       report_chars: $report_chars
     },
     result_summary: $result_summary
   }'
