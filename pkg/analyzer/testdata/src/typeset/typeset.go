package typeset

import "fmt"

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
	case SmallInt, int32, int64, nil:
	}

	switch a.(type) { // want "non-exhaustive type switch. missing cases: nil"
	case SmallInt, int32, int64:
	}
}
