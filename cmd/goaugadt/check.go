package main

import (
	"errors"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"strings"

	"golang.org/x/tools/go/packages"
)

type augtError struct {
	err error
	pos token.Pos
}

const pkgLoadMode = packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo

type goAugAdtDecl struct {
	sumtype   string
	permitted []string
}

type evGoAugAdtDecl struct {
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
			if strings.HasPrefix(cmt.Text, "// goaugadt:") {
				adtDeclLine = strings.TrimLeft(cmt.Text, "// goaugadt:")
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

func analysisTypeSpecWithCmt(cmtmap ast.CommentMap, tspc *ast.TypeSpec) ([]goAugAdtDecl, error) {
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
	return []goAugAdtDecl{{sumtype: sumtype, permitted: permitted}}, nil
}

func analysisTypeSpec(tspc *ast.TypeSpec) (string, error) {
	switch v := tspc.Type.(type) {
	case *ast.Ident:
		if v.Name != "any" {
			return "", fmt.Errorf("goaugadt variable should be any or interface{}, pos - %v", tspc.Pos())
		}
		return tspc.Name.Name, nil
	case *ast.InterfaceType:
		if len(v.Methods.List) != 0 {
			return "", fmt.Errorf("goaugadt variable should be any or interface{}, pos - %v", tspc.Pos())
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

func analysisTypeDeclWithCmt(cmtmap ast.CommentMap, v *ast.GenDecl) ([]goAugAdtDecl, error) {
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
	return []goAugAdtDecl{{sumtype: sumtype, permitted: permitted}}, nil

}

type source struct {
	cmtmap ast.CommentMap
}

type collected struct {
	adtdecls        []goAugAdtDecl
	declassigns     []*ast.ValueSpec
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
			ispt.col.adtdecls = append(ispt.col.adtdecls, d...)
		case token.VAR:
			vspc := v.Specs[0].(*ast.ValueSpec)
			if len(vspc.Values) == 0 {
				break
			}
			ispt.col.declassigns = append(ispt.col.declassigns, vspc)
		}
	case *ast.TypeSpec:
		d, err := analysisTypeSpecWithCmt(ispt.src.cmtmap, v)
		if err != nil {
			ispt.col.e = append(ispt.col.e, err)
			break
		}
		ispt.col.adtdecls = append(ispt.col.adtdecls, d...)
	case *ast.TypeSwitchStmt:
		ispt.col.typeSwitchStmts = append(ispt.col.typeSwitchStmts, v)
	default:
	}
	return true
}

func findSumtypeByType(decls []evGoAugAdtDecl, t types.Type) evGoAugAdtDecl {
	for i := range decls {
		if decls[i].sumtype.Type.String() == t.String() {
			return decls[i]
		}
	}
	return evGoAugAdtDecl{}
}

func isPermitted(tinfo *types.Info, sumtype evGoAugAdtDecl, expr ast.Expr) bool {
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

func getIdentByName(tinfo *types.Info, n string) *ast.Ident {
	for id := range tinfo.Defs {
		if id.Name == n {
			return id
		}
	}
	return nil
}

func getTypeByName(tinfo *types.Info, n string) types.Object {
	for id, tobj := range tinfo.Defs {
		if id.Name == n {
			return tobj
		}
	}
	return nil
}

func checkAssign(tinfo *types.Info, col collected, evdecl []evGoAugAdtDecl) []augtError {
	var err []augtError
	for _, a := range col.assignStmts {
		for i, lhs := range a.Lhs {
			lhsident := lhs.(*ast.Ident)
			t := getTypeByName(tinfo, lhsident.Name)
			sumt := findSumtypeByType(evdecl, t.Type())
			if sumt.sumtype.Type == nil {
				continue
			}
			if !isPermitted(tinfo, sumt, a.Rhs[i]) {
				err = append(err, augtError{
					err: errors.New("invalid assigning"),
					pos: a.TokPos,
				})
			}
		}
	}
	for _, a := range col.declassigns {
		for i, n := range a.Names {
			t := getTypeByName(tinfo, n.Name)
			sumt := findSumtypeByType(evdecl, t.Type())
			if sumt.sumtype.Type == nil {
				continue
			}
			if !isPermitted(tinfo, sumt, a.Values[i]) {
				err = append(err, augtError{
					err: errors.New("invalid declaration"),
					pos: a.Pos(),
				})
			}
		}
	}
	return err
}

func checkTypeSwitch(tinfo *types.Info, col collected, evdecl []evGoAugAdtDecl) []augtError {
	var err []augtError
	for _, stmt := range col.typeSwitchStmts {
		exprstmt := stmt.Assign.(*ast.ExprStmt)
		taexpr := exprstmt.X.(*ast.TypeAssertExpr)
		chkident := taexpr.X.(*ast.Ident)
		t := getTypeByName(tinfo, chkident.Name)
		sumt := findSumtypeByType(evdecl, t.Type())
		if sumt.sumtype.Type == nil {
			continue
		}
		if len(sumt.permitted) != len(stmt.Body.List) {
			err = append(
				err,
				augtError{
					err: errors.New("invalid type switch"),
					pos: stmt.Pos(),
				})
		}
		for _, bdstmt := range stmt.Body.List {
			clause := bdstmt.(*ast.CaseClause)
			expr := clause.List[0]
			if !isPermitted(tinfo, sumt, expr) {
				err = append(
					err,
					augtError{
						err: errors.New("invalid type switch"),
						pos: stmt.Pos(),
					})
			}
		}
	}
	return err
}

func check(tinfo *types.Info, col collected, evdecl []evGoAugAdtDecl) []augtError {
	var err []augtError

	asserr := checkAssign(tinfo, col, evdecl)
	err = append(err, asserr...)

	tserr := checkTypeSwitch(tinfo, col, evdecl)
	err = append(err, tserr...)

	return err
}

func collectInfo(pkg *packages.Package) (collected, []evGoAugAdtDecl) {
	col := collected{}
	var pkgAdtDecl []evGoAugAdtDecl
	for _, astf := range pkg.Syntax {
		cmtmap := ast.NewCommentMap(pkg.Fset, astf, astf.Comments)
		ispt := inspector{
			src: source{cmtmap: cmtmap},
			col: collected{},
		}
		ast.Inspect(astf, ispt.inspect)
		adtDecls := make([]evGoAugAdtDecl, len(ispt.col.adtdecls))
		for i, decl := range ispt.col.adtdecls {
			sumt, err := types.Eval(pkg.Fset, pkg.Types, token.NoPos, decl.sumtype)
			if err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			permitted := make([]types.TypeAndValue, len(decl.permitted))
			for i, prm := range decl.permitted {
				prmt, err := types.Eval(pkg.Fset, pkg.Types, token.NoPos, prm)
				if err != nil {
					fmt.Fprintln(os.Stderr, err.Error())
					os.Exit(1)
				}
				permitted[i] = prmt
			}
			adtDecls[i] = evGoAugAdtDecl{sumtype: sumt, permitted: permitted}
		}
		col.adtdecls = append(col.adtdecls, ispt.col.adtdecls...)
		col.declassigns = append(col.declassigns, ispt.col.declassigns...)
		col.assignStmts = append(col.assignStmts, ispt.col.assignStmts...)
		col.typeSwitchStmts = append(col.typeSwitchStmts, ispt.col.typeSwitchStmts...)
		col.e = append(col.e, ispt.col.e...)
		pkgAdtDecl = append(pkgAdtDecl, adtDecls...)
	}
	return col, pkgAdtDecl
}

func inspect(pkg *packages.Package) []augtError {
	col, pkgAdtDecl := collectInfo(pkg)
	return check(pkg.TypesInfo, col, pkgAdtDecl)
}

func printErrs(pkg *packages.Package, errs []augtError) {
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "%s - %s\n",
			pkg.Fset.Position(e.pos).String(),
			e.err.Error(),
		)
	}
}

func checkPkg(pkg *packages.Package) {
	errs := inspect(pkg)
	printErrs(pkg, errs)
}

func checkPkgs(pkgs []*packages.Package) {
	for _, pkg := range pkgs {
		checkPkg(pkg)
	}
}
