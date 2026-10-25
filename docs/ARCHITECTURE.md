# `goaugtype` Architecture Overview

This document provides a high-level architectural view of `goaugtype`, illustrating its end-to-end analysis pipeline, core `SumTypeSet` data structures, cross-package `analysis.Fact` propagation, verification algorithms, and upstream synchronization workflow.

For detailed language specifications and rationale aligned with [golang/go#57644](https://github.com/golang/go/issues/57644), see [DESIGN.md](./DESIGN.md) ([DESIGN.ko.md](./DESIGN.ko.md)).

---

## 1. System Context & Component Topology

`goaugtype` is structured into three primary layers:
1. **CLI & Driver Layer (`cmd/goaugadt`)**: Wraps the analyzer with `golang.org/x/tools/go/analysis/singlechecker` for standalone CLI execution and standard linter integration.
2. **Core Analysis Engine (`pkg/analyzer`)**: Implements the two-pass AST/type-set static analyzer ([`adt.go`](../pkg/analyzer/adt.go), [`check.go`](../pkg/analyzer/check.go), and the 7 `go/types`-ported files [`typeterm.go`](../pkg/analyzer/typeterm.go), [`termlist.go`](../pkg/analyzer/termlist.go), [`typeset.go`](../pkg/analyzer/typeset.go), [`union.go`](../pkg/analyzer/union.go), [`instantiate.go`](../pkg/analyzer/instantiate.go), [`operand.go`](../pkg/analyzer/operand.go), and [`assignments.go`](../pkg/analyzer/assignments.go)).
3. **Upstream Synchronization, Patch Pipeline & Release Gatekeeper (`cmd/upstreamchecker`, `internal/upstream/`, `Makefile`, `scripts/`, `upstream.json`)**: Keeps pristine `src/go/types` snapshots in `internal/upstream/testdata/gotypes/`, applies unified diffs from `internal/upstream/patches/` via `go generate` (`make generate`), and enforces drift-free synchronization across Go releases.

![Architecture Topology](./diagrams/architecture-topology.svg)
*Source: [`docs/diagrams/architecture-topology.d2`](./docs/diagrams/architecture-topology.d2)*

```mermaid
flowchart TB
    subgraph Tooling["CLI & Release Tooling"]
        CLI["cmd/goaugadt<br/>(singlechecker driver)"]
        UCheck["cmd/upstreamchecker<br/>(go/types drift detector)"]
        Scripts["scripts/check-upstream.sh<br/>scripts/release.sh"]
        UJSON[("upstream.json<br/>base_revision & SHA-256 hashes")]
    end

    subgraph Engine["Core Analyzer (pkg/analyzer)"]
        Entry["Analyzer.Run (run)<br/>check.go"]
        Collector["Pass 1: AST & Directive Collector<br/>(collectInfo / inspector)"]
        Builder["Pass 2: Registry & TypeSet Builder<br/>(BuildRegistry)"]
        Registry["Registry & SumTypeSet<br/>adt.go + {typeterm,termlist,typeset}.go"]
        Checker["Pass 3: 8-Path & Switch Verifier<br/>check.go + assignments.go"]
        Facts["SumTypeFact<br/>(analysis.Fact Gob Codec)"]
    end

    subgraph GoStd["Go Toolchain & x/tools"]
        GoAnalysis["golang.org/x/tools/go/analysis"]
        GoTypes["go/ast + go/types<br/>(TypesInfo, Implements, Eval)"]
    end

    CLI --> Entry
    Scripts --> UCheck
    UCheck --> UJSON
    UCheck -. "verifies UpstreamBaseRevision" .-> Registry

    Entry --> Collector
    Collector --> Builder
    Builder --> Registry
    Registry <--> Facts
    Entry --> Checker
    Checker --> Registry

    Collector --> GoTypes
    Builder --> GoTypes
    Checker --> GoTypes
    Facts --> GoAnalysis
```

---

## 2. Two-Pass Analysis Pipeline (`Analyzer.Run`)

Because Go allows types to be declared after the functions that reference them (or across multiple files in the same package), `goaugtype` decouples **AST & directive collection** from **type evaluation and verification**.

![Analysis Pipeline](./diagrams/analysis-pipeline.svg)
*Source: [`docs/diagrams/analysis-pipeline.d2`](./diagrams/analysis-pipeline.d2)*

```mermaid
sequenceDiagram
    autonumber
    participant Pass as analysis.Pass
    participant Insp as inspector (check.go)
    participant Reg as BuildRegistry (adt.go)
    participant Chk as check() (check.go)

    Note over Pass,Insp: Phase A: AST & Directive Collection (collectInfo)
    loop For each file in pass.Files
        Pass->>Insp: ast.NewCommentMap(fset, file, comments)
        Pass->>Insp: ast.Inspect(file, ispt.inspect)
        Insp-->>Insp: Extract // goaugtype: directives (adtDecls)
        Insp-->>Insp: Collect 8 assignability/assertion AST nodes + TypeSwitchStmts
    end

    Note over Pass,Reg: Phase B: Two-Stage Registry Construction (BuildRegistry)
    Pass->>Reg: BuildRegistry(pass, col.adtDecls)
    loop Stage 1: Raw Evaluation
        Reg->>Reg: Reject ~T and nil in directives
        Reg->>Reg: types.Eval(sumType) & types.Eval(permittedTerms)
    end
    loop Stage 2: Flattening, Disjointness & Fact Export
        Reg->>Reg: Recursively flatten nested sum types (cycle-safe DFS)
        Reg->>Reg: Partition into ConcreteTerms vs. IfaceTerms
        Reg->>Reg: ValidateTerms() (method set #9 + pairwise disjointness)
        Reg->>Pass: ExportObjectFact(TypeName, &SumTypeFact) (if valid & exported)
    end

    Note over Pass,Chk: Phase C: Static Verification (check)
    Pass->>Chk: check(pass.TypesInfo, col, reg)
    Chk->>Chk: 1. checkAssign (AssignStmt & ValueSpec, 1:1 & Tuple)
    Chk->>Chk: 2. checkTypeSwitch (Leaf matching, narrowing, shadowing, nil, default)
    Chk->>Chk: 3. checkTypeConversion (T(x))
    Chk->>Chk: 4. checkReturn (FuncDecl & FuncLit, 1:1 & Tuple)
    Chk->>Chk: 5. checkCallArgs (Normal, Variadic ..., Tuple forwarding)
    Chk->>Chk: 6. checkCompositeLit (Slice, Array, Map K/V, Struct keyed/unkeyed)
    Chk->>Chk: 7. checkSend (ch <- val)
    Chk->>Chk: 8. checkTypeAssert (x.(Tree) and tree.(T))
    Chk-->>Pass: pass.Reportf(pos, diagnostic)
```

---

## 3. Core Data Model (`pkg/analyzer/adt.go`)

Standard `go/types.Union` cannot represent sum types containing interfaces with methods (such as `error`) because `computeUnionTypeSet` in `go/types` discards interface methods in unions and collapses the union into `any` ($\mathcal{U}$).

To prevent this collapse while preserving `#57644` semantics, `SumTypeSet` partitions flattened leaf terms into `ConcreteTerms` and `IfaceTerms`:

```mermaid
classDiagram
    class Registry {
        -pass *analysis.Pass
        -sets []*SumTypeSet
        +Lookup(t types.Type) *SumTypeSet
    }

    class SumTypeSet {
        +Name *types.TypeName
        +SumType types.Type
        +SelfIface *types.Interface
        +RawTerms []types.Type
        +ConcreteTerms []types.Type
        +IfaceTerms []types.Type
        +Pos token.Pos
        +ValidateTerms() []augtError
        +Includes(v types.Type) bool
        +SubsetOf(other *SumTypeSet) bool
        +IsPermittedType(reg *Registry, valType types.Type) bool
        +IsPermittedValue(tinfo *types.Info, reg *Registry, expr ast.Expr) bool
        +IsPermittedCaseType(reg *Registry, caseType types.Type) bool
    }

    class SumTypeFact {
        +ConcreteTerms []string
        +IfaceTerms []string
        +AFact()
        +String() string
    }

    class collected {
        +adtDecls []goAugADTDecl
        +declAssigns []*ast.ValueSpec
        +assignStmts []*ast.AssignStmt
        +typeSwitchStmts []*ast.TypeSwitchStmt
        +callExprs []*ast.CallExpr
        +funcDecls []*ast.FuncDecl
        +funcLits []*ast.FuncLit
        +compositeLits []*ast.CompositeLit
        +sendStmts []*ast.SendStmt
        +typeAssertExprs []*ast.TypeAssertExpr
        +errs []augtError
    }

    Registry "1" *-- "*" SumTypeSet : manages & caches
    SumTypeSet ..> SumTypeFact : serializes to / restores from
    collected ..> Registry : feeds adtDecls & AST nodes
```

---

## 4. Declaration-Site Validation & Disjoint Union Invariant

When `BuildRegistry` constructs a `SumTypeSet`, it flattens any nested sum types into leaf terms and runs `ValidateTerms()` to enforce both `#57644` general interface constraints and **Pairwise Inclusion Disjointness**:

![TypeSet Disjointness and Switch State Machine](./diagrams/typeset-and-switch.svg)
*Source: [`docs/diagrams/typeset-and-switch.d2`](./diagrams/typeset-and-switch.d2)*

```mermaid
flowchart TD
    Start["Raw Directive<br/>// goaugtype: T1 | T2 | ..."] --> SyntaxCheck{"Contains '~T'<br/>or 'nil'?"}
    SyntaxCheck -- Yes --> ErrSyntax["Report Syntax Error<br/>(~T and nil prohibited)"]
    SyntaxCheck -- No --> Flatten["DFS Flatten Nested Sum Types<br/>Partition into ConcreteTerms (C) & IfaceTerms (I)"]

    Flatten --> MethodCheck{"SelfIface.NumMethods() > 0?"}
    MethodCheck -- Yes --> VerifyMethods["Verify ∀t ∈ C ∪ I:<br/>types.Implements(t, SelfIface)"]
    MethodCheck -- No --> Disjoint1
    VerifyMethods --> Disjoint1

    subgraph Disjointness["Pairwise Disjointness Invariant (ValidateTerms)"]
        Disjoint1["1. Concrete vs Concrete:<br/>∀i≠j, !types.Identical(C_i, C_j)"] --> Disjoint2["2. Non-Empty Interface Check:<br/>∀I_k ∈ I, I_k.NumMethods() > 0<br/>(reject 'any' / 'interface{}')"]
        Disjoint2 --> Disjoint3["3. Concrete vs Interface:<br/>∀C_i ∈ C, ∀I_k ∈ I, !types.Implements(C_i, I_k)<br/>(reject '*MyError | error')"]
        Disjoint3 --> Disjoint4["4. Interface vs Interface:<br/>∀k≠m, !Implements(I_k, I_m) && !Implements(I_m, I_k)<br/>(reject 'io.ReadWriter | io.Reader')"]
    end

    Disjoint4 --> Valid{"0 Validation Errors?"}
    Valid -- Yes --> Export["Export SumTypeFact<br/>(if TypeName.Exported())"]
    Valid -- No --> Report["Report Declaration Errors"]
```

---

## 5. `type switch` Verification State Machine (`checkTypeSwitch`)

Because Go erases sum type interfaces at runtime to their underlying method set (often `interface{}`), `checkTypeSwitch` enforces **leaf-only matching**, supports **subtype narrowing** when ordered before a general interface term, detects **shadowed/unreachable cases**, requires **`case nil:`**, and rejects **redundant `default:` branches**:

```mermaid
flowchart TD
    Switch["switch x.(type)<br/>where x has SumType S"] --> Init["Initialize:<br/>unhandled = ConcreteTerms ∪ IfaceTerms<br/>seenConcrete = ∅, seenIfaces = ∅<br/>nilHandled = false, defaultClause = nil"]

    Init --> NextCase["Inspect next CaseClauseExpr T"]
    NextCase --> IsDefault{"Is 'default:'?"}
    IsDefault -- Yes --> SaveDefault["defaultClause = clause<br/>(Does NOT clear unhandled)"]
    IsDefault -- No --> IsNil{"Is 'case nil:'?"}

    IsNil -- Yes --> CheckDupNil{"nilHandled == true?"}
    CheckDupNil -- Yes --> ErrDupNil["Report Duplicate/Shadowed 'nil'"]
    CheckDupNil -- No --> MarkNil["nilHandled = true"]

    IsNil -- No --> CheckNested{"reg.Lookup(T) != nil?<br/>(Is T a SumType?)"}
    CheckNested -- Yes --> ErrNested["Report Invalid Case:<br/>Nested SumType erased at runtime"]
    CheckNested -- No --> CheckPermitted{"S.Includes(T)?"}
    CheckPermitted -- No --> ErrInvalid["Report Invalid Case:<br/>T not in S"]

    CheckPermitted -- Yes --> CheckShadow{"T in seenConcrete/seenIfaces<br/>OR ∃I ∈ seenIfaces: Implements(T, I)?"}
    CheckShadow -- Yes --> ErrShadow["Report Shadowed Case:<br/>T already covered by earlier case"]
    CheckShadow -- No --> UpdateState["Record T in seenConcrete / seenIfaces<br/>Remove T from unhandled"]

    SaveDefault --> More{"More cases?"}
    ErrDupNil --> More
    MarkNil --> More
    ErrNested --> More
    ErrInvalid --> More
    ErrShadow --> More
    UpdateState --> More

    More -- Yes --> NextCase
    More -- No --> CheckExhaustive{"unhandled == ∅<br/>AND nilHandled?"}
    CheckExhaustive -- No --> ErrMissing["Report Non-Exhaustive Switch<br/>(list missing leaf terms & nil)"]
    CheckExhaustive -- Yes --> CheckRedundantDefault{"defaultClause != nil?"}
    CheckRedundantDefault -- Yes --> ErrDefault["Report Redundant 'default:'<br/>in Exhaustive Switch"]
    CheckRedundantDefault -- No --> Pass["Switch Validated ✓"]
```

---

## 6. Cross-Package Analysis via `analysis.Fact`

When package `pkgb` imports a sum type defined in `pkga`, `pkga`'s AST comments are no longer available. `goaugtype` bridges package boundaries using `SumTypeFact`:

```mermaid
sequenceDiagram
    participant PkgA as Package A (pkga)
    participant Framework as go/analysis Fact Store
    participant PkgB as Package B (pkgb)

    Note over PkgA: Declares: // goaugtype: *Alpha | *Beta<br/>type ExportedSum any
    PkgA->>PkgA: BuildRegistry flattens & validates ExportedSum
    PkgA->>Framework: pass.ExportObjectFact(ExportedSum, &SumTypeFact{<br/>ConcreteTerms: ["*pkga.Alpha", "*pkga.Beta"]})

    Note over PkgB: Imports "pkga"<br/>var x pkga.ExportedSum = ...
    PkgB->>PkgB: reg.Lookup(pkga.ExportedSum)
    PkgB->>Framework: pass.ImportObjectFact(obj, &fact)
    Framework-->>PkgB: SumTypeFact{ConcreteTerms: ["*pkga.Alpha", "*pkga.Beta"]}
    PkgB->>PkgB: resolveTypeByCanonicalName(pass.Pkg, obj.Pkg(), name)
    PkgB->>PkgB: Cache reconstructed *SumTypeSet in Registry
    PkgB->>PkgB: Enforce all 8 assignability paths & type switches on pkga.ExportedSum
```

---

## 7. Upstream `go/types` Synchronization, Patch Pipeline & Release Gatekeeper

To keep the 7 ported `go/types` files maintainable and verifiable, `goaugtype` stores unmodified upstream snapshots in [`internal/upstream/testdata/gotypes/`](../internal/upstream/testdata/gotypes) and unified diff patches in [`internal/upstream/patches/`](../internal/upstream/patches). `cmd/upstreamchecker`, `Makefile`, and `scripts/release.sh` enforce a 4-way consistency check (code constant ↔ `upstream.json` ↔ local pristine snapshots + patches ↔ latest upstream Go release):

| Upstream `golang/go` Source | Pristine Snapshot | Patch File | Ported `pkg/analyzer` Target | Purpose |
| :--- | :--- | :--- | :--- | :--- |
| `src/go/types/typeterm.go` | [`gotypes/typeterm.go`](../internal/upstream/testdata/gotypes/typeterm.go) | [`typeterm.patch`](../internal/upstream/patches/typeterm.patch) | [`pkg/analyzer/typeterm.go`](../pkg/analyzer/typeterm.go) | Single type term operations (`includes`, `subsetOf`, `disjoint`, `union`, `intersect`) with interface support |
| `src/go/types/termlist.go` | [`gotypes/termlist.go`](../internal/upstream/testdata/gotypes/termlist.go) | [`termlist.patch`](../internal/upstream/patches/termlist.patch) | [`pkg/analyzer/termlist.go`](../pkg/analyzer/termlist.go) | Normalized term list set algebra (`norm`, `includes`, `supersetOf`, `subsetOf`) |
| `src/go/types/typeset.go` | [`gotypes/typeset.go`](../internal/upstream/testdata/gotypes/typeset.go) | [`typeset.patch`](../internal/upstream/patches/typeset.patch) | [`pkg/analyzer/typeset.go`](../pkg/analyzer/typeset.go) | `_TypeSet` representation, method set lookup, and `computeInterfaceTypeSet` |
| `src/go/types/union.go` | [`gotypes/union.go`](../internal/upstream/testdata/gotypes/union.go) | [`union.patch`](../internal/upstream/patches/union.patch) | [`pkg/analyzer/union.go`](../pkg/analyzer/union.go) | Union term parsing (`parseTilde`), pairwise disjointness (`overlappingTerm`), and nested sum type flattening (`computeUnionTypeSet`) |
| `src/go/types/instantiate.go` | [`gotypes/instantiate.go`](../internal/upstream/testdata/gotypes/instantiate.go) | [`instantiate.patch`](../internal/upstream/patches/instantiate.patch) | [`pkg/analyzer/instantiate.go`](../pkg/analyzer/instantiate.go) | `implements` verification (method set satisfaction + `_TypeSet` inclusion/subset check) |
| `src/go/types/operand.go` | [`gotypes/operand.go`](../internal/upstream/testdata/gotypes/operand.go) | [`operand.patch`](../internal/upstream/patches/operand.patch) | [`pkg/analyzer/operand.go`](../pkg/analyzer/operand.go) | `operand` representation and `assignableTo` with `Registry` sum type hook |
| `src/go/types/assignments.go` | [`gotypes/assignments.go`](../internal/upstream/testdata/gotypes/assignments.go) | [`assignments.patch`](../internal/upstream/patches/assignments.patch) | [`pkg/analyzer/assignments.go`](../pkg/analyzer/assignments.go) | 1:1 and multi-value tuple (`unpackRecv`) assignment verification (`initVars`, `assignVars`, `returnVars`) |

```mermaid
flowchart LR
    subgraph Repo["goaugtype Repository"]
        AdtConst["pkg/analyzer/adt.go<br/>UpstreamBaseRevision + //go:generate"]
        Snapshots["internal/upstream/testdata/gotypes/<br/>7 Pristine go/types Files"]
        Patches["internal/upstream/patches/<br/>7 Unified Diff (.patch) Files"]
        Ported["pkg/analyzer/<br/>{typeterm,termlist,typeset,union,<br/>instantiate,operand,assignments}.go"]
        Config["upstream.json<br/>base_revision + 7 file_hashes"]
        ReleaseSh["Makefile & scripts/release.sh"]
    end

    subgraph Upstream["golang/go (GitHub / Local GOROOT)"]
        Tags["GitHub Tags API<br/>Latest Official Release (go1.x.y)"]
        Files["src/go/types/<br/>{typeset,termlist,typeterm,union,<br/>instantiate,assignments,operand}.go"]
    end

    Snapshots -->|"git apply --3way<br/>(make generate)"| Ported
    Patches -->|"git apply --3way"| Ported
    Ported -. "git diff --no-index<br/>(make update-patches)" .-> Patches
    ReleaseSh -->|"1. Verify clean tree & patch drift"| Ported
    AdtConst <-->|"Must equal"| Config
    Snapshots <-->|"SHA-256 must equal"| Config
    Config <-->|"Must equal latest tag & SHA-256"| Files
    ReleaseSh -->|"2. Check latest Go release"| Tags
```
