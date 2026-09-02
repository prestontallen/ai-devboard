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

# ---------- skill deployment targets ------------------------------------
# One mechanism for every agent: full copies of each skill dir, drift caught
# by --check and healed by re-running. Targets come from a saved config
# (written by the interactive prompt) or, absent that, from detecting known
# agent dirs. Edit the config or rerun interactively to change targets.
TARGETS_CONF="${XDG_CONFIG_HOME:-$HOME/.config}/ai-devboard/targets"

detect_targets() {
  local d
  for d in "$HOME/.claude" "$HOME/.cursor" "$HOME/.windsurf" "$HOME/.codex"; do
    [[ -d "$d" ]] && printf '%s/skills\n' "$d"
  done
  return 0
}

prompt_targets() { # writes config; prints chosen targets on stdout
  local sel=() dets=() d a custom extra
  echo "Select skill install targets (saved to $TARGETS_CONF):" >&2
  # NOTE: no loop-wide redirect here — the answer reads must hit stdin,
  # not the detection list.
  mapfile -t dets < <(detect_targets)
  for d in "${dets[@]}"; do
    read -r -p "  deploy to $d? [Y/n] " a || a=""
    [[ "$a" == [nN]* ]] || sel+=("$d")
  done
  read -r -p "  additional paths (comma-separated, blank for none): " custom || custom=""
  if [[ -n "$custom" ]]; then
    IFS=',' read -ra extra <<<"$custom"
    for d in "${extra[@]}"; do
      d="$(echo "$d" | xargs)"
      [[ -n "$d" ]] && sel+=("${d/#\~/$HOME}")
    done
  fi
  mkdir -p "$(dirname "$TARGETS_CONF")"
  printf '%s\n' "${sel[@]}" > "$TARGETS_CONF"
  echo "targets saved; edit $TARGETS_CONF or rerun interactively to change" >&2
  printf '%s\n' "${sel[@]}"
}

load_targets() {
  if [[ -f "$TARGETS_CONF" ]]; then
    grep -vE '^[[:space:]]*(#|$)' "$TARGETS_CONF" || true
  elif [[ "$mode" == install ]] && { [[ -t 0 ]] || [[ -n "${INSTALL_PROMPT_FORCE:-}" ]]; }; then
    prompt_targets
  else
    detect_targets
  fi
}

mapfile -t TARGETS < <(load_targets)
[[ -f "$TARGETS_CONF" ]] || note "targets: using detected agent dirs (no $TARGETS_CONF yet; run interactively to choose)"
(( ${#TARGETS[@]} )) || warn "no skill targets detected or configured; skills not deployed"

# deploy_dir/deploy_file: copy with diff-verified idempotency. Legacy
# symlinks from older installs are migrated to copies.
deploy_dir() {
  local src="$1" dst="$2" label="$3"
  if [[ -L "$dst" ]]; then
    case "$mode" in
      check)  stale "$label: legacy symlink (re-run install to convert to a copy)"; return 0 ;;
      dryrun) plan  "replace symlink $dst with a copy"; return 0 ;;
      install) rm "$dst" ;;
    esac
  fi
  if diff -rq "$src" "$dst" >/dev/null 2>&1; then
    [[ "$mode" == install ]] && note "$label: up to date"
    return 0
  fi
  case "$mode" in
    check)  stale "$label: missing or differs from repo" ;;
    dryrun) plan  "copy $src -> $dst" ;;
    install)
      [[ -n "$dst" && "$dst" != "/" ]] || fail "deploy_dir: refusing bad destination"
      rm -rf "$dst"; mkdir -p "$dst"; cp -R "$src/." "$dst/"
      note "$label: copied" ;;
  esac
}

deploy_file() {
  local src="$1" dst="$2" label="$3"
  if diff -q "$src" "$dst" >/dev/null 2>&1; then
    [[ "$mode" == install ]] && note "$label: up to date"
    return 0
  fi
  case "$mode" in
    check)  stale "$label: missing or differs from repo" ;;
    dryrun) plan  "copy $src -> $dst" ;;
    install) mkdir -p "$(dirname "$dst")"; cp "$src" "$dst"; note "$label: copied" ;;
  esac
}

for tdir in "${TARGETS[@]}"; do
  for skill in dev-context contract fan-out; do
    deploy_dir "$REPO_ROOT/$skill" "$tdir/$skill" "skill $skill -> $tdir"
  done
  deploy_file "$REPO_ROOT/worklog/skill/SKILL.md" "$tdir/worklog/SKILL.md" "skill worklog -> $tdir"
  # claude-specific extra: the /worklog slash-command file
  if [[ "$tdir" == "$HOME/.claude/skills" ]]; then
    deploy_file "$REPO_ROOT/worklog/skill/claude/command.md" "$HOME/.claude/commands/worklog.md" "command worklog -> ~/.claude/commands"
  fi
done

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
