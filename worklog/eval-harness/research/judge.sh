#!/usr/bin/env bash
# Score one research-eval transcript's judgment rules with an LLM judge that is
# blind to the arm: it sees the agent's own text and tool calls, with the arm,
# model, and skill-installation stripped out.
#
# Usage: judge.sh <transcript.jsonl> <meta.json>
# Prints one JSON object to stdout.
set -euo pipefail

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TRANSCRIPT="${1:?usage: judge.sh <transcript.jsonl> <meta.json>}"
META="${2:?usage: judge.sh <transcript.jsonl> <meta.json>}"

[ -f "$TRANSCRIPT" ] || { echo "no such transcript: $TRANSCRIPT" >&2; exit 64; }

# Flatten to a readable transcript: assistant prose, tool calls with their key
# argument, and the final report. Nothing here names the arm or the model.
FLAT="$(jq -r '
  if .type=="assistant" then
    (.message.content[]? |
      if .type=="text" then "AGENT: " + .text
      elif .type=="tool_use" then
        "TOOL[" + .name + "]: " +
        ((.input.command // .input.file_path // .input.pattern // .input.prompt // .input.description // "") | tostring | .[0:400])
      else empty end)
  elif .type=="result" then "FINAL REPORT: " + (.result // "")
  else empty end
' "$TRANSCRIPT" 2>/dev/null | head -400)"

NOTES="$(jq -r '.notes_written // ""' "$META")"

PROMPT="$(cat "$HARNESS_DIR/judge.md")

--- TRANSCRIPT ---
$FLAT

--- NOTES THE AGENT PERSISTED ---
$NOTES
"

CFG_DIR="$(mktemp -d)"
trap 'rm -rf "$CFG_DIR"' EXIT
[ -f "$HOME/.claude/.credentials.json" ] && ln -s "$HOME/.claude/.credentials.json" "$CFG_DIR/.credentials.json"

RAW="$(CLAUDE_CONFIG_DIR="$CFG_DIR" claude -p "$PROMPT" \
        --model claude-fable-5 --max-budget-usd 0.50 --allowedTools "" 2>/dev/null || true)"

# The judge is told to emit bare JSON; tolerate a fenced block anyway.
CLEAN="$(printf '%s' "$RAW" | sed -n '/{/,/}/p')"
if ! printf '%s' "$CLEAN" | jq -e . >/dev/null 2>&1; then
  jq -n --arg raw "$RAW" '{judge_error:"unparseable", raw:$raw}'
  exit 0
fi
printf '%s' "$CLEAN" | jq -c .
