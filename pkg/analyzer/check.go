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
	Name:      "goaugtype",
	Doc:       "reports non-exhaustive type switches and invalid assignments on sum types",
	Run:       run,
	FactTypes: []analysis.Fact{new(SumTypeFact)},
}

func printErrs(errs []augtError, pass *analysis.Pass) {
	for _, e := range errs {
		pass.Reportf(e.pos, "%v", e.err)
	}
}

func run(pass *analysis.Pass) (any, error) {
	col, reg := collectInfo(pass)
	printErrs(col.errs, pass)
	errs := check(pass.TypesInfo, col, reg)
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
	if strings.TrimSpace(adtDeclLine) == "" {
		return nil, pos, fmt.Errorf("invalid format, pos :%v", pos)
	}
	items := strings.Split(adtDeclLine, "|")
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
		if items[i] == "" {
			return nil, pos, fmt.Errorf("invalid format, pos :%v", pos)
		}
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
	callExprs       []*ast.CallExpr
	funcDecls       []*ast.FuncDecl
	funcLits        []*ast.FuncLit
	compositeLits   []*ast.CompositeLit
	sendStmts       []*ast.SendStmt
	typeAssertExprs []*ast.TypeAssertExpr

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
	case *ast.CallExpr:
		ispt.col.callExprs = append(ispt.col.callExprs, v)
	case *ast.AssignStmt:
		ispt.col.assignStmts = append(ispt.col.assignStmts, v)
	case *ast.FuncDecl:
		ispt.col.funcDecls = append(ispt.col.funcDecls, v)
	case *ast.FuncLit:
		ispt.col.funcLits = append(ispt.col.funcLits, v)
	case *ast.CompositeLit:
		ispt.col.compositeLits = append(ispt.col.compositeLits, v)
	case *ast.SendStmt:
		ispt.col.sendStmts = append(ispt.col.sendStmts, v)
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
			for _, spc := range v.Specs {
				if vspc, ok := spc.(*ast.ValueSpec); ok && len(vspc.Values) > 0 {
					ispt.col.declAssigns = append(ispt.col.declAssigns, vspc)
				}
			}
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
	case *ast.TypeAssertExpr:
		if v.Type != nil {
			ispt.col.typeAssertExprs = append(ispt.col.typeAssertExprs, v)
		}
	default:
		break
	}

	return true
}

func checkAssign(tinfo *types.Info, col collected, reg *Registry) []augtError {
	var errs []augtError
	for _, a := range col.assignStmts {
		errs = append(errs, assignVars(tinfo, reg, a)...)
	}
	for _, d := range col.declAssigns {
		errs = append(errs, initVars(tinfo, reg, d)...)
	}
	return errs
}

func checkTypeSwitch(tinfo *types.Info, col collected, reg *Registry) []augtError {
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
		targetType := tinfo.TypeOf(targetExpr)
		if targetType == nil {
			continue
		}
		sumt := reg.Lookup(targetType)
		if sumt == nil {
			continue
		}

		// 3. Track unhandled types using a Set (map)
		unhandled := make(map[string]bool)
		for _, ct := range sumt.ConcreteTerms {
			unhandled[getCanonicalName(ct)] = true
		}
		for _, it := range sumt.IfaceTerms {
			unhandled[getCanonicalName(it)] = true
		}

		var seenConcrete []types.Type
		var seenIfaces []types.Type
		nilHandled := false
		var defaultClause *ast.CaseClause

		// 4. Iterate through all cases
		for _, bdstmt := range stmt.Body.List {
			clause, ok := bdstmt.(*ast.CaseClause)
			if !ok {
				continue
			}

			// default clause
			if len(clause.List) == 0 {
				defaultClause = clause
				continue
			}

			// Check each type in the case list (case A, B:)
			for _, expr := range clause.List {
				tv, ok := tinfo.Types[expr]
				if !ok {
					continue
				}

				if tv.IsNil() || (tv.Type != nil && types.Identical(tv.Type, types.Typ[types.UntypedNil])) {
					if nilHandled {
						errs = append(errs, augtError{
							err: fmt.Errorf("duplicate or shadowed type switch case: nil"),
							pos: expr.Pos(),
						})
					}
					nilHandled = true
					continue
				}

				if tv.Type != nil {
					if !sumt.IsPermittedCaseType(reg, tv.Type) {
						errs = append(errs, augtError{
							err: fmt.Errorf("invalid type switch case: %s", getCanonicalName(tv.Type)),
							pos: expr.Pos(),
						})
						continue
					}

					// Check shadowing against previously seen cases
					if containsType(seenConcrete, tv.Type) || containsType(seenIfaces, tv.Type) {
						errs = append(errs, augtError{
							err: fmt.Errorf("shadowed type switch case: %s", getCanonicalName(tv.Type)),
							pos: expr.Pos(),
						})
						continue
					}

					var coveredBy types.Type
					for _, prevIface := range seenIfaces {
						if iface, ok := prevIface.Underlying().(*types.Interface); ok && types.Implements(tv.Type, iface) {
							coveredBy = prevIface
							break
						}
					}
					if coveredBy != nil {
						errs = append(errs, augtError{
							err: fmt.Errorf("shadowed type switch case: %s is already covered by %s", getCanonicalName(tv.Type), getCanonicalName(coveredBy)),
							pos: expr.Pos(),
						})
						continue
					}

					if types.IsInterface(tv.Type) {
						seenIfaces = append(seenIfaces, tv.Type)
						delete(unhandled, getCanonicalName(tv.Type))
						if iface, ok := tv.Type.Underlying().(*types.Interface); ok {
							for _, ct := range sumt.ConcreteTerms {
								if types.Implements(ct, iface) {
									delete(unhandled, getCanonicalName(ct))
								}
							}
						}
					} else {
						seenConcrete = append(seenConcrete, tv.Type)
						delete(unhandled, getCanonicalName(tv.Type))
					}
				}
			}
		}

		// 5. Check for exhaustiveness
		var missing []string
		for _, ct := range sumt.ConcreteTerms {
			cName := getCanonicalName(ct)
			if unhandled[cName] {
				missing = append(missing, cName)
				delete(unhandled, cName)
			}
		}
		for _, it := range sumt.IfaceTerms {
			cName := getCanonicalName(it)
			if unhandled[cName] {
				missing = append(missing, cName)
				delete(unhandled, cName)
			}
		}

		if !nilHandled {
			missing = append(missing, "nil")
		}

		if len(missing) > 0 {
			errs = append(
				errs,
				augtError{
					err: fmt.Errorf("non-exhaustive type switch. missing cases: %v", strings.Join(missing, ", ")),
					pos: stmt.Pos(),
				})
		} else if defaultClause != nil {
			errs = append(
				errs,
				augtError{
					err: fmt.Errorf("redundant default case in exhaustive type switch"),
					pos: defaultClause.Pos(),
				})
		}
	}

	return errs
}

func checkTypeConversion(tinfo *types.Info, col collected, reg *Registry) []augtError {
	var errs []augtError
	for _, call := range col.callExprs {
		funTV, ok := tinfo.Types[call.Fun]
		if !ok || !funTV.IsType() || len(call.Args) != 1 {
			continue
		}
		sumt := reg.Lookup(funTV.Type)
		if sumt == nil {
			continue
		}
		if !sumt.IsPermittedValue(tinfo, reg, call.Args[0]) {
			errs = append(errs, augtError{
				err: errors.New("invalid type conversion: type is not permitted"),
				pos: call.Pos(),
			})
		}
	}
	return errs
}

func checkFuncBodyReturns(tinfo *types.Info, reg *Registry, sig *types.Signature, body *ast.BlockStmt) []augtError {
	if sig == nil || body == nil || sig.Results().Len() == 0 {
		return nil
	}
	var errs []augtError
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}

		if len(ret.Results) == 0 {
			return true
		}

		if len(ret.Results) == sig.Results().Len() {
			for i, resExpr := range ret.Results {
				resType := sig.Results().At(i).Type()
				if sumt := reg.Lookup(resType); sumt != nil {
					if !sumt.IsPermittedValue(tinfo, reg, resExpr) {
						errs = append(errs, augtError{
							err: errors.New("invalid return: type is not permitted"),
							pos: resExpr.Pos(),
						})
					}
				}
			}
			return true
		}

		if len(ret.Results) == 1 && sig.Results().Len() > 1 {
			rhsTV, ok := tinfo.Types[ret.Results[0]]
			if ok && rhsTV.Type != nil {
				if tuple, ok := rhsTV.Type.(*types.Tuple); ok {
					for i := 0; i < sig.Results().Len() && i < tuple.Len(); i++ {
						if sumt := reg.Lookup(sig.Results().At(i).Type()); sumt != nil {
							if !sumt.IsPermittedType(reg, tuple.At(i).Type()) {
								errs = append(errs, augtError{
									err: errors.New("invalid return: type is not permitted"),
									pos: ret.Results[0].Pos(),
								})
							}
						}
					}
				}
			}
			return true
		}

		return true
	})
	return errs
}

func checkReturn(tinfo *types.Info, col collected, reg *Registry) []augtError {
	var errs []augtError
	for _, fd := range col.funcDecls {
		if fd.Body == nil {
			continue
		}
		obj := tinfo.ObjectOf(fd.Name)
		if obj != nil {
			if sig, ok := obj.Type().(*types.Signature); ok {
				errs = append(errs, checkFuncBodyReturns(tinfo, reg, sig, fd.Body)...)
			}
		}
	}
	for _, fl := range col.funcLits {
		if fl.Body == nil {
			continue
		}
		tv, ok := tinfo.Types[fl]
		if ok && tv.Type != nil {
			if sig, ok := tv.Type.(*types.Signature); ok {
				errs = append(errs, checkFuncBodyReturns(tinfo, reg, sig, fl.Body)...)
			}
		}
	}
	return errs
}

func checkCallArgs(tinfo *types.Info, col collected, reg *Registry) []augtError {
	var errs []augtError
	for _, call := range col.callExprs {
		funTV, ok := tinfo.Types[call.Fun]
		if !ok || funTV.IsType() || funTV.Type == nil {
			continue
		}
		sig, ok := funTV.Type.Underlying().(*types.Signature)
		if !ok || sig.Params().Len() == 0 {
			continue
		}

		paramTypeAt := func(i int) types.Type {
			numParams := sig.Params().Len()
			if sig.Variadic() && i >= numParams-1 {
				if slice, ok := sig.Params().At(numParams - 1).Type().(*types.Slice); ok {
					if call.Ellipsis.IsValid() && i == len(call.Args)-1 {
						return slice
					}
					return slice.Elem()
				}
			}
			if i < numParams {
				return sig.Params().At(i).Type()
			}
			return nil
		}

		if len(call.Args) == 1 && sig.Params().Len() > 1 {
			if argTV, ok := tinfo.Types[call.Args[0]]; ok && argTV.Type != nil {
				if tuple, ok := argTV.Type.(*types.Tuple); ok {
					for i := 0; i < tuple.Len(); i++ {
						pType := paramTypeAt(i)
						if sumt := reg.Lookup(pType); sumt != nil {
							if !sumt.IsPermittedType(reg, tuple.At(i).Type()) {
								errs = append(errs, augtError{
									err: errors.New("invalid argument: type is not permitted"),
									pos: call.Args[0].Pos(),
								})
							}
						}
					}
					continue
				}
			}
		}

		for i, argExpr := range call.Args {
			pType := paramTypeAt(i)
			if sumt := reg.Lookup(pType); sumt != nil {
				if !sumt.IsPermittedValue(tinfo, reg, argExpr) {
					errs = append(errs, augtError{
						err: errors.New("invalid argument: type is not permitted"),
						pos: argExpr.Pos(),
					})
				}
			}
		}
	}
	return errs
}

func checkCompositeLit(tinfo *types.Info, col collected, reg *Registry) []augtError {
	var errs []augtError
	for _, lit := range col.compositeLits {
		litType := tinfo.TypeOf(lit)
		if litType == nil {
			continue
		}
		if ptr, ok := litType.Underlying().(*types.Pointer); ok {
			litType = ptr.Elem()
		}
		switch u := litType.Underlying().(type) {
		case *types.Slice:
			elemType := u.Elem()
			if sumt := reg.Lookup(elemType); sumt != nil {
				for _, elt := range lit.Elts {
					valExpr := elt
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						valExpr = kv.Value
					}
					if !sumt.IsPermittedValue(tinfo, reg, valExpr) {
						errs = append(errs, augtError{
							err: errors.New("invalid composite literal element: type is not permitted"),
							pos: valExpr.Pos(),
						})
					}
				}
			}
		case *types.Array:
			elemType := u.Elem()
			if sumt := reg.Lookup(elemType); sumt != nil {
				for _, elt := range lit.Elts {
					valExpr := elt
					if kv, ok := elt.(*ast.KeyValueExpr); ok {
						valExpr = kv.Value
					}
					if !sumt.IsPermittedValue(tinfo, reg, valExpr) {
						errs = append(errs, augtError{
							err: errors.New("invalid composite literal element: type is not permitted"),
							pos: valExpr.Pos(),
						})
					}
				}
			}
		case *types.Map:
			keySum := reg.Lookup(u.Key())
			elemSum := reg.Lookup(u.Elem())
			if keySum == nil && elemSum == nil {
				continue
			}
			for _, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if keySum != nil && !keySum.IsPermittedValue(tinfo, reg, kv.Key) {
						errs = append(errs, augtError{
							err: errors.New("invalid composite literal key: type is not permitted"),
							pos: kv.Key.Pos(),
						})
					}
					if elemSum != nil && !elemSum.IsPermittedValue(tinfo, reg, kv.Value) {
						errs = append(errs, augtError{
							err: errors.New("invalid composite literal element: type is not permitted"),
							pos: kv.Value.Pos(),
						})
					}
				}
			}
		case *types.Struct:
			for i, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					var fieldType types.Type
					if kvKeyIdent, ok := kv.Key.(*ast.Ident); ok {
						if obj := tinfo.ObjectOf(kvKeyIdent); obj != nil {
							fieldType = obj.Type()
						}
					}
					if fieldType == nil {
						fieldType = tinfo.TypeOf(kv.Key)
					}
					if sumt := reg.Lookup(fieldType); sumt != nil {
						if !sumt.IsPermittedValue(tinfo, reg, kv.Value) {
							errs = append(errs, augtError{
								err: errors.New("invalid composite literal field: type is not permitted"),
								pos: kv.Value.Pos(),
							})
						}
					}
				} else {
					if i < u.NumFields() {
						fieldType := u.Field(i).Type()
						if sumt := reg.Lookup(fieldType); sumt != nil {
							if !sumt.IsPermittedValue(tinfo, reg, elt) {
								errs = append(errs, augtError{
									err: errors.New("invalid composite literal field: type is not permitted"),
									pos: elt.Pos(),
								})
							}
						}
					}
				}
			}
		}
	}
	return errs
}

func checkSend(tinfo *types.Info, col collected, reg *Registry) []augtError {
	var errs []augtError
	for _, send := range col.sendStmts {
		chType := tinfo.TypeOf(send.Chan)
		if chType == nil {
			continue
		}
		if ch, ok := chType.Underlying().(*types.Chan); ok {
			if sumt := reg.Lookup(ch.Elem()); sumt != nil {
				if !sumt.IsPermittedValue(tinfo, reg, send.Value) {
					errs = append(errs, augtError{
						err: errors.New("invalid channel send: type is not permitted"),
						pos: send.Value.Pos(),
					})
				}
			}
		}
	}
	return errs
}

func checkTypeAssert(tinfo *types.Info, col collected, reg *Registry) []augtError {
	var errs []augtError
	for _, ta := range col.typeAssertExprs {
		srcType := tinfo.TypeOf(ta.X)
		if srcType == nil {
			continue
		}
		assertedType := tinfo.TypeOf(ta.Type)
		if assertedType == nil {
			continue
		}

		if targetSum := reg.Lookup(assertedType); targetSum != nil {
			if !targetSum.IsPermittedValue(tinfo, reg, ta.X) {
				errs = append(errs, augtError{
					err: errors.New("invalid type assertion to sum type: type is not permitted"),
					pos: ta.Pos(),
				})
			}
			continue
		}

		if srcSum := reg.Lookup(srcType); srcSum != nil {
			if !srcSum.IsPermittedCaseType(reg, assertedType) {
				errs = append(errs, augtError{
					err: errors.New("invalid type assertion from sum type: type is not permitted"),
					pos: ta.Pos(),
				})
			}
		}
	}
	return errs
}

func check(tinfo *types.Info, col collected, reg *Registry) []augtError {
	var errs []augtError

	asserr := checkAssign(tinfo, col, reg)
	errs = append(errs, asserr...)

	tserr := checkTypeSwitch(tinfo, col, reg)
	errs = append(errs, tserr...)

	tcerr := checkTypeConversion(tinfo, col, reg)
	errs = append(errs, tcerr...)

	reterr := checkReturn(tinfo, col, reg)
	errs = append(errs, reterr...)

	callerr := checkCallArgs(tinfo, col, reg)
	errs = append(errs, callerr...)

	literr := checkCompositeLit(tinfo, col, reg)
	errs = append(errs, literr...)

	senderr := checkSend(tinfo, col, reg)
	errs = append(errs, senderr...)

	taerr := checkTypeAssert(tinfo, col, reg)
	errs = append(errs, taerr...)

	return errs
}

func collectInfo(pass *analysis.Pass) (collected, *Registry) {
	col := collected{}
	for _, astf := range pass.Files {
		cmtmap := ast.NewCommentMap(pass.Fset, astf, astf.Comments)
		ispt := inspector{
			src: source{cmtmap: cmtmap},
			col: collected{},
		}
		ast.Inspect(astf, ispt.inspect)
		col.adtDecls = append(col.adtDecls, ispt.col.adtDecls...)
		col.declAssigns = append(col.declAssigns, ispt.col.declAssigns...)
		col.assignStmts = append(col.assignStmts, ispt.col.assignStmts...)
		col.typeSwitchStmts = append(col.typeSwitchStmts, ispt.col.typeSwitchStmts...)
		col.callExprs = append(col.callExprs, ispt.col.callExprs...)
		col.funcDecls = append(col.funcDecls, ispt.col.funcDecls...)
		col.funcLits = append(col.funcLits, ispt.col.funcLits...)
		col.compositeLits = append(col.compositeLits, ispt.col.compositeLits...)
		col.sendStmts = append(col.sendStmts, ispt.col.sendStmts...)
		col.typeAssertExprs = append(col.typeAssertExprs, ispt.col.typeAssertExprs...)
		col.errs = append(col.errs, ispt.col.errs...)
	}

	reg, regErrs := BuildRegistry(pass, col.adtDecls)
	col.errs = append(col.errs, regErrs...)

	return col, reg
}
