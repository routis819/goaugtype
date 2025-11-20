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

const (
	Directive              = "goaugtype"
	DirectiveCommentPrefix = "// " + Directive + ":"
)

var Analyzer = &analysis.Analyzer{
	Name: "goaugtype",
	Doc:  "reports non-exhaustive type switches and invalid assignments on sum types",
	Run:  run,
}

func printErrs(errs []augtError, pass *analysis.Pass) {
	for _, e := range errs {
		pass.Reportf(e.pos, "%v", e.err)
	}
}

func run(pass *analysis.Pass) (any, error) {
	col, pkgAdtDecl := collectInfo(pass)
	if len(col.errs) != 0 {
		printErrs(col.errs, pass)

		return nil, nil
	}
	errs := check(pass.TypesInfo, col, pkgAdtDecl)
	printErrs(errs, pass)

	return nil, nil
}

type augtError struct {
	err error
	pos token.Pos
}

type goAugADTDecl struct {
	sumtype   string
	permitted []string
	// pos is the position of the directive comment.
	// It is required to evaluate imported types correctly within the file scope.
	pos token.Pos
}

// evGoAugADTDecl is abbreviation of "evaluated goaugtype ADT Declaration"
// goAugADTDecl -> (eval) -> evGoAugADTDecl
type evGoAugADTDecl struct {
	sumtype   types.TypeAndValue
	permitted []types.TypeAndValue
}

// parseAdtDeclCmt parses the comment group and returns the permitted types and the position of the directive.
func parseAdtDeclCmt(cmtgrps []*ast.CommentGroup) ([]string, token.Pos, error) {
	adtDeclLine := ""
	var pos token.Pos
SEARCHING:
	for _, cmtgrp := range cmtgrps {
		for _, cmt := range cmtgrp.List {
			if after, ok := strings.CutPrefix(cmt.Text, DirectiveCommentPrefix); ok {
				pos = cmt.Pos()
				adtDeclLine = after
				break SEARCHING
			}
		}
	}
	if adtDeclLine == "" {
		return nil, token.NoPos, nil
	}
	items := strings.Split(adtDeclLine, "|")
	if len(items) <= 1 {
		return nil, pos, fmt.Errorf("invalid format, pos :%v", pos)
	}
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
	}

	return items, pos, nil
}

func analysisTypeSpecWithCmt(cmtmap ast.CommentMap, tspc *ast.TypeSpec) ([]goAugADTDecl, error) {
	cmt, ok := cmtmap[tspc]
	if !ok {
		return nil, nil
	}
	permitted, pos, err := parseAdtDeclCmt(cmt)
	if err != nil {
		return nil, err
	}
	if permitted == nil {
		return nil, nil
	}
	sumtype, err := analysisTypeSpec(tspc)
	if err != nil {
		return nil, err
	}

	// Store the position to use for evaluation later
	return []goAugADTDecl{{sumtype: sumtype, permitted: permitted, pos: pos}}, nil
}

func analysisTypeSpec(tspc *ast.TypeSpec) (string, error) {
	switch v := tspc.Type.(type) {
	case *ast.Ident:
		if v.Name != "any" {
			return "", fmt.Errorf("goaugtype variable should be any or interface{}, pos - %v", tspc.Pos())
		}

		return tspc.Name.Name, nil
	case *ast.InterfaceType:
		if len(v.Methods.List) != 0 {
			return "", fmt.Errorf("goaugtype variable should be any or interface{}, pos - %v", tspc.Pos())
		}

		return tspc.Name.Name, nil
	default:
		return "", nil
	}
}

func analysisTypeDeclSpecs(decl *ast.GenDecl) (string, error) {
	if len(decl.Specs) != 1 {
		// golang allows to declare multiple types in a single
		// parenthesis. In that case, the comment does not
		// belong to ast.GenDecl node but ast.TypeSpec node.
		return "", fmt.Errorf("invalid format - pos:%v", decl.Pos())
	}
	spc := decl.Specs[0]
	tspc := spc.(*ast.TypeSpec)

	return analysisTypeSpec(tspc)
}

func analysisTypeDeclWithCmt(cmtmap ast.CommentMap, v *ast.GenDecl) ([]goAugADTDecl, error) {
	cmt, ok := cmtmap[v]
	if !ok {
		return nil, nil
	}
	permitted, pos, err := parseAdtDeclCmt(cmt)
	if err != nil {
		return nil, err
	}
	if permitted == nil {
		return nil, nil
	}
	sumtype, err := analysisTypeDeclSpecs(v)
	if err != nil {
		return nil, err
	}

	return []goAugADTDecl{{sumtype: sumtype, permitted: permitted, pos: pos}}, nil

}

type source struct {
	cmtmap ast.CommentMap
}

type collected struct {
	adtDecls []goAugADTDecl

	declAssigns     []*ast.ValueSpec
	assignStmts     []*ast.AssignStmt
	typeSwitchStmts []*ast.TypeSwitchStmt

	errs []augtError
}

type inspector struct {
	src source
	col collected
}

func (ispt *inspector) inspect(n ast.Node) bool {
	if ispt.src.cmtmap == nil {
		return false
	}
	switch v := n.(type) {
	case *ast.AssignStmt:
		ispt.col.assignStmts = append(ispt.col.assignStmts, v)
	case *ast.GenDecl:
		switch v.Tok {
		case token.TYPE:
			d, err := analysisTypeDeclWithCmt(ispt.src.cmtmap, v)
			if err != nil {
				ispt.col.errs = append(ispt.col.errs, augtError{err: err, pos: v.Pos()})

				break
			}
			ispt.col.adtDecls = append(ispt.col.adtDecls, d...)
		case token.VAR:
			vspc := v.Specs[0].(*ast.ValueSpec)
			if len(vspc.Values) == 0 {
				break
			}
			ispt.col.declAssigns = append(ispt.col.declAssigns, vspc)
		}
	case *ast.TypeSpec:
		d, err := analysisTypeSpecWithCmt(ispt.src.cmtmap, v)
		if err != nil {
			ispt.col.errs = append(ispt.col.errs, augtError{err: err, pos: v.Pos()})

			break
		}
		ispt.col.adtDecls = append(ispt.col.adtDecls, d...)
	case *ast.TypeSwitchStmt:
		ispt.col.typeSwitchStmts = append(ispt.col.typeSwitchStmts, v)
	default:
		break
	}

	return true
}

func findSumtypeByType(decls []evGoAugADTDecl, t types.Type) evGoAugADTDecl {
	for i := range decls {
		if types.Identical(t, decls[i].sumtype.Type) {
			return decls[i]
		}
	}

	return evGoAugADTDecl{}
}

// isPermitted checks if the expression's type is allowed in the sum type.
// It leverages types.TypeAndValue for robust checking of nil and type identity.
func isPermitted(tinfo *types.Info, decl evGoAugADTDecl, expr ast.Expr) bool {
	tv, ok := tinfo.Types[expr]
	if !ok {
		return false
	}

	// Allow assigning the sum type itself (e.g. t1 = t2)
	if tv.Type != nil && decl.sumtype.Type != nil && types.Identical(tv.Type, decl.sumtype.Type) {
		return true
	}

	// Iterate permitted types (which are also TypeAndValue)
	for _, permitted := range decl.permitted {
		// Case 1: Handle Nil
		// Check if the expression is nil (tv.IsNil()) AND the permitted type is also nil.
		// permitted.IsNil() handles "untyped nil" evaluated from directive.
		if tv.IsNil() {
			if permitted.IsNil() {
				return true
			}
			// Fallback: sometimes "nil" evaluates to a Type with string "untyped nil" but IsNil() might vary based on context.
			// Checking IsNil() is usually sufficient for `types.Eval("nil")`.
			continue
		}

		// Case 2: Handle Types
		// Use types.Identical for strict type equality instead of string comparison.
		if tv.Type != nil && permitted.Type != nil && types.Identical(tv.Type, permitted.Type) {
			return true
		}
	}

	return false
}

func getTypeByName(tinfo *types.Info, n string) types.Object {
	for id, tobj := range tinfo.Defs {
		if id.Name == n {
			return tobj
		}
	}

	return nil
}

func checkAssign(tinfo *types.Info, col collected, evdecl []evGoAugADTDecl) []augtError {
	var errs []augtError
	for _, a := range col.assignStmts {
		for i, lhs := range a.Lhs {
			lhsident, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			// 수정됨: 이름 기반 검색 -> ObjectOf를 통한 정확한 객체 참조
			tObj := tinfo.ObjectOf(lhsident)
			if tObj == nil {
				continue
			}
			sumt := findSumtypeByType(evdecl, tObj.Type())
			if sumt.sumtype.Type == nil {
				continue
			}
			if !isPermitted(tinfo, sumt, a.Rhs[i]) {
				errs = append(errs, augtError{
					err: errors.New("invalid assigning: type is not permitted"),
					pos: a.TokPos,
				})
			}
		}
	}
	for _, a := range col.declAssigns {
		for i, n := range a.Names {
			// 수정됨: 이름 기반 검색 -> ObjectOf를 통한 정확한 객체 참조
			tObj := tinfo.ObjectOf(n)
			if tObj == nil {
				continue
			}
			sumt := findSumtypeByType(evdecl, tObj.Type())
			if sumt.sumtype.Type == nil {
				continue
			}
			if !isPermitted(tinfo, sumt, a.Values[i]) {
				errs = append(errs, augtError{
					err: errors.New("invalid declaration: type is not permitted"),
					pos: a.Pos(),
				})
			}
		}
	}

	return errs
}

// getCanonicalName returns a fully qualified string representation of the type.
// It ensures that package paths are always included (e.g., "github.com/pkg/adt.Leaf").
func getCanonicalName(t types.Type) string {
	return types.TypeString(t, func(p *types.Package) string {
		return p.Path()
	})
}

func checkTypeSwitch(tinfo *types.Info, col collected, evdecl []evGoAugADTDecl) []augtError {
	var errs []augtError

	for _, stmt := range col.typeSwitchStmts {
		var targetExpr ast.Expr

		// 1. Identify the target expression of the switch
		switch t := stmt.Assign.(type) {
		case *ast.AssignStmt: // switch v := x.(type)
			if len(t.Rhs) > 0 {
				if ta, ok := t.Rhs[0].(*ast.TypeAssertExpr); ok {
					targetExpr = ta.X
				}
			}
		case *ast.ExprStmt: // switch x.(type)
			if ta, ok := t.X.(*ast.TypeAssertExpr); ok {
				targetExpr = ta.X
			}
		}

		if targetExpr == nil {
			continue
		}

		// 2. Resolve type of the target expression
		ident, ok := targetExpr.(*ast.Ident)
		if !ok {
			continue
		}
		// 수정됨: 이름 기반 검색 -> ObjectOf를 통한 정확한 객체 참조
		tObj := tinfo.ObjectOf(ident)
		if tObj == nil {
			continue
		}
		sumt := findSumtypeByType(evdecl, tObj.Type())
		if sumt.sumtype.Type == nil {
			continue
		}

		// 3. Track unhandled types using a Set (map)
		// We use getCanonicalName for safer map keys (includes package path).
		unhandled := make(map[string]bool)
		nilPermitted := false
		for _, p := range sumt.permitted {
			if p.IsNil() || (p.Type != nil && p.Type.String() == "untyped nil") {
				nilPermitted = true
			} else if p.Type != nil {
				unhandled[getCanonicalName(p.Type)] = true
			}
		}

		// 4. Iterate through all cases
		for _, bdstmt := range stmt.Body.List {
			clause, ok := bdstmt.(*ast.CaseClause)
			if !ok {
				continue
			}

			// Check each type in the case list (case A, B:)
			for _, expr := range clause.List {
				tv, ok := tinfo.Types[expr]
				if !ok {
					continue
				}

				// Handle "case nil:" safely using TypeAndValue
				if tv.IsNil() {
					if !nilPermitted {
						errs = append(
							errs,
							augtError{
								err: errors.New("invalid type switch case: nil is not permitted"),
								pos: expr.Pos(),
							})
					} else {
						// nil is handled
						nilPermitted = false // Mark as handled
					}
				} else {
					// Handle normal types
					if tv.Type != nil {
						// Use canonical name for map lookup
						tStr := getCanonicalName(tv.Type)
						if unhandled[tStr] {
							delete(unhandled, tStr)
						}

						// Double check validity using strict check
						if !isPermitted(tinfo, sumt, expr) {
							errs = append(errs, augtError{
								err: fmt.Errorf("invalid type switch case: %s", tStr),
								pos: expr.Pos(),
							})
						}
					}
				}
			}
		}

		// 5. Check for exhaustiveness
		// We need to know if 'nil' was handled.
		nilWasHandled := false
		for _, bdstmt := range stmt.Body.List {
			clause, ok := bdstmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, expr := range clause.List {
				if tv, ok := tinfo.Types[expr]; ok && tv.IsNil() {
					nilWasHandled = true
				}
			}
		}

		var missing []string
		for k := range unhandled {
			missing = append(missing, k)
		}
		if nilPermitted && !nilWasHandled {
			missing = append(missing, "nil")
		}

		if len(missing) > 0 {
			errs = append(
				errs,
				augtError{
					err: fmt.Errorf("non-exhaustive type switch. missing cases: %v", strings.Join(missing, ", ")),
					pos: stmt.Pos(),
				})
		}
	}

	return errs
}

func check(tinfo *types.Info, col collected, evdecl []evGoAugADTDecl) []augtError {
	var errs []augtError

	asserr := checkAssign(tinfo, col, evdecl)
	errs = append(errs, asserr...)

	tserr := checkTypeSwitch(tinfo, col, evdecl)
	errs = append(errs, tserr...)

	return errs
}

func collectInfo(pass *analysis.Pass) (collected, []evGoAugADTDecl) {
	col := collected{}
	var pkgADTDecl []evGoAugADTDecl
	for _, astf := range pass.Files {
		cmtmap := ast.NewCommentMap(pass.Fset, astf, astf.Comments)
		ispt := inspector{
			src: source{cmtmap: cmtmap},
			col: collected{},
		}
		ast.Inspect(astf, ispt.inspect)
		adtDecls := make([]evGoAugADTDecl, 0, len(ispt.col.adtDecls))
		for _, decl := range ispt.col.adtDecls {
			// FIXME(isr): try to provide valid token position. -> SOLVED
			// Evaluate sumtype using the directive's position to resolve imports correctly.
			sumt, err := types.Eval(pass.Fset, pass.Pkg, decl.pos, decl.sumtype)
			if err != nil {
				pass.Reportf(decl.pos, "goaugtype: failed to evaluate sum type: %v", err)
				continue
			}
			permitted := make([]types.TypeAndValue, 0, len(decl.permitted))
			hasError := false
			for _, prm := range decl.permitted {
				// Evaluate permitted types using the directive's position.
				prmt, err := types.Eval(pass.Fset, pass.Pkg, decl.pos, prm)
				if err != nil {
					pass.Reportf(decl.pos, "goaugtype: failed to evaluate permitted type '%s': %v", prm, err)
					hasError = true
					break
				}
				permitted = append(permitted, prmt)
			}
			if hasError {
				continue
			}
			adtDecls = append(adtDecls, evGoAugADTDecl{sumtype: sumt, permitted: permitted})
		}
		col.adtDecls = append(col.adtDecls, ispt.col.adtDecls...)
		col.declAssigns = append(col.declAssigns, ispt.col.declAssigns...)
		col.assignStmts = append(col.assignStmts, ispt.col.assignStmts...)
		col.typeSwitchStmts = append(col.typeSwitchStmts, ispt.col.typeSwitchStmts...)
		col.errs = append(col.errs, ispt.col.errs...)
		pkgADTDecl = append(pkgADTDecl, adtDecls...)
	}

	return col, pkgADTDecl
}
