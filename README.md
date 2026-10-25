# goaugtype (goaugadt)

[English](./README.md) | [한국어](./README.ko.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/routiz/goaugt.svg)](https://pkg.go.dev/github.com/routiz/goaugt)
[![Go Report Card](https://goreportcard.com/badge/github.com/routiz/goaugt)](https://goreportcard.com/report/github.com/routiz/goaugt)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

**`goaugtype` is a static analysis tool that brings pure [golang/go#57644](https://github.com/golang/go/issues/57644) General Interface Sum Types (Algebraic Data Types / ADTs) to Go.**

It enforces compile-time type safety for sum types, turning runtime type errors and unhandled `type switch` cases into immediate compile-time errors.

---

## Table of Contents

- [Why `goaugtype`?](#why-goaugtype)
- [Installation](#installation)
- [Quick Start](#quick-start)
  - [1. Define Your Sum Type](#1-define-your-sum-type)
  - [2. Exhaustive Type Switch](#2-exhaustive-type-switch)
  - [3. Run the Analyzer](#3-run-the-analyzer)
- [Core Features](#core-features)
  - [General Interfaces with Method Contracts (#9)](#general-interfaces-with-method-contracts-9)
  - [Non-Empty Interface Union Terms (#5)](#non-empty-interface-union-terms-5)
  - [Nested Sum Types & Subset Assignability (#10)](#nested-sum-types--subset-assignability-10)
  - [Pairwise Disjoint Union Invariant](#pairwise-disjoint-union-invariant)
  - [Strict Type Switch Verification](#strict-type-switch-verification)
  - [Complete 8-Path Assignability & Type Assertions](#complete-8-path-assignability--type-assertions)
  - [Cross-Package Fact Export/Import](#cross-package-fact-exportimport)
- [Documentation & Contributing](#documentation--contributing)
- [License](#license)

---

## Why `goaugtype`?

Standard Go interfaces are open sets: any type that satisfies an interface's method set can be assigned to it, and `switch x.(type)` statements cannot verify whether all possible concrete types have been handled. This presents significant reliability risks when modeling closed domain states, AST nodes, compiler passes, or state machines.

`goaugtype` implements the pure **[golang/go#57644](https://github.com/golang/go/issues/57644)** specification ("general interfaces as sum types"):
- Define closed sum types using standard interface syntax augmented with `// goaugtype: Type1 | Type2`.
- Directly ported from Go standard library's `src/go/types/` type set algebra (`typeterm`, `termlist`, `typeset`, `union`, `instantiate`, `operand`, `assignments`).
- Enforces compile-time exhaustiveness, leaf-only matching, subtype ordering, pairwise disjointness, and 8-path assignability.

---

## Installation

```bash
go install github.com/routiz/goaugt/cmd/goaugadt@latest
```

Or run via `go vet`:

```bash
go vet -vettool=$(which goaugadt) ./...
```

---

## Quick Start

### 1. Define Your Sum Type

Declare an interface and define its permitted types using the `// goaugtype:` directive comment:

```go
package tree

// Tree represents an algebraic sum type over *Leaf and *Node.
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
> Under the pure `#57644` specification, `nil` is **NOT** written in the `// goaugtype:` directive. `nil` is the implicit zero value of every interface sum type. Instead, handling `nil` is mandatory in exhaustive `type switch` statements.

### 2. Exhaustive Type Switch

`goaugtype` validates that `switch x.(type)` statements handle every permitted case plus `nil`:

```go
// ✅ Correct: Exhaustive handling of *Leaf, *Node, and nil
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
// ❌ Incorrect: Missing nil case
switch v := t.(type) { // Error: non-exhaustive type switch. missing cases: nil
case *Leaf:
case *Node:
}
```

```go
// ❌ Incorrect: Redundant default in exhaustive switch
switch v := t.(type) {
case *Leaf:
case *Node:
case nil:
default: // Error: redundant default block in exhaustive type switch
    panic("unreachable")
}
```

### 3. Run the Analyzer

```bash
goaugadt ./...
```

---

## Core Features

### General Interfaces with Method Contracts (#9)

Sum types are not restricted to `any`. You can define method signatures on the sum type interface. All permitted types in the union must satisfy the interface's method set:

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

If a permitted type fails to implement `Area()`, `goaugtype` flags it at the declaration site:
```
permitted type *Triangle does not implement Shape (missing method Area)
```

### Non-Empty Interface Union Terms (#5)

Sum types can include regular (non-sum) interfaces with methods (e.g. `error`, `io.Reader`) as union terms:

```go
// goaugtype: string | error
type Result any
```

Any concrete type implementing `error` is assignable to `Result`. In `type switch` statements, concrete terms that implement an interface term must be ordered **before** the interface case (subtype narrowing).

### Nested Sum Types & Subset Assignability (#10)

Sum types can reference other sum types. Nested sum types are recursively flattened into concrete and interface terms, and mathematical subset ($\subseteq$) assignability is enforced:

```go
// goaugtype: int8 | int16
type SmallInt any

// goaugtype: SmallInt | int32 | int64
type AllInt any
```

- **Assigning `SmallInt` to `AllInt`**: Allowed, because $\{ \text{int8}, \text{int16} \} \subseteq \{ \text{int8}, \text{int16}, \text{int32}, \text{int64} \}$.
- **Assigning `AllInt` to `SmallInt`**: Rejected at compile time.
- **Type Switch**: Because `SmallInt` is erased to `any` at Go runtime, `switch x.(type)` on `AllInt` requires matching the flattened leaf terms (`case int8, int16, int32, int64:` and `case nil:`) and rejects `case SmallInt:`.

### Pairwise Disjoint Union Invariant

To prevent ambiguous semantics, all sum types enforce pairwise disjointness across their flattened terms:
- No duplicate concrete types (`int | int` ❌).
- No empty interfaces (`any` or `interface{}` ❌).
- No concrete type that implements an interface term in the same union ($C \subset I$ ❌).
- No interface term that is a subtype of another interface term ($I_1 \subseteq I_2$ ❌).
- Nested sum type identifiers are forbidden as `case` expressions in `switch` (leaf-only matching).

### Strict Type Switch Verification

1. **Leaf-Only Matching**: Cases must match concrete leaves or declared interface terms.
2. **Subtype Narrowing & Shadowing Detection**: If case $A$ is a subtype of case $B$ ($A \subset B$), case $A$ must appear before case $B$. Reversing the order produces a compile error for dead/shadowed code.
3. **Mandatory `case nil:`**: Since `nil` is valid for any interface, it must be explicitly handled.
4. **Redundant `default:` Rejection**: If all cases (including `nil`) are handled, a `default:` block is flagged as redundant dead code.

### Complete 8-Path Assignability & Type Assertions

`goaugtype` verifies all 8 Go assignability and conversion paths:
1. Variable initialization: `var x Tree = &Leaf{}`
2. Short variable declaration: `x := Tree(&Leaf{})`
3. Assignment statements: `x = &Leaf{}`
4. Multi-value tuple assignment: `x, err = getTree()`
5. Return statements: `return &Leaf{}`
6. Channel send: `ch <- &Leaf{}`
7. Composite literal values: `[]Tree{&Leaf{}}`, `map[string]Tree{"root": &Leaf{}}`
8. Type assertions and conversions: `t.(Tree)`, `Tree(v)`

### Cross-Package Fact Export/Import

`goaugtype` serializes sum type definitions into `analysis.Fact` (`SumTypeFact`). When package `B` imports sum types from package `A`, `goaugtype` automatically reconstructs their `SumTypeSet`, providing seamless cross-package type switch exhaustiveness and assignability verification.

---

## Documentation & Contributing

- **Design Specification**: [docs/DESIGN.md](./docs/DESIGN.md) | [한국어](./docs/DESIGN.ko.md)
- **Architecture Overview**: [docs/ARCHITECTURE.md](./docs/ARCHITECTURE.md) | [한국어](./docs/ARCHITECTURE.ko.md)
- **Contributor Guide & Patch Workflow**: [CONTRIBUTING.md](./CONTRIBUTING.md) | [한국어](./CONTRIBUTING.ko.md)

---

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE) for details.
Portions of the type-set engine in `pkg/analyzer/` are derived from the Go standard library (`src/go/types/`), © 2013–2026 The Go Authors (BSD-style license).
