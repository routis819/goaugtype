// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analyzer

import (
	"fmt"
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

// overlappingTerm reports the index of the term x in terms which is
// overlapping (not disjoint) from y. The result is < 0 if there is no such term.
// Ported from src/go/types/union.go (lines 171–189).
// [goaugtype] Under the Disjoint Union Invariant of #57644, we run
// !x.disjoint(y) across ALL flattened leaf terms (both concrete types and regular interfaces).
func overlappingTerm(terms []*term, y *term) int {
	for i, x := range terms {
		if x == nil || x.typ == nil || y == nil || y.typ == nil {
			continue
		}
		if !x.disjoint(y) {
			return i
		}
	}
	return -1
}

// computeUnionTypeSet computes the _TypeSet for a sum type and validates union terms.
// Ported and adapted from src/go/types/typeset.go (lines 375–415) and union.go (lines 91–134).
func computeUnionTypeSet(reg *Registry, pos token.Pos, sumType types.Type, selfIface *types.Interface, rawTerms []types.Type) (*_TypeSet, []augtError) {
	var errs []augtError

	// 1. Method set validation (#9)
	if selfIface != nil && selfIface.NumMethods() > 0 {
		for _, t := range rawTerms {
			if !types.Implements(t, selfIface) {
				errs = append(errs, augtError{
					pos: pos,
					err: fmt.Errorf("permitted type %s does not implement %s", getCanonicalName(t), getCanonicalName(sumType)),
				})
			}
		}
	}

	// 2. Syntactic term flattening & validation
	visited := make(map[string]bool)
	visited[getCanonicalName(sumType)] = true

	var leafTerms []*term
	var expand func(terms []types.Type)
	expand = func(terms []types.Type) {
		for _, t := range terms {
			if t == nil {
				continue
			}
			// [goaugtype] If t is a nested sum type in reg, expand its leaf terms
			if nested := reg.Lookup(t); nested != nil {
				cName := getCanonicalName(nested.SumType)
				if visited[cName] {
					continue
				}
				visited[cName] = true
				expand(nested.RawTerms)
				delete(visited, cName)
			} else if iface, ok := t.Underlying().(*types.Interface); ok {
				// [goaugtype] Reject empty interface terms (any / interface{})
				if iface.NumMethods() == 0 {
					errs = append(errs, augtError{
						pos: pos,
						err: fmt.Errorf("empty interface %s is not permitted in sum type %s", getCanonicalName(t), getCanonicalName(sumType)),
					})
				} else {
					// [goaugtype] Preserve regular non-empty interface term as a distinct term
					leafTerms = append(leafTerms, &term{tilde: false, typ: t})
				}
			} else {
				leafTerms = append(leafTerms, &term{tilde: false, typ: t})
			}
		}
	}
	expand(rawTerms)

	// 3. Check pairwise disjointness using overlappingTerm (from union.go)
	for i, t := range leafTerms {
		if j := overlappingTerm(leafTerms[:i], t); j >= 0 {
			prev := leafTerms[j]
			prevIsIface := types.IsInterface(prev.typ)
			currIsIface := types.IsInterface(t.typ)

			switch {
			case !prevIsIface && !currIsIface && types.Identical(prev.typ, t.typ):
				errs = append(errs, augtError{
					pos: pos,
					err: fmt.Errorf("duplicate concrete type %s in sum type %s", getCanonicalName(t.typ), getCanonicalName(sumType)),
				})
			case !currIsIface && prevIsIface:
				errs = append(errs, augtError{
					pos: pos,
					err: fmt.Errorf("overlapping union terms: %s already implements %s in sum type %s", getCanonicalName(t.typ), getCanonicalName(prev.typ), getCanonicalName(sumType)),
				})
			case currIsIface && !prevIsIface:
				errs = append(errs, augtError{
					pos: pos,
					err: fmt.Errorf("overlapping union terms: %s already implements %s in sum type %s", getCanonicalName(prev.typ), getCanonicalName(t.typ), getCanonicalName(sumType)),
				})
			case currIsIface && prevIsIface:
				errs = append(errs, augtError{
					pos: pos,
					err: fmt.Errorf("overlapping interface terms %s and %s in sum type %s", getCanonicalName(prev.typ), getCanonicalName(t.typ), getCanonicalName(sumType)),
				})
			default:
				errs = append(errs, augtError{
					pos: pos,
					err: fmt.Errorf("overlapping union terms %s and %s in sum type %s", getCanonicalName(prev.typ), getCanonicalName(t.typ), getCanonicalName(sumType)),
				})
			}
		}
	}

	tset := &_TypeSet{
		selfIface: selfIface,
		terms:     termlist(leafTerms).norm(),
	}

	return tset, errs
}

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
