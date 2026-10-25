// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analyzer

import (
	"errors"
	"go/ast"
	"go/token"
	"go/types"
)

// assignment checks whether operand x can be assigned to target type T.
// Ported from (*Checker).assignment in src/go/types/assignments.go (lines 24–118).
func assignment(tinfo *types.Info, reg *Registry, x *operand, T types.Type) bool {
	if x == nil || !x.isValid() || T == nil {
		return true
	}
	// Check sum type assignability
	if reg != nil && reg.Lookup(T) != nil {
		return x.assignableTo(reg, T, nil)
	}
	if reg != nil && reg.Lookup(x.typ()) != nil {
		return x.assignableTo(reg, T, nil)
	}
	return true
}

// initVar checks a single variable initialization.
// Ported from (*Checker).initVar in src/go/types/assignments.go.
func initVar(tinfo *types.Info, reg *Registry, lhsType types.Type, x *operand, pos token.Pos, errType string) *augtError {
	if lhsType == nil || x == nil || !x.isValid() {
		return nil
	}
	if !assignment(tinfo, reg, x, lhsType) {
		return &augtError{
			err: errors.New(errType),
			pos: pos,
		}
	}
	return nil
}

// assignVar checks a single variable assignment.
// Ported from (*Checker).assignVar in src/go/types/assignments.go.
func assignVar(tinfo *types.Info, reg *Registry, lhs ast.Expr, x *operand, pos token.Pos) *augtError {
	lhsType := tinfo.TypeOf(lhs)
	if lhsType == nil {
		return nil
	}
	return initVar(tinfo, reg, lhsType, x, pos, "invalid assigning: type is not permitted")
}

// unpackRecv unpacks a multi-valued expression (such as a call or channel receive).
func unpackRecv(tinfo *types.Info, expr ast.Expr) []*operand {
	if expr == nil {
		return nil
	}
	tv, ok := tinfo.Types[expr]
	if !ok || tv.Type == nil {
		return nil
	}
	if tuple, ok := tv.Type.(*types.Tuple); ok {
		var res []*operand
		for i := 0; i < tuple.Len(); i++ {
			res = append(res, &operand{mode_: value, typ_: tuple.At(i).Type()})
		}
		return res
	}
	return nil
}

// initVars checks variable declarations (ValueSpec).
// Ported from (*Checker).initVars in src/go/types/assignments.go (lines 387–530).
func initVars(tinfo *types.Info, reg *Registry, spec *ast.ValueSpec) []augtError {
	var errs []augtError
	l, r := len(spec.Names), len(spec.Values)

	// 1:1 mapping (lines 406-417 in assignments.go)
	if l == r {
		for i, n := range spec.Names {
			lhsType := tinfo.TypeOf(n)
			x := newOperand(tinfo, spec.Values[i])
			if err := initVar(tinfo, reg, lhsType, x, spec.Pos(), "invalid declaration: type is not permitted"); err != nil {
				errs = append(errs, *err)
			}
		}
		return errs
	}

	// Multi-value unpack (lines 440-452 in assignments.go)
	if r == 1 && l > 1 {
		rhs := unpackRecv(tinfo, spec.Values[0])
		if len(rhs) > 0 {
			for i, n := range spec.Names {
				if i >= len(rhs) {
					break
				}
				lhsType := tinfo.TypeOf(n)
				if err := initVar(tinfo, reg, lhsType, rhs[i], spec.Pos(), "invalid declaration: type is not permitted"); err != nil {
					errs = append(errs, *err)
				}
			}
		}
	}

	return errs
}

// assignVars checks assignment statements.
// Ported from (*Checker).assignVars in src/go/types/assignments.go (lines 473–530).
func assignVars(tinfo *types.Info, reg *Registry, stmt *ast.AssignStmt) []augtError {
	var errs []augtError
	l, r := len(stmt.Lhs), len(stmt.Rhs)

	// 1:1 mapping (lines 490-495 in assignments.go)
	if l == r {
		for i, lhs := range stmt.Lhs {
			x := newOperand(tinfo, stmt.Rhs[i])
			if err := assignVar(tinfo, reg, lhs, x, stmt.TokPos); err != nil {
				errs = append(errs, *err)
			}
		}
		return errs
	}

	// Multi-value unpack (lines 509-521 in assignments.go)
	if r == 1 && l > 1 {
		rhs := unpackRecv(tinfo, stmt.Rhs[0])
		if len(rhs) > 0 {
			for i, lhs := range stmt.Lhs {
				if i >= len(rhs) {
					break
				}
				lhsType := tinfo.TypeOf(lhs)
				if err := initVar(tinfo, reg, lhsType, rhs[i], stmt.TokPos, "invalid assigning: type is not permitted"); err != nil {
					errs = append(errs, *err)
				}
			}
		}
	}

	return errs
}

// returnVars checks return statements against function result signatures.
// Ported from return statement handling in src/go/types/assignments.go.
func returnVars(tinfo *types.Info, reg *Registry, sig *types.Signature, ret *ast.ReturnStmt) []augtError {
	if sig == nil || ret == nil || sig.Results().Len() == 0 || len(ret.Results) == 0 {
		return nil
	}
	var errs []augtError

	if len(ret.Results) == sig.Results().Len() {
		for i, resExpr := range ret.Results {
			resType := sig.Results().At(i).Type()
			if sumt := reg.Lookup(resType); sumt != nil {
				x := newOperand(tinfo, resExpr)
				if !assignment(tinfo, reg, x, resType) {
					errs = append(errs, augtError{
						err: errors.New("invalid return: type is not permitted"),
						pos: resExpr.Pos(),
					})
				}
			}
		}
		return errs
	}

	if len(ret.Results) == 1 && sig.Results().Len() > 1 {
		rhs := unpackRecv(tinfo, ret.Results[0])
		for i := 0; i < sig.Results().Len() && i < len(rhs); i++ {
			resType := sig.Results().At(i).Type()
			if sumt := reg.Lookup(resType); sumt != nil {
				if !assignment(tinfo, reg, rhs[i], resType) {
					errs = append(errs, augtError{
						err: errors.New("invalid return: type is not permitted"),
						pos: ret.Results[0].Pos(),
					})
				}
			}
		}
		return errs
	}

	return nil
}
