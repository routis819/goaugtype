# `goaugtype` Design Document: Sum Types Based on General Interfaces (#57644)

## 1. Background & Vision

`goaugtype` (`goaugadt`) is a static analysis linter built on `golang.org/x/tools/go/analysis` that brings **Sum Types (Algebraic Data Types)** to Go.

To resolve specification ambiguities and soundness holes identified in the initial prototype, this document establishes the specification and architecture of `goaugtype` based strictly on the **pure model of [golang/go#57644 (`proposal: spec: sum types based on general interfaces`)](https://github.com/golang/go/issues/57644)**, proposed by Ian Lance Taylor of the Go team.

### Core Design Philosophy
1. **Pure Pre-implementation of `golang/go#57644`**:
   - Proposal `#57644` extends type unions (`T1 | T2`), which are currently restricted to generic type parameter constraints, to **ordinary (general) interfaces** used as variable types.
   - Because the current Go compiler rejects union elements inside ordinary interfaces used as variable types (`interface contains type constraints`), `goaugtype` uses the `// goaugtype: T1 | T2` directive on standard Go interface declarations (`type I any` or `type I interface { ... }`) to **statically emulate the exact Type Set semantics of `#57644`**.
2. **Consistent Type Set Semantics**:
   - Rather than inventing an ad-hoc type system or custom `nil` annotations, sum types are defined as a natural extension of Go 1.18+ **Interface Type Sets, `Implements`, and `Assignability` rules**.
3. **Static Exhaustiveness Checking**:
   - While `#57644` deferred compiler-enforced `type switch` exhaustiveness for backward compatibility, `goaugtype` acts as a strict linter that verifies whether `type switch` statements exhaustively handle all elements in a sum type's type set plus the interface zero value (`nil`).

---

## 2. Specification Aligned with #57644 (Pure Model)

### 2.1. General Interface Support
> **From #57644**: *"We propose that we permit an ordinary interface type to embed a union of terms, where each term is itself a type."*

- **Legacy Constraint (Deprecated)**: Previously, [`analysisTypeSpec`](../pkg/analyzer/check.go) only permitted `type T any` or empty `interface{}` declarations without methods.
- **Updated Specification**:
  - The `// goaugtype:` directive is permitted on **any interface declaration, including interfaces with method signatures**, not just empty interfaces (`any`, `interface{}`).
  - When an interface $I$ declares both a method set $M$ and a union directive `// goaugtype: T_1 | T_2 | ... | T_n`, the effective type set of $I$ is the **intersection of the types implementing $M$ and the types in the union**.
  - **Declaration-Site Validation**: If any union term $T_k$ does not implement the method set $M$ of interface $I$ (`!types.Implements(T_k, I)`), the analyzer immediately reports a diagnostic at the sum type declaration site.

```go
// goaugtype: *Leaf | *Node
type Tree interface {
    Walk()
}
```

### 2.2. Strict Union Terms and Rejection of `~T` and `nil`
> **From #57644**: *"We propose that we permit an ordinary interface type to embed a union of terms, where each term is itself a type... This proposal does not permit the underlying type syntax `~T` to be used in an ordinary interface type... The types `MyInt` and `MyFloat` implement `I1`. The type `MyOtherInt` does not implement `I1`."*

- **Only Types Allowed in Union Terms (`nil` Keyword Removed)**:
  - In `#57644`, every union term in `T1 | T2` must itself be a type. Because `nil` is an untyped predeclared identifier (zero value) rather than a type, `nil` is **not** written in `// goaugtype:` directives.
  - If `nil` appears in a `// goaugtype:` directive, the analyzer reports an invalid union term diagnostic.
- **No Underlying Type Approximation (`~T`)**:
  - As noted in `#57644`, the underlying type syntax (`~int`) is disallowed in general interface sum types because Go lacks a language mechanism to extract an unknown defined type from an interface in a `type switch` across package boundaries.
  - If `~T` appears in a directive, the analyzer reports an explicit syntax diagnostic (`underlying type syntax ~T is not permitted in sum types`).
- **Strict Identity for Concrete Types**:
  - When a union term $T_k$ is a non-interface (concrete) type, a value of type $V$ must satisfy `types.Identical(V, T_k)`. A distinct defined type sharing the same underlying type is not permitted.
- **Untyped Constants**:
  - Following standard Go assignability rules, when an untyped constant (e.g., `10`, `"hello"`) is assigned to an interface, it is first evaluated as its default type (`int`, `float64`, `string`, `bool`, etc.) before checking membership in the type set.
  - Example: Assigning an untyped constant `10` (default type `int`) to `SmallInt` (`// goaugtype: int8 | int16`) is rejected because `int` is not in the type set; an explicit conversion (`int8(10)`) is required.

### 2.3. Interface Terms and Nested/Composed Sum Types
> **From #57644**: *"Embedding a union in an interface affects the interface's type set."*

Each term in a sum type union may be a concrete type, a regular interface, or another sum type.

1. **Type Set Flattening and Subset Assignability**:
   - If sum type $I_2$ includes another sum type $I_1$ in its directive (`// goaugtype: I1 | float64`), the effective type set of $I_2$ is flattened to $\text{TypeSet}(I_1) \cup \{\text{float64}\}$.
   - Assigning a value of sum type $S$ to a target of sum type $T$ is permitted even when $S$ and $T$ are not identical (`!types.Identical(S, T)`), provided that **$\text{TypeSet}(S) \subseteq \text{TypeSet}(T)$**.
   - In a `switch x.(type)` statement, a `case I1:` branch marks all member types in $\text{TypeSet}(I_1)$ as handled.
2. **Regular Interface Terms (Issue #5)**:
   - When a union includes a regular (non-sum) interface term (e.g., `error`, `io.Reader`), assigning a value of type $V$ is valid if $V$ implements that interface (`types.Implements(V, TermInterface)`).

### 2.4. Zero Value (`nil`) Semantics in the Pure #57644 Model
> **From #57644**: *"In particular, the zero value of an interface type with an embedded union would be `nil`, just as for any interface type. So this is a form of sum type in which there is always another possible option, namely `nil`."*

Under the pure `#57644` model, a sum type is an ordinary Go interface type with a restricted type set, and its zero value semantics follow Go's interface rules without exception:

1. **Zero Value is Always `nil`**:
   - Every sum type variable has `nil` as its zero value. Uninitialized variable declarations (`var x Tree`), zero-initialized composite fields, and explicit `nil` assignments (`x = nil`) are **always valid** for any sum type.
2. **`case nil:` is Always Required in `type switch`**:
   - Because `nil` is always a possible value of any sum type interface, every exhaustive `switch x.(type)` over a sum type must explicitly handle `case nil:` (unless a `default:` branch is permitted via analyzer configuration).
3. **Typed `nil` vs. Untyped `nil`**:
   - In `var leaf *Leaf = nil; var t Tree = leaf`, `t` holds a non-nil interface value with dynamic type `*Leaf` (`(*Leaf)(nil)`). It matches `case *Leaf:` in a `type switch` and is treated as `*Leaf` for type set membership.

### 2.5. Complete Assignability and Conversion Verification
> **From #57644**: *"As always, a variable of interface type may store a value of any type that is in its type set... Inversely, a variable of interface type may not store a value of any type that is not in its type set."*

A non-`nil` value whose type is outside the sum type's type set must not be stored into or converted to that sum type through any code path. Therefore, `goaugtype` inspects **all AST locations where Go assignability or type conversion occurs**, not just simple identifier assignments (`ident = expr`):

| # | AST Location | Example | Legacy Behavior | Target Behavior |
| :--- | :--- | :--- | :--- | :--- |
| 1 | Assignments (`*ast.AssignStmt`) | `x = val`, `s.field = val`, `arr[i] = val`, `*p = val` | Checked only simple `*ast.Ident` (Issue #2) | Inspect all LHS expressions via `TypesInfo.TypeOf(lhs)` + support multi-value returns (`a, b = fn()`) |
| 2 | Value declarations (`*ast.ValueSpec`) | `var x Tree = val`, `var ( a Tree = v1; b Tree = v2 )` | Checked only `Specs[0]` | Iterate over all `GenDecl.Specs` and validate assigned `Values` against target sum types |
| 3 | **Explicit type conversions (`*ast.CallExpr`)** | `Tree(NotTree{})` | **Unchecked (critical soundness hole)** | When `TypesInfo.Types[call.Fun].IsType()` targets a sum type, validate `call.Args[0]` |
| 4 | Return statements (`*ast.ReturnStmt`) | `func f() Tree { return NotTree{} }` | Unchecked (Issue #1) | Match `return` expressions against enclosing function's return `*types.Signature` |
| 5 | Function call arguments (`*ast.CallExpr`) | `takeTree(NotTree{})` | Unchecked | Match call arguments against parameter types (including variadic parameters) |
| 6 | Composite literals (`*ast.CompositeLit`) | `[]Tree{NotTree{}}`, `MyStruct{TreeField: NotTree{}}` | Unchecked | Validate slice, array, map, and struct literal elements against their target element/field sum types |
| 7 | Channel sends (`*ast.SendStmt`) | `ch <- NotTree{}` (`var ch chan Tree`) | Unchecked | Validate sent expression when `chan.Elem()` is a sum type |

### 2.6. `type switch` Verification Rules (`checkTypeSwitch`)
1. **Arbitrary Target Expressions**:
   - In `switch x.(type)`, `x` is inspected via `TypesInfo.TypeOf(targetExpr)` regardless of whether it is a simple identifier (`*ast.Ident`) or a complex expression (`s.tree.(type)`, `getTree().(type)`, `trees[0].(type)`).
2. **Case Validity and `case nil:` Requirement**:
   - Reports a diagnostic if an unpermitted type appears in a `case` clause.
   - Tracks `nilHandled` as per-switch state and reports a missing `nil` case if `case nil:` is omitted. Duplicate `case nil:` clauses are already rejected by the Go type checker or handled cleanly without mutating sum type metadata.
3. **`default:` Clause Policy**:
   - To preserve the primary value of sum types—catching unhandled variants at compile time when a new member is added—all permitted member types (and `nil`) must be explicitly covered by `case` clauses by default.
   - Whether a `default:` branch suppresses missing-case diagnostics can be exposed as an optional analyzer flag (e.g., `-allow-default`).

### 2.7. Cross-Package Analysis Architecture (`analysis.Fact`)
- **Problem**: Go's unit of compilation and analysis is a single package; AST comments (`// goaugtype:`) from imported packages are not present in `pass.Files`.
- **Architecture**:
  - Uses the **`analysis.Fact`** mechanism from `golang.org/x/tools/go/analysis`.
  - When analyzing a package, `goaugtype` exports a serializable fact (`pass.ExportObjectFact(typeNameObj, &fact)`) attached to each sum type's `*types.TypeName` object.
  - When inspecting types imported from external packages, `goaugtype` retrieves sum type metadata via `pass.ImportObjectFact(obj, &fact)`, enabling full assignability and `type switch` exhaustiveness checks across package boundaries.

### 2.8. `SumTypeSet` Engine Adapted from `go/types` & Upstream Tracking (#14)

Go's standard library `go/types` package (`typeset.go`, `termlist.go`, `typeterm.go`, `union.go`, `instantiate.go`) already implements type set operations for type parameter constraints, and `golang.org/x/tools/internal/typeparams/copytermlist.go` similarly adapts this logic.

#### Why Adapt `go/types` Instead of Calling `types.Union` Directly?
1. **Preventing `allTermlist` (`𝓤`) Explosion on Interface Terms with Methods (Issue #5)**:
   - Because standard Go generics disallow interfaces with methods (such as `error`) inside unions, `computeUnionTypeSet` in `go/types` ignores an interface term's methods and merges only its `.terms` (`allTermlist = 𝓤`).
   - Passing `string | error` directly to `types.NewUnion` would therefore collapse the entire union into `any` (`𝓤`).
2. **Expanding Nested Sum Types (`I1 | float64`) Beyond Their Underlying `any` Type**:
   - In standard Go AST/types, `type I1 any` has `any` as its underlying type. Flattening `I1` requires looking up `I1`'s `SumTypeSet` from `goaugtype`'s registry rather than `go/types`.
3. **Computing `type switch` Set Differences**:
   - `goaugtype` must compute missing cases (set cover / difference over `concreteTerms`, `ifaceTerms`, and `nil`) for `type switch` exhaustiveness.

#### `SumTypeSet` Data Model (Adapted from `go/types._TypeSet`)
```go
type SumTypeSet struct {
    selfIface     *types.Interface   // The sum type interface itself (for method set validation, #9)
    concreteTerms []types.Type       // Concrete type terms (matched via types.Identical)
    ifaceTerms    []*types.Interface // Regular interface terms (e.g., error, io.Reader -> matched via types.Implements, #5)
}
```
- `Includes(V types.Type) bool`: Analogous to `implements` / `term.includes` in `go/types`.
- `SubsetOf(other *SumTypeSet) bool`: Analogous to `_TypeSet.subsetOf` in `go/types` (#10).
- `MissingCases(handled []types.Type, nilHandled bool) []string`: Computes uncovered variants (and `nil` if `!nilHandled`) for `type switch` exhaustiveness.

#### Automated Upstream (`golang/go`) Revision Tracking Strategy
To reliably track whether the reference files in `src/go/types/{typeset,termlist,typeterm,union,instantiate}.go` (mirrored from `src/cmd/compile/internal/types2/`) change in future Go releases, the repository includes a configuration file, a dedicated CLI tool (`./cmd/upstreamchecker`), and wrapper scripts (`./scripts`):

1. **Baseline Revision Configuration (`upstream.json`) & Code Constant (`analyzer.UpstreamBaseRevision`)**:
   - `upstream.json` explicitly records the `golang/go` revision (`base_revision`), the list of `tracked_files`, and their SHA-256 `file_hashes` against which `goaugtype`'s type set engine was written.
   - `pkg/analyzer/adt.go` defines `UpstreamBaseRevision` so the implementation explicitly declares which upstream Go release it is synchronized with.
2. **Comparison & Verification CLI (`./cmd/upstreamchecker`)**:
   - A standalone Go tool that resolves GitHub tags using `go/version` and compares SHA-256 file hashes:
     - **Default (no arguments)**: Queries the `golang/go` repository for the **latest official release tag (`go1.x.y`, excluding `rc`/`beta`)**, verifies that `upstream.json` and `analyzer.UpstreamBaseRevision` match that release tag, and compares all `tracked_files`.
     - **Explicit Target (`-target <revision>`)**: Compares against a specific tag (e.g., `go1.26.7`), commit SHA, `master` branch, or the local toolchain (`-target local` via `go env GOROOT`).
     - **Baseline Update (`-update`)**: Updates `base_revision` and `file_hashes` in `upstream.json` after upstream changes have been reviewed.
3. **Automation & Release Verification Scripts (`./scripts`)**:
   - `./scripts/check-upstream.sh`: Invokes `./cmd/upstreamchecker` for local verification or scheduled CI jobs.
   - `./scripts/release.sh`: Release gatekeeper script that requires a clean working tree, `base_revision` consistency, synchronization with the latest official Go release tag, and passing `go test ./...` before creating a release tag.

---

## 3. Implementation Roadmap & GitHub Issues

While preserving continuity with existing issues (#1, #2, #5, #6; note that #4 is superseded by the pure #57644 zero-value model), the implementation is structured into the following milestones:

1. **[#14] `SumTypeSet` Engine Adapted from `go/types` & `./cmd/upstreamchecker`**:
   - Implement `SumTypeSet` modeled after `go/types/typeset.go` and `instantiate.go`, along with `upstream.json`, `./cmd/upstreamchecker` (defaulting to the latest official release tag), `./scripts/check-upstream.sh`, and `./scripts/release.sh`.
2. **[#8] Core Bugfixes & AST Expression Generalization** (supersedes #2):
   - Replace `*ast.Ident`-only assumptions with `TypesInfo.TypeOf`, inspect all specs in `var (...)` blocks, prevent slice out-of-bounds panics on multi-return assignments, and isolate per-switch `nilHandled` state in `checkTypeSwitch`.
3. **[#7] Type Conversion Bypass Prevention (`SumType(x)`)**:
   - Inspect explicit type conversions in `*ast.CallExpr` when the target type is a sum type.
4. **[#9, #10] General Interface & Type Set Semantics (Pure #57644 Alignment)** (extends #5):
   - Support interfaces with methods and validate union terms via `types.Implements` (#9), explicitly reject `~T` and `nil` in union directives (#9), support regular interface union terms (#5), and implement nested sum type flattening and subset ($\subseteq$) assignability (#10).
5. **[#11] Complete Assignability Contexts** (extends #1):
   - Enforce sum type assignability across `return` statements (#1), function call arguments, composite literals (`struct`, `slice`, `map`), and channel sends (`ch <- x`).
6. **[#12] Cross-Package Analysis via `analysis.Fact`**:
   - Export and import sum type metadata across packages using `ExportObjectFact` / `ImportObjectFact`.
7. **[#13] Automated Test Suite (`analysistest`)**:
   - Comprehensive unit and integration test suite powered by `golang.org/x/tools/go/analysis/analysistest`.
