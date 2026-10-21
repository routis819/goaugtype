package typeset

import (
	"fmt"
	"io"
)

// --- General interface with methods (#9) ---

type Leaf struct{}

func (l *Leaf) Walk() string { return "leaf" }

type Node struct{}

func (n *Node) Walk() string { return "node" }

type NonWalker struct{}

// goaugtype: *Leaf | *Node
type NodeWalker interface { // want NodeWalker:"SumTypeFact\\(\\*typeset.Leaf \\| \\*typeset.Node\\)"
	Walk() string
}

// goaugtype: *Leaf | *NonWalker // want "permitted type .* does not implement .*"
type BadNodeWalker interface {
	Walk() string
}

// goaugtype: ~int // want "underlying type syntax ~int is not permitted in sum types"
type BadUnderlying any

// goaugtype: nil // want "nil is not a type and is not permitted in sum type directives"
type BadNil any

// goaugtype: int | int // want "duplicate concrete type int in sum type typeset.BadDup"
type BadDup any

// goaugtype: any // want "empty interface any is not permitted in sum type typeset.BadEmpty"
type BadEmpty any

// goaugtype: *MyError | error // want "overlapping union terms: \\*typeset.MyError already implements error in sum type typeset.BadOverlapConcrete"
type BadOverlapConcrete any

// goaugtype: io.ReadWriter | io.Reader // want "overlapping interface terms io.ReadWriter and io.Reader in sum type typeset.BadOverlapIfaces"
type BadOverlapIfaces any

var _ io.Reader

func testNodeWalker() {
	var nw NodeWalker
	nw = &Leaf{}
	nw = &Node{}
	nw = nil

	switch nw.(type) {
	case *Leaf:
	case *Node:
	case nil:
	}

	switch nw.(type) { // want "non-exhaustive type switch. missing cases: nil"
	case *Leaf:
	case *Node:
	}
}

// --- Regular interface term in union (#5) ---

type MyError struct{}

func (e *MyError) Error() string { return "my error" }

// goaugtype: string | error
type StrOrErr any // want StrOrErr:"SumTypeFact\\(string \\| error\\)"

func testStrOrErr() {
	var x StrOrErr
	var myErr *MyError = &MyError{}
	var stdErr error = fmt.Errorf("error")

	x = myErr
	x = stdErr
	x = "hello"
	x = nil
	x = 123 // want "invalid assigning: type is not permitted"

	switch x.(type) {
	case string, error, nil:
	}

	switch x.(type) { // want "non-exhaustive type switch. missing cases: nil"
	case string, error:
	}

	// Valid narrowing: *MyError before error
	switch x.(type) {
	case *MyError:
	case string, error, nil:
	}

	// Invalid narrowing: *MyError without error
	switch x.(type) { // want "non-exhaustive type switch. missing cases: error"
	case *MyError:
	case string, nil:
	}

	// Shadowed case: error before *MyError
	switch x.(type) {
	case string, error, nil:
	case *MyError: // want "shadowed type switch case: \\*typeset.MyError is already covered by error"
	}

	// Redundant default in exhaustive switch
	switch x.(type) {
	case string, error, nil:
	default: // want "redundant default case in exhaustive type switch"
	}
}

// --- Nested sum types and subset assignability (#10) ---

// goaugtype: int8 | int16
type SmallInt any // want SmallInt:"SumTypeFact\\(int8 \\| int16\\)"

// goaugtype: SmallInt | int32 | int64
type AllInt any // want AllInt:"SumTypeFact\\(int8 \\| int16 \\| int32 \\| int64\\)"

func testNestedSum() {
	var s SmallInt
	var a AllInt

	a = s // SubsetOf succeeds
	s = a // want "invalid assigning: type is not permitted"

	a = int8(1)
	a = int16(2)
	a = int32(3)
	a = int64(4)
	a = nil
	a = "not an int" // want "invalid assigning: type is not permitted"

	switch a.(type) {
	case int8, int16, int32, int64, nil:
	}

	switch a.(type) { // want "non-exhaustive type switch. missing cases: nil"
	case int8, int16, int32, int64:
	}

	switch a.(type) { // want "non-exhaustive type switch. missing cases: int8, int16"
	case SmallInt, int32, int64, nil: // want "invalid type switch case: typeset.SmallInt"
	}
}
