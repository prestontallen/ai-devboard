#!/usr/bin/env bash
# install.sh — one command from fresh checkout to working setup.
#
# Installs the worklog binary, deploys the repo's skills, prepares the
# devboard data dir, and offers the optional pieces (CLAUDE.md directive,
# devboard container). Linux and macOS; Windows is pointed at WSL.
#
# Modes:
#   (default)   install/update everything, prompting only for optional pieces
#   --check     report drift; exit 0 if fully current, 1 if anything differs
#   --dry-run   print what would happen; change nothing
#
# Exit codes: 0 ok/current · 1 preflight or drift · 64 usage

set -euo pipefail

REPO_ROOT="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &>/dev/null && pwd )"
BIN_DIR="$HOME/.local/bin"
SKILLS_DIR="$HOME/.claude/skills"
DEVBOARD_DIR="${DEVBOARD_DATA:-$HOME/.local/share/devboard}"

mode="install"
case "${1:-}" in
  -h|--help) sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  --check)   mode="check" ;;
  --dry-run) mode="dryrun" ;;
  "")        ;;
  *) echo "unknown arg: $1 (try --help)" >&2; exit 64 ;;
esac

drift=0
note()  { printf '%s\n' "$*"; }
warn()  { printf 'WARN: %s\n' "$*" >&2; }
fail()  { printf 'ERROR: %s\n' "$*" >&2; exit 1; }
plan()  { printf 'would: %s\n' "$*"; }          # dry-run narration
stale() { printf 'drift: %s\n' "$*"; drift=1; } # check narration

# ---------- platform ----------------------------------------------------
case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    fail "native Windows isn't supported — run this repo under WSL (https://learn.microsoft.com/windows/wsl/)" ;;
esac
platform="$("$REPO_ROOT/worklog/scripts/detect-platform.sh")" \
  || fail "unsupported platform; nothing was installed"
note "platform: $platform"

# ---------- preflight (before any change) -------------------------------
command -v go >/dev/null 2>&1 \
  || fail "go toolchain not found on PATH — install Go >= 1.26 first; nothing was installed"
command -v git >/dev/null 2>&1 \
  || fail "git not found on PATH; nothing was installed"
if ! command -v docker >/dev/null 2>&1; then
  warn "docker not found — devboard container won't be available (worklog + skills still install)"
fi

# ---------- worklog binary ----------------------------------------------
rev="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo none)"
# a dirty worklog tree must read as a different version, or edits never install
if ! git -C "$REPO_ROOT" diff --quiet HEAD -- worklog 2>/dev/null; then
  rev="$rev-dirty"
fi
want_version="0.2.0-dev ($rev, $(date -u +%Y-%m-%d))"
have_version="$("$BIN_DIR/worklog" --version 2>/dev/null | sed 's/^worklog version //' || true)"

binary_current=false
[[ "$have_version" == *"($rev,"* ]] && binary_current=true

case "$mode" in
  check)
    $binary_current || stale "worklog binary ($BIN_DIR/worklog): have '${have_version:-absent}', want commit $rev" ;;
  dryrun)
    $binary_current && note "worklog binary: current ($have_version)" \
      || plan "build worklog at $rev and install to $BIN_DIR/worklog" ;;
  install)
    if $binary_current; then
      note "worklog binary: up to date ($have_version)"
    else
      mkdir -p "$BIN_DIR"
      ( cd "$REPO_ROOT/worklog" && go build \
          -ldflags "-X main.version=0.2.0-dev -X main.commit=$rev -X 'main.date=$(date -u +%Y-%m-%d)'" \
          -o "$BIN_DIR/worklog" ./cmd/worklog )
      note "installed: $BIN_DIR/worklog ($("$BIN_DIR/worklog" --version | sed 's/^worklog version //'))"
    fi ;;
esac
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) warn "$BIN_DIR is not on PATH — add it to your shell profile" ;;
esac

# ---------- skills ------------------------------------------------------
# dev-context and contract: symlinked, so repo edits apply live (checkout
# installs). worklog SKILL + command: deployed by its own sync.sh (copies).
link_skill() {
  local name="$1" target="$REPO_ROOT/$1" link="$SKILLS_DIR/$1"
  if [[ "$(readlink "$link" 2>/dev/null)" == "$target" ]]; then
    [[ "$mode" == install ]] && note "skill $name: up to date (symlink)"
    return 0
  fi
  case "$mode" in
    check)  stale "skill $name: $link is not a symlink to the repo" ;;
    dryrun) plan  "symlink $link -> $target" ;;
    install)
      [[ -e "$link" && ! -L "$link" ]] && fail "$link exists and is not a symlink; move it aside first"
      mkdir -p "$SKILLS_DIR"
      ln -sfn "$target" "$link"
      note "skill $name: symlinked" ;;
  esac
}
link_skill dev-context
link_skill contract

case "$mode" in
  check)   "$REPO_ROOT/worklog/scripts/sync.sh" --check >/dev/null \
             || stale "worklog skill files differ from repo (run worklog/scripts/sync.sh)" ;;
  dryrun)  "$REPO_ROOT/worklog/scripts/sync.sh" --dry-run | sed 's/^would: //; s/^/would: /' ;;
  install) "$REPO_ROOT/worklog/scripts/sync.sh" | sed 's/^/skill worklog: /' ;;
esac

# ---------- tone hook ---------------------------------------------------
if compgen -G "$SKILLS_DIR/*tone*" >/dev/null 2>&1; then
  note "tone skill: $(basename "$(compgen -G "$SKILLS_DIR/*tone*" | head -1)") found"
else
  warn "no personal *tone* skill installed — dev-context ship phase falls back to its default voice"
fi

# ---------- devboard data dir -------------------------------------------
if [[ -d "$DEVBOARD_DIR" ]]; then
  [[ "$mode" == install ]] && note "devboard data dir: $DEVBOARD_DIR"
else
  case "$mode" in
    check)   stale "devboard data dir missing: $DEVBOARD_DIR" ;;
    dryrun)  plan  "create $DEVBOARD_DIR" ;;
    install) mkdir -p "$DEVBOARD_DIR"; note "devboard data dir: created $DEVBOARD_DIR" ;;
  esac
fi

# ---------- optional, prompted (never in check/dry-run, never sans TTY) --
if [[ "$mode" == install && -t 0 ]]; then
  if ! grep -qs "dev-context" "$HOME/.claude/CLAUDE.md" 2>/dev/null; then
    read -r -p "Append the dev-context directive to ~/.claude/CLAUDE.md? [y/N] " a
    if [[ "$a" == [yY]* ]]; then
      mkdir -p "$HOME/.claude"
      cat "$REPO_ROOT/CLAUDE.md" >> "$HOME/.claude/CLAUDE.md"
      note "CLAUDE.md directive: appended"
    fi
  fi
  if command -v docker >/dev/null 2>&1 && ! docker ps --format '{{.Names}}' 2>/dev/null | grep -q devboard; then
    read -r -p "Build and start the devboard container now? [y/N] " a
    if [[ "$a" == [yY]* ]]; then
      ( cd "$REPO_ROOT/devboard" && docker compose up --build -d )
      note "devboard: running at http://localhost:8484"
    fi
  fi
elif [[ "$mode" == install ]]; then
  note "hint: rerun interactively to opt into the CLAUDE.md directive / devboard container"
fi

# ---------- verdict -----------------------------------------------------
if [[ "$mode" == check ]]; then
  if (( drift )); then note "check: drift found"; exit 1; fi
  note "check: everything current"
fi
[[ "$mode" == dryrun ]] && note "dry-run: nothing changed"
exit 0
