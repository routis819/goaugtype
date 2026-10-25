#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

usage() {
  cat <<EOF
Usage: $(basename "$0") [flags]

Verifies 5-way upstream consistency between:
  1. go version directive in go.mod
  2. analyzer.UpstreamBaseRevision in pkg/analyzer/adt.go
  3. base_revision and files[].sha256 in upstream.json
  4. internal/upstream/testdata/gotypes/*.go and internal/upstream/patches/*.patch drift check
  5. Target revision (default: go version from go.mod; 'latest' for latest GitHub release; 'local' for GOROOT; or explicit tag/commit)

Flags:
  -config <path>   Path to config file (default: upstream.json)
  -target <rev>    Target revision to compare against (default: go version from go.mod; 'latest' for latest GitHub release; 'local' for GOROOT; or explicit tag/commit)
  -h, --help       Show this help message
EOF
  exit 0
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
fi

cd "${PROJECT_ROOT}"
exec go run ./cmd/upstreamchecker -config upstream.json "$@"
