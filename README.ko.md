# goaugtype (goaugadt)

[English](./README.md) | [한국어](./README.ko.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/routiz/goaugt.svg)](https://pkg.go.dev/github.com/routiz/goaugt)
[![Go Report Card](https://goreportcard.com/badge/github.com/routiz/goaugt)](https://goreportcard.com/report/github.com/routiz/goaugt)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

**`goaugtype`은 순수 [golang/go#57644](https://github.com/golang/go/issues/57644) 일반 인터페이스 합타입(General Interface Sum Types / Algebraic Data Types, ADT) 명세를 Go 언어에 제공하는 정적 분석 도구입니다.**

합타입에 대한 컴파일 타임 타입 안정성을 강제하며, 런타임 타입 오류 및 누락된 `type switch` 분기를 즉각적인 컴파일 에러로 전환합니다.

---

## 목차

- [왜 `goaugtype`인가?](#왜-goaugtype인가)
- [설치 방법](#설치-방법)
- [빠른 시작](#빠른-시작)
  - [1. 합타입 정의](#1-합타입-정의)
  - [2. 완전한 Type Switch 작성](#2-완전한-type-switch-작성)
  - [3. 분석기 실행](#3-분석기-실행)
- [핵심 기능](#핵심-기능)
  - [메서드 규약을 갖는 일반 인터페이스 합타입 (#9)](#메서드-규약을-갖는-일반-인터페이스-합타입-9)
  - [메서드를 포함하는 일반 인터페이스 항 지원 (#5)](#메서드를-포함하는-일반-인터페이스-항-지원-5)
  - [중첩 합타입 평탄화 및 부분집합 대입 판정 (#10)](#중첩-합타입-평탄화-및-부분집합-대입-판정-10)
  - [쌍별 서로소 합집합(Disjoint Union) 불변식](#쌍별-서로소-합집합disjoint-union-불변식)
  - [엄격한 Type Switch 완전성 검증](#엄격한-type-switch-완전성-검증)
  - [8대 대입 경로 및 타입 단언/변환 전수 검증](#8대-대입-경로-및-타입-단언변환-전수-검증)
  - [패키지 경계를 넘는 Fact 직렬화](#패키지-경계를-넘는-fact-직렬화)
- [문서 및 기여 안내](#문서-및-기여-안내)
- [라이선스](#라이선스)

---

## 왜 `goaugtype`인가?

표준 Go 인터페이스는 열린 집합(Open Set)입니다. 인터페이스의 메서드 셋을 만족하는 모든 타입이 대입될 수 있으며, `switch x.(type)` 문에서는 가능한 모든 구체 타입이 처리되었는지 컴파일 시점에 보장할 수 없습니다. 이는 닫힌 도메인 상태, AST 노드, 컴파일러 패스, 상태 기계를 모델링할 때 심각한 런타임 패닉 위험을 초래합니다.

`goaugtype`은 순수 **[golang/go#57644](https://github.com/golang/go/issues/57644)** 명세("general interfaces as sum types")를 구현합니다:
- 표준 인터페이스 문법과 `// goaugtype: Type1 | Type2` 지시자 주석을 사용하여 닫힌 합타입을 정의합니다.
- Go 표준 라이브러리 `src/go/types/`의 타입 셋 대수 엔진(`typeterm`, `termlist`, `typeset`, `union`, `instantiate`, `operand`, `assignments`)을 1:1로 직접 포팅하여 완벽한 호환성을 제공합니다.
- 컴파일 타임 완전성(Exhaustiveness), 리프 매칭, 서브타입 정렬, 쌍별 서로소 불변식, 8대 대입 경로를 엄격하게 검증합니다.

---

## 설치 방법

```bash
go install github.com/routiz/goaugt/cmd/goaugadt@latest
```

또는 `go vet` 도구와 연동하여 실행할 수 있습니다:

```bash
go vet -vettool=$(which goaugadt) ./...
```

---

## 빠른 시작

### 1. 합타입 정의

인터페이스를 선언하고 `// goaugtype:` 지시자 주석을 통해 허용되는 타입들을 정의합니다:

```go
package tree

// Tree는 *Leaf와 *Node로 구성된 대수적 합타입입니다.
// goaugtype: *Leaf | *Node
type Tree any

type Leaf struct {
    Value int
}

type Node struct {
    Left  Tree
    Right Tree
}
```

> [!NOTE]
> 순수 `#57644` 명세에 따라 `nil`은 `// goaugtype:` 지시자 주석에 **작성하지 않습니다**. `nil`은 모든 인터페이스 합타입의 암시적 기본값(Zero value)이며, 지시자 대신 `type switch` 문에서 `case nil:`로 반드시 처리해야 합니다.

### 2. 완전한 Type Switch 작성

`goaugtype`은 `switch x.(type)` 문이 선언된 모든 허용 타입과 `nil`을 처리하는지 검증합니다:

```go
// ✅ 정상: *Leaf, *Node, nil을 전수 처리하는 완전한 switch
func Describe(t Tree) string {
    switch v := t.(type) {
    case *Leaf:
        return fmt.Sprintf("Leaf(%d)", v.Value)
    case *Node:
        return fmt.Sprintf("Node(%s, %s)", Describe(v.Left), Describe(v.Right))
    case nil:
        return "EmptyTree"
    }
}
```

```go
// ❌ 오류: nil 케이스가 누락됨
switch v := t.(type) { // Error: non-exhaustive type switch. missing cases: nil
case *Leaf:
case *Node:
}
```

```go
// ❌ 오류: 완전한 switch에서 불필요한 default 블록 사용
switch v := t.(type) {
case *Leaf:
case *Node:
case nil:
default: // Error: redundant default block in exhaustive type switch
    panic("unreachable")
}
```

### 3. 분석기 실행

```bash
goaugadt ./...
```

---

## 핵심 기능

### 메서드 규약을 갖는 일반 인터페이스 합타입 (#9)

합타입은 단순한 `any`에 국한되지 않습니다. 인터페이스에 메서드 시그니처를 선언할 수 있으며, 유니온에 지정된 모든 허용 타입은 해당 메서드 셋을 반드시 구현해야 합니다:

```go
// goaugtype: *Circle | *Square
type Shape interface {
    Area() float64
}

type Circle struct{ Radius float64 }
func (c *Circle) Area() float64 { return math.Pi * c.Radius * c.Radius }

type Square struct{ Side float64 }
func (s *Square) Area() float64 { return s.Side * s.Side }
```

만약 허용 타입이 `Area()`를 구현하지 못하면 선언 위치에서 즉시 오류가 보고됩니다:
```
permitted type *Triangle does not implement Shape (missing method Area)
```

### 메서드를 포함하는 일반 인터페이스 항 지원 (#5)

합타입 유니온 항으로 메서드를 포함하는 비어 있지 않은 일반 인터페이스(예: `error`, `io.Reader`)를 지정할 수 있습니다:

```go
// goaugtype: string | error
type Result any
```

`error`를 구현하는 모든 구체 타입은 `Result`에 안전하게 대입될 수 있습니다. `type switch` 문에서는 일반 인터페이스 항을 구현하는 구체 타입 케이스가 해당 인터페이스 케이스보다 반드시 **앞에** 위치해야 합니다(서브타입 좁히기 / Narrowing).

### 중첩 합타입 평탄화 및 부분집합 대입 판정 (#10)

합타입이 다른 합타입을 항으로 참조할 수 있습니다. 중첩된 합타입은 구체 타입 및 인터페이스 항으로 재귀적으로 평탄화되며, 집합론적 부분집합($\subseteq$) 대입 관계가 성립합니다:

```go
// goaugtype: int8 | int16
type SmallInt any

// goaugtype: SmallInt | int32 | int64
type AllInt any
```

- **`SmallInt` → `AllInt` 대입**: 허용됨 ($\{ \text{int8}, \text{int16} \} \subseteq \{ \text{int8}, \text{int16}, \text{int32}, \text{int64} \}$).
- **`AllInt` → `SmallInt` 대입**: 컴파일 에러로 거부됨.
- **Type Switch 완전성**: Go 런타임에서 `SmallInt`는 `any`로 타입 소거(Type Erasure)되므로, `AllInt`에 대한 `switch x.(type)`에서는 `case SmallInt:` 사용이 금지되며 평탄화된 리프 항(`case int8, int16, int32, int64:` 및 `case nil:`)으로 매칭해야 합니다.

### 쌍별 서로소 합집합(Disjoint Union) 불변식

의미론적 모호성을 방지하기 위해, 모든 합타입은 평탄화된 항들 사이의 쌍별 서로소(Pairwise Disjointness)를 강제합니다:
- 동일한 구체 타입 중복 금지 (`int | int` ❌).
- 빈 인터페이스 항 금지 (`any` 또는 `interface{}` ❌).
- 유니온 내 다른 인터페이스를 구현하는 구체 타입 항 금지 ($C \subset I$ ❌).
- 다른 인터페이스의 부분집합인 인터페이스 항 금지 ($I_1 \subseteq I_2$ ❌).
- `switch` 문 내에서 중첩 합타입 식별자를 case 타입으로 사용하는 행위 금지 (리프 전용 매칭 원칙).

### 엄격한 Type Switch 완전성 검증

1. **리프 전용 매칭(Leaf-Only Matching)**: 케이스는 구체 리프 타입 또는 선언된 인터페이스 항과 직접 매칭되어야 합니다.
2. **서브타입 좁히기 및 가려짐(Shadowing) 감지**: 케이스 $A$가 케이스 $B$의 서브타입인 경우($A \subset B$), 반드시 $A$가 $B$보다 먼저 등장해야 합니다. 순서가 바뀌면 도달할 수 없는 데드 코드로 판정됩니다.
3. **`case nil:` 필수화**: 모든 인터페이스에서 유효한 `nil`은 반드시 명시적으로 처리되어야 합니다.
4. **불필요한 `default:` 거부**: 모든 케이스(nil 포함)가 처리된 경우, `default:` 블록은 불필요한 데드 코드로 간주되어 거부됩니다.

### 8대 대입 경로 및 타입 단언/변환 전수 검증

`goaugtype`은 Go의 8가지 모든 대입 및 변환 경로를 빈틈없이 검증합니다:
1. 변수 초기화 선언: `var x Tree = &Leaf{}`
2. 짧은 변수 선언: `x := Tree(&Leaf{})`
3. 대입문: `x = &Leaf{}`
4. 다중 반환 튜플 대입: `x, err = getTree()`
5. 반환문: `return &Leaf{}`
6. 채널 전송: `ch <- &Leaf{}`
7. 복합 리터럴 값: `[]Tree{&Leaf{}}`, `map[string]Tree{"root": &Leaf{}}`
8. 명시적 타입 단언 및 변환: `t.(Tree)`, `Tree(v)`

### 패키지 경계를 넘는 Fact 직렬화

`goaugtype`은 `analysis.Fact` 메커니즘을 통해 합타입 정의를 `SumTypeFact`로 직렬화하여 내보냅니다. 패키지 `B`가 패키지 `A`의 합타입을 가져올 때 자동으로 `SumTypeSet`을 복원하여, 외부 패키지 합타입에 대해서도 동일한 대입 검증 및 완전성 검사를 지원합니다.

---

## 문서 및 기여 안내

- **설계 명세서**: [docs/DESIGN.ko.md](./docs/DESIGN.ko.md) | [English](./docs/DESIGN.md)
- **아키텍처 개요**: [docs/ARCHITECTURE.ko.md](./docs/ARCHITECTURE.ko.md) | [English](./docs/ARCHITECTURE.md)
- **기여자 가이드 및 패치 워크플로우**: [CONTRIBUTING.ko.md](./CONTRIBUTING.ko.md) | [English](./CONTRIBUTING.md)

---

## 라이선스

이 프로젝트는 MIT 라이선스 하에 배포됩니다. 자세한 내용은 [LICENSE](./LICENSE)를 참조하십시오.
`pkg/analyzer/` 내 타입 셋 대수 엔진의 일부는 Go 표준 라이브러리(`src/go/types/`)에서 파생되었습니다 (© 2013–2026 The Go Authors, BSD 스타일 라이선스).
