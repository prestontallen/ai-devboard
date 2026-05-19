#!/usr/bin/env bash
# detect-platform.sh — print a Makefile platform target for this host.
#
# Supported outputs (one line, no trailing newline issues for Make):
#   linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64
#
# Usage:
#   ./scripts/detect-platform.sh
#   make "$(./scripts/detect-platform.sh)"

set -euo pipefail

os="$(uname -s)"
case "$os" in
	Linux)  goos=linux ;;
	Darwin) goos=darwin ;;
	*)
		echo "detect-platform: unsupported OS: $os (want Linux or Darwin)" >&2
		exit 1
		;;
esac

arch="$(uname -m)"
case "$arch" in
	x86_64|amd64) goarch=amd64 ;;
	aarch64|arm64) goarch=arm64 ;;
	*)
		echo "detect-platform: unsupported architecture: $arch (want amd64 or arm64)" >&2
		exit 1
		;;
esac

echo "${goos}-${goarch}"
