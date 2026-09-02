#!/usr/bin/env bash
# install.sh — bootstrap for `worklog install`.
#
# This script does only what a not-yet-built binary cannot: obtain the
# worklog binary (local `go build` today; latest release download lands in
# M3 of the install contract), then exec `worklog install`, where all real
# installer logic lives (target selection, skill deployment, drift checks,
# opt-in extras).
#
# Modes: (default) install/update · --check report drift, exit 1 ·
# --dry-run narrate, change nothing. In check/dry-run the script never
# builds: a missing or stale binary is REPORTED, and the rest of the check
# is delegated only when a binary exists to delegate to.
#
# Exit codes: 0 ok/current · 1 preflight or drift · 64 usage

set -euo pipefail

REPO_ROOT="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &>/dev/null && pwd )"
BIN="$HOME/.local/bin/worklog"

mode="install"
case "${1:-}" in
  -h|--help) sed -n '2,16p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  --check)   mode="check" ;;
  --dry-run) mode="dryrun" ;;
  "")        ;;
  *) echo "unknown arg: $1 (try --help)" >&2; exit 64 ;;
esac

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    echo "ERROR: native Windows isn't supported — run under WSL" >&2; exit 1 ;;
esac
"$REPO_ROOT/worklog/scripts/detect-platform.sh" >/dev/null \
  || { echo "ERROR: unsupported platform; nothing was installed" >&2; exit 1; }
command -v git >/dev/null 2>&1 || { echo "ERROR: git not found on PATH" >&2; exit 1; }

# The binary's rev stamp; algorithm mirrored in Go (installer.RepoRev).
rev="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo none)"
git -C "$REPO_ROOT" diff --quiet HEAD -- worklog 2>/dev/null || rev="$rev-dirty"
have="$("$BIN" --version 2>/dev/null | sed 's/^worklog version //' || true)"
current=false; [[ "$have" == *"($rev,"* ]] && current=true
# A -dirty stamp matches ANY dirty state, so it can never prove currency:
# install mode always rebuilds on a dirty tree (cheap; go caches).
[[ "$mode" == install && "$rev" == *-dirty ]] && current=false

if [[ "$mode" != "install" ]]; then
  # Bash owns binary drift: on a fresh clone there is no binary to ask.
  flag="--check"; [[ "$mode" == "dryrun" ]] && flag="--dry-run"
  if ! $current; then
    echo "drift: worklog binary ($BIN): have '${have:-absent}', want commit $rev"
    if [[ -x "$BIN" ]]; then
      "$BIN" install --repo "$REPO_ROOT" "$flag" || exit 1
    else
      echo "note: binary absent; skill state unknown until built (run ./install.sh)"
    fi
    exit 1
  fi
  exec "$BIN" install --repo "$REPO_ROOT" "$flag"
fi

command -v go >/dev/null 2>&1 \
  || { echo "ERROR: go toolchain not found — install Go >= 1.26; nothing was installed" >&2; exit 1; }

if ! $current; then
  mkdir -p "$(dirname "$BIN")"
  ( cd "$REPO_ROOT/worklog" && go build \
      -ldflags "-X main.version=0.2.0-dev -X main.commit=$rev -X 'main.date=$(date -u +%Y-%m-%d)'" \
      -o "$BIN" ./cmd/worklog )
  echo "installed: $BIN ($("$BIN" --version | sed 's/^worklog version //'))"
fi

exec "$BIN" install --repo "$REPO_ROOT"
