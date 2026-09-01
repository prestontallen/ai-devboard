#!/usr/bin/env bash
# build.sh — detect host platform and build ./worklog via the Makefile.
#
# Usage:
#   ./scripts/build.sh
#   ./scripts/build.sh -- <extra make args>

set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

platform="$("$root/scripts/detect-platform.sh")"
echo "build: detected platform ${platform}" >&2
exec make "$platform" OUTPUT="./worklog" "$@"
