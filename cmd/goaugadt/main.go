package main

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"strings"

	"golang.org/x/tools/go/packages"
)

func parsePkgList() []string {
	// TODO stub
	return []string{"github.com/routiz/goaugt/test/adtsample"}
}

const pkgLoadMode = packages.NeedTypes |
	packages.NeedSyntax |
	packages.NeedTypesInfo

type decl struct {
	sumtype   string
	permitted []string
}

type assign struct {
	lhs, typ, ident string
}

type collected struct {
	// k - type name, v - variable names
	idmap map[string][]string
	d     []decl
	a     []assign
}

func parseAdtDeclCmt(cmtgrps []*ast.CommentGroup) ([]string, bool) {
	adtDeclLine := ""
SEARCHING:
	for _, cmtgrp := range cmtgrps {
		for _, cmt := range cmtgrp.List {
			fmt.Printf("cmt.Text: %v\n", cmt.Text)
			if strings.HasPrefix(cmt.Text, "// goaugadt:") {
				adtDeclLine = strings.TrimLeft(cmt.Text, "// goaugadt:")
				break SEARCHING
			}
		}
	}
	if adtDeclLine == "" {
		return nil, false
	}
	items := strings.Split(adtDeclLine, "|")
	if len(items) <= 1 {
		return nil, false
	}
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
	}
	return items, true
}

func analysisTypeSpec(tspc *ast.TypeSpec) (string, bool) {
	switch v := tspc.Type.(type) {
	case *ast.Ident:
		return tspc.Name.Name, v.Name == "any"
	case *ast.InterfaceType:
		return tspc.Name.Name, len(v.Methods.List) == 0
	default:
		return "", false
	}
}

func analysisGenDeclSpecs(decl *ast.GenDecl) (string, bool) {
	if len(decl.Specs) != 1 {
		// golang allows to declare multiple types in a single
		// parenthesis. In that case, the comment does not
		// belong to ast.GenDecl node but ast.TypeSpec node.
		return "", false
	}
	spc := decl.Specs[0]
	tspc, ok := spc.(*ast.TypeSpec)
	if !ok {
		return "", false
	}
	return analysisTypeSpec(tspc)
}

func analysisTypeDecl(cmtmap ast.CommentMap, v *ast.GenDecl) []decl {
	cmt, ok := cmtmap[v]
	if !ok {
		return nil
	}
	permitted, ok := parseAdtDeclCmt(cmt)
	if !ok {
		return nil
	}
	sumtype, ok := analysisGenDeclSpecs(v)
	if !ok {
		return nil
	}
	return []decl{{sumtype: sumtype, permitted: permitted}}

}

func analysisVarDecl(r map[string][]string, v *ast.GenDecl) map[string][]string {
	vspc := v.Specs[0].(*ast.ValueSpec)
	typename := vspc.Type.(*ast.Ident).Name
	fmt.Printf("vspc.Values: %v\n", vspc.Values)
	fmt.Printf("vspc.Values: %T\n", vspc.Values)
	for i := range vspc.Values {
		fmt.Printf("vspc.Values[i]: %v\n", vspc.Values[i])
		fmt.Printf("vspc.Values[i]: %T\n", vspc.Values[i])
	}
	for i := range vspc.Names {
		stored := r[typename]
		stored = append(stored, vspc.Names[i].Name)
		r[typename] = stored
	}
	fmt.Printf("r[typename]: %v\n", r[typename])
	return r
}

func evalUnaryExpr(expr *ast.UnaryExpr) (string, error) {
	// var typename string
	// switch expr.Op {
	// case token.AND:
	// 	typename += "*"
	// 	rvx, ok := expr.X.(*ast.CompositeLit)
	// 	if !ok {
	// 		fmt.Println("cannot handle this yet.")
	// 		continue
	// 	}
	// 	ident, ok := rvx.Type.(*ast.Ident)
	// 	if !ok {
	// 		fmt.Println("cannot handle this yet.")
	// 		continue
	// 	}
	// }
	// typ := "*" + ident.Name
	// r = append(r, assign{lhs: idlhs.Name, typ: typ})
	// return typename, nil
	return "", nil
}

func analysisVarAssign(v *ast.AssignStmt) []assign {
	var r []assign
	for i := range v.Lhs {
		idlhs := v.Lhs[i].(*ast.Ident)
		fmt.Printf("idlhs.Name: %v\n", idlhs.Name)
		switch rv := v.Rhs[i].(type) {
		case *ast.CompositeLit:
			ident, ok := rv.Type.(*ast.Ident)
			if !ok {
				fmt.Println("cannot handle this yet.")
				continue
			}
			fmt.Printf("ident.Name: %v\n", ident.Name)
			r = append(r, assign{lhs: idlhs.Name, typ: ident.Name})
		case *ast.Ident:
			fmt.Printf("rv.Name: %v\n", rv.Name)
			r = append(r, assign{lhs: idlhs.Name, ident: rv.Name})
		case *ast.UnaryExpr:
			// evalUnaryExpr(expr * ast.UnaryExpr)
		default:
			fmt.Printf("Rhs is type of %T, which cannot handle yet.\n", rv)
		}
	}
	return r
}

type inspector struct {
	cmtmap ast.CommentMap
	c      collected
}

func (ispt *inspector) inspect(n ast.Node) bool {
	// fmt.Printf("%T\n", n)
	if ispt.cmtmap == nil {
		return false
	}
	switch v := n.(type) {
	case *ast.AssignStmt:
		a := analysisVarAssign(v)
		ispt.c.a = append(ispt.c.a, a...)
		fmt.Printf("ispt.c.a: %v\n", ispt.c.a)
	case *ast.GenDecl:
		switch v.Tok {
		case token.TYPE:
			d := analysisTypeDecl(ispt.cmtmap, v)
			ispt.c.d = append(ispt.c.d, d...)
		case token.VAR:
			ispt.c.idmap = analysisVarDecl(ispt.c.idmap, v)

		}
	case *ast.TypeSpec:
		cmt, ok := ispt.cmtmap[n]
		if !ok {
			break
		}
		permitted, ok := parseAdtDeclCmt(cmt)
		if !ok {
			break
		}
		sumtype, ok := analysisTypeSpec(v)
		if !ok {
			break
		}
		ispt.c.d = append(ispt.c.d, decl{
			sumtype: sumtype, permitted: permitted,
		})
	case *ast.TypeSwitchStmt:
		// fmt.Printf("v.Assign: %v\n", v.Assign)
		// fmt.Printf("type of v.Assign: %T\n", v.Assign)
		// exprStmt := v.Assign.(*ast.ExprStmt)
		// fmt.Printf("exprStmt: %v\n", exprStmt)
		// fmt.Printf("exprStmt.X: %v\n", exprStmt.X)
		// fmt.Printf("type of exprStmt.X: %T\n", exprStmt.X)
		// taExprStmt := exprStmt.X.(*ast.TypeAssertExpr)
		// fmt.Printf("taExprStmt: %v\n", taExprStmt)
		// fmt.Printf("taExprStmt.Type: %v\n", taExprStmt.Type)
		// fmt.Printf("taExprStmt.X: %v\n", taExprStmt.X)
		// fmt.Printf("type of taExprStmt.X: %v\n", taExprStmt.X)
	default:
	}
	return true
}

func main() {
	pkgpaths := parsePkgList()
	cfg := &packages.Config{Mode: pkgLoadMode}
	pkgs, err := packages.Load(cfg, pkgpaths...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if packages.PrintErrors(pkgs) != 0 {
		os.Exit(1)
	}

	for _, p := range pkgs {
		// for k, v := range p.TypesInfo.Defs {
		// 	fmt.Printf("k: %v\n", k)
		// 	fmt.Printf("v: %v\n", v)
		// }

		// for k, v := range p.TypesInfo.Types {
		// 	fmt.Printf("k: %v\n", k)
		// 	fmt.Printf("v: %v\n", v)
		// }

		for _, astf := range p.Syntax {
			cmtmap := ast.NewCommentMap(p.Fset, astf, astf.Comments)
			ispt := &inspector{
				cmtmap: cmtmap,
				c: collected{
					idmap: map[string][]string{},
				},
			}
			ast.Inspect(astf, ispt.inspect)
		}
	}

}
