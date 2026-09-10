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
# It runs three ways: from a checkout, from a downloaded copy, and piped
# straight from curl with no checkout at all. The piped case is the one
# with teeth. There is no $0 to read and no BASH_SOURCE to derive a repo
# from, so everything that used to assume "the directory I live in is the
# checkout" has to ask instead of assume. REPO_ROOT is empty unless it
# resolves to a directory that actually looks like this repo, and every
# consumer of it is guarded on that.
#
# Skills come from the binary, not from here. `worklog install` carries an
# embedded copy and deploys from it when no checkout is around, so the
# bootstrap no longer has to hand it a --repo to be useful.
#
# Modes: (default) install/update · --check report drift, exit 1 ·
# --dry-run narrate, change nothing. Check/dry-run never build or
# download, though they do ask GitHub for the latest tag; a missing or
# unreplaceable binary is REPORTED, and the rest of the check is delegated
# only when a binary exists to delegate to.
#
# --uninstall hands straight over to `worklog uninstall` and returns. It is
# matched BEFORE the preflight on purpose: everything below this point
# exists to obtain a binary, and running it first would download or compile
# one in order to delete it. Pass --commit through to actually remove;
# without it the binary prints its plan and changes nothing.
#
# Exit codes: 0 ok/current · 1 preflight or drift · 64 usage

set -euo pipefail

# REPO_ROOT is a checkout or it is nothing. Under `curl | bash` there is no
# BASH_SOURCE at all, and `set -u` made that an abort on this script's first
# executable line — the piped install died here, before reaching any of the
# checkout dependencies it was supposed to be fixed for. The :- guard is what
# makes the pipe survivable; the go.mod test is what keeps a stray directory
# from being mistaken for the repo.
REPO_ROOT=""
if [[ -n "${BASH_SOURCE[0]:-}" ]]; then
  _self_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &>/dev/null && pwd )" || _self_dir=""
  if [[ -n "$_self_dir" && -f "$_self_dir/worklog/go.mod" ]]; then
    REPO_ROOT="$_self_dir"
  fi
  unset _self_dir
fi

# Passing --repo "" is not the same as passing nothing: the flag still
# consumes the empty string as its value, and the binary only falls through
# to its own resolution because it happens to trim first. Build the argument
# instead of emptying it.
repo_arg=()
[[ -n "$REPO_ROOT" ]] && repo_arg=(--repo "$REPO_ROOT")

BIN="$HOME/.local/bin/worklog"
RELEASE_BASE="https://github.com/prestontallen/ai-devboard/releases/latest/download"

# Help is a literal, not a slice of this file. It used to be
# `sed -n '2,33p' "$0"`, which needs $0 to name a readable file: under
# `curl | bash` that is "bash", so --help broke on exactly the path this
# script exists to support. Line numbers were the other half of the problem
# — any edit to the header silently retargeted the range.
usage() {
  cat <<'USAGE'
install.sh — bootstrap for `worklog install`.

Obtains the worklog binary, then execs `worklog install`, where all the
real installer logic lives. On a checkout with Go it builds from source;
otherwise it downloads the latest GitHub release for this platform and
verifies it against the published sha256.

Needs no checkout. Skills and the CLAUDE.md directive ship inside the
binary, so a curl-pipe install on a bare machine is a supported path:

  curl -fsSL https://github.com/prestontallen/ai-devboard/releases/latest/download/install.sh | bash

It never replaces a binary newer than what it would install, and it stops
a running devboard unit before writing over the binary that unit executes.

Modes:
  (default)     install or update
  --check       report drift and exit 1
  --dry-run     narrate, change nothing
  --uninstall   hand over to `worklog uninstall` (add --commit to remove)
  -h, --help    this text

Anything else is forwarded verbatim to `worklog install`, which is how a
fresh machine is set up headlessly:

  ... | bash -s -- --with-session-hook --with-claude-md

Exit codes: 0 ok/current · 1 preflight or drift · 64 usage
USAGE
}

mode="install"
# Anything this script does not itself act on is forwarded verbatim to
# `worklog install`. That is what lets one command set up a fresh machine
# headlessly (--with-session-hook, --with-claude-md, --with-devboard-service);
# previously any such flag was rejected here with exit 64 and never reached
# the binary that implements it. A genuine typo now surfaces as the binary's
# own unknown-flag error rather than this script's, which is a fair trade for
# not having to mirror the binary's flag set in bash.
forward=()
while (( $# )); do
  case "$1" in
    -h|--help) usage; exit 0 ;;
    --check)     mode="check" ;;
    --dry-run)   mode="dryrun" ;;
    --uninstall) mode="uninstall" ;;
    "")        ;;
    *) forward+=("$1") ;;
  esac
  shift
done

# Uninstall is a pure dispatch and deliberately reaches none of the machinery
# below: no platform probe, no git requirement, no release lookup, no build.
# The binary owns every decision, and it needs no checkout to make them — which
# is the whole reason uninstall is a subcommand rather than a flag on install.
if [[ "$mode" == "uninstall" ]]; then
  if [[ ! -x "$BIN" ]]; then
    echo "nothing to uninstall: no worklog binary at $BIN"
    exit 0
  fi
  exec "$BIN" uninstall "${forward[@]+"${forward[@]}"}"
fi

case "$(uname -s)" in
  MINGW*|MSYS*|CYGWIN*)
    echo "ERROR: native Windows isn't supported — run under WSL" >&2; exit 1 ;;
esac
# Platform detection is inlined rather than delegated to
# worklog/scripts/detect-platform.sh, which only a checkout has — and picking
# the release asset is precisely the no-checkout path, so the download branch
# cannot depend on a file that only exists when you did not need to download.
# The script itself stays: `make build` and scripts/build.sh still call it.
# Two copies of this mapping now exist, and they can drift. Both feed the same
# worklog_<os>_<arch> asset names, so drift surfaces as a failed download
# rather than a wrong binary.
detect_platform() {
  local os arch goos goarch
  os="$(uname -s)"; arch="$(uname -m)"
  case "$os" in
    Linux)  goos=linux ;;
    Darwin) goos=darwin ;;
    *) echo "unsupported OS: $os (want Linux or Darwin)" >&2; return 1 ;;
  esac
  case "$arch" in
    x86_64|amd64)  goarch=amd64 ;;
    aarch64|arm64) goarch=arm64 ;;
    *) echo "unsupported architecture: $arch (want amd64 or arm64)" >&2; return 1 ;;
  esac
  printf '%s-%s' "$goos" "$goarch"
}
platform="$(detect_platform)" \
  || { echo "ERROR: unsupported platform; nothing was installed" >&2; exit 1; }
asset="worklog_${platform/-/_}"

# git is a build-path requirement, not a universal one. Demanding it up front
# made a download-only machine — the whole point of the release path — fail
# preflight over a tool it was never going to use.
rev="none"
if [[ -n "$REPO_ROOT" ]] && command -v git >/dev/null 2>&1; then
  # Rev stamp; algorithm mirrored in Go (installer.RepoRev). Guarded on a real
  # checkout because `git -C ""` is not a no-op: it runs against the caller's
  # cwd, so an empty REPO_ROOT would stamp this binary from whatever unrelated
  # repository the user happened to be standing in.
  rev="$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null || echo none)"
  git -C "$REPO_ROOT" diff --quiet HEAD -- worklog 2>/dev/null || rev="$rev-dirty"
fi
have="$("$BIN" --version 2>/dev/null | sed 's/^worklog version //' || true)"

# Currency. The comparison itself lives in Go (internal/version), asked of the
# installed binary, because it is the code that destroyed a binary here: the
# shell only ever did string equality, so "different from the latest tag" and
# "older than it" were one state, and a build nine commits ahead was replaced
# by the release it contained. The shell now fetches and acts; it does not judge.
latest=""
# "absent" is its own relation. It used to fall through as "unknown", which
# is a claim about an installed binary's stamp, not about the absence of one
# — and "unknown" sets current=true, so a machine with no binary was judged
# up to date and obtained nothing. On a checkout the checkout-ahead probe
# below flipped that back by accident; with no checkout nothing did, so the
# bare-machine install exec'd a binary it had never written.
relation="absent"
if [[ -n "$have" ]]; then
  relation="unknown"
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
  absent)    : ;;   # nothing installed: obtain it
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
if [[ -n "$REPO_ROOT" ]] && command -v go >/dev/null 2>&1; then
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
      "$BIN" install "${repo_arg[@]+"${repo_arg[@]}"}" "$flag" "${forward[@]+"${forward[@]}"}" || exit 1
    else
      echo "note: binary absent; skill state unknown until installed (re-run without --check/--dry-run)"
    fi
    exit 1
  fi
  exec "$BIN" install "${repo_arg[@]+"${repo_arg[@]}"}" "$flag" "${forward[@]+"${forward[@]}"}"
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

# sha256sum is GNU coreutils and is not on a stock macOS; shasum is. The
# download path published darwin binaries and then verified them with a
# command those machines do not have, so release installs there could never
# get past this step. Prefer sha256sum, fall back to shasum, and refuse to
# install unverified if neither exists — a missing checksum tool is not a
# reason to trust the bytes.
verify_checksum() {
  local dir="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    ( cd "$dir" && sha256sum -c --ignore-missing --quiet checksums.txt )
  elif command -v shasum >/dev/null 2>&1; then
    # shasum has no --ignore-missing, so narrow the list to our asset first.
    # Anchored at end of line: an unanchored match would also select a longer
    # asset name that happens to start with this one.
    ( cd "$dir" && grep -E "[[:space:]]${asset}$" checksums.txt > expected.txt \
        && shasum -a 256 -c expected.txt >/dev/null )
  else
    echo "ERROR: no sha256sum or shasum on PATH; refusing to install unverified" >&2
    return 1
  fi
}

obtain_release() {
  command -v curl >/dev/null 2>&1 || return 1
  local tmp; tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  curl -fsSL --max-time 60 -o "$tmp/$asset" "$RELEASE_BASE/$asset" || return 1
  curl -fsSL --max-time 15 -o "$tmp/checksums.txt" "$RELEASE_BASE/checksums.txt" || return 1
  verify_checksum "$tmp" \
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
  # Guarded here as well as at both call sites: every path below joins onto
  # REPO_ROOT, and with an empty one this would `cd /worklog` or, worse,
  # resolve relative to the caller's cwd.
  [[ -n "$REPO_ROOT" ]] || return 1
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
  if [[ -n "$REPO_ROOT" ]] && command -v go >/dev/null 2>&1; then
    obtain_build
  else
    rc=0; obtain_release || rc=$?   # errexit-safe capture
    if (( rc == 2 )); then exit 1; fi        # checksum mismatch: hard stop
    if (( rc != 0 )); then
      # Only offer the build fallback when there is something to build from.
      # Announcing "falling back to local build" with no checkout named a
      # remedy that could not run, and buried the real cause (no network, or
      # no curl) under a Go error from a directory that does not exist.
      if [[ -z "$REPO_ROOT" ]]; then
        echo "ERROR: cannot obtain worklog — the release download failed and there is no checkout to build from." >&2
        echo "       Remedies: restore network access and re-run, or clone the repo and re-run from it." >&2
        exit 1
      fi
      echo "note: release download unavailable; falling back to local build"
      obtain_build || {
        echo "ERROR: cannot obtain worklog — no release download (network/curl) and no Go toolchain." >&2
        echo "       Remedies: install Go >= 1.26, or restore network access and re-run." >&2
        exit 1
      }
    fi
  fi
fi

exec "$BIN" install "${repo_arg[@]+"${repo_arg[@]}"}" "${forward[@]+"${forward[@]}"}"
