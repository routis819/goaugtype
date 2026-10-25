#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${PROJECT_ROOT}"

usage() {
  cat <<EOF >&2
Usage:
  $0 <tag>          Create an annotated release tag (e.g. v0.3.0) after running verification
  $0 --check-only   Run release verification checks without tagging
  $0 -h, --help     Show this help message
EOF
  exit "${1:-1}"
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage 0
fi

if [[ $# -lt 1 ]]; then
  usage 1
fi

CHECK_ONLY=false
TAG=""

if [[ "$1" == "--check-only" ]]; then
  CHECK_ONLY=true
else
  TAG="$1"
  if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
    echo "Error: Tag '$TAG' is invalid. Tag must match pattern ^v[0-9]+\.[0-9]+\.[0-9]+ (e.g. v0.3.0)" >&2
    usage
  fi
fi

FAILED=0

echo "=== Release Verification Pipeline ==="

# Step 1: Verify working tree is clean unless --check-only is passed.
echo "==> Step 1: Verifying working tree status..."
if [[ "$CHECK_ONLY" == false ]]; then
  if [[ -n "$(git status --porcelain)" ]]; then
    echo "Error: Working tree is not clean. Commit or stash changes before releasing." >&2
    git status --short >&2
    exit 1
  fi
  echo "Working tree is clean."
else
  echo "Check-only mode: skipping clean working tree requirement."
  if [[ -n "$(git status --porcelain)" ]]; then
    echo "Notice: Working tree has uncommitted modifications."
  fi
fi

# Step 2: Verify implementation and upstream.json match configured base_revision.
echo "==> Step 2: Verifying base_revision consistency..."
BASE_REVISION="$(grep '"base_revision"' upstream.json | sed -E 's/.*"base_revision":[[:space:]]*"([^"]+)".*/\1/')"
if [[ -z "$BASE_REVISION" ]]; then
  echo "Error: Could not extract base_revision from upstream.json" >&2
  exit 1
fi
echo "Configured base_revision: ${BASE_REVISION}"
if ! ./scripts/check-upstream.sh -target "${BASE_REVISION}"; then
  echo "Error: Step 2 verification failed: tracked files or implementation mismatch base_revision (${BASE_REVISION})." >&2
  FAILED=1
  if [[ "$CHECK_ONLY" == false ]]; then
    exit 1
  fi
fi

# Step 3: Verify upstream.json and implementation match latest official Go release tag.
echo "==> Step 3: Verifying synchronization with latest official Go release..."
if ! ./scripts/check-upstream.sh; then
  echo "Warning/Error: Step 3 verification failed: repository is not synchronized with latest official Go release." >&2
  FAILED=1
  if [[ "$CHECK_ONLY" == false ]]; then
    exit 1
  fi
fi

# Step 4: Run full test suite.
echo "==> Step 4: Running test suite..."
if ! go test -v ./...; then
  echo "Error: Step 4 test suite failed." >&2
  FAILED=1
  if [[ "$CHECK_ONLY" == false ]]; then
    exit 1
  fi
fi

# Summary for check-only mode
if [[ "$CHECK_ONLY" == true ]]; then
  echo "======================================"
  if [[ "$FAILED" -ne 0 ]]; then
    echo "Release check completed with failures." >&2
    exit 1
  fi
  echo "All release checks passed successfully."
  exit 0
fi

# Step 5: Create annotated git tag.
echo "==> Step 5: Creating annotated git tag ${TAG}..."
git tag -a "${TAG}" -m "Release ${TAG}"
echo "Successfully tagged release ${TAG}."
echo ""
echo "To publish this release tag, run:"
echo "  git push origin ${TAG}"
