#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

usage() {
  cat <<EOF
Usage: $(basename "$0") [flags]

Verifies 4-way upstream consistency between:
  1. analyzer.UpstreamBaseRevision in pkg/analyzer/adt.go
  2. base_revision and file_hashes in upstream.json
  3. internal/upstream/testdata/gotypes/*.go and internal/upstream/patches/*.patch drift check
  4. Target revision (local GOROOT or remote golang/go release tag)

Flags:
  -config <path>   Path to config file (default: upstream.json)
  -target <rev>    Target revision to compare against (empty for latest release tag, 'local' for GOROOT, or explicit tag/branch/commit)
  -h, --help       Show this help message
EOF
  exit 0
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
fi

cd "${PROJECT_ROOT}"
exec go run ./cmd/upstreamchecker -config upstream.json "$@"
