# goaugtype (goaugadt)

[](https://www.google.com/search?q=https://pkg.go.dev/github.com/routiz/goaugt)
[](https://www.google.com/search?q=https://goreportcard.com/report/github.com/routiz/goaugt)
[](https://opensource.org/licenses/MIT)

**`goaugtype` is a static analysis tool that brings Sum Type-like features (also known as Algebraic Data Types or ADTs) to the Go language.**

It prevents runtime bugs by ensuring that `switch x.(type)` statements are exhaustive. When you add a new type to a sum type definition, `goaugtype` flags any `switch` statements that have failed to handle the new case, turning a potential runtime panic into a compile-time error.

## Why `goaugtype`?

Go does not natively support Sum Types or Sealed Classes. This makes it difficult to guarantee that all possible types are handled when working with tree-like data structures, such as in compilers, interpreters, or state machines.

`goaugtype` solves this by letting you explicitly define the set of types that an interface can be, using a simple comment directive. It then checks `switch` statements to ensure they cover all the defined types.

## Installation

```bash
go install github.com/routiz/goaugt/cmd/goaugadt@latest
```

## Usage

1.  **Define Your Sum Type**
    Create a type alias for `any` (or `interface{}`) and declare the set of permitted types in a `// goaugadt:` comment, separated by `|`.

    Here is an example from the `test/adtsample/main.go` file.

    ```go
    // Tree represents a tree structure, a perfect use case for ADTs.
    // goaugadt: *Leaf | *Node | nil
    type Tree interface{} // type spec line comment

    type Leaf struct{}
    type Node struct{}
    ```

    Now, a variable of type `Tree` can only be assigned values of type `*Leaf`, `*Node`, or `nil`.

2.  **Check Your Code**
    `goaugadt` will now validate that `type switch` statements on the `Tree` type handle all permitted cases (`*Leaf`, `*Node`, and `nil`).

    **✅ Correct Code (Exhaustive):**

    ```go
    // No error will be reported
    switch v := valt.(type) {
    case *Leaf:
    case *Node:
    case nil:
    }
    ```

    **❌ Incorrect Code (Non-Exhaustive):**
    The `nil` case is missing. `goaugadt` will report an error for this `switch` statement.

    ```go
    // An error will be reported
    switch v := valt.(type) {
    case *Leaf:
    case *Node:
    }
    ```

3.  **Run the Linter**
    To check all packages in your project, run the following command from the root directory:

    ```bash
    goaugadt ./...
    ```

## Current Features

Based on the core logic implemented in `check.go`.

  * **Exhaustiveness check** for `type switch` statements on sum types defined with `// goaugadt:`.
  * **Assignment and declaration check** to ensure only permitted types are assigned to a sum type variable.

## Roadmap

`goaugtype` is currently a functional proof-of-concept. The goal is to evolve it into a standard linter for the Go community.

  - [X] **Migrate to the `go/analysis` framework:** This is the foundation for `golangci-lint` integration and IDE support.
  - [ ] **Improve Error Messages:** Provide more detailed error messages, such as specifying which case is missing in a non-exhaustive `switch`.
  - [ ] **Automate Testing:** Build a robust testing pipeline using the `analysistest` package.

> **Note:** Generics are not currently supported.

## License

This project is licensed under the MIT License. See the `LICENSE` file for details.
