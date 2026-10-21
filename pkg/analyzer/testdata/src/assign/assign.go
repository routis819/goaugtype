package assign

// goaugtype: *Leaf | *Node
type Tree any // want Tree:"SumTypeFact\\(\\*assign.Leaf \\| \\*assign.Node\\)"

type Leaf struct{}
type Node struct{}
type NotTree struct{}

type Holder struct {
	t Tree
}

func twoValid() (Tree, Tree) {
	return &Leaf{}, &Node{}
}

func oneValidOneInvalid() (Tree, NotTree) {
	return &Leaf{}, NotTree{}
}

var (
	v1 Tree = &Leaf{}
	v2 Tree = NotTree{} // want "invalid declaration: type is not permitted"
)

func testAssign() {
	// Array/slice index assignment (Issue #2)
	trees := make([]Tree, 2)
	trees[0] = &Leaf{}
	trees[0] = NotTree{} // want "invalid assigning: type is not permitted"

	// Struct field assignment (Issue #8)
	holder := Holder{}
	holder.t = &Leaf{}
	holder.t = NotTree{} // want "invalid assigning: type is not permitted"

	// Pointer dereference assignment
	var t Tree
	ptr := &t
	*ptr = &Leaf{}
	*ptr = NotTree{} // want "invalid assigning: type is not permitted"

	// Multi-return assignment (Issue #8)
	var t1, t2 Tree
	t1, t2 = twoValid()
	_ = t1
	_ = t2
	t1, t2 = oneValidOneInvalid() // want "invalid assigning: type is not permitted"

	var a, b Tree = oneValidOneInvalid() // want "invalid declaration: type is not permitted"
	_ = a
	_ = b

	// Explicit type conversion (Issue #7)
	_ = Tree(&Leaf{})
	_ = Tree(nil)
	_ = Tree(NotTree{}) // want "invalid type conversion: type is not permitted"
	_ = Tree(123)       // want "invalid type conversion: type is not permitted"

	// Type assertions (Issue #7)
	_ = trees[0].(*Leaf)
	_ = trees[0].(NotTree) // want "invalid type assertion from sum type: type is not permitted"

	var anyVal any = &Leaf{}
	_ = anyVal.(Tree) // want "invalid type assertion to sum type: type is not permitted"

	// Type switch on complex target expressions (Issue #8)
	switch trees[0].(type) { // want "non-exhaustive type switch. missing cases: nil"
	case *Leaf:
	case *Node:
	}

	switch holder.t.(type) { // want "non-exhaustive type switch. missing cases: nil"
	case *Leaf:
	case *Node:
	}
}
