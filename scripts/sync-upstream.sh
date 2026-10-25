#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

usage() {
  cat <<EOF
Usage: $(basename "$0") [flags]

Synchronizes internal/upstream/testdata/gotypes/*.go and upstream.json with the latest
golang/go release tag (or a target revision specified via -target), applies patches
to pkg/analyzer via 3-way merge, and refreshes the unified diff patches.

Flags:
  -target <rev>    Target revision to synchronize with (empty for latest release tag, 'local' for GOROOT, or explicit tag/branch/commit)
  -config <path>   Path to config file (default: upstream.json)
  -h, --help       Show this help message
EOF
  exit 0
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
fi

cd "${PROJECT_ROOT}"
exec go run ./cmd/upstreamchecker -config upstream.json -update "$@"
