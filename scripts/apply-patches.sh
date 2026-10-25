#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

usage() {
  cat <<EOF
Usage: $(basename "$0") [flags]

Applies patches from internal/upstream/patches/*.patch onto pristine upstream snapshots
in internal/upstream/testdata/gotypes/*.go, regenerating the 7 ported files in pkg/analyzer/.

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
exec go run ./cmd/upstreamchecker -config upstream.json -apply-patches "$@"
