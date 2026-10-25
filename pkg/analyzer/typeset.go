// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analyzer

import (
	"go/token"
	"go/types"
)

// A _TypeSet represents the type set of an interface or sum type.
// Ported from src/go/types/typeset.go.
type _TypeSet struct {
	selfIface *types.Interface // [goaugtype] underlying interface of the sum type
	terms     termlist         // type terms of the type set
}

// IsEmpty reports whether s is the empty set.
func (s *_TypeSet) IsEmpty() bool { return s.terms.isEmpty() }

// IsAll reports whether s is the set of all types.
func (s *_TypeSet) IsAll() bool { return s.terms.isAll() }

// IsMethodSet reports whether the interface is described only by methods without specific terms.
func (s *_TypeSet) IsMethodSet() bool { return s.terms.isAll() }

// NumMethods returns the number of methods in selfIface, if any.
func (s *_TypeSet) NumMethods() int {
	if s.selfIface != nil {
		return s.selfIface.NumMethods()
	}
	return 0
}

// hasTerms reports whether s has specific type terms.
func (s *_TypeSet) hasTerms() bool { return !s.terms.isEmpty() && !s.terms.isAll() }

// subsetOf reports whether s1 ⊆ s2.
func (s1 *_TypeSet) subsetOf(s2 *_TypeSet) bool {
	if s1 == nil || s2 == nil {
		return false
	}
	return s1.terms.subsetOf(s2.terms)
}

// is calls f with the specific type terms of s and reports whether
// all calls to f returned true. If there are no specific terms, is
// returns the result of f(nil).
func (s *_TypeSet) is(f func(*term) bool) bool {
	if !s.hasTerms() {
		return f(nil)
	}
	for _, t := range s.terms {
		if t != nil && t.typ != nil {
			if !f(t) {
				return false
			}
		}
	}
	return true
}

func (s *_TypeSet) String() string {
	switch {
	case s.IsEmpty():
		return "∅"
	case s.IsAll():
		return "𝓤"
	}
	return s.terms.String()
}

// topTypeSet may be used as type set for the empty interface.
var topTypeSet = _TypeSet{terms: allTermlist}

// computeInterfaceTypeSet computes the type set for an interface.
// Ported from src/go/types/typeset.go (lines 155–230).
func computeInterfaceTypeSet(pos token.Pos, ityp *types.Interface) *_TypeSet {
	if ityp == nil {
		return &topTypeSet
	}
	return &_TypeSet{
		selfIface: ityp,
		terms:     allTermlist,
	}
}
