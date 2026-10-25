#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

usage() {
  cat <<EOF
Usage: $(basename "$0") [flags]

Regenerates unified diff patches in internal/upstream/patches/*.patch from local edits
made to pkg/analyzer/{typeterm,termlist,typeset,union,instantiate,operand,assignments}.go
compared against pristine snapshots in internal/upstream/testdata/gotypes/*.go.

Flags:
  -config <path>   Path to config file (default: upstream.json)
  -h, --help       Show this help message
EOF
  exit 0
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
fi

cd "${PROJECT_ROOT}"
exec go run ./cmd/upstreamchecker -config upstream.json -update-patches "$@"
