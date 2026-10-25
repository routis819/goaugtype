# `goaugtype` Design Document: Sum Types Based on General Interfaces (#57644)

## 1. 배경 및 목표 (Background & Vision)

`goaugtype`(`goaugadt`)은 Go 언어에서 정적 분석(`golang.org/x/tools/go/analysis`)을 통해 **합타입(Sum Types / Algebraic Data Types)**을 지원하는 린터(Linter)입니다.

본 설계 문서는 초기 프로토타입에서 발견된 스펙상의 모호함과 타입 안전성 구멍(Soundness Holes)을 해결하기 위해, Go 코어 팀의 Ian Lance Taylor가 제안한 **[golang/go#57644 (`proposal: spec: sum types based on general interfaces`)](https://github.com/golang/go/issues/57644)의 순수 모델(Pure Model)**을 프로젝트의 이론적·명세적 기준으로 삼아 `goaugtype`의 스펙과 아키텍처를 재정립합니다.

### 핵심 설계 철학
1. **`golang/go#57644`의 순수 선행 구현(Pure Pre-implementation)**:
   - `#57644`는 현재 제네릭 타입 제약(Type parameter constraint)에만 허용되는 타입 유니온(`T1 | T2`)을 **일반 인터페이스(Ordinary/General Interface)**에도 허용하자는 제안입니다.
   - 현행 Go 컴파일러는 변수 타입으로 사용되는 일반 인터페이스 내에 `T1 | T2`를 직접 작성하면 컴파일 에러(`interface contains type constraints`)를 발생시킵니다.
   - 따라서 `goaugtype`은 표준 Go 문법과 호환되는 인터페이스 선언(`type I any` 또는 `type I interface { ... }`) 위에 `// goaugtype: T1 | T2` 주석 지시자를 부여하여, **`#57644`의 타입 셋(Type Set) 규칙을 있는 그대로 정적 분석기로 에뮬레이션**합니다.
2. **타입 셋(Type Set) 기반의 일관된 시맨틱 및 서로소 직교성(Disjoint Orthogonality)**:
   - 합타입을 별도의 특수한 문법 체계나 커스텀 `nil` 어노테이션으로 정의하지 않고, Go 1.18+ 스펙의 **인터페이스 타입 셋(Type Set)과 구현(Implements / Assignability) 규칙**의 자연스러운 확장으로 정의합니다.
   - 여기에 더해 평탄화(Flattening)된 유니온 항들 간의 **선언부 서로소(Pairwise Disjointness)**를 강제함으로써, 합타입에 새로운 타입을 추가했을 때 상위 인터페이스에 흡수되어 기존 `type switch` 문이 조용히 통과해 버리는 사각지대를 원천 차단합니다.
3. **정적 완전성(Exhaustiveness) 보장 및 타입 소거(Type Erasure) 안전성**:
   - `#57644` 언어 제안 자체는 하위 호환성 문제로 컴파일러 차원의 `type switch` 완전성 강제를 유보했으나, `goaugtype`은 린터로서 `type switch`가 평탄화된 모든 리프 원소와 인터페이스의 제로값(`nil`)을 빠짐없이 처리했는지 검증하고, 런타임 타입 소거로 인한 분기 가리움(Shadowing)과 잉여 `default:`를 차단합니다.

---

## 2. 상세 스펙 명세 (Specification Aligned with Pure #57644 Model)

### 2.1. 일반 인터페이스(General Interface) 지원
> **#57644 원문**: *"We propose that we permit an ordinary interface type to embed a union of terms, where each term is itself a type."*

- **기존 제약(폐기)**: 기존 [`analysisTypeSpec`](../pkg/analyzer/check.go)은 `type T any` 또는 메서드가 없는 빈 `interface{}`만 합타입으로 허용했습니다.
- **변경 스펙**:
  - 빈 인터페이스(`any`, `interface{}`)뿐만 아니라 **메서드 시그니처를 포함한 모든 인터페이스 선언**에 `// goaugtype:` 지시자를 사용할 수 있습니다.
  - 인터페이스 $I$가 메서드 집합 $M$과 유니온 지시자 `// goaugtype: T_1 | T_2 | ... | T_n`을 모두 가질 때, $I$의 최종 타입 셋은 **메서드 집합 $M$을 구현하는 타입들의 집합과 유니온 타입들의 교집합(Intersection)**입니다.
  - 선언 시점 검증: 유니온에 나열된 각 타입 $T_k$가 인터페이스 $I$의 메서드 집합 $M$을 구현하지 않으면(`!types.Implements(T_k, I)`), 합타입 선언 위치에서 즉시 진단 에러를 보고합니다.

```go
// goaugtype: *Leaf | *Node
type Tree interface {
    Walk()
}
```

### 2.2. 유니온 항(Union Terms)의 엄격성 및 `~T`, `nil` 키워드 불허
> **#57644 원문**: *"We propose that we permit an ordinary interface type to embed a union of terms, where each term is itself a type... This proposal does not permit the underlying type syntax `~T` to be used in an ordinary interface type... The types `MyInt` and `MyFloat` implement `I1`. The type `MyOtherInt` does not implement `I1`."*

- **유니온 항에는 타입만 허용 (`nil` 지시자 키워드 제거)**:
  - `#57644`에서 유니온 `T1 | T2`의 각 항은 반드시 **타입(Type)**이어야 합니다. `nil`은 타입이 아니라 사전 선언된 비타입 식별자(제로값)이므로 `// goaugtype:` 지시자 내에 `nil`을 작성하지 않습니다.
  - 지시자에 `nil`이 포함될 경우 유효하지 않은 유니온 타입 항으로 진단 에러를 보고합니다.
- **근사 타입(`~T`) 불허**:
  - `#57644`에서 Ian Lance Taylor가 밝혔듯, `~int`와 같은 기저 타입(Underlying type) 근사 문법은 외부 패키지에서 `type switch`로 구체 타입을 추출할 수 있는 언어적 메커니즘이 없으므로 허용하지 않습니다.
  - 지시자에 `~T`가 포함될 경우 명시적인 문법 에러(`underlying type syntax ~T is not permitted in sum types`)를 보고합니다.
- **구체 타입(Concrete Type)의 엄격한 일치**:
  - 유니온 항이 비-인터페이스(구체 타입) $T_k$인 경우, 값의 타입 $V$는 `types.Identical(V, T_k)`를 만족해야 합니다. 기저 타입이 같더라도 다른 정의 타입(Defined Type)이면 허용되지 않습니다.
- **비타입 상수(Untyped Constants)의 대입**:
  - Go 표준 Assignability 규칙에 따라, 비타입 상수(예: `10`, `"hello"`)가 인터페이스에 대입될 때는 해당 상수의 기본 타입(Default Type: `int`, `float64`, `string`, `bool` 등)으로 평가된 후 타입 셋 포함 여부를 판정합니다.
  - 예: `// goaugtype: int8 | int16`인 `SmallInt`에 비타입 상수 `10`(기본 타입 `int`)을 대입하면 `int`가 타입 셋에 없으므로 거부됩니다(`int8(10)`으로 명시적 변환 필요).

### 2.3. 인터페이스 항, 중첩 합타입, 그리고 선언부 서로소 원칙(Disjoint Union Invariant)
> **#57644 원문**: *"Embedding a union in an interface affects the interface's type set."*

Go의 인터페이스 유니온에서 각 항(Term)은 구체 타입일 수도 있고, 다른 인터페이스나 또 다른 합타입일 수도 있습니다.

1. **중첩 합타입의 평탄화(Type Set Flattening) 및 부분집합 대입**:
   - 합타입 $I_2$의 허용 목록에 다른 합타입 $I_1$이 포함되면(`// goaugtype: I1 | float64`), $I_2$의 유효 타입 셋은 $\text{TypeSet}(I_1) \cup \{\text{float64}\}$로 평탄화됩니다.
   - 합타입 $S$의 값을 합타입 $T$에 대입할 때, $S$와 $T$가 동일한 타입(`types.Identical`)이 아니더라도 **$\text{TypeSet}(S) \subseteq \text{TypeSet}(T)$이면 대입을 허용**합니다.
2. **일반 인터페이스 항 지원 (Issue #5 연계)**:
   - 허용 목록에 합타입이 아닌 일반 인터페이스(예: `error`, `io.Reader`)가 포함된 경우, 대입되는 값의 타입 $V$가 해당 인터페이스를 구현(`types.Implements(V, TermInterface)`)하면 유효한 대입으로 판정합니다.
3. **선언부 서로소 불변식 (Disjoint Union Invariant — No Overlapping Terms)**:
   - Haskell이나 Rust의 Tagged Union과 동일한 수준의 엄격한 완전성 검사(합타입에 새로운 항을 추가하면 기존 `type switch` 문에서 반드시 미처리 케이스 에러가 발생함)를 보장하기 위해, 평탄화를 거친 최종 리프 항들 사이에 **쌍별 포함 서로소(Pairwise Inclusion Disjointness)**를 강제합니다:
     1. **구체 타입 중복 금지**: 평탄화된 두 구체 타입 항 $C_1, C_2$가 `types.Identical(C_1, C_2)`이면 중복 구체 타입 에러를 보고합니다.
     2. **일반 인터페이스 중첩 및 빈 인터페이스 금지**:
        - 모든 일반(비-합타입) 인터페이스 항 $I$는 최소 1개 이상의 메서드를 선언해야 합니다(`I.NumMethods() > 0`; `any` / `interface{}`는 거부됨).
        - 구체 타입 항 $C$가 같은 합타입 내의 일반 인터페이스 항 $I$를 구현하면(`types.Implements(C, I)`) 선언 에러를 보고합니다. 예를 들어 `// goaugtype: *MyError | error`는 `*MyError`가 이미 `error`의 부분집합이므로 선언 시점에 거부됩니다.
        - 두 일반 인터페이스 항 $I_1, I_2$가 포함 관계(`types.Implements(I_1, I_2)` 또는 `types.Implements(I_2, I_1)`)를 가지면 선언 에러를 보고합니다.

### 2.4. 순수 #57644 모델의 제로값(`nil`) 시맨틱
> **#57644 원문**: *"In particular, the zero value of an interface type with an embedded union would be `nil`, just as for any interface type. So this is a form of sum type in which there is always another possible option, namely `nil`."*

순수 `#57644` 모델에서 합타입은 타입 셋이 제한된 일반 Go 인터페이스이므로, 제로값 시맨틱은 예외 없이 Go 인터페이스의 기본 규칙을 그대로 따릅니다:

1. **모든 합타입의 제로값은 항상 `nil`**:
   - 모든 합타입 변수의 제로값은 `nil`입니다. 따라서 초기값 없는 변수 선언(`var x Tree`), 복합 리터럴의 제로 초기화 필드, 명시적인 `nil` 대입(`x = nil`)은 모든 합타입에서 **항상 유효**합니다.
2. **`type switch`에서 `case nil:` 항상 필수**:
   - 모든 합타입 인터페이스에는 항상 `nil`이라는 선택지가 존재하므로, 합타입에 대한 모든 `switch x.(type)` 문은 유니온 타입 항들과 더불어 **`case nil:`을 항상 명시적으로 처리**해야 완전한 것으로 인정합니다.
3. **Typed `nil`과 Untyped `nil`의 구분**:
   - `var leaf *Leaf = nil; var t Tree = leaf`에서 `t`는 인터페이스로서 `nil`이 아니라 동적 타입 `*Leaf`를 가진 값(`(*Leaf)(nil)`)입니다. 이는 `case *Leaf:`로 분기되며, 타입 셋 차원에서는 `*Leaf`로 취급됩니다.

### 2.5. 완전한 대입/변환/단언 검증 범위 (8 Paths)
> **#57644 원문**: *"As always, a variable of interface type may store a value of any type that is in its type set... Inversely, a variable of interface type may not store a value of any type that is not in its type set."*

`#57644`에서 타입 셋에 속하지 않는(그리고 `nil`이 아닌) 값은 어떠한 경로로도 해당 인터페이스 타입의 값으로 저장·변환·단언될 수 없습니다. 따라서 `goaugtype`은 Go 스펙상 **Assignability, Type Conversion, Type Assertion이 발생하는 8가지 AST 지점을 전수 검사**합니다:

| 번호 | 검사 지점 (AST Node) | 예시 코드 | 기존 동작 | 목표 동작 |
| :--- | :--- | :--- | :--- | :--- |
| 1 | 일반 대입문 (`*ast.AssignStmt`) | `x = val`, `s.field = val`, `arr[i] = val`, `*p = val` | 단순 `*ast.Ident`만 검사 (Issue #2) | 모든 LHS 표현식에 대해 `TypesInfo.TypeOf(lhs)`로 검사 + 다중 반환(`a, b = fn()`) 처리 |
| 2 | 변수/상수 선언 (`*ast.ValueSpec`) | `var x Tree = val`, `var ( a Tree = v1; b Tree = v2 )` | 첫 번째 `Specs[0]`만 검사 | 모든 `GenDecl.Specs`를 순회하며 초기값(`Values`)이 합타입에 유효한지 검사 |
| 3 | **명시적 타입 변환 (`*ast.CallExpr`)** | `Tree(NotTree{})` | **검사 누락 (우회 가능 치명적 구멍)** | `TypesInfo.Types[call.Fun].IsType()`일 때 대상 타입이 합타입이면 `call.Args[0]` 검사 |
| 4 | 함수 반환문 (`*ast.ReturnStmt`) | `func f() Tree { return NotTree{} }` | 미검사 (Issue #1) | 현재 함수의 반환 시그니처(`*types.Signature`)와 `return` 표현식들을 대조 검사 |
| 5 | 함수 호출 인자 (`*ast.CallExpr`) | `takeTree(NotTree{})` | 미검사 | 호출 대상 함수의 파라미터 타입(가변 인자 포함)이 합타입이면 인자 표현식 검사 |
| 6 | 복합 리터럴 (`*ast.CompositeLit`) | `[]Tree{NotTree{}}`, `MyStruct{TreeField: NotTree{}}` | 미검사 | 슬라이스/배열/맵/구조체 리터럴의 각 요소가 대입되는 대상 타입이 합타입이면 검사 |
| 7 | 채널 전송 (`*ast.SendStmt`) | `ch <- NotTree{}` (`var ch chan Tree`) | 미검사 | 채널의 엘리먼트 타입(`chan.Elem()`)이 합타입이면 전송 표현식 검사 |
| 8 | **타입 단언 (`*ast.TypeAssertExpr`)** | `a.(Tree)`, `tree.(NotTree)` | 미검사 | 런타임에 `Tree`가 인터페이스로 소거되어 모든 타입이 통과하는 구멍을 막기 위해 합타입으로의 무분별한 타입 단언(`a.(Tree)`) 및 합타입에서 허용되지 않은 타입으로의 단언(`tree.(NotTree)`) 차단 |

### 2.6. `type switch` 검증 규칙 (`checkTypeSwitch`)
1. **임의의 타겟 표현식 지원**:
   - `switch x.(type)`에서 `x`가 단순 변수(`*ast.Ident`)뿐만 아니라 `s.tree.(type)`, `getTree().(type)`, `trees[0].(type)` 등 임의의 표현식이더라도 `TypesInfo.TypeOf(targetExpr)`를 통해 합타입 여부를 식별합니다.
2. **평탄화된 리프 매칭 강제 (중첩 합타입 분기 금지)**:
   - Go 런타임에서 중첩 합타입(예: `type SmallInt any`)은 빈 인터페이스(`any`) 또는 메서드 셋으로 타입 소거(Type Erasure)됩니다. 따라서 `AllInt`(`SmallInt | int32 | int64`)에 대한 `switch`에서 `case SmallInt:`를 허용하면 런타임에 `int32`, `int64`까지 모두 `case SmallInt:`가 집어삼키는 치명적인 버그가 발생합니다.
   - 따라서 `type switch`의 `case`에는 중첩 합타입 자체를 사용할 수 없으며, 반드시 평탄화된 리프 항(`case int8, int16:`)으로 매칭해야 합니다.
3. **하위 타입 특수화(Narrowing) 허용 및 분기 가리움(Shadowing) 차단**:
   - 선언부에서는 `*MyError | error`가 금지되지만, `string | error` 합타입에 대한 `switch x.(type)` 문에서는 `case error:` **앞에** 구체화된 `case *MyError:` 분기를 작성하는 것(Narrowing)은 허용됩니다.
   - 이때 `*MyError`는 `error`에 속하므로 유효한 케이스로 인정되지만, `*MyError`만으로는 `error` 전체가 소거되지 않으므로 뒤에 `case error:`도 반드시 존재해야 합니다.
   - 반대로 `case error:`가 먼저 등장한 **뒤에** `case *MyError:`가 등장하거나 동일한 케이스가 중복 등장하면, 런타임에 앞선 분기에 의해 가로채어져 절대 도달할 수 없으므로 **분기 가리움(Shadowed / Unreachable case) 에러**를 보고합니다.
4. **`case nil:` 절대 필수 및 잉여 `default:` 금지**:
   - 평탄화된 모든 리프 항(`ConcreteTerms`, `IfaceTerms`)과 `case nil:`이 반드시 명시되어야 합니다.
   - 모든 리프 항과 `case nil:`이 처리된 상태에서 `default:` 절은 도달 불가능한 죽은 코드이거나 새로운 타입 추가 시 린터를 무력화하고 내부에서 `nil` 패닉을 유발할 수 있으므로, `default:` 절은 누락된 케이스를 대체하지 않으며 잉여 `default:` 절 사용을 금지합니다.

### 2.7. 패키지 간(Cross-Package) 분석 아키텍처 (`analysis.Fact`)
- **문제**: Go의 컴파일 단위는 패키지이며, 다른 패키지에서 import한 타입의 AST 주석(`// goaugtype:`)은 현재 패키지의 `pass.Files`에 존재하지 않습니다.
- **해결 설계**:
  - `go/analysis` 프레임워크의 **`analysis.Fact`** 메커니즘을 도입합니다.
  - 패키지 분석 시 합타입으로 선언된 `*types.TypeName` 객체에 직렬화 가능한 Fact 구조체(허용된 타입들의 패키지 경로 및 타입명)를 `pass.ExportObjectFact(typeNameObj, &fact)`로 내보냅니다.
  - 외부 패키지의 타입을 사용할 때는 `pass.ImportObjectFact(obj, &fact)`로 합타입 메타데이터를 조회하여, 외부 패키지에서 정의된 합타입에 대해서도 대입 및 `type switch` 완전성 검사가 동일하게 작동하도록 합니다.

### 2.8. `go/types` 코드 직접 포팅 기반 엔진 설계 및 업스트림 자동 추적 (#14)

Go 표준 라이브러리의 `go/types` 패키지(`typeset.go`, `termlist.go`, `typeterm.go`, `union.go`, `instantiate.go`, `operand.go`, `assignments.go`)에는 이미 타입 셋 연산, 유니온 중복 검사, `implements`, 그리고 변수 대입/다중 반환 튜플 분해 로직이 구현되어 있으며, `golang.org/x/tools/internal/typeparams/copytermlist.go` 역시 이를 복사·변형하여 사용하고 있습니다.

[`go.mod`](../go.mod)의 `go` 지시자가 기준 버전의 단일 진실 공급원(Single Source of Truth)으로 동작합니다. [`pkg/analyzer/adt.go`](../pkg/analyzer/adt.go)의 `UpstreamBaseRevision`과 [`upstream.json`](../upstream.json)의 `base_revision`은 항상 `go.mod` 버전에 일치해야 하며, `cmd/upstreamchecker`와 `scripts/release.sh`는 5중 무결성(`go.mod` ↔ `UpstreamBaseRevision` ↔ `upstream.json` ↔ 로컬 원본 스냅샷 + 패치 ↔ 업스트림 `golang/go`)을 강제합니다.

#### 업스트림(`src/go/types`) ↔ 원본 스냅샷 ↔ 패치 ↔ `pkg/analyzer` 1:1 매핑
총 7개의 업스트림 원본 파일이 [`internal/upstream/testdata/gotypes/`](../internal/upstream/testdata/gotypes)에 **100% 무수정 상태**로 보관되며, [`internal/upstream/patches/`](../internal/upstream/patches)의 Unified Diff 패치를 `go generate ./...` 또는 `./scripts/apply-patches.sh`로 적용하여 [`pkg/analyzer/`](../pkg/analyzer) 내 7개 파일을 생성합니다:

| 업스트림 `golang/go` 원본 | 로컬 무수정 스냅샷 | 패치 파일 | 포팅/생성된 `pkg/analyzer` 파일 | 포팅된 주요 심볼 및 `[goaugtype]` 확장 내역 |
| :--- | :--- | :--- | :--- | :--- |
| `src/go/types/typeterm.go` | [`gotypes/typeterm.go`](../internal/upstream/testdata/gotypes/typeterm.go) | [`typeterm.patch`](../internal/upstream/patches/typeterm.patch) | [`pkg/analyzer/typeterm.go`](../pkg/analyzer/typeterm.go) | `term`, `(*term).String`, `equal`, `union`, `intersect`, `includes`, `subsetOf`, `disjoint` — 비어 있지 않은 일반 인터페이스 항에 대해 `types.Implements` 판정 추가 (`#5`). |
| `src/go/types/termlist.go` | [`gotypes/termlist.go`](../internal/upstream/testdata/gotypes/termlist.go) | [`termlist.patch`](../internal/upstream/patches/termlist.patch) | [`pkg/analyzer/termlist.go`](../pkg/analyzer/termlist.go) | `termlist`, `allTermlist`, `String`, `isEmpty`, `isAll`, `norm`, `union`, `intersect`, `equal`, `includes`, `supersetOf`, `subsetOf` — 원본 알고리즘 100% 보존. |
| `src/go/types/typeset.go` | [`gotypes/typeset.go`](../internal/upstream/testdata/gotypes/typeset.go) | [`typeset.patch`](../internal/upstream/patches/typeset.patch) | [`pkg/analyzer/typeset.go`](../pkg/analyzer/typeset.go) | `_TypeSet`, `IsEmpty`, `IsAll`, `IsMethodSet`, `IsComparable`, `NumMethods`, `Method`, `LookupMethod`, `String`, `hasTerms`, `subsetOf`, `terms` (`computeInterfaceTypeSet`). |
| `src/go/types/union.go` | [`gotypes/union.go`](../internal/upstream/testdata/gotypes/union.go) | [`union.patch`](../internal/upstream/patches/union.patch) | [`pkg/analyzer/union.go`](../pkg/analyzer/union.go) | `parseTilde`(`~T` 및 `nil` 금지, `#9`), `overlappingTerm`(구체↔구체, 구체↔인터페이스, 인터페이스↔인터페이스 포함/중복 감지), `computeUnionTypeSet`(`Registry`를 통한 중첩 합타입 재귀 평탄화, `#10`). |
| `src/go/types/instantiate.go` | [`gotypes/instantiate.go`](../internal/upstream/testdata/gotypes/instantiate.go) | [`instantiate.patch`](../internal/upstream/patches/instantiate.patch) | [`pkg/analyzer/instantiate.go`](../pkg/analyzer/instantiate.go) | `implements` — 합타입 자체의 메서드 셋 구현 검증(`#9`) 및 타입 셋 포함/부분집합 판정(`tset.subsetOf` / `tset.includes`). |
| `src/go/types/operand.go` | [`gotypes/operand.go`](../internal/upstream/testdata/gotypes/operand.go) | [`operand.patch`](../internal/upstream/patches/operand.patch) | [`pkg/analyzer/operand.go`](../pkg/analyzer/operand.go) | `operandMode`, `operand`, `(*operand).assignableTo` — `Registry.Lookup(T)`를 후킹하여 `implements` 기반 합타입 대입 가능성 검증. |
| `src/go/types/assignments.go` | [`gotypes/assignments.go`](../internal/upstream/testdata/gotypes/assignments.go) | [`assignments.patch`](../internal/upstream/patches/assignments.patch) | [`pkg/analyzer/assignments.go`](../pkg/analyzer/assignments.go) | `assignment`, `initVar`, `assignVar`, `unpackRecv`, `initVars`, `assignVars`, `returnVars` — `var`, `=`, `:=`, `return`에서의 1:1 및 다중 반환 튜플(`CommaOk` / `()`) 분해 대입 검증. |

#### 왜 `types.Union`을 그대로 호출하지 않고 `go/types` 내부 구현을 포팅하는가?
1. **메서드가 있는 인터페이스 항의 `allTermlist`(`𝓤`) 폭발 방지 (Issue #5 연계)**:
   - 현행 Go 제네릭은 유니온 내에 메서드가 있는 인터페이스(예: `error`)를 문법적으로 금지하므로, `go/types`의 `computeUnionTypeSet`은 인터페이스 항을 만났을 때 메서드를 무시하고 `.terms`(`allTermlist = 𝓤`)만 합집합해 버립니다.
   - 따라서 `types.NewUnion`에 `string | error`를 그대로 넣으면 타입 셋 전체가 `any`(`𝓤`)로 오염됩니다.
2. **중첩 합타입(`I1 | float64`)의 기저 타입(`any`) 치환**:
   - 실제 Go 코드에서 `type I1 any`의 기저 타입은 빈 인터페이스이므로, `go/types`가 아닌 `goaugtype`의 자체 합타입 레지스트리에서 `I1`의 타입 셋을 찾아 평탄화해야 합니다.
3. **선언부 서로소 검증 및 `type switch` 차집합(Missing Cases) 계산**:
   - `overlappingTerm`을 통한 쌍별 서로소(Pairwise Disjointness) 검증과 `type switch`의 완전성(Set Cover / Difference) 연산을 수행해야 합니다.

#### `SumTypeSet` 데이터 모델 ([`pkg/analyzer/adt.go`](../pkg/analyzer/adt.go) 내 `_TypeSet` 래퍼)
```go
type SumTypeSet struct {
    Name          *types.TypeName
    SumType       types.Type
    SelfIface     *types.Interface
    RawTerms      []types.Type
    ConcreteTerms []types.Type // 구체 타입 항들 (types.Identical 비교)
    IfaceTerms    []types.Type // 비어 있지 않은 일반 인터페이스 항들 (types.Implements 비교, #5)
    Pos           token.Pos
    tset          *_TypeSet    // 포팅된 go/types _TypeSet 내부 표현
}
```
- `ValidateTerms() []augtError`: `computeUnionTypeSet`(`overlappingTerm`) 및 `implements`(`#9`)를 호출하여 메서드 셋 구현 여부 및 쌍별 서로소(Pairwise Disjointness) 검증.
- `Includes(V types.Type) bool`: `tset.includes(V)`(`termlist.go`의 `termlist.includes`)로 위임.
- `SubsetOf(other *SumTypeSet) bool`: `tset.subsetOf(other.tset)`(`typeset.go`의 `_TypeSet.subsetOf`, `#10`)로 위임.

#### 업스트림(`golang/go`) 코드 변경 자동 추적 전략
참조 원본인 7개 파일 `src/go/types/{typeset,termlist,typeterm,union,instantiate,assignments,operand}.go`(원본 소스: `src/cmd/compile/internal/types2/`)가 향후 Go 버전에서 변경되었는지 정확하고 안정적으로 추적하기 위해 설정 파일과 전용 CLI 도구(`./cmd/upstreamchecker`), 그리고 래퍼 스크립트(`./scripts`)로 구성된 체계를 구축합니다:

1. **기준 리비전 설정 파일 (`upstream.json`) 및 코드 상수 (`analyzer.UpstreamBaseRevision`)**:
   - `go.mod`의 `go` 지시자가 기준 버전의 단일 진실 공급원입니다.
   - `upstream.json`에 디렉토리 매핑(`snapshot_dir`, `patch_dir`, `target_dir`), `golang/go`의 기준 리비전(`base_revision`), 그리고 `files` 배열(`upstream`, `snapshot`, `patch`, `target`, `sha256`)을 선언적으로 기록합니다.
   - `pkg/analyzer/adt.go`에 `UpstreamBaseRevision` 상수를 선언하여 `go.mod` 버전과 동기화합니다.
2. **비교 및 검증 CLI 도구 (`./cmd/upstreamchecker`)**:
   - 쉘 스크립트로는 처리하기 까다로운 GitHub API/Git 태그 해석, 파일별 해시 대조를 수행하는 Go 프로그램입니다.
   - **기본 실행 (인자 없음)**: `go.mod`에 선언된 Go 버전을 기본 타겟으로 설정하고, 5중 일관성을 검증합니다.
   - **최신 릴리즈 (`-target latest`)**: GitHub `golang/go`의 최신 정식 릴리즈 태그와 비교합니다.
   - **로컬 툴체인 (`-target local`)**: 로컬 `$(go env GOROOT)/src/go/types`와 비교합니다.
   - **특정 리비전 지정 (`-target <revision>`)**: 명시된 특정 태그, 커밋 SHA 또는 브랜치와 비교합니다.
   - **스냅샷 동기화 (`-sync`)**: 원본 스냅샷 갱신, `upstream.json` 해시 갱신, 3-way 패치 적용 및 패치 파일 갱신을 일괄 수행합니다.
3. **자동화 및 릴리즈 검증 스크립트 (`./scripts`)**:
   - `./scripts/check-upstream.sh`: 5중 일관성 및 패치 드리프트를 검증하는 스크립트입니다.
   - `./scripts/apply-patches.sh`: 원본 스냅샷에 패치를 적용하여 `pkg/analyzer/*.go`를 재생성합니다.
   - `./scripts/update-patches.sh`: `pkg/analyzer/*.go`의 로컬 수정사항으로부터 패치를 갱신합니다.
   - `./scripts/sync-upstream.sh`: `./cmd/upstreamchecker -sync`를 통해 `go.mod` 버전에 맞추어 스냅샷과 패치를 동기화합니다.
   - `./scripts/release.sh`: 릴리즈 태그 생성 전 워킹 트리 상태, 5중 일관성, 전체 테스트 통과 여부를 검증하는 릴리즈 게이트키퍼 스크립트입니다.

---

## 3. 단계별 구현 로드맵 및 이슈 구성

기존 이슈(#1, #2, #5 — #4와 #6은 순수 #57644 모델 및 정적 반환 검사로 대체됨)와의 연속성을 유지하면서 위 설계를 구현하기 위한 작업 단위는 다음과 같습니다:

1. **[#14] `go/types` 기반 `SumTypeSet` 엔진 도입 및 `./cmd/upstreamchecker` 구축**:
   - `go/types/typeset.go` 및 `instantiate.go` 구조를 기반으로 `SumTypeSet`을 구현하고, 기준 리비전 설정(`upstream.json`), `./cmd/upstreamchecker` (기본값: 최신 정식 릴리즈 태그 비교), `./scripts/check-upstream.sh` 및 `./scripts/release.sh` 구축.
2. **[#8] Core Bugfixes & AST Expression Generalization** (기존 #2 해결 포함):
   - `*ast.Ident` 제약을 제거하고 `TypesInfo.TypeOf` 기반으로 전환, `var (...)` 블록 다중 스펙 누락 수정, 다중 반환 함수 대입 시 슬라이스 인덱스 패닉 수정, `checkTypeSwitch`의 스위치별 `nilHandled` 상태 분리.
3. **[#7] Type Conversion & Type Assertion Bypass Prevention (`SumType(x)`, `x.(SumType)`)**:
   - `*ast.CallExpr`에서의 명시적 타입 변환 및 `*ast.TypeAssertExpr`에서의 타입 단언을 검사하여 타입 안전성 우회 차단.
4. **[#9, #10] General Interface, Disjoint Union Invariant & Type Set Semantics (Pure #57644 Alignment)** (기존 #5 확장):
   - 메서드를 가진 인터페이스 허용 및 `types.Implements` 검증(#9), 지시자 내 `~T` 및 `nil` 문법 명시적 거부(#9), 평탄화 항 간 서로소 불변식(Pairwise Disjointness) 강제, 비어 있지 않은 일반 인터페이스 유니온 항 지원(#5), 중첩 합타입 평탄화(Flattening) 및 부분집합($\subseteq$) 대입 지원(#10).
5. **[#11] Complete Assignability Contexts** (기존 #1 확장):
   - `return` 문(#1), 함수 호출 인자, 복합 리터럴(`struct`, `slice`, `map`), 채널 전송(`ch <- x`) 검사 구현.
6. **[#12] Cross-Package Analysis via `analysis.Fact`**:
   - 다른 패키지에서 정의되고 import된 합타입에 대한 `ExportObjectFact` / `ImportObjectFact` 지원.
7. **[#13] Automated Test Suite (`analysistest`)**:
   - `golang.org/x/tools/go/analysis/analysistest` 기반의 단위/통합 테스트 구축.
