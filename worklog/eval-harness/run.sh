#!/usr/bin/env bash
# Run one headless claude -p session against one worklog SKILL.md variant,
# in an isolated CLAUDE_CONFIG_DIR + scratch worklog data dir. Never touches
# Preston's real ~/.local/share/worklog/.
#
# Usage: run.sh <variant> <run-label>
#   variant:   verbose | trimmed-prose | ears-lite
#   run-label: any string used in the output filename (e.g. 1, 2, 3, smoke)
#
# Writes:
#   results/raw/<variant>-<run-label>.jsonl       — full stream-json transcript
#   results/raw/<variant>-<run-label>.datadir.txt — the scratch WORKLOG_DIR path,
#                                                    recorded before cleanup so
#                                                    grade.sh can match paths in
#                                                    the transcript against it
set -euo pipefail

HARNESS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VARIANT="${1:?usage: run.sh <variant> <run-label>}"
RUN_LABEL="${2:?usage: run.sh <variant> <run-label>}"
VARIANT_DIR="$HARNESS_DIR/variants/$VARIANT"
TASK_FILE="$HARNESS_DIR/task.txt"
RESULTS_DIR="$HARNESS_DIR/results/raw"

if [ ! -f "$VARIANT_DIR/SKILL.md" ]; then
  echo "unknown variant: $VARIANT (looked for $VARIANT_DIR/SKILL.md)" >&2
  exit 64
fi

mkdir -p "$RESULTS_DIR"

CFG_DIR="$(mktemp -d)"
DATA_DIR="$(mktemp -d)"
WORKSPACE_DIR="$(mktemp -d)"
# Points DEVBOARD_DATA at a path that doesn't exist, so every worklog
# devboard-sync write silently no-ops (per the worklog skill's documented
# behavior) instead of landing in Preston's real ~/.local/share/devboard/.
DEVBOARD_SCRATCH="$(mktemp -u)"
cleanup() { rm -rf "$CFG_DIR" "$DATA_DIR" "$WORKSPACE_DIR" "$DEVBOARD_SCRATCH"; }
trap cleanup EXIT

mkdir -p "$CFG_DIR/skills/worklog"
if [ -f "$HOME/.claude/.credentials.json" ]; then
  ln -s "$HOME/.claude/.credentials.json" "$CFG_DIR/.credentials.json"
fi
cp "$VARIANT_DIR/SKILL.md" "$CFG_DIR/skills/worklog/SKILL.md"
if [ -d "$VARIANT_DIR/references" ]; then
  cp -r "$VARIANT_DIR/references" "$CFG_DIR/skills/worklog/references"
fi

cat > "$DATA_DIR/WORK.md" <<'EOF'
## Now

## Waiting

## Next

## Someday
EOF

OUT_FILE="$RESULTS_DIR/${VARIANT}-${RUN_LABEL}.jsonl"
DATADIR_RECORD="$RESULTS_DIR/${VARIANT}-${RUN_LABEL}.datadir.txt"
echo "$DATA_DIR" > "$DATADIR_RECORD"

echo "run: variant=$VARIANT label=$RUN_LABEL cfg=$CFG_DIR data=$DATA_DIR" >&2

(
  cd "$WORKSPACE_DIR"
  CLAUDE_CONFIG_DIR="$CFG_DIR" WORKLOG_DIR="$DATA_DIR" DEVBOARD_DATA="$DEVBOARD_SCRATCH" \
    claude -p "$(cat "$TASK_FILE")" \
      --output-format stream-json \
      --verbose \
      --max-budget-usd 0.50 \
      --allowedTools "Bash Read Edit Write" \
      > "$OUT_FILE"
)

echo "done: $OUT_FILE" >&2
