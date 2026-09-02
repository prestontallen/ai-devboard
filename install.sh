#!/usr/bin/env bash
# install.sh — bootstrap for `worklog install`.
#
# Obtains the worklog binary, then execs `worklog install` where all real
# installer logic lives. Binary acquisition is release-first: download the
# latest GitHub release for this platform (sha256-verified), falling back
# to a local `go build` when the download isn't possible. A dev-stamped
# binary on a checkout with Go keeps the build path, so development
# machines are never silently switched onto release binaries.
#
# Modes: (default) install/update · --check report drift, exit 1 ·
# --dry-run narrate, change nothing. Check/dry-run never build or
# download; a missing or stale binary is REPORTED, and the rest of the
# check is delegated only when a binary exists to delegate to.
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

# Mode-aware currency: dev/snapshot stamps compare against the repo rev;
# release stamps compare against the latest published tag.
dev_stamped=false; [[ "$have" == *-dev* || "$have" == *-snapshot* ]] && dev_stamped=true
current=false
# Only the release branch below fetches a tag, but the drift report reads this
# on every path — including a dev stamp that no longer matches the checkout,
# and an absent binary. Empty is what makes that report say "commit <rev>".
latest=""
if $dev_stamped; then
  [[ "$have" == *"($rev,"* ]] && current=true
  # a -dirty stamp matches ANY dirty state; install mode always rebuilds
  [[ "$mode" == install && "$rev" == *-dirty ]] && current=false
elif [[ -n "$have" ]]; then
  latest="$(curl -fsSL --max-time 5 https://api.github.com/repos/prestontallen/ai-devboard/releases/latest 2>/dev/null \
            | grep -m1 '"tag_name"' | sed 's/.*"v\{0,1\}\([^"]*\)".*/\1/' || true)"
  if [[ -z "$latest" ]]; then
    current=true   # offline: cannot judge; do not thrash
    echo "note: cannot reach GitHub to compare release versions; assuming current"
  elif [[ "$have" == "$latest "* || "$have" == "v$latest "* ]]; then
    current=true
  fi
  # Surface binary/checkout skew: skills always deploy from the checkout,
  # but binary features lag until the next release or a local build.
  if $current && [[ "$rev" == *-dirty ]]; then
    echo "note: release binary v${have%% *} predates local worklog changes; 'go build' path available"
  fi
fi

if [[ "$mode" != "install" ]]; then
  flag="--check"; [[ "$mode" == "dryrun" ]] && flag="--dry-run"
  if ! $current; then
    want="commit $rev"; [[ -n "$latest" ]] && want="release v$latest"
    echo "drift: worklog binary ($BIN): have '${have:-absent}', want $want"
    if [[ -x "$BIN" ]]; then
      "$BIN" install --repo "$REPO_ROOT" "$flag" || exit 1
    else
      echo "note: binary absent; skill state unknown until installed (run ./install.sh)"
    fi
    exit 1
  fi
  exec "$BIN" install --repo "$REPO_ROOT" "$flag"
fi

obtain_release() {
  command -v curl >/dev/null 2>&1 || return 1
  local tmp; tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN
  curl -fsSL --max-time 60 -o "$tmp/$asset" "$RELEASE_BASE/$asset" || return 1
  curl -fsSL --max-time 15 -o "$tmp/checksums.txt" "$RELEASE_BASE/checksums.txt" || return 1
  ( cd "$tmp" && sha256sum -c --ignore-missing --quiet checksums.txt ) \
    || { echo "ERROR: checksum mismatch for $asset — refusing the download" >&2; return 2; }
  mkdir -p "$(dirname "$BIN")"
  install -m 0755 "$tmp/$asset" "$BIN"
  echo "installed: $BIN ($("$BIN" --version | sed 's/^worklog version //')) [release]"
}

obtain_build() {
  command -v go >/dev/null 2>&1 || return 1
  # Dev stamp tracks the latest tag, so it never goes stale across releases.
  local ver; ver="$(git -C "$REPO_ROOT" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')"
  mkdir -p "$(dirname "$BIN")"
  ( cd "$REPO_ROOT/worklog" && go build \
      -ldflags "-X main.version=${ver:-0.0.0}-dev -X main.commit=$rev -X 'main.date=$(date -u +%Y-%m-%d)'" \
      -o "$BIN" ./cmd/worklog )
  echo "installed: $BIN ($("$BIN" --version | sed 's/^worklog version //')) [local build]"
}

if ! $current; then
  if $dev_stamped && command -v go >/dev/null 2>&1; then
    obtain_build   # dev machines stay on the build path
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
