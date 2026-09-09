#!/usr/bin/env bash
# install.sh — bootstrap for `worklog install`.
#
# Obtains the worklog binary, then execs `worklog install` where all real
# installer logic lives. On a checkout with Go it builds from source, which
# is the newest thing available; otherwise it downloads the latest GitHub
# release for this platform (sha256-verified).
#
# It NEVER replaces a binary that is newer than what it would install. The
# comparison is not done here: it is asked of the installed binary, which
# knows its own stamp (`worklog install --relate`). A binary it cannot
# compare is left alone rather than guessed at. This is not a nicety — the
# previous version compared version strings for equality, so "different
# from the latest tag" and "older than it" were one state, and it replaced
# a build nine commits ahead with the release that build contained.
#
# A running devboard unit is stopped before the binary is written and
# restarted after: the unit executes that exact path, so writing over it
# while it runs fails, and the failure leaves the OLD binary serving.
#
# Modes: (default) install/update · --check report drift, exit 1 ·
# --dry-run narrate, change nothing. Check/dry-run never build or
# download, though they do ask GitHub for the latest tag; a missing or
# unreplaceable binary is REPORTED, and the rest of the check is delegated
# only when a binary exists to delegate to.
#
# Exit codes: 0 ok/current · 1 preflight or drift · 64 usage

set -euo pipefail

REPO_ROOT="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &>/dev/null && pwd )"
BIN="$HOME/.local/bin/worklog"
RELEASE_BASE="https://github.com/prestontallen/ai-devboard/releases/latest/download"

mode="install"
case "${1:-}" in
  -h|--help) sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
  --check)   mode="check" ;;
  --dry-run) mode="dryrun" ;;
  "")        ;;
  *) echo "unknown arg: $1 (try --help)" >&2; exit 64 ;;
esac

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    echo "ERROR: native Windows isn't supported — run under WSL" >&2; exit 1 ;;
esac
platform="$("$REPO_ROOT/worklog/scripts/detect-platform.sh")" \
  || { echo "ERROR: unsupported platform; nothing was installed" >&2; exit 1; }
asset="worklog_${platform/-/_}"
command -v git >/dev/null 2>&1 || { echo "ERROR: git not found on PATH" >&2; exit 1; }

# Rev stamp; algorithm mirrored in Go (installer.RepoRev).
rev="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo none)"
git -C "$REPO_ROOT" diff --quiet HEAD -- worklog 2>/dev/null || rev="$rev-dirty"
have="$("$BIN" --version 2>/dev/null | sed 's/^worklog version //' || true)"

# Currency. The comparison itself lives in Go (internal/version), asked of the
# installed binary, because it is the code that destroyed a binary here: the
# shell only ever did string equality, so "different from the latest tag" and
# "older than it" were one state, and a build nine commits ahead was replaced
# by the release it contained. The shell now fetches and acts; it does not judge.
latest=""
relation="unknown"
if [[ -n "$have" ]]; then
  latest="$(curl -fsSL --max-time 5 https://api.github.com/repos/prestontallen/ai-devboard/releases/latest 2>/dev/null \
            | grep -m1 '"tag_name"' | sed 's/.*"\(v\{0,1\}[^"]*\)".*/\1/' || true)"
  if [[ -z "$latest" ]]; then
    # Fail open. Under a never-downgrade rule the safe answer when we cannot
    # judge is to leave the working binary alone.
    relation="same"
    echo "note: cannot reach GitHub to compare versions; leaving the installed binary alone"
  else
    # An older binary does not know --relate. That is not the same as an
    # uncomparable stamp, and saying so saves the reader chasing the wrong fix.
    if relation="$("$BIN" install --relate "$latest" 2>/dev/null)"; then :; else
      relation="pre-guard"
    fi
  fi
fi

current=false
case "$relation" in
  same)      current=true ;;
  downgrade) current=true
             echo "note: installed worklog ${have%% *} is NEWER than release $latest; keeping it" ;;
  unknown)   current=true
             echo "note: installed worklog '${have%% *}' carries no comparable version; keeping it"
             echo "      (rebuild with 'make build' or ./install.sh for a stamp the guard can order)" ;;
  pre-guard) current=true
             echo "note: installed worklog '${have%% *}' predates the version guard and cannot be compared; keeping it"
             echo "      (rebuild with ./install.sh once, and future runs can compare)" ;;
esac

# The release is only half the question. A development machine also has to
# pick up its own checkout, and comparing solely against the published tag
# silently drops that — a regression in the first cut of this guard, caught
# before it shipped. Source on a machine with Go is the newest thing there
# is, so a checkout ahead of the installed binary rebuilds regardless of what
# the release comparison said.
checkout_ahead=false
if [[ -d "$REPO_ROOT/worklog" ]] && command -v go >/dev/null 2>&1; then
  have_commit="$(sed -n 's/.*(\([^,]*\),.*/\1/p' <<<"$have")"
  if [[ "$rev" == *-dirty || -z "$have_commit" || "$have_commit" != "$rev" ]]; then
    checkout_ahead=true
    current=false
  fi
fi

if [[ "$mode" != "install" ]]; then
  flag="--check"; [[ "$mode" == "dryrun" ]] && flag="--dry-run"
  if ! $current; then
    if [[ -z "$have" ]]; then
      echo "absent: worklog binary ($BIN) is not installed"
    elif $checkout_ahead; then
      echo "stale: worklog binary ($BIN) is behind the checkout (binary $have_commit, repo $rev)"
    else
      echo "$relation: worklog binary ($BIN): have '${have%% *}', release ${latest:-unknown}"
    fi
    if [[ -x "$BIN" ]]; then
      "$BIN" install --repo "$REPO_ROOT" "$flag" || exit 1
    else
      echo "note: binary absent; skill state unknown until installed (run ./install.sh)"
    fi
    exit 1
  fi
  exec "$BIN" install --repo "$REPO_ROOT" "$flag"
fi


# The devboard unit runs $BIN, so writing over it while the unit is up fails
# with ETXTBSY — and the failure is quiet in the worst way: the install stops,
# the OLD binary keeps running, and the next write goes wherever that old
# binary thinks the store lives. That happened here on 2026-09-08, right after
# the store moved. Stop, write, start.
devboard_was_running=false
stop_devboard() {
  command -v systemctl >/dev/null 2>&1 || return 0
  systemctl --user is-active --quiet devboard.service 2>/dev/null || return 0
  devboard_was_running=true
  echo "stopping devboard.service (it holds $BIN open)"
  systemctl --user stop devboard.service
}
start_devboard() {
  $devboard_was_running || return 0
  systemctl --user start devboard.service && echo "restarted devboard.service"
}

obtain_release() {
  command -v curl >/dev/null 2>&1 || return 1
  local tmp; tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  curl -fsSL --max-time 60 -o "$tmp/$asset" "$RELEASE_BASE/$asset" || return 1
  curl -fsSL --max-time 15 -o "$tmp/checksums.txt" "$RELEASE_BASE/checksums.txt" || return 1
  ( cd "$tmp" && sha256sum -c --ignore-missing --quiet checksums.txt ) \
    || { echo "ERROR: checksum mismatch for $asset — refusing the download" >&2; return 2; }
  mkdir -p "$(dirname "$BIN")"
  stop_devboard
  if ! install -m 0755 "$tmp/$asset" "$BIN"; then
    start_devboard
    echo "ERROR: could not write $BIN" >&2
    return 3
  fi
  start_devboard
  echo "installed: $BIN ($("$BIN" --version | sed 's/^worklog version //')) [release]"
}

obtain_build() {
  command -v go >/dev/null 2>&1 || return 1
  # Dev stamp tracks the latest tag, so it never goes stale across releases.
  local ver; ver="$(git -C "$REPO_ROOT" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')"
  mkdir -p "$(dirname "$BIN")"
  stop_devboard
  ( cd "$REPO_ROOT/worklog" && go build \
      -ldflags "-X main.version=$(git -C "$REPO_ROOT" describe --tags --long --always --dirty 2>/dev/null || echo dev) -X main.commit=$rev -X 'main.date=$(date -u +%Y-%m-%dT%H:%M:%SZ)'" \
      -o "$BIN" ./cmd/worklog ) || { start_devboard; echo "ERROR: build failed; $BIN unchanged" >&2; return 1; }
  start_devboard
  echo "installed: $BIN ($("$BIN" --version | sed 's/^worklog version //')) [local build]"
}

if ! $current; then
  # A checkout with Go builds from source: that is the newest thing available
  # and it is what a development machine wants. Everything else downloads.
  if command -v go >/dev/null 2>&1 && [[ -d "$REPO_ROOT/worklog" ]]; then
    obtain_build
  else
    rc=0; obtain_release || rc=$?   # errexit-safe capture
    if (( rc == 2 )); then exit 1; fi        # checksum mismatch: hard stop
    if (( rc != 0 )); then
      echo "note: release download unavailable; falling back to local build"
      obtain_build || {
        echo "ERROR: cannot obtain worklog — no release download (network/curl) and no Go toolchain." >&2
        echo "       Remedies: install Go >= 1.26, or restore network access and re-run." >&2
        exit 1
      }
    fi
  fi
fi

exec "$BIN" install --repo "$REPO_ROOT"
