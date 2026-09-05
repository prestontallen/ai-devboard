#!/usr/bin/env bash
# Run one headless `claude -p` research session for one arm of the three-way
# research eval, in an isolated CLAUDE_CONFIG_DIR + scratch worklog data dir +
# a throwaway clone of the repo. Never touches Preston's real
# ~/.local/share/worklog/, ~/.local/share/devboard/, or the real checkout.
#
# Usage: run.sh <arm> <run-label>
#   arm: weak-bare   — sonnet, no workflow skills installed
#        weak-skill  — sonnet, with dev-context + contract + fan-out installed
#        strong-bare — fable, no workflow skills installed
#
# The three arms separate "is this process a model capability?" (strong-bare vs
# weak-bare) from "does the skill text transfer it?" (weak-skill vs weak-bare).
#
# Writes:
#   results/raw/<arm>-<label>.jsonl      — full stream-json transcript
#   results/raw/<arm>-<label>.meta.json  — scratch paths, for the grader
set -euo pipefail

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARM="${1:?usage: run.sh <arm> <run-label>}"
RUN_LABEL="${2:?usage: run.sh <arm> <run-label>}"
SOURCE_REPO="${SOURCE_REPO:-/home/preston/ai-devboard}"
RESULTS_DIR="$HARNESS_DIR/results/raw"
BUDGET="${BUDGET_USD:-2.00}"

case "$ARM" in
  weak-bare)   MODEL="sonnet"; INSTALL_SKILLS=0 ;;
  weak-skill)  MODEL="sonnet"; INSTALL_SKILLS=1 ;;
  strong-bare) MODEL="claude-fable-5"; INSTALL_SKILLS=0 ;;
  *) echo "unknown arm: $ARM (weak-bare|weak-skill|strong-bare)" >&2; exit 64 ;;
esac

mkdir -p "$RESULTS_DIR"

CFG_DIR="$(mktemp -d)"
DATA_DIR="$(mktemp -d)"
CLONE_DIR="$(mktemp -d)"
# A path that does not exist, so every devboard sync silently no-ops.
DEVBOARD_SCRATCH="$(mktemp -u)"
cleanup() { rm -rf "$CFG_DIR" "$DATA_DIR" "$CLONE_DIR" "$DEVBOARD_SCRATCH"; }
trap cleanup EXIT

# Throwaway clone: any stray write lands here, never in the real checkout.
git clone --quiet --depth 1 "file://$SOURCE_REPO" "$CLONE_DIR/ai-devboard"
REPO="$CLONE_DIR/ai-devboard"

# The repo's CLAUDE.md is auto-loaded and tells any agent to run the
# dev-context workflow — naming the research phase and the spike track. That
# would hand the bare arms the very process under test, so it is removed in
# EVERY arm: the only difference between arms stays (model, skills installed).
rm -f "$REPO/CLAUDE.md"

mkdir -p "$CFG_DIR/skills"
if [ -f "$HOME/.claude/.credentials.json" ]; then
  ln -s "$HOME/.claude/.credentials.json" "$CFG_DIR/.credentials.json"
fi
if [ "$INSTALL_SKILLS" = "1" ]; then
  for s in dev-context contract fan-out; do
    cp -r "$SOURCE_REPO/$s" "$CFG_DIR/skills/$s"
  done
fi

# Scratch worklog seeded with the spike ticket in ## Now, so `worklog note`
# has a real target and the run never touches the real data dir.
cat > "$DATA_DIR/WORK.md" <<'EOF'
## Now
- [~] **ADB-DEVBOARD-BACKLOG-VISIBILITY** — Research: devboard has no view of not-yet-started worklog tickets (Next/Someday)
  - **ID**: adb-devboard-backlog-visibility
  - **Repo**: ai-devboard
  - **Type**: spike
  - **Started**: 2026-09-02

## Waiting

## Next

## Someday
EOF

OUT_FILE="$RESULTS_DIR/${ARM}-${RUN_LABEL}.jsonl"
META_FILE="$RESULTS_DIR/${ARM}-${RUN_LABEL}.meta.json"

TASK="$(sed "s|REPO_PATH_PLACEHOLDER|$REPO|" "$HARNESS_DIR/task.txt")"

echo "run: arm=$ARM label=$RUN_LABEL model=$MODEL skills=$INSTALL_SKILLS repo=$REPO" >&2

set +e
(
  cd "$REPO"
  CLAUDE_CONFIG_DIR="$CFG_DIR" WORKLOG_DIR="$DATA_DIR" DEVBOARD_DATA="$DEVBOARD_SCRATCH" \
    claude -p "$TASK" \
      --model "$MODEL" \
      --output-format stream-json \
      --verbose \
      --max-budget-usd "$BUDGET" \
      --allowedTools "Bash Read Grep Glob Agent Task Skill WebSearch" \
      > "$OUT_FILE"
)
RC=$?
set -e

# Notes the run wrote are part of the deliverable (rubric R4), so capture them
# before the scratch dir is cleaned up.
NOTES="$(cat "$DATA_DIR"/notes/*.md 2>/dev/null || true)"

jq -n --arg arm "$ARM" --arg label "$RUN_LABEL" --arg model "$MODEL" \
      --arg data_dir "$DATA_DIR" --arg repo "$REPO" \
      --argjson skills "$INSTALL_SKILLS" --argjson rc "$RC" \
      --arg notes "$NOTES" \
  '{arm:$arm, label:$label, model:$model, skills_installed:($skills==1),
    data_dir:$data_dir, repo:$repo, exit_code:$rc, notes_written:$notes}' \
  > "$META_FILE"

echo "done: $OUT_FILE (rc=$RC)" >&2
