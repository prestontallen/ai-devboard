#!/usr/bin/env bash
# Run one headless `claude -p` research session for one arm of the three-way
# research eval, in an isolated sandbox: scratch CLAUDE_CONFIG_DIR, scratch
# worklog data dir + scratch store, scratch devboard dir, and a throwaway
# clone of the repo. The store directory is DERIVED from WORKLOG_DIR (it is
# that path with "-store" appended), so WORKLOG_DIR alone now isolates both;
# the store is still seeded with `worklog migrate` + `worklog adopt --commit`
# because every write path opens it. Never touches Preston's real
# ~/.local/share/worklog/, its store sibling, ~/.local/share/devboard/, or
# the real checkout.
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
# Derived, not independent: the binary computes this from WORKLOG_DIR.
MIGRATION_DIR="${DATA_DIR}-store"
CLONE_DIR="$(mktemp -d)"
# Must EXIST (adopt census lstats every live root), unlike the pre-cutover
# harness's mktemp -u. Board syncs land here and are discarded with the run.
BOARD_DIR="$(mktemp -d)"
cleanup() { rm -rf "$CFG_DIR" "$DATA_DIR" "$MIGRATION_DIR" "$CLONE_DIR" "$BOARD_DIR"; }
trap cleanup EXIT

# Throwaway clone: any stray write lands here, never in the real checkout.
git clone --quiet --depth 1 "file://$SOURCE_REPO" "$CLONE_DIR/ai-devboard"
REPO="$CLONE_DIR/ai-devboard"

# The repo's CLAUDE.md is auto-loaded and tells any agent to run the
# dev-context workflow — naming the research phase and the spike track. That
# would hand the bare arms the very process under test, so it is removed in
# EVERY arm: the only difference between arms stays (model, skills installed).
rm -f "$REPO/CLAUDE.md"
# The harness itself is committed in the repo: the judge rubric (which now
# carries the question's ground-truth key) and prior-round transcripts would
# hand any arm the answer sheet. Stripped from every clone.
rm -rf "$REPO/worklog/eval-harness"
[ ! -e "$REPO/CLAUDE.md" ] && [ ! -e "$REPO/worklog/eval-harness" ] \
  || { echo "clone prep failed: CLAUDE.md or eval-harness still present" >&2; exit 65; }

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
# has a real target and the run never touches the real data dir. The store
# is rebuilt from the seeded WORK.md and adopted, since every write path
# verifies projections against the store before writing.
seed_worklog() {
  rm -rf "$DATA_DIR" "$MIGRATION_DIR" "$BOARD_DIR"
  mkdir -p "$DATA_DIR" "$BOARD_DIR"
  cat > "$DATA_DIR/WORK.md" <<'EOF'
## Now
- [~] **WL-SCRATCH-ISOLATION** — Research: a fresh WORKLOG_DIR refuses every write as hand-edited
  - **ID**: wl-scratch-isolation
  - **Repo**: ai-devboard
  - **Type**: spike
  - **Started**: 2026-09-08

## Waiting

## Next

## Someday
EOF
  WORKLOG_DIR="$DATA_DIR" DEVBOARD_DATA="$BOARD_DIR" \
    worklog migrate >&2
  WORKLOG_DIR="$DATA_DIR" DEVBOARD_DATA="$BOARD_DIR" \
    worklog adopt --commit >&2
}

# Self-check: prove `worklog note` works in THIS sandbox before spending a
# cent, then re-seed so the agent gets a pristine store with no harness note.
seed_worklog
if ! WORKLOG_DIR="$DATA_DIR" DEVBOARD_DATA="$BOARD_DIR" \
     worklog note wl-scratch-isolation "harness seed self-check" >&2 \
   || ! grep -q "harness seed self-check" "$DATA_DIR/notes/wl-scratch-isolation.md"; then
  echo "seed self-check failed: worklog note does not work in the sandbox — aborting before any spend" >&2
  exit 65
fi
seed_worklog

OUT_FILE="$RESULTS_DIR/${ARM}-${RUN_LABEL}.jsonl"
META_FILE="$RESULTS_DIR/${ARM}-${RUN_LABEL}.meta.json"

TASK="$(sed "s|REPO_PATH_PLACEHOLDER|$REPO|" "$HARNESS_DIR/task.txt")"

echo "run: arm=$ARM label=$RUN_LABEL model=$MODEL skills=$INSTALL_SKILLS repo=$REPO" >&2

set +e
(
  cd "$REPO"
  CLAUDE_CONFIG_DIR="$CFG_DIR" WORKLOG_DIR="$DATA_DIR" \
  DEVBOARD_DATA="$BOARD_DIR" \
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
