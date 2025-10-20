package analyzer

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
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

func run(pass *analysis.Pass) (any, error) {
	col, pkgAdtDecl := collectInfo(pass)
	// TODO: handle errors from collectInfo

	errs := check(pass.TypesInfo, col, pkgAdtDecl)

	for _, e := range errs {
		pass.Reportf(e.pos, "%v", e.err)
	}

	return nil, nil
}

type augtError struct {
	err error
	pos token.Pos
}

type goAugADTDecl struct {
	sumtype   string
	permitted []string
}

// goAugADTDecl -> (eval) -> evGoAugADTDecl
type evGoAugADTDecl struct {
	sumtype   types.TypeAndValue
	permitted []types.TypeAndValue
}

func parseAdtDeclCmt(cmtgrps []*ast.CommentGroup) ([]string, error) {
	adtDeclLine := ""
	var pos token.Pos
SEARCHING:
	for _, cmtgrp := range cmtgrps {
		for _, cmt := range cmtgrp.List {
			pos = cmt.Pos()
			if after, ok := strings.CutPrefix(cmt.Text, DirectiveCommentPrefix); ok {
				adtDeclLine = after

				break SEARCHING
			}
		}
	}
	if adtDeclLine == "" {
		return nil, nil
	}
	items := strings.Split(adtDeclLine, "|")
	if len(items) <= 1 {
		return nil, fmt.Errorf("invalid format, pos :%v", pos)
	}
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
	}

	return items, nil
}

func analysisTypeSpecWithCmt(cmtmap ast.CommentMap, tspc *ast.TypeSpec) ([]goAugADTDecl, error) {
	cmt, ok := cmtmap[tspc]
	if !ok {
		return nil, nil
	}
	permitted, err := parseAdtDeclCmt(cmt)
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

	return []goAugADTDecl{{sumtype: sumtype, permitted: permitted}}, nil
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
	permitted, err := parseAdtDeclCmt(cmt)
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

	return []goAugADTDecl{{sumtype: sumtype, permitted: permitted}}, nil

}

type source struct {
	cmtmap ast.CommentMap
}

type collected struct {
	adtDecls        []goAugADTDecl
	declAssigns     []*ast.ValueSpec
	assignStmts     []*ast.AssignStmt
	typeSwitchStmts []*ast.TypeSwitchStmt
	e               []error
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
				ispt.col.e = append(ispt.col.e, err)

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
			ispt.col.e = append(ispt.col.e, err)

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
		if decls[i].sumtype.Type.String() == t.String() {
			return decls[i]
		}
	}

	return evGoAugADTDecl{}
}

func isPermitted(tinfo *types.Info, sumtype evGoAugADTDecl, expr ast.Expr) bool {
	t := tinfo.TypeOf(expr)
	if t == nil {
		return false
	}
	if t.String() == sumtype.sumtype.Type.String() {
		return true
	}
	for i := range sumtype.permitted {
		if sumtype.permitted[i].Type.String() == t.String() {
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
			t := getTypeByName(tinfo, lhsident.Name)
			if t == nil {
				continue
			}
			sumt := findSumtypeByType(evdecl, t.Type())
			if sumt.sumtype.Type == nil {
				continue
			}
			if !isPermitted(tinfo, sumt, a.Rhs[i]) {
				errs = append(errs, augtError{
					err: errors.New("invalid assigning"),
					pos: a.TokPos,
				})
			}
		}
	}
	for _, a := range col.declAssigns {
		for i, n := range a.Names {
			t := getTypeByName(tinfo, n.Name)
			if t == nil {
				continue
			}
			sumt := findSumtypeByType(evdecl, t.Type())
			if sumt.sumtype.Type == nil {
				continue
			}
			if !isPermitted(tinfo, sumt, a.Values[i]) {
				errs = append(errs, augtError{
					err: errors.New("invalid declaration"),
					pos: a.Pos(),
				})
			}
		}
	}

	return errs
}

func checkTypeSwitch(tinfo *types.Info, col collected, evdecl []evGoAugADTDecl) []augtError {
	var errs []augtError
	for _, stmt := range col.typeSwitchStmts {
		exprstmt, ok := stmt.Assign.(*ast.ExprStmt)
		if !ok {
			continue
		}
		taexpr, ok := exprstmt.X.(*ast.TypeAssertExpr)
		if !ok {
			continue
		}
		chkident, ok := taexpr.X.(*ast.Ident)
		if !ok {
			continue
		}
		t := getTypeByName(tinfo, chkident.Name)
		if t == nil {
			continue
		}
		sumt := findSumtypeByType(evdecl, t.Type())
		if sumt.sumtype.Type == nil {
			continue
		}
		if len(sumt.permitted) != len(stmt.Body.List) {
			errs = append(
				errs,
				augtError{
					err: errors.New("invalid type switch"),
					pos: stmt.Pos(),
				})
		}
		for _, bdstmt := range stmt.Body.List {
			clause, ok := bdstmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			expr := clause.List[0]
			if !isPermitted(tinfo, sumt, expr) {
				errs = append(
					errs,
					augtError{
						err: errors.New("invalid type switch"),
						pos: stmt.Pos(),
					})
			}
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
		adtDecls := make([]evGoAugADTDecl, len(ispt.col.adtDecls))
		for i, decl := range ispt.col.adtDecls {
			// FIXME(isr): try to provide valid token position.
			sumt, err := types.Eval(pass.Fset, pass.Pkg, token.NoPos, decl.sumtype)
			if err != nil {
				// TODO: create a proper error
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			permitted := make([]types.TypeAndValue, len(decl.permitted))
			for j, prm := range decl.permitted {
				prmt, err := types.Eval(pass.Fset, pass.Pkg, token.NoPos, prm)
				if err != nil {
					// TODO: create a proper error
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}
				permitted[j] = prmt
			}
			adtDecls[i] = evGoAugADTDecl{sumtype: sumt, permitted: permitted}
		}
		col.adtDecls = append(col.adtDecls, ispt.col.adtDecls...)
		col.declAssigns = append(col.declAssigns, ispt.col.declAssigns...)
		col.assignStmts = append(col.assignStmts, ispt.col.assignStmts...)
		col.typeSwitchStmts = append(col.typeSwitchStmts, ispt.col.typeSwitchStmts...)
		col.e = append(col.e, ispt.col.e...)
		pkgADTDecl = append(pkgADTDecl, adtDecls...)
	}

	return col, pkgADTDecl
}
