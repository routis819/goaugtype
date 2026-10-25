// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analyzer

import (
	"fmt"
	"go/types"
)

// implements reports whether type V implements or satisfies type T.
// Ported from (*Checker).implements in src/go/types/instantiate.go (lines 243–384).
func (reg *Registry) implements(V, T types.Type, constraint bool, cause *string) bool {
	if V == nil || T == nil {
		return true
	}
	V = types.Unalias(V)
	T = types.Unalias(T)

	if types.Identical(V, T) {
		return true
	}

	// Untyped nil satisfies interface types
	if types.Identical(V, types.Typ[types.UntypedNil]) {
		_, ok := T.Underlying().(*types.Interface)
		return ok
	}

	sumT := reg.Lookup(T)
	if sumT == nil {
		// T is not a sum type; fall back to standard Go interface checking
		if Ti, ok := T.Underlying().(*types.Interface); ok {
			return types.Implements(V, Ti)
		}
		return types.Identical(V, T)
	}

	// V must implement T's methods, if any (lines 294–300 in instantiate.go).
	if sumT.SelfIface != nil && sumT.SelfIface.NumMethods() > 0 {
		if !types.Implements(V, sumT.SelfIface) {
			if cause != nil {
				*cause = fmt.Sprintf("%s does not implement %s", V, T)
			}
			return false
		}
	}

	// If V is itself a sum type, V's type set must be a subset of T's type set
	// (lines 339–348 in instantiate.go).
	if sumV := reg.Lookup(V); sumV != nil {
		if !sumV.tset.subsetOf(&sumT.tset) {
			if cause != nil {
				*cause = fmt.Sprintf("%s is not a subset of %s", V, T)
			}
			return false
		}
		return true
	}

	// Otherwise, V's type must be included in T's type set terms
	// (lines 350–381 in instantiate.go).
	if !sumT.tset.terms.includes(V) {
		if cause != nil {
			*cause = fmt.Sprintf("%s is not in type set of %s", V, T)
		}
		return false
	}

	return true
}
