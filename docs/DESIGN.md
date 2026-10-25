# `goaugtype` Design Document: Sum Types Based on General Interfaces (#57644)

## 1. Background & Vision

`goaugtype` (`goaugadt`) is a static analysis linter built on `golang.org/x/tools/go/analysis` that brings **Sum Types (Algebraic Data Types)** to Go.

To resolve specification ambiguities and soundness holes identified in the initial prototype, this document establishes the specification and architecture of `goaugtype` based strictly on the **pure model of [golang/go#57644 (`proposal: spec: sum types based on general interfaces`)](https://github.com/golang/go/issues/57644)**, proposed by Ian Lance Taylor of the Go team.

### Core Design Philosophy
1. **Pure Pre-implementation of `golang/go#57644`**:
   - Proposal `#57644` extends type unions (`T1 | T2`), which are currently restricted to generic type parameter constraints, to **ordinary (general) interfaces** used as variable types.
   - Because the current Go compiler rejects union elements inside ordinary interfaces used as variable types (`interface contains type constraints`), `goaugtype` uses the `// goaugtype: T1 | T2` directive on standard Go interface declarations (`type I any` or `type I interface { ... }`) to **statically emulate the exact Type Set semantics of `#57644`**.
2. **Consistent Type Set Semantics with Disjoint Orthogonality**:
   - Rather than inventing an ad-hoc type system or custom `nil` annotations, sum types are defined as a natural extension of Go 1.18+ **Interface Type Sets, `Implements`, and `Assignability` rules**, reinforced with **Pairwise Disjointness** across flattened union terms so that adding a new variant always triggers exhaustiveness errors across existing `type switch` statements.
3. **Static Exhaustiveness Checking & Type Erasure Safety**:
   - While `#57644` deferred compiler-enforced `type switch` exhaustiveness for backward compatibility, `goaugtype` acts as a strict linter that verifies whether `type switch` statements exhaustively handle all leaf elements in a sum type's type set plus the interface zero value (`nil`), while preventing runtime type erasure shadowing.

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

### 2.3. Interface Terms, Nested Sum Types, and Disjoint Union Invariant
> **From #57644**: *"Embedding a union in an interface affects the interface's type set."*

Each term in a sum type union may be a concrete type, a regular interface, or another sum type.

1. **Type Set Flattening and Subset Assignability**:
   - If sum type $I_2$ includes another sum type $I_1$ in its directive (`// goaugtype: I1 | float64`), the effective type set of $I_2$ is flattened to $\text{TypeSet}(I_1) \cup \{\text{float64}\}$.
   - Assigning a value of sum type $S$ to a target of sum type $T$ is permitted even when $S$ and $T$ are not identical (`!types.Identical(S, T)`), provided that **$\text{TypeSet}(S) \subseteq \text{TypeSet}(T)$**.
2. **Regular Interface Terms (Issue #5)**:
   - When a union includes a regular (non-sum) interface term (e.g., `error`, `io.Reader`), assigning a value of type $V$ is valid if $V$ implements that interface (`types.Implements(V, TermInterface)`).
3. **Disjoint Union Invariant (No Overlapping Terms)**:
   - To guarantee strict exhaustiveness checking comparable to tagged unions in languages like Haskell or Rust—where adding a new variant to a sum type always triggers unhandled-case diagnostics across existing `type switch` statements—`goaugtype` enforces **pairwise inclusion disjointness** across all flattened union terms:
     1. **No Duplicate Concrete Types**: No two flattened concrete terms $C_1, C_2$ may satisfy `types.Identical(C_1, C_2)`.
     2. **No Non-Empty Interface Overlap**:
        - Every regular (non-sum) interface term $I$ must declare at least one method (`I.NumMethods() > 0`; `any` / `interface{}` is rejected).
        - No concrete term $C$ may implement any regular interface term $I$ in the same sum type (`types.Implements(C, I)` is rejected). For example, `// goaugtype: *MyError | error` is rejected at the declaration site because `*MyError` is already a subset of `error`.
        - No two regular interface terms $I_1, I_2$ may have a subtype/supertype relationship (`types.Implements(I_1, I_2)` or `types.Implements(I_2, I_1)` is rejected).

### 2.4. Zero Value (`nil`) Semantics in the Pure #57644 Model
> **From #57644**: *"In particular, the zero value of an interface type with an embedded union would be `nil`, just as for any interface type. So this is a form of sum type in which there is always another possible option, namely `nil`."*

Under the pure `#57644` model, a sum type is an ordinary Go interface type with a restricted type set, and its zero value semantics follow Go's interface rules without exception:

1. **Zero Value is Always `nil`**:
   - Every sum type variable has `nil` as its zero value. Uninitialized variable declarations (`var x Tree`), zero-initialized composite fields, and explicit `nil` assignments (`x = nil`) are **always valid** for any sum type.
2. **`case nil:` is Always Required in `type switch`**:
   - Because `nil` is always a possible value of any sum type interface, every exhaustive `switch x.(type)` over a sum type must explicitly handle `case nil:`.
3. **Typed `nil` vs. Untyped `nil`**:
   - In `var leaf *Leaf = nil; var t Tree = leaf`, `t` holds a non-nil interface value with dynamic type `*Leaf` (`(*Leaf)(nil)`). It matches `case *Leaf:` in a `type switch` and is treated as `*Leaf` for type set membership.

### 2.5. Complete Assignability, Conversion, and Assertion Verification (8 Paths)
> **From #57644**: *"As always, a variable of interface type may store a value of any type that is in its type set... Inversely, a variable of interface type may not store a value of any type that is not in its type set."*

A non-`nil` value whose type is outside the sum type's type set must not be stored into, converted to, or asserted from/to that sum type through any code path. Therefore, `goaugtype` inspects **all 8 AST locations where Go assignability, type conversion, or type assertion occurs**:

| # | AST Location | Example | Legacy Behavior | Target Behavior |
| :--- | :--- | :--- | :--- | :--- |
| 1 | Assignments (`*ast.AssignStmt`) | `x = val`, `s.field = val`, `arr[i] = val`, `*p = val` | Checked only simple `*ast.Ident` (Issue #2) | Inspect all LHS expressions via `TypesInfo.TypeOf(lhs)` + support multi-value returns (`a, b = fn()`) |
| 2 | Value declarations (`*ast.ValueSpec`) | `var x Tree = val`, `var ( a Tree = v1; b Tree = v2 )` | Checked only `Specs[0]` | Iterate over all `GenDecl.Specs` and validate assigned `Values` against target sum types |
| 3 | **Explicit type conversions (`*ast.CallExpr`)** | `Tree(NotTree{})` | **Unchecked (critical soundness hole)** | When `TypesInfo.Types[call.Fun].IsType()` targets a sum type, validate `call.Args[0]` |
| 4 | Return statements (`*ast.ReturnStmt`) | `func f() Tree { return NotTree{} }` | Unchecked (Issue #1) | Match `return` expressions against enclosing function's return `*types.Signature` |
| 5 | Function call arguments (`*ast.CallExpr`) | `takeTree(NotTree{})` | Unchecked | Match call arguments against parameter types (including variadic parameters) |
| 6 | Composite literals (`*ast.CompositeLit`) | `[]Tree{NotTree{}}`, `MyStruct{TreeField: NotTree{}}` | Unchecked | Validate slice, array, map, and struct literal elements against their target element/field sum types |
| 7 | Channel sends (`*ast.SendStmt`) | `ch <- NotTree{}` (`var ch chan Tree`) | Unchecked | Validate sent expression when `chan.Elem()` is a sum type |
| 8 | **Type assertions (`*ast.TypeAssertExpr`)** | `a.(Tree)`, `tree.(NotTree)` | Unchecked | Reject asserting an arbitrary value to a sum type (`x.(Tree)` when `x` is not statically in `Tree`'s type set, due to Go runtime interface erasure) and reject asserting a sum type to an unpermitted type (`tree.(NotTree)`) |

### 2.6. `type switch` Verification Rules (`checkTypeSwitch`)
1. **Arbitrary Target Expressions**:
   - In `switch x.(type)`, `x` is inspected via `TypesInfo.TypeOf(targetExpr)` regardless of whether it is a simple identifier (`*ast.Ident`) or a complex expression (`s.tree.(type)`, `getTree().(type)`, `trees[0].(type)`).
2. **Flattened Leaf Matching Only (No Nested Sum Types in `case`)**:
   - Because a nested sum type (`type SmallInt any`) is erased to `interface{}` (or its method set) at Go runtime, writing `case SmallInt:` in a `type switch` over `AllInt` (`SmallInt | int32 | int64`) would match *every* value at runtime and swallow subsequent branches.
   - Therefore, nested sum types are **prohibited** as `case` targets in a `type switch`; cases must match flattened leaf terms (`case int8, int16:`).
3. **Subtype Specialization (Narrowing) and Shadowing Prevention**:
   - While `*MyError | error` is prohibited in the union declaration, a `switch x.(type)` over `string | error` **may** include a specialized `case *MyError:` branch **before** `case error:`.
   - `case *MyError:` is valid because `*MyError` is included in `error`, does not exhaust `error` on its own, and is safe as long as it precedes `case error:`.
   - Conversely, if `case *MyError:` appears **after** `case error:` (or after a duplicate concrete type), the analyzer reports a **shadowed / unreachable case** diagnostic because `case error:` already intercepts all `*MyError` values at runtime.
4. **Mandatory `case nil:` and No Redundant `default:`**:
   - All flattened leaf terms (`ConcreteTerms` and `IfaceTerms`) and `case nil:` must be explicitly handled.
   - Once all flattened leaf terms and `case nil:` are covered, a `default:` branch is unreachable dead code that can mask future bugs or cause nil panics; therefore, `default:` does not substitute for missing cases and is rejected as redundant when all cases are exhausted.

### 2.7. Cross-Package Analysis Architecture (`analysis.Fact`)
- **Problem**: Go's unit of compilation and analysis is a single package; AST comments (`// goaugtype:`) from imported packages are not present in `pass.Files`.
- **Architecture**:
  - Uses the **`analysis.Fact`** mechanism from `golang.org/x/tools/go/analysis`.
  - When analyzing a package, `goaugtype` exports a serializable fact (`pass.ExportObjectFact(typeNameObj, &fact)`) attached to each sum type's `*types.TypeName` object.
  - When inspecting types imported from external packages, `goaugtype` retrieves sum type metadata via `pass.ImportObjectFact(obj, &fact)`, enabling full assignability and `type switch` exhaustiveness checks across package boundaries.

### 2.8. `SumTypeSet` Engine Ported from `go/types` & Upstream Tracking (#14)

Go's standard library `go/types` package (`typeset.go`, `termlist.go`, `typeterm.go`, `union.go`, `instantiate.go`, `operand.go`, `assignments.go`) already implements type set operations, union overlapping checks, `implements`, and assignment/tuple decomposition, and `golang.org/x/tools/internal/typeparams/copytermlist.go` similarly ports this logic.

#### Upstream (`src/go/types`) to `pkg/analyzer` 1:1 File & Patch Mapping
All 7 upstream files are stored **100% unmodified** in [`internal/upstream/testdata/gotypes/`](../internal/upstream/testdata/gotypes), tracked in [`upstream.json`](../upstream.json), and transformed into [`pkg/analyzer/`](../pkg/analyzer) via unified diff patches in [`internal/upstream/patches/`](../internal/upstream/patches) using `go generate ./...` or `./scripts/apply-patches.sh`:

| Upstream `golang/go` File | Pristine Snapshot | Unified Patch | Generated / Ported File | Ported Symbols & `[goaugtype]` Extensions |
| :--- | :--- | :--- | :--- | :--- |
| `src/go/types/typeterm.go` | [`gotypes/typeterm.go`](../internal/upstream/testdata/gotypes/typeterm.go) | [`typeterm.patch`](../internal/upstream/patches/typeterm.patch) | [`pkg/analyzer/typeterm.go`](../pkg/analyzer/typeterm.go) | `term`, `(*term).String`, `equal`, `union`, `intersect`, `includes`, `subsetOf`, `disjoint` — extended with `types.Implements` for non-empty interface terms (`#5`). |
| `src/go/types/termlist.go` | [`gotypes/termlist.go`](../internal/upstream/testdata/gotypes/termlist.go) | [`termlist.patch`](../internal/upstream/patches/termlist.patch) | [`pkg/analyzer/termlist.go`](../pkg/analyzer/termlist.go) | `termlist`, `allTermlist`, `String`, `isEmpty`, `isAll`, `norm`, `union`, `intersect`, `equal`, `includes`, `supersetOf`, `subsetOf` — preserved intact (`100%` algorithmic parity). |
| `src/go/types/typeset.go` | [`gotypes/typeset.go`](../internal/upstream/testdata/gotypes/typeset.go) | [`typeset.patch`](../internal/upstream/patches/typeset.patch) | [`pkg/analyzer/typeset.go`](../pkg/analyzer/typeset.go) | `_TypeSet`, `IsEmpty`, `IsAll`, `IsMethodSet`, `IsComparable`, `NumMethods`, `Method`, `LookupMethod`, `String`, `hasTerms`, `subsetOf`, `terms` (`computeInterfaceTypeSet`). |
| `src/go/types/union.go` | [`gotypes/union.go`](../internal/upstream/testdata/gotypes/union.go) | [`union.patch`](../internal/upstream/patches/union.patch) | [`pkg/analyzer/union.go`](../pkg/analyzer/union.go) | `parseTilde` (rejects `~T` and `nil`, `#9`), `overlappingTerm` (detects concrete-concrete, concrete-interface, and interface-interface overlap), `computeUnionTypeSet` (flattens nested sum types via `Registry`, `#10`). |
| `src/go/types/instantiate.go` | [`gotypes/instantiate.go`](../internal/upstream/testdata/gotypes/instantiate.go) | [`instantiate.patch`](../internal/upstream/patches/instantiate.patch) | [`pkg/analyzer/instantiate.go`](../pkg/analyzer/instantiate.go) | `implements` — checks method set satisfaction (`#9`) and term-set inclusion (`tset.subsetOf(Tset)` / `Tset.includes(V)`). |
| `src/go/types/operand.go` | [`gotypes/operand.go`](../internal/upstream/testdata/gotypes/operand.go) | [`operand.patch`](../internal/upstream/patches/operand.patch) | [`pkg/analyzer/operand.go`](../pkg/analyzer/operand.go) | `operandMode`, `operand`, `(*operand).assignableTo` — hooks `Registry.Lookup(T)` to enforce sum type assignability via `implements`. |
| `src/go/types/assignments.go` | [`gotypes/assignments.go`](../internal/upstream/testdata/gotypes/assignments.go) | [`assignments.patch`](../internal/upstream/patches/assignments.patch) | [`pkg/analyzer/assignments.go`](../pkg/analyzer/assignments.go) | `assignment`, `initVar`, `assignVar`, `unpackRecv`, `initVars`, `assignVars`, `returnVars` — handles 1:1 and multi-value tuple (`CommaOk` / `()`) decomposition across `var`, `=`, `:=`, and `return`. |

#### Why Port `go/types` Instead of Calling `types.Union` Directly?
1. **Preventing `allTermlist` (`𝓤`) Explosion on Interface Terms with Methods (Issue #5)**:
   - Because standard Go generics disallow interfaces with methods (such as `error`) inside unions, `computeUnionTypeSet` in `go/types` ignores an interface term's methods and merges only its `.terms` (`allTermlist = 𝓤`).
   - Passing `string | error` directly to `types.NewUnion` would therefore collapse the entire union into `any` (`𝓤`).
2. **Expanding Nested Sum Types (`I1 | float64`) Beyond Their Underlying `any` Type**:
   - In standard Go AST/types, `type I1 any` has `any` as its underlying type. Flattening `I1` requires looking up `I1`'s `SumTypeSet` from `goaugtype`'s registry rather than `go/types`.
3. **Enforcing Pairwise Disjointness & Computing `type switch` Set Differences**:
   - `goaugtype` validates pairwise disjointness via `overlappingTerm` and computes missing cases (set cover / difference over `concreteTerms`, `ifaceTerms`, and `nil`) for `type switch` exhaustiveness.

#### `SumTypeSet` Data Model (Wrapping `_TypeSet` in [`pkg/analyzer/adt.go`](../pkg/analyzer/adt.go))
```go
type SumTypeSet struct {
    Name          *types.TypeName
    SumType       types.Type
    SelfIface     *types.Interface
    RawTerms      []types.Type
    ConcreteTerms []types.Type // Concrete type terms (matched via types.Identical)
    IfaceTerms    []types.Type // Regular non-empty interface terms (matched via types.Implements, #5)
    Pos           token.Pos
    tset          *_TypeSet    // Upstream go/types _TypeSet representation
}
```
- `ValidateTerms() []augtError`: Runs `computeUnionTypeSet` (`overlappingTerm`) and `implements` (`#9`) to verify method set satisfaction and pairwise disjointness.
- `Includes(V types.Type) bool`: Delegates to `tset.includes(V)` (`termlist.includes` in `termlist.go`).
- `SubsetOf(other *SumTypeSet) bool`: Delegates to `tset.subsetOf(other.tset)` (`_TypeSet.subsetOf` in `typeset.go`, `#10`).

#### Automated Upstream (`golang/go`) Revision Tracking Strategy
To reliably track whether the 7 reference files in `src/go/types/{typeset,termlist,typeterm,union,instantiate,assignments,operand}.go` (mirrored from `src/cmd/compile/internal/types2/`) change in future Go releases, the repository includes a configuration file, a dedicated CLI tool (`./cmd/upstreamchecker`), and wrapper scripts (`./scripts`):

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

While preserving continuity with existing issues (#1, #2, #5; note that #4 and #6 are superseded by the pure #57644 model and static return checking), the implementation is structured into the following milestones:

1. **[#14] `SumTypeSet` Engine Adapted from `go/types` & `./cmd/upstreamchecker`**:
   - Implement `SumTypeSet` modeled after `go/types/typeset.go` and `instantiate.go`, along with `upstream.json`, `./cmd/upstreamchecker` (defaulting to the latest official release tag), `./scripts/check-upstream.sh`, and `./scripts/release.sh`.
2. **[#8] Core Bugfixes & AST Expression Generalization** (supersedes #2):
   - Replace `*ast.Ident`-only assumptions with `TypesInfo.TypeOf`, inspect all specs in `var (...)` blocks, prevent slice out-of-bounds panics on multi-return assignments, and isolate per-switch `nilHandled` state in `checkTypeSwitch`.
3. **[#7] Type Conversion & Type Assertion Bypass Prevention (`SumType(x)`, `x.(SumType)`)**:
   - Inspect explicit type conversions in `*ast.CallExpr` and type assertions in `*ast.TypeAssertExpr` involving sum types.
4. **[#9, #10] General Interface, Disjoint Union Invariant & Type Set Semantics (Pure #57644 Alignment)** (extends #5):
   - Support interfaces with methods and validate union terms via `types.Implements` (#9), explicitly reject `~T` and `nil` in union directives (#9), enforce pairwise disjointness across flattened terms, support regular non-empty interface union terms (#5), and implement nested sum type flattening and subset ($\subseteq$) assignability (#10).
5. **[#11] Complete Assignability Contexts** (extends #1):
   - Enforce sum type assignability across `return` statements (#1), function call arguments, composite literals (`struct`, `slice`, `map`), and channel sends (`ch <- x`).
6. **[#12] Cross-Package Analysis via `analysis.Fact`**:
   - Export and import sum type metadata across packages using `ExportObjectFact` / `ImportObjectFact`.
7. **[#13] Automated Test Suite (`analysistest`)**:
   - Comprehensive unit and integration test suite powered by `golang.org/x/tools/go/analysis/analysistest`.
