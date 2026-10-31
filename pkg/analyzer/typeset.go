// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package analyzer

// A _TypeSet represents the normalized term list of a sum type.
// Modeled after _TypeSet in src/go/types/typeset.go.
type _TypeSet struct {
	terms termlist // type terms of the type set
}

// subsetOf reports whether s1 ⊆ s2.
func (s1 *_TypeSet) subsetOf(s2 *_TypeSet) bool {
	if s1 == nil || s2 == nil {
		return false
	}
	return s1.terms.subsetOf(s2.terms)
}

func (s *_TypeSet) String() string {
	switch {
	case s.terms.isEmpty():
		return "∅"
	case s.terms.isAll():
		return "𝓤"
	}
	return s.terms.String()
}
