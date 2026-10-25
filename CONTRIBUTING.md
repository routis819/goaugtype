# Contributing to `goaugtype`

[English](./CONTRIBUTING.md) | [한국어](./CONTRIBUTING.ko.md)

Thank you for your interest in contributing to `goaugtype`! This document details the repository architecture, the upstream Go `src/go/types/` synchronization and patch pipeline, and step-by-step developer workflows.

---

## Table of Contents

- [Repository Architecture](#repository-architecture)
- [1:1 Upstream Mapping Table](#11-upstream-mapping-table)
- [Developer Workflows](#developer-workflows)
  - [1. Running Tests](#1-running-tests)
  - [2. Regenerating Ported Files from Pristine Snapshots](#2-regenerating-ported-files-from-pristine-snapshots)
  - [3. Modifying Ported `go/types` Files & Updating Patches](#3-modifying-ported-gotypes-files--updating-patches)
  - [4. Synchronizing with a New Upstream Go Release](#4-synchronizing-with-a-new-upstream-go-release)
  - [5. Release Verification & Tagging](#5-release-verification--tagging)
- [Engineering Guidelines](#engineering-guidelines)
- [Related Documentation](#related-documentation)

---

## Repository Architecture

```
goaugtype/
├── cmd/
│   ├── goaugadt/               # Standalone CLI driver (singlechecker)
│   └── upstreamchecker/        # Upstream drift detector, snapshot & patch manager
├── pkg/
│   └── analyzer/               # Core static analysis package
│       ├── adt.go              # Registry, SumTypeSet, SumTypeFact, //go:generate
│       ├── check.go            # AST collector, 8-path assignability, type switch checks
│       ├── doc.go              # Package analyzer documentation
│       ├── analyzer_test.go    # analysistest test runner
│       ├── testdata/           # Static test cases (simple, imported, typeset)
│       └── [7 ported files]   # Ported from src/go/types (see mapping table below)
├── internal/
│   └── upstream/
│       ├── testdata/gotypes/   # 100% byte-for-byte unmodified snapshots of src/go/types
│       └── patches/            # Unified diff (.patch) files for each ported file
├── scripts/
│   ├── check-upstream.sh       # 5-way consistency and patch drift verification
│   ├── apply-patches.sh        # Regenerates pkg/analyzer/*.go from snapshots + patches
│   ├── update-patches.sh       # Regenerates patches from local edits in pkg/analyzer/
│   ├── sync-upstream.sh        # Syncs snapshots with go.mod Go version & applies 3-way merge
│   └── release.sh              # Release gatekeeper pipeline (--check-only or <tag>)
├── docs/                       # Specifications, architecture, and diagrams
├── upstream.json               # Tracks base_revision and SHA-256 hashes of snapshots
└── go.mod
```

> [!IMPORTANT]
> Pristine Go standard library files are stored under `internal/upstream/testdata/gotypes/`. Because they reside in a `testdata` directory, Go compiler tools, `go test`, and `gopls` ignore their `package types` declarations, allowing them to remain 100% unmodified without polluting the build.

---

## 1:1 Upstream Mapping Table

`goaugtype` directly ports 7 files from the Go compiler's type checker (`src/go/types/`). Each file has an exact 1:1 relationship between the pristine upstream snapshot, its unified patch, and the generated target in `pkg/analyzer/`:

| Upstream `src/go/types/` File | Pristine Snapshot (`internal/upstream/testdata/gotypes/`) | Unified Patch (`internal/upstream/patches/`) | Generated File (`pkg/analyzer/`) | Ported Symbols & `[goaugtype]` Roles |
| :--- | :--- | :--- | :--- | :--- |
| `typeterm.go` | [`gotypes/typeterm.go`](./internal/upstream/testdata/gotypes/typeterm.go) | [`typeterm.patch`](./internal/upstream/patches/typeterm.patch) | [`typeterm.go`](./pkg/analyzer/typeterm.go) | `term`, `equal`, `union`, `intersect`, `includes`, `subsetOf`, `disjoint` — supports non-empty interface terms (`#5`). |
| `termlist.go` | [`gotypes/termlist.go`](./internal/upstream/testdata/gotypes/termlist.go) | [`termlist.patch`](./internal/upstream/patches/termlist.patch) | [`termlist.go`](./pkg/analyzer/termlist.go) | `termlist`, `allTermlist`, `norm`, `union`, `intersect`, `equal`, `includes`, `supersetOf`, `subsetOf` — 100% parity. |
| `typeset.go` | [`gotypes/typeset.go`](./internal/upstream/testdata/gotypes/typeset.go) | [`typeset.patch`](./internal/upstream/patches/typeset.patch) | [`typeset.go`](./pkg/analyzer/typeset.go) | `_TypeSet`, `IsEmpty`, `IsAll`, `IsMethodSet`, `NumMethods`, `Method`, `LookupMethod`, `hasTerms`, `subsetOf`, `computeInterfaceTypeSet`. |
| `union.go` | [`gotypes/union.go`](./internal/upstream/testdata/gotypes/union.go) | [`union.patch`](./internal/upstream/patches/union.patch) | [`union.go`](./pkg/analyzer/union.go) | `parseTilde` (rejects `~T` & `nil`), `overlappingTerm` (disjointness validation), `computeUnionTypeSet` (nested sum type flattening). |
| `instantiate.go` | [`gotypes/instantiate.go`](./internal/upstream/testdata/gotypes/instantiate.go) | [`instantiate.patch`](./internal/upstream/patches/instantiate.patch) | [`instantiate.go`](./pkg/analyzer/instantiate.go) | `(reg *Registry) implements` — verifies method set contracts (`#9`) and term set inclusion/subset. |
| `operand.go` | [`gotypes/operand.go`](./internal/upstream/testdata/gotypes/operand.go) | [`operand.patch`](./internal/upstream/patches/operand.patch) | [`pkg/analyzer/operand.go`](./pkg/analyzer/operand.go) | `operandMode`, `operand`, `newOperand`, `(*operand).assignableTo` — hooks `Registry.Lookup` for sum type assignability. |
| `assignments.go` | [`gotypes/assignments.go`](./internal/upstream/testdata/gotypes/assignments.go) | [`assignments.patch`](./internal/upstream/patches/assignments.patch) | [`assignments.go`](./pkg/analyzer/assignments.go) | `assignment`, `initVar`, `assignVar`, `unpackRecv`, `initVars`, `assignVars`, `returnVars` — handles 1:1 and tuple assignments. |

---

## Developer Workflows

### 1. Running Tests

Run the full analysis test suite across all test packages:

```bash
go test -v ./...
```

To run individual test packages:
```bash
go test -v ./pkg/analyzer
```

### 2. Regenerating Ported Files from Pristine Snapshots

To cleanly restore and apply patches to all 7 files in `pkg/analyzer/`:

```bash
# Using go generate:
go generate ./...

# Or using the helper script:
./scripts/apply-patches.sh
```

### 3. Modifying Ported `go/types` Files & Updating Patches

When adding features or fixing bugs in any of the 7 ported files:

1. **Edit the target file** in `pkg/analyzer/<file>.go`.
2. **Mark your modifications** clearly with comment headers:
   ```go
   // [goaugtype]: Custom sum type handling...
   ```
3. **Regenerate the patch files**:
   ```bash
   ./scripts/update-patches.sh
   ```
4. **Verify 5-way consistency and patch drift**:
   ```bash
   ./scripts/check-upstream.sh -target local
   ```
5. **Run tests**:
   ```bash
   go test -v ./...
   ```

### 4. Synchronizing with a New Upstream Go Release

**Principle**: The `go` version directive in [`go.mod`](./go.mod) is the canonical single source of truth. `UpstreamBaseRevision` in [`pkg/analyzer/adt.go`](./pkg/analyzer/adt.go) and `base_revision` in [`upstream.json`](./upstream.json) must always match the Go version declared in `go.mod` (`"go" + <go.mod version>`).

When upgrading the project's Go version in `go.mod` (e.g., to `1.28.0`):

1. **Update the `go` version in [`go.mod`](./go.mod)**:
   ```go
   go 1.28.0
   ```
2. **Align `UpstreamBaseRevision` with `go.mod`** in [`pkg/analyzer/adt.go`](./pkg/analyzer/adt.go):
   ```go
   const UpstreamBaseRevision = "go1.28.0"
   ```
3. **Synchronize snapshots and apply 3-way merge** (without needing `-target`, as it automatically resolves from `go.mod`):
   ```bash
   ./scripts/sync-upstream.sh
   ```
   This command:
   - Reads the target Go version directly from `go.mod` (`go1.28.0`).
   - Fetches the new upstream files from `golang/go`.
   - Updates `internal/upstream/testdata/gotypes/*.go`.
   - Computes new SHA-256 hashes in `upstream.json`.
   - Applies existing patches via `git apply --3way`.
4. **Refresh patches** if line offsets shifted:
   ```bash
   ./scripts/update-patches.sh
   ```
5. **Verify everything passes**:
   ```bash
   ./scripts/release.sh --check-only
   ```

### 5. Release Verification & Tagging

Before publishing a release, execute the verification pipeline:

```bash
# Dry run verification:
./scripts/release.sh --check-only

# Tag a new release (requires clean working tree):
./scripts/release.sh v0.3.0

# Publish tag:
git push origin v0.3.0
```

---

## Engineering Guidelines

1. **Align `UpstreamBaseRevision` with `go.mod`**: `UpstreamBaseRevision` in [`pkg/analyzer/adt.go`](./pkg/analyzer/adt.go) and `base_revision` in [`upstream.json`](./upstream.json) must always match the `go` version specified in [`go.mod`](./go.mod) (e.g., `go 1.27.3` → `UpstreamBaseRevision = "go1.27.3"`).
2. **Maintain Minimal Diff**: When modifying ported files, keep diffs against upstream minimal. Mark all custom code with `// [goaugtype]`.
3. **Preserve Disjoint Union Invariant**: Every sum type must strictly enforce pairwise disjointness across all flattened terms. Never allow `any` or subtype collisions ($C \subset I$, $I_1 \subseteq I_2$) in sum types.
4. **Verify with `gopls`**: Ensure zero diagnostics across all packages prior to submitting pull requests.
5. **Always Update Patches**: Never commit modifications to `pkg/analyzer/{typeterm,termlist,typeset,union,instantiate,operand,assignments}.go` without running `./scripts/update-patches.sh` and committing the resulting `internal/upstream/patches/*.patch` files.

---

## Related Documentation

- [Design Specification (`DESIGN.md`)](./docs/DESIGN.md) | [설계 명세서 (`DESIGN.ko.md`)](./docs/DESIGN.ko.md)
- [Architecture Overview (`ARCHITECTURE.md`)](./docs/ARCHITECTURE.md) | [아키텍처 문서 (`ARCHITECTURE.ko.md`)](./docs/ARCHITECTURE.ko.md)
