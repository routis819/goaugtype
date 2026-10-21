package assignctx

// goaugtype: *Leaf | *Node
type Tree any // want Tree:"SumTypeFact\\(\\*assignctx.Leaf \\| \\*assignctx.Node\\)"

type Leaf struct{}
type Node struct{}
type NotTree struct{}

type Holder struct {
	t Tree
}

// --- Return Statements (Issue #1) ---

func retValidLeaf() Tree {
	return &Leaf{}
}

func retValidNil() Tree {
	return nil
}

func retInvalid() Tree {
	return NotTree{} // want "invalid return: type is not permitted"
}

func retMultiValid() (Tree, Tree) {
	return &Leaf{}, &Node{}
}

func retMultiInvalid() (Tree, Tree) {
	return &Leaf{}, NotTree{} // want "invalid return: type is not permitted"
}

func badMulti() (Tree, NotTree) {
	return &Leaf{}, NotTree{}
}

func goodMulti() (Tree, Tree) {
	return &Leaf{}, &Node{}
}

func forwardBadMulti() (Tree, Tree) {
	return badMulti() // want "invalid return: type is not permitted"
}

func forwardGoodMulti() (Tree, Tree) {
	return goodMulti()
}

func testFuncLitReturn() {
	_ = func() Tree {
		return NotTree{} // want "invalid return: type is not permitted"
	}
	_ = func() Tree {
		return &Leaf{}
	}
}

// --- Function and Method Call Arguments (Issue #11) ---

func takeTree(t Tree) {}

func (h Holder) methodTakeTree(t Tree) {}

func takeVariadic(trees ...Tree) {}

func takeTwoTrees(t1, t2 Tree) {}

func testCallArgs() {
	takeTree(&Leaf{})
	takeTree(nil)
	takeTree(NotTree{}) // want "invalid argument: type is not permitted"

	h := Holder{}
	h.methodTakeTree(&Leaf{})
	h.methodTakeTree(NotTree{}) // want "invalid argument: type is not permitted"

	takeVariadic(&Leaf{}, &Node{})
	takeVariadic(&Leaf{}, NotTree{}) // want "invalid argument: type is not permitted"

	takeTwoTrees(goodMulti())
	takeTwoTrees(badMulti()) // want "invalid argument: type is not permitted"
}

// --- Composite Literals (Issue #11) ---

func testCompositeLit() {
	// Slice & array literals
	_ = []Tree{&Leaf{}, nil}
	_ = []Tree{NotTree{}}     // want "invalid composite literal element: type is not permitted"
	_ = [1]Tree{0: NotTree{}} // want "invalid composite literal element: type is not permitted"

	// Map literals
	_ = map[Tree]Tree{&Leaf{}: &Node{}}
	_ = map[Tree]Tree{NotTree{}: &Leaf{}} // want "invalid composite literal key: type is not permitted"
	_ = map[Tree]Tree{&Leaf{}: NotTree{}} // want "invalid composite literal element: type is not permitted"

	// Keyed and unkeyed struct literals
	_ = Holder{t: &Leaf{}}
	_ = Holder{t: NotTree{}} // want "invalid composite literal field: type is not permitted"
	_ = Holder{NotTree{}}    // want "invalid composite literal field: type is not permitted"
}

// --- Channel Sends (Issue #11) ---

func testSend() {
	ch := make(chan Tree, 1)
	ch <- &Leaf{}
	ch <- nil
	ch <- NotTree{} // want "invalid channel send: type is not permitted"
}
