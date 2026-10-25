# `goaugtype` 아키텍처 개요 (Architecture Overview)

본 문서는 `goaugtype`의 전체 아키텍처와 큰 그림을 한눈에 파악할 수 있도록 **시스템 토폴로지, 2-Pass 정적 분석 파이프라인, `SumTypeSet` 핵심 데이터 모델, 선언부 서로소 검증 알고리즘, `type switch` 상태 머신, 패키지 간 `analysis.Fact` 전파, 그리고 업스트림 동기화 체계**를 시각적 다이어그램과 함께 설명합니다.

[golang/go#57644](https://github.com/golang/go/issues/57644)에 기반한 상세 언어 스펙 및 설계 근거는 [DESIGN.ko.md](./DESIGN.ko.md) ([DESIGN.md](./DESIGN.md))를 참고하십시오.

---

## 1. 시스템 컨텍스트 및 컴포넌트 토폴로지

`goaugtype`은 크게 3개의 계층으로 구성됩니다:

1. **CLI 및 드라이버 계층 (`cmd/goaugadt`)**: `golang.org/x/tools/go/analysis/singlechecker`를 통해 독립 실행형 린터 CLI 및 외부 린터 연동을 제공합니다.
2. **코어 정적 분석 엔진 (`pkg/analyzer`)**: AST/주석 수집, `SumTypeSet` 타입 셋 구성, 8가지 할당/변환/단언 경로 및 `type switch` 완전성 검증을 수행합니다 ([`adt.go`](../pkg/analyzer/adt.go), [`check.go`](../pkg/analyzer/check.go) 및 `go/types`에서 1:1로 포팅/생성된 7개 파일 [`typeterm.go`](../pkg/analyzer/typeterm.go), [`termlist.go`](../pkg/analyzer/termlist.go), [`typeset.go`](../pkg/analyzer/typeset.go), [`union.go`](../pkg/analyzer/union.go), [`instantiate.go`](../pkg/analyzer/instantiate.go), [`operand.go`](../pkg/analyzer/operand.go), [`assignments.go`](../pkg/analyzer/assignments.go)).
3. **업스트림 스냅샷·패치 파이프라인 및 릴리즈 게이트키퍼 (`cmd/upstreamchecker`, `internal/upstream/`, `scripts/`, `upstream.json`)**: `src/go/types` 원본 7개 파일을 [`internal/upstream/testdata/gotypes/`](../internal/upstream/testdata/gotypes)에 무수정 보관하고, [`internal/upstream/patches/`](../internal/upstream/patches)의 패치를 `go generate ./...` 또는 `./scripts/apply-patches.sh`로 적용하며, `go.mod` ↔ `UpstreamBaseRevision` ↔ `upstream.json` ↔ 원본 스냅샷 + 패치 ↔ 업스트림 `golang/go`의 5중 일관성을 검증합니다.

![Architecture Topology](./diagrams/architecture-topology.svg)
*원본 소스: [`docs/diagrams/architecture-topology.d2`](./diagrams/architecture-topology.d2)*

```mermaid
flowchart TB
    subgraph Tooling["CLI 및 릴리즈 도구 계층"]
        CLI["cmd/goaugadt<br/>(singlechecker 드라이버)"]
        UCheck["cmd/upstreamchecker<br/>(go/types 변경 감지기)"]
        Scripts["scripts/check-upstream.sh<br/>scripts/release.sh"]
        UJSON[("upstream.json<br/>base_revision & SHA-256 해시")]
    end

    subgraph Engine["코어 분석 엔진 (pkg/analyzer)"]
        Entry["Analyzer.Run (run)<br/>check.go"]
        Collector["Pass 1: AST 및 지시자 수집기<br/>(collectInfo / inspector)"]
        Builder["Pass 2: 레지스트리 및 TypeSet 빌더<br/>(BuildRegistry)"]
        Registry["Registry & SumTypeSet<br/>adt.go + {typeterm,termlist,typeset}.go"]
        Checker["Pass 3: 8대 경로 및 Switch 검증기<br/>check.go + assignments.go"]
        Facts["SumTypeFact<br/>(analysis.Fact Gob 직렬화)"]
    end

    subgraph GoStd["Go 툴체인 & x/tools"]
        GoAnalysis["golang.org/x/tools/go/analysis"]
        GoTypes["go/ast + go/types<br/>(TypesInfo, Implements, Eval)"]
    end

    CLI --> Entry
    Scripts --> UCheck
    UCheck --> UJSON
    UCheck -. "UpstreamBaseRevision 일치 검증" .-> Registry

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

## 2. 2-Pass 정적 분석 파이프라인 (`Analyzer.Run`)

Go 언어에서는 타입 선언이 이를 사용하는 함수보다 파일 하단에 위치하거나 패키지 내 다른 파일에 흩어져 있을 수 있습니다. 따라서 `goaugtype`은 **Pass 1(AST 및 주석 수집)**과 **Pass 2(`Registry` 빌드 및 평탄화)**, 그리고 **Pass 3(일괄 정적 검증)**으로 파이프라인을 분리하여 선언 순서와 무관하게 중첩 합타입을 처리합니다.

![Analysis Pipeline](./diagrams/analysis-pipeline.svg)
*원본 소스: [`docs/diagrams/analysis-pipeline.d2`](./diagrams/analysis-pipeline.d2)*

```mermaid
sequenceDiagram
    autonumber
    participant Pass as analysis.Pass
    participant Insp as inspector (check.go)
    participant Reg as BuildRegistry (adt.go)
    participant Chk as check() (check.go)

    Note over Pass,Insp: Phase A: AST 및 지시자 수집 (collectInfo)
    loop pass.Files 내 모든 파일 순회
        Pass->>Insp: ast.NewCommentMap(fset, file, comments)
        Pass->>Insp: ast.Inspect(file, ispt.inspect)
        Insp-->>Insp: // goaugtype: 지시자 추출 (adtDecls)
        Insp-->>Insp: 8대 할당/변환/단언 AST 노드 및 TypeSwitchStmt 수집
    end

    Note over Pass,Reg: Phase B: 2단계 레지스트리 구축 (BuildRegistry)
    Pass->>Reg: BuildRegistry(pass, col.adtDecls)
    loop Stage 1: 원시 타입 평가
        Reg->>Reg: 지시자 내 ~T 및 nil 키워드 거부
        Reg->>Reg: types.Eval(sumType) 및 types.Eval(permittedTerms)
    end
    loop Stage 2: 평탄화, 서로소 검증 및 Fact 내보내기
        Reg->>Reg: 중첩 합타입 재귀 평탄화 (사이클 방지 DFS)
        Reg->>Reg: ConcreteTerms vs. IfaceTerms 분리
        Reg->>Reg: ValidateTerms() (메서드 셋 #9 + 쌍별 서로소 검증)
        Reg->>Pass: ExportObjectFact(TypeName, &SumTypeFact) (유효한 Exported 합타입)
    end

    Note over Pass,Chk: Phase C: 일괄 정적 검증 (check)
    Pass->>Chk: check(pass.TypesInfo, col, reg)
    Chk->>Chk: 1. checkAssign (AssignStmt & ValueSpec, 1:1 및 Tuple 분해)
    Chk->>Chk: 2. checkTypeSwitch (리프 매칭, Narrowing, Shadowing, nil, default)
    Chk->>Chk: 3. checkTypeConversion (T(x) 명시적 변환)
    Chk->>Chk: 4. checkReturn (FuncDecl & FuncLit, 1:1 및 Tuple 반환)
    Chk->>Chk: 5. checkCallArgs (일반 인자, 가변 인자 ..., Tuple 전달)
    Chk->>Chk: 6. checkCompositeLit (Slice, Array, Map K/V, Struct 필드)
    Chk->>Chk: 7. checkSend (ch <- val 채널 전송)
    Chk->>Chk: 8. checkTypeAssert (x.(Tree) 및 tree.(T) 타입 단언)
    Chk-->>Pass: pass.Reportf(pos, diagnostic)
```

---

## 3. 핵심 데이터 모델 (`pkg/analyzer/adt.go`)

표준 라이브러리의 `go/types.Union`은 메서드가 있는 인터페이스(예: `error`)가 유니온에 들어오면 메서드를 무시하고 `.terms`(`allTermlist = 𝓤`)만 합집합하여 타입 셋 전체가 `any`($\mathcal{U}$)로 폭발하는 문제가 있습니다.

이를 방지하기 위해 `SumTypeSet`은 평탄화된 리프 항들을 **`ConcreteTerms`(구체 타입 → `types.Identical` 비교)**와 **`IfaceTerms`(비어 있지 않은 일반 인터페이스 → `types.Implements` 비교)**로 이원화하여 관리합니다:

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

    Registry "1" *-- "*" SumTypeSet : 관리 및 캐싱
    SumTypeSet ..> SumTypeFact : 직렬화 / 역직렬화
    collected ..> Registry : adtDecls 및 AST 노드 전달
```

---

## 4. 선언부 검증 및 서로소 불변식 (`ValidateTerms`)

`BuildRegistry`가 중첩 합타입을 리프 항으로 평탄화한 뒤, `ValidateTerms()`는 `#57644`의 일반 인터페이스 메서드 구현 여부와 **쌍별 포함 서로소(Pairwise Inclusion Disjointness)** 4대 조건을 검증합니다:

![TypeSet Disjointness and Switch State Machine](./diagrams/typeset-and-switch.svg)
*원본 소스: [`docs/diagrams/typeset-and-switch.d2`](./diagrams/typeset-and-switch.d2)*

```mermaid
flowchart TD
    Start["원시 지시자<br/>// goaugtype: T1 | T2 | ..."] --> SyntaxCheck{"'~T' 또는 'nil'<br/>포함 여부?"}
    SyntaxCheck -- 예 --> ErrSyntax["문법 에러 보고<br/>(~T 및 nil 불허)"]
    SyntaxCheck -- 아니오 --> Flatten["중첩 합타입 DFS 평탄화<br/>ConcreteTerms (C) & IfaceTerms (I) 분리"]

    Flatten --> MethodCheck{"SelfIface.NumMethods() > 0?"}
    MethodCheck -- 예 --> VerifyMethods["∀t ∈ C ∪ I 검증:<br/>types.Implements(t, SelfIface)"]
    MethodCheck -- 아니오 --> Disjoint1
    VerifyMethods --> Disjoint1

    subgraph Disjointness["선언부 서로소 불변식 검증 (ValidateTerms)"]
        Disjoint1["1. 구체 타입 중복 검사:<br/>∀i≠j, !types.Identical(C_i, C_j)"] --> Disjoint2["2. 빈 인터페이스 항 금지:<br/>∀I_k ∈ I, I_k.NumMethods() > 0<br/>('any' / 'interface{}' 차단)"]
        Disjoint2 --> Disjoint3["3. 구체 타입 ⊂ 인터페이스 항 금지:<br/>∀C_i ∈ C, ∀I_k ∈ I, !types.Implements(C_i, I_k)<br/>('*MyError | error' 차단)"]
        Disjoint3 --> Disjoint4["4. 인터페이스 항 간 포함 관계 금지:<br/>∀k≠m, !Implements(I_k, I_m) && !Implements(I_m, I_k)<br/>('io.ReadWriter | io.Reader' 차단)"]
    end

    Disjoint4 --> Valid{"검증 에러 0건?"}
    Valid -- 예 --> Export["SumTypeFact 내보내기<br/>(TypeName.Exported()인 경우)"]
    Valid -- 아니오 --> Report["선언부 에러 리포트"]
```

---

## 5. `type switch` 검증 상태 머신 (`checkTypeSwitch`)

Go 런타임에서는 합타입 인터페이스가 기저 메서드 셋(대부분 `interface{}`)으로 타입 소거(Type Erasure)되므로, `checkTypeSwitch`는 **평탄화된 리프 매칭만 허용**하고(중첩 합타입 분기 금지), 상위 인터페이스보다 앞에 오는 **하위 타입 특수화(Narrowing)**는 허용하되 뒤에 오는 **분기 가리움(Shadowing)**은 차단하며, **`case nil:` 필수** 및 **잉여 `default:` 금지**를 수행합니다:

```mermaid
flowchart TD
    Switch["switch x.(type)<br/>(x의 타입은 합타입 S)"] --> Init["초기화:<br/>unhandled = ConcreteTerms ∪ IfaceTerms<br/>seenConcrete = ∅, seenIfaces = ∅<br/>nilHandled = false, defaultClause = nil"]

    Init --> NextCase["다음 CaseClause 표현식 T 검사"]
    NextCase --> IsDefault{"'default:' 분기인가?"}
    IsDefault -- 예 --> SaveDefault["defaultClause = clause 기록<br/>(unhandled를 소거하지 않음)"]
    IsDefault -- 아니오 --> IsNil{"'case nil:'인가?"}

    IsNil -- 예 --> CheckDupNil{"nilHandled == true?"}
    CheckDupNil -- 예 --> ErrDupNil["중복/가려진 'nil' 에러 보고"]
    CheckDupNil -- 아니오 --> MarkNil["nilHandled = true"]

    IsNil -- 아니오 --> CheckNested{"reg.Lookup(T) != nil?<br/>(T가 합타입인가?)"}
    CheckNested -- 예 --> ErrNested["유효하지 않은 케이스 보고:<br/>런타임 타입 소거로 중첩 합타입 분기 불가"]
    CheckNested -- 아니오 --> CheckPermitted{"S.Includes(T)?"}
    CheckPermitted -- 아니오 --> ErrInvalid["유효하지 않은 케이스 보고:<br/>T는 S의 허용 범위 밖"]

    CheckPermitted -- 예 --> CheckShadow{"T가 seenConcrete/seenIfaces에 존재<br/>OR ∃I ∈ seenIfaces: Implements(T, I)?"}
    CheckShadow -- 예 --> ErrShadow["Shadowed 케이스 에러 보고:<br/>선행 분기에 의해 이미 가려짐"]
    CheckShadow -- 아니오 --> UpdateState["seenConcrete / seenIfaces에 T 기록<br/>unhandled에서 T 제거"]

    SaveDefault --> More{"다음 케이스 존재?"}
    ErrDupNil --> More
    MarkNil --> More
    ErrNested --> More
    ErrInvalid --> More
    ErrShadow --> More
    UpdateState --> More

    More -- 예 --> NextCase
    More -- 아니오 --> CheckExhaustive{"unhandled == ∅<br/>AND nilHandled?"}
    CheckExhaustive -- 아니오 --> ErrMissing["Non-Exhaustive 에러 보고<br/>(누락된 리프 항 및 nil 나열)"]
    CheckExhaustive -- 예 --> CheckRedundantDefault{"defaultClause != nil?"}
    CheckRedundantDefault -- 예 --> ErrDefault["잉여 'default:' 에러 보고<br/>(완전한 switch 내 죽은 분기)"]
    CheckRedundantDefault -- 아니오 --> Pass["Type Switch 검증 통과 ✓"]
```

---

## 6. `analysis.Fact` 기반 패키지 간(Cross-Package) 분석 흐름

외부 패키지(`pkga`)에 선언된 합타입을 `pkgb`에서 import할 때, `pkga`의 AST 주석은 `pkgb` 분석 시점에 존재하지 않습니다. `goaugtype`은 `SumTypeFact`를 통해 패키지 경계를 넘어 합타입 메타데이터를 전달합니다:

```mermaid
sequenceDiagram
    participant PkgA as Package A (pkga)
    participant Framework as go/analysis Fact 저장소
    participant PkgB as Package B (pkgb)

    Note over PkgA: 선언: // goaugtype: *Alpha | *Beta<br/>type ExportedSum any
    PkgA->>PkgA: BuildRegistry에서 평탄화 및 서로소 검증
    PkgA->>Framework: pass.ExportObjectFact(ExportedSum, &SumTypeFact{<br/>ConcreteTerms: ["*pkga.Alpha", "*pkga.Beta"]})

    Note over PkgB: "pkga"를 import하여 사용<br/>var x pkga.ExportedSum = ...
    PkgB->>PkgB: reg.Lookup(pkga.ExportedSum) 호출
    PkgB->>Framework: pass.ImportObjectFact(obj, &fact) 조회
    Framework-->>PkgB: SumTypeFact{ConcreteTerms: ["*pkga.Alpha", "*pkga.Beta"]} 반환
    PkgB->>PkgB: resolveTypeByCanonicalName(pass.Pkg, obj.Pkg(), name)으로 types.Type 복원
    PkgB->>PkgB: 복원된 *SumTypeSet을 Registry에 캐싱
    PkgB->>PkgB: pkga.ExportedSum에 대해 8대 경로 및 type switch 완전성 검증 수행
```

---

## 7. 업스트림(`go/types`) 스냅샷·패치 파이프라인 및 릴리즈 게이트키퍼

포팅된 7개의 `go/types` 파일들을 투명하게 유지보수하기 위해, `goaugtype`은 [`internal/upstream/testdata/gotypes/`](../internal/upstream/testdata/gotypes)에 무수정 원본 스냅샷을 보관하고 [`internal/upstream/patches/`](../internal/upstream/patches)에 Unified Diff 패치 파일을 관리합니다. `go.mod`의 `go` 버전이 기준 버전의 단일 진실 공급원(Single Source of Truth)으로 동작하며, `cmd/upstreamchecker`, `scripts/*.sh`, `scripts/release.sh`는 **`go.mod` ↔ 코드 상수(`UpstreamBaseRevision`) ↔ `upstream.json` ↔ 로컬 원본 스냅샷 + 패치 적용 결과물 ↔ 업스트림 Go 릴리즈**의 5중 무결성을 강제합니다:

| 업스트림 `golang/go` 원본 | 로컬 무수정 스냅샷 | 패치 파일 | 포팅/생성된 `pkg/analyzer` 대상 | 주요 역할 |
| :--- | :--- | :--- | :--- | :--- |
| `src/go/types/typeterm.go` | [`gotypes/typeterm.go`](../internal/upstream/testdata/gotypes/typeterm.go) | [`typeterm.patch`](../internal/upstream/patches/typeterm.patch) | [`pkg/analyzer/typeterm.go`](../pkg/analyzer/typeterm.go) | 단일 타입 항 집합 연산 (`includes`, `subsetOf`, `disjoint`, `union`, `intersect`) 및 일반 인터페이스 항 지원 |
| `src/go/types/termlist.go` | [`gotypes/termlist.go`](../internal/upstream/testdata/gotypes/termlist.go) | [`termlist.patch`](../internal/upstream/patches/termlist.patch) | [`pkg/analyzer/termlist.go`](../pkg/analyzer/termlist.go) | 정규화된 항 리스트 집합 대수 (`norm`, `includes`, `supersetOf`, `subsetOf`) |
| `src/go/types/typeset.go` | [`gotypes/typeset.go`](../internal/upstream/testdata/gotypes/typeset.go) | [`typeset.patch`](../internal/upstream/patches/typeset.patch) | [`pkg/analyzer/typeset.go`](../pkg/analyzer/typeset.go) | `_TypeSet` 구조체, 메서드 셋 조회 및 `computeInterfaceTypeSet` |
| `src/go/types/union.go` | [`gotypes/union.go`](../internal/upstream/testdata/gotypes/union.go) | [`union.patch`](../internal/upstream/patches/union.patch) | [`pkg/analyzer/union.go`](../pkg/analyzer/union.go) | 유니온 항 파싱(`parseTilde`), 쌍별 서로소 검사(`overlappingTerm`), 중첩 합타입 평탄화(`computeUnionTypeSet`) |
| `src/go/types/instantiate.go` | [`gotypes/instantiate.go`](../internal/upstream/testdata/gotypes/instantiate.go) | [`instantiate.patch`](../internal/upstream/patches/instantiate.patch) | [`pkg/analyzer/instantiate.go`](../pkg/analyzer/instantiate.go) | `implements` 검증 (합타입 메서드 셋 구현 + `_TypeSet` 포함/부분집합 판정) |
| `src/go/types/operand.go` | [`gotypes/operand.go`](../internal/upstream/testdata/gotypes/operand.go) | [`operand.patch`](../internal/upstream/patches/operand.patch) | [`pkg/analyzer/operand.go`](../pkg/analyzer/operand.go) | `operand` 표현 및 `Registry` 합타입 후킹이 적용된 `assignableTo` |
| `src/go/types/assignments.go` | [`gotypes/assignments.go`](../internal/upstream/testdata/gotypes/assignments.go) | [`assignments.patch`](../internal/upstream/patches/assignments.patch) | [`pkg/analyzer/assignments.go`](../pkg/analyzer/assignments.go) | 1:1 및 다중 반환 튜플(`unpackRecv`) 대입 검증 (`initVars`, `assignVars`, `returnVars`) |

```mermaid
flowchart LR
    subgraph Repo["goaugtype 레포지토리"]
        GoMod["go.mod<br/>Go 기준 버전 (단일 진실 공급원)"]
        AdtConst["pkg/analyzer/adt.go<br/>UpstreamBaseRevision + //go:generate"]
        Snapshots["internal/upstream/testdata/gotypes/<br/>무수정 원본 스냅샷 7개"]
        Patches["internal/upstream/patches/<br/>Unified Diff (.patch) 7개"]
        Ported["pkg/analyzer/<br/>{typeterm,termlist,typeset,union,<br/>instantiate,operand,assignments}.go"]
        Config["upstream.json<br/>base_revision, 디렉토리 경로 및 files[]"]
        ReleaseSh["scripts/*.sh 및 릴리즈 파이프라인"]
    end

    subgraph Upstream["golang/go (GitHub / 로컬 GOROOT)"]
        Tags["대상 릴리즈 태그 / GOROOT<br/>(go.mod 버전과 일치)"]
        Files["src/go/types/<br/>{typeset,termlist,typeterm,union,<br/>instantiate,assignments,operand}.go"]
    end

    Snapshots -->|"git apply --3way<br/>(go generate / apply-patches.sh)"| Ported
    Patches -->|"git apply --3way"| Ported
    Ported -. "git diff --no-index<br/>(update-patches.sh)" .-> Patches
    ReleaseSh -->|"1. 워킹 트리, 5중 일관성 및 패치 드리프트 검증"| Ported
    GoMod <-->|"반드시 일치"| AdtConst
    GoMod <-->|"반드시 일치"| Config
    AdtConst <-->|"반드시 일치"| Config
    Snapshots <-->|"SHA-256 해시 일치"| Config
    Config <-->|"대상 태그 및 SHA-256 일치"| Files
    ReleaseSh -->|"2. 업스트림 대상 버전 대조"| Tags
```
