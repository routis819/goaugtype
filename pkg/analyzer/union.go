// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analyzer

import (
	"fmt"
	"go/token"
	"go/types"
	"strings"
)

// parseTilde validates that ~T syntax is not used in sum types.
// Modeled after parseTilde in src/go/types/union.go (lines 139–165).
// [goaugtype] As specified in #57644, underlying type syntax ~T is not permitted in sum types.
func parseTilde(termStr string) error {
	if strings.HasPrefix(termStr, "~") {
		return fmt.Errorf("underlying type syntax %s is not permitted in sum types", termStr)
	}
	return nil
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
		terms: termlist(leafTerms).norm(),
	}

	return tset, errs
}
