package analyzer

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// UpstreamBaseRevision specifies the golang/go release tag that the SumTypeSet
// implementation in this package is synchronized with.
//
//go:generate go run ../../cmd/upstreamchecker -config ../../upstream.json -apply-patches
const UpstreamBaseRevision = "go1.27.3"

func getCanonicalName(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		if p == nil {
			return ""
		}
		return p.Path()
	})
}

// SumTypeFact is an analysis.Fact attached to a *types.TypeName object
// representing a sum type exported across package boundaries.
type SumTypeFact struct {
	ConcreteTerms []string
	IfaceTerms    []string
}

func (*SumTypeFact) AFact() {}

func (f *SumTypeFact) String() string {
	var all []string
	all = append(all, f.ConcreteTerms...)
	all = append(all, f.IfaceTerms...)
	return "SumTypeFact(" + strings.Join(all, " | ") + ")"
}

type SumTypeSet struct {
	Name          *types.TypeName
	SumType       types.Type       // Named interface type (e.g. Tree)
	SelfIface     *types.Interface // Underlying interface of SumType
	tset          _TypeSet         // Normalized type set terms
	RawTerms      []types.Type     // Direct evaluated union terms before flattening
	ConcreteTerms []types.Type     // Flattened concrete (non-interface) types
	IfaceTerms    []types.Type     // Flattened regular (non-sum) interface types
	Pos           token.Pos        // Position of the directive comment
}

func (s *SumTypeSet) Includes(v types.Type) bool {
	if s == nil || v == nil {
		return false
	}
	if types.Identical(v, s.SumType) {
		return true
	}
	return s.tset.terms.includes(v)
}

func (s *SumTypeSet) SubsetOf(other *SumTypeSet) bool {
	if s == nil || other == nil {
		return false
	}
	if types.Identical(s.SumType, other.SumType) {
		return true
	}
	return s.tset.subsetOf(&other.tset)
}

func (s *SumTypeSet) IsPermittedType(reg *Registry, valType types.Type) bool {
	if s == nil || valType == nil {
		return false
	}
	if reg != nil {
		return reg.implements(valType, s.SumType, false, nil)
	}
	return s.Includes(valType)
}

func (s *SumTypeSet) IsPermittedValue(tinfo *types.Info, reg *Registry, expr ast.Expr) bool {
	if s == nil || expr == nil || tinfo == nil {
		return false
	}
	x := newOperand(tinfo, expr)
	return x.assignableTo(reg, s.SumType, nil)
}

func (s *SumTypeSet) IsPermittedCaseType(reg *Registry, caseType types.Type) bool {
	if s == nil || caseType == nil {
		return false
	}
	if reg != nil && reg.Lookup(caseType) != nil {
		return false
	}
	return s.tset.terms.includes(caseType)
}

type Registry struct {
	pass *analysis.Pass
	sets []*SumTypeSet
}

func resolveTypeByCanonicalName(rootPkg *types.Package, ownerPkg *types.Package, name string) types.Type {
	if strings.HasPrefix(name, "*") {
		elem := resolveTypeByCanonicalName(rootPkg, ownerPkg, strings.TrimPrefix(name, "*"))
		if elem == nil {
			return nil
		}
		return types.NewPointer(elem)
	}

	if obj := types.Universe.Lookup(name); obj != nil {
		return obj.Type()
	}

	lastDot := strings.LastIndex(name, ".")
	if lastDot < 0 {
		if rootPkg != nil {
			if obj := rootPkg.Scope().Lookup(name); obj != nil {
				return obj.Type()
			}
		}
		if ownerPkg != nil {
			if obj := ownerPkg.Scope().Lookup(name); obj != nil {
				return obj.Type()
			}
		}
		return nil
	}

	pkgPath := name[:lastDot]
	typeName := name[lastDot+1:]

	var findPkg func(p *types.Package, visited map[string]bool) *types.Package
	findPkg = func(p *types.Package, visited map[string]bool) *types.Package {
		if p == nil || visited[p.Path()] {
			return nil
		}
		visited[p.Path()] = true
		if p.Path() == pkgPath || p.Name() == pkgPath {
			return p
		}
		for _, imp := range p.Imports() {
			if found := findPkg(imp, visited); found != nil {
				return found
			}
		}
		return nil
	}

	visited := make(map[string]bool)
	targetPkg := findPkg(rootPkg, visited)
	if targetPkg == nil && ownerPkg != nil {
		targetPkg = findPkg(ownerPkg, visited)
	}
	if targetPkg != nil {
		if obj := targetPkg.Scope().Lookup(typeName); obj != nil {
			return obj.Type()
		}
	}
	return nil
}

func (r *Registry) Lookup(t types.Type) *SumTypeSet {
	if r == nil || t == nil {
		return nil
	}
	for _, s := range r.sets {
		if types.Identical(t, s.SumType) {
			return s
		}
	}
	if r.pass != nil {
		t = types.Unalias(t)
		if named, ok := t.(*types.Named); ok {
			obj := named.Obj()
			if obj != nil && obj.Pkg() != nil && obj.Pkg() != r.pass.Pkg {
				var fact SumTypeFact
				if r.pass.ImportObjectFact(obj, &fact) {
					var concreteTerms []types.Type
					for _, cName := range fact.ConcreteTerms {
						if resolved := resolveTypeByCanonicalName(r.pass.Pkg, obj.Pkg(), cName); resolved != nil {
							concreteTerms = append(concreteTerms, resolved)
						}
					}
					var ifaceTerms []types.Type
					for _, iName := range fact.IfaceTerms {
						if resolved := resolveTypeByCanonicalName(r.pass.Pkg, obj.Pkg(), iName); resolved != nil {
							ifaceTerms = append(ifaceTerms, resolved)
						}
					}

					var selfIface *types.Interface
					if iface, ok := named.Underlying().(*types.Interface); ok {
						selfIface = iface
					}

					var terms termlist
					for _, ct := range concreteTerms {
						terms = append(terms, &term{tilde: false, typ: ct})
					}
					for _, it := range ifaceTerms {
						terms = append(terms, &term{tilde: false, typ: it})
					}

					newSet := &SumTypeSet{
						Name:          obj,
						SumType:       named,
						SelfIface:     selfIface,
						tset:          _TypeSet{terms: terms.norm()},
						ConcreteTerms: concreteTerms,
						IfaceTerms:    ifaceTerms,
						Pos:           obj.Pos(),
					}
					r.sets = append(r.sets, newSet)
					return newSet
				}
			}
		}
	}
	return nil
}

func shouldExportFact(pass *analysis.Pass, s *SumTypeSet) bool {
	return s != nil && s.Name != nil && s.Name.Exported() && pass != nil
}

func containsType(list []types.Type, t types.Type) bool {
	for _, item := range list {
		if types.Identical(item, t) {
			return true
		}
	}
	return false
}

func BuildRegistry(pass *analysis.Pass, rawDecls []goAugADTDecl) (*Registry, []augtError) {
	reg := &Registry{pass: pass}
	var errs []augtError

	// Pass 1: Evaluate declarations
	for _, decl := range rawDecls {
		sumTV, err := types.Eval(pass.Fset, pass.Pkg, decl.pos, decl.sumtype)
		if err != nil {
			errs = append(errs, augtError{pos: decl.pos, err: fmt.Errorf("failed to evaluate sum type: %w", err)})
			continue
		}
		if sumTV.Type == nil {
			errs = append(errs, augtError{pos: decl.pos, err: fmt.Errorf("%s is not a type", decl.sumtype)})
			continue
		}
		selfIface, ok := sumTV.Type.Underlying().(*types.Interface)
		if !ok {
			errs = append(errs, augtError{pos: decl.pos, err: fmt.Errorf("%s is not an interface", decl.sumtype)})
			continue
		}

		var rawTerms []types.Type
		hasErr := false
		for _, prm := range decl.permitted {
			if err := parseTilde(prm); err != nil {
				errs = append(errs, augtError{pos: decl.pos, err: err})
				hasErr = true
				continue
			}
			if prm == "nil" {
				errs = append(errs, augtError{pos: decl.pos, err: errors.New("nil is not a type and is not permitted in sum type directives")})
				hasErr = true
				continue
			}
			prmt, err := types.Eval(pass.Fset, pass.Pkg, decl.pos, prm)
			if err != nil {
				errs = append(errs, augtError{pos: decl.pos, err: fmt.Errorf("failed to evaluate permitted type '%s': %w", prm, err)})
				hasErr = true
				continue
			}
			if prmt.IsNil() || !prmt.IsType() {
				errs = append(errs, augtError{pos: decl.pos, err: fmt.Errorf("%s is not a type", prm)})
				hasErr = true
				continue
			}
			rawTerms = append(rawTerms, prmt.Type)
		}
		if hasErr {
			continue
		}

		var typeName *types.TypeName
		if named, ok := sumTV.Type.(*types.Named); ok {
			typeName = named.Obj()
		}

		reg.sets = append(reg.sets, &SumTypeSet{
			Name:      typeName,
			SumType:   sumTV.Type,
			SelfIface: selfIface,
			RawTerms:  rawTerms,
			Pos:       decl.pos,
		})
	}

	// Pass 2: Compute _TypeSet and validate union terms via computeUnionTypeSet
	for _, s := range reg.sets {
		tset, valErrs := computeUnionTypeSet(reg, s.Pos, s.SumType, s.SelfIface, s.RawTerms)
		s.tset = *tset
		errs = append(errs, valErrs...)

		for _, t := range s.tset.terms {
			if t != nil && t.typ != nil {
				if types.IsInterface(t.typ) {
					s.IfaceTerms = append(s.IfaceTerms, t.typ)
				} else {
					s.ConcreteTerms = append(s.ConcreteTerms, t.typ)
				}
			}
		}

		if len(valErrs) == 0 && shouldExportFact(pass, s) {
			var concreteNames []string
			for _, ct := range s.ConcreteTerms {
				concreteNames = append(concreteNames, getCanonicalName(ct))
			}
			var ifaceNames []string
			for _, it := range s.IfaceTerms {
				ifaceNames = append(ifaceNames, getCanonicalName(it))
			}
			pass.ExportObjectFact(s.Name, &SumTypeFact{
				ConcreteTerms: concreteNames,
				IfaceTerms:    ifaceNames,
			})
		}
	}

	return reg, errs
}
