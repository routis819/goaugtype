// Package simple provides code examples to verify the behavior of goaugtype.
package simple

import (
	"fmt"
)

// Tree represents a tree structure, which is very suitable for ADT testing.
// goaugtype: *Leaf | *Node | nil
type Tree any // type spec line comment

type Leaf struct{}

type Node struct{}

type NotTree struct{}

type (
	// TestInt is a sum type of all int family.
	// goaugtype: int8 | int16 | int32 | int64
	TestInt any
)

func treeBuilder() Tree {
	return nil
}

func nonTreeBuilder() int {
	return 0
}

var gbvalt Tree = nil

func SimpleTest() {
	gbvalt = &Leaf{}
	var valt Tree = &Leaf{}
	valt = Leaf{} // want "invalid assigning: type is not permitted"
	valt = &Node{}
	valt = Node{} // want "invalid assigning: type is not permitted"
	valt = nil
	valt = &NotTree{} // want "invalid assigning: type is not permitted"
	valt = NotTree{}  // want "invalid assigning: type is not permitted"
	valt = treeBuilder()
	fmt.Printf("printing valt to suppres SA4006: %v\n", valt)
	valt = nonTreeBuilder() // want "invalid assigning: type is not permitted"

	valt1 := treeBuilder()
	fmt.Printf("valt1: %v\n", valt1)
	valt1 = 2 // want "invalid assigning: type is not permitted"

	valt2 := nonTreeBuilder()
	fmt.Printf("valt2: %v\n", valt2)
	valt2 = 2

	// should NOT make error
	switch valt.(type) {
	case *Leaf:
	case *Node:
	case nil:
	}

	// should make error
	switch valt.(type) { // want "non-exhaustive type switch. missing cases: nil"
	case *Leaf:
	case *Node:
	}

	// should make error
	switch valt.(type) { // want "non-exhaustive type switch. missing cases: "
	case *Node:
	}
}
