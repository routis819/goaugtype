package main

import (
	"fmt"
)

// Tree 는 트리구조를 나타내는 타입. ADT 테스트용으로 매우 적절함.
// goaugadt: *Leaf | *Node | nil
type Tree interface{} // type spec line comment

type Leaf struct{}

type Node struct{}

type NotTree struct{}

type (
	// goaugadt: int8 | int16 | int32 | int64
	TestInt any
)

func treeBuilder() Tree {
	return nil
}

func nonTreeBuilder() int {
	return 0
}

var gbvalt Tree = nil

var just int8

func main() {
	gbvalt = &Leaf{}
	var valt Tree = &Leaf{}
	valt = Leaf{} // should make error
	valt = &Node{}
	valt = Node{} // should make error
	valt = nil
	valt = &NotTree{} // should make error
	valt = NotTree{}  // should make error
	valt = treeBuilder()
	valt = nonTreeBuilder() // should make error

	valt1 := treeBuilder()
	fmt.Printf("valt1: %v\n", valt1)
	valt1 = 2

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
	switch valt.(type) {
	case *Leaf:
	case *Node:
	}

	// should make error
	switch valt.(type) {
	case *Node:
	}

	if valt == nil {
		fmt.Println("t is nil")
	}
}
