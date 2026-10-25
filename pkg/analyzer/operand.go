// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analyzer

import (
	"go/ast"
	"go/types"
)

type operandMode byte

const (
	invalid operandMode = iota
	nilvalue
	constant_
	variable
	value
)

// operand represents an expression's type and evaluation mode.
// Ported from src/go/types/operand.go.
type operand struct {
	mode_ operandMode
	expr  ast.Expr
	typ_  types.Type
}

func newOperand(tinfo *types.Info, expr ast.Expr) *operand {
	if expr == nil {
		return &operand{mode_: invalid}
	}
	if tv, ok := tinfo.Types[expr]; ok {
		if tv.IsNil() || (tv.Type != nil && types.Identical(tv.Type, types.Typ[types.UntypedNil])) {
			return &operand{mode_: nilvalue, expr: expr, typ_: types.Typ[types.UntypedNil]}
		}
		if tv.Value != nil {
			return &operand{mode_: constant_, expr: expr, typ_: tv.Type}
		}
		if tv.Type != nil {
			return &operand{mode_: value, expr: expr, typ_: tv.Type}
		}
		return &operand{mode_: invalid, expr: expr}
	}
	if id, ok := expr.(*ast.Ident); ok && id.Name == "nil" {
		return &operand{mode_: nilvalue, expr: expr, typ_: types.Typ[types.UntypedNil]}
	}
	if t := tinfo.TypeOf(expr); t != nil {
		return &operand{mode_: value, expr: expr, typ_: t}
	}
	return &operand{mode_: invalid, expr: expr}
}

func (x *operand) mode() operandMode { return x.mode_ }
func (x *operand) typ() types.Type   { return x.typ_ }
func (x *operand) isNil() bool {
	return x.mode_ == nilvalue || types.Identical(x.typ_, types.Typ[types.UntypedNil])
}
func (x *operand) isValid() bool { return x.mode_ != invalid && x.typ_ != nil }

func isUntyped(t types.Type) bool {
	if basic, ok := t.Underlying().(*types.Basic); ok {
		return basic.Info()&types.IsUntyped != 0
	}
	return false
}

// assignableTo reports whether x is assignable to a variable of type T.
// Ported from (*operand).assignableTo in src/go/types/operand.go (lines 328–470).
func (x *operand) assignableTo(reg *Registry, T types.Type, cause *string) bool {
	if !x.isValid() || T == nil {
		return true
	}

	V := types.Unalias(x.typ())
	T = types.Unalias(T)

	// x's type is identical to T (lines 338-340)
	if types.Identical(V, T) {
		return true
	}

	// Untyped nil (lines 51-56 in assignments.go)
	if x.isNil() {
		_, ok := T.Underlying().(*types.Interface)
		return ok
	}

	// x is an untyped value representable by a value of type T (lines 348-366)
	if isUntyped(V) {
		target := types.Default(V)
		if reg != nil {
			return reg.implements(target, T, false, cause)
		}
		if iface, ok := T.Underlying().(*types.Interface); ok {
			return types.Implements(target, iface)
		}
		return types.AssignableTo(target, T)
	}

	// T is an interface type or sum type, and V implements T (lines 376-381)
	if _, ok := T.Underlying().(*types.Interface); ok {
		if reg != nil {
			return reg.implements(V, T, false, cause)
		}
		if iface, ok := T.Underlying().(*types.Interface); ok {
			return types.Implements(V, iface)
		}
		return false
	}

	return types.AssignableTo(V, T)
}
