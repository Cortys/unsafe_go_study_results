package cfg

import (
	"os"
	"io"
	"io/ioutil"
	"fmt"
	"sync"
	"regexp"
	"go/ast"
	"go/types"
	"strings"
	"go/format"
	"go/token"
	"go/parser"
	"encoding/json"
	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/packages"
	"github.com/godoctor/godoctor/analysis/cfg"
	"github.com/godoctor/godoctor/analysis/dataflow"
	"github.com/Cortys/goblin"
	"github.com/stg-tud/unsafe_go_study_results/scripts/data-acquisition-tool/base"
)

type CFGLookup struct {
	Pkg func(*types.Package) int
	Type func(types.Type) int
	Var func(*types.Var) int
	Func func(*types.Func) int
	pkgList []base.CFGPkg
	typeList []base.CFGType
	varList []base.CFGVar
	funcList []base.CFGFunc
}

func lookupPkg(lut *CFGLookup, pkgMap map[*types.Package]int, pkg *types.Package) int {
	var pkgId int = -1
	if pkg != nil {
		var ok bool
		pkgId, ok = pkgMap[pkg]
		if !ok {
			lut.pkgList = append(lut.pkgList, base.CFGPkg{
				Name: pkg.Name(),
				Path: pkg.Path(),
			})
			pkgId = len(lut.pkgList) - 1
			pkgMap[pkg] = pkgId
		}
	}
	return pkgId
}

func lookupType(lut *CFGLookup, typeMap map[string]int, typ types.Type) int {
	var typeId int = -1
	if typ != nil {
		ts := typ.String()
		var ok bool
		typeId, ok = typeMap[ts]
		if !ok {
			var underId, elemId int
			typeId = len(lut.typeList)
			typeMap[ts] = typeId
			ctyp := base.CFGType{}
			lut.typeList = append(lut.typeList, ctyp)
			switch t := typ.(type) {
			case *types.Array:
				ctyp["type"] = "Array"
				elemId = lut.Type(t.Elem())
				ctyp["elem"] = elemId
			case *types.Basic:
				ctyp["type"] = "Basic"
				if t.Kind() == types.UnsafePointer {
					ctyp["local-name"] = "Pointer"
					ctyp["package"] = lut.Pkg(types.Unsafe)
				}
			case *types.Chan:
				ctyp["type"] = "Chan"
				elemId = lut.Type(t.Elem())
				ctyp["elem"] = elemId
				ctyp["dir"] = goblin.DumpChanDir(goblin.ConvertChanDir(t.Dir()))
			case *types.Interface:
				ctyp["type"] = "Interface"
				methods := make([]map[string]interface{}, t.NumMethods())
				for i := 0; i < t.NumMethods(); i++ {
					m := t.Method(i)
					var mtyp int
					mtyp = lut.Type(m.Type())
					methods[i] = map[string]interface{}{
						"name": m.Name(),
						"type": mtyp,
					}
				}
				ctyp["methods"] = methods
			case *types.Map:
				ctyp["type"] = "Map"
				elemId = lut.Type(t.Elem())
				ctyp["elem"] = elemId
				elemId = lut.Type(t.Key())
				ctyp["key"] = elemId
			case *types.Named:
				ctyp["type"] = "Named"
				if tname := t.Obj(); tname != nil {
					ctyp["package"] = lut.Pkg(tname.Pkg())
					ctyp["local-name"] = tname.Name()
				}
			case *types.Pointer:
				ctyp["type"] = "Pointer"
				elemId = lut.Type(t.Elem())
				ctyp["elem"] = elemId
			case *types.Signature:
				ctyp["type"] = "Signature"
				var params, res int
				params = lut.Type(t.Params())
				res = lut.Type(t.Results())
				ctyp["params"] = params
				ctyp["recv"] = lut.Var(t.Recv())
				ctyp["results"] = res
				ctyp["variadic"] = t.Variadic()
			case *types.Slice:
				ctyp["type"] = "Slice"
				elemId = lut.Type(t.Elem())
				ctyp["elem"] = elemId
			case *types.Struct:
				ctyp["type"] = "Struct"
				fields := make([]map[string]interface{}, t.NumFields())
				for i := 0; i < t.NumFields(); i++ {
					f := t.Field(i)
					var ftyp int
					ftyp = lut.Type(f.Type())
					fields[i] = map[string]interface{}{
						"name": f.Name(),
						"type": ftyp,
					}
				}
				ctyp["fields"] = fields
			case *types.Tuple:
				ctyp["type"] = "Tuple"
				fields := make([]map[string]interface{}, t.Len())
				for i := 0; i < t.Len(); i++ {
					f := t.At(i)
					var ftyp int
					ftyp = lut.Type(f.Type())
					fields[i] = map[string]interface{}{
						"name": f.Name(),
						"type": ftyp,
					}
				}
				ctyp["fields"] = fields
			default:
			}
			ctyp["name"] = typ.String()
			underId = lut.Type(typ.Underlying())
			ctyp["underlying"] = underId
		}
	}
	return typeId
}

func lookupVar(lut *CFGLookup, varMap map[*types.Var]int, v *types.Var) int {
	var varId int = -1
	if v != nil {
		var ok bool
		varId, ok = varMap[v]
		if !ok {
			lut.varList = append(lut.varList, base.CFGVar{
				Name: v.Name(),
				Pkg: lut.Pkg(v.Pkg()),
				Type: lut.Type(v.Type()),
				Exported: v.Exported(),
				Embedded: v.Embedded(),
				Field: v.IsField(),
			})
			varId = len(lut.varList) - 1
			varMap[v] = varId
		}
	}
	return varId
}

func lookupFunc(lut *CFGLookup, funcMap map[*types.Func]int, f *types.Func) int {
	var funcId int = -1
	if f != nil {
		var ok bool
		funcId, ok = funcMap[f]
		if !ok {
			lut.funcList = append(lut.funcList, base.CFGFunc{
				Name: f.Name(),
				Pkg: lut.Pkg(f.Pkg()),
				Type: lut.Type(f.Type()),
				Exported: f.Exported(),
			})
			funcId = len(lut.funcList) - 1
			funcMap[f] = funcId
		}
	}
	return funcId
}

func makeBlockAst(block ast.Stmt, fset *token.FileSet, c *cfg.CFG) interface{} {
	switch b := block.(type) {
	case *ast.LabeledStmt:
		return goblin.DumpStmt(b.Stmt, fset)
	case *ast.IfStmt:
		blockAst := goblin.DumpStmt(b, fset).(map[string]interface{})
		delete(blockAst, "init")
		delete(blockAst, "body")
		delete(blockAst, "else")
		return blockAst
	case *ast.ForStmt, *ast.RangeStmt,
		*ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt:
		blockAst := goblin.DumpStmt(b, fset).(map[string]interface{})
		delete(blockAst, "init")
		delete(blockAst, "assign")
		delete(blockAst, "post")
		delete(blockAst, "body")
		return blockAst
	case *ast.CaseClause:
		switchBlock := c.Preds(c.Preds(block)[0])[0]
		var blockAst map[string]interface{}
		switch switchBlock.(type) {
		case *ast.TypeSwitchStmt:
			blockAst = goblin.DumpTypeSwitchBodyStmt(b, fset).(map[string]interface{})
		default:
			blockAst = goblin.DumpStmt(b, fset).(map[string]interface{})
		}
		delete(blockAst, "body")
		return blockAst
	default:
		return goblin.DumpStmt(b, fset)
	}
}

func nodeToString(stmt ast.Node, fset *token.FileSet) string {
	var sb strings.Builder
	format.Node(&sb, fset, stmt)
	return sb.String()
}

func fieldToVars(defaultName string, field *ast.Field, info *loader.PackageInfo) []*types.Var {
	t := info.Info.TypeOf(field.Type)
	if field.Names == nil {
		return []*types.Var{types.NewVar(field.Pos(), info.Pkg, defaultName, t)}
	}
	vars := make([]*types.Var, 0)
	for _, id := range field.Names {
		if v, ok := info.ObjectOf(id).(*types.Var); ok {
			vars = append(vars, v)
		}
	}
	return vars
}

func printCFG(f io.Writer, decl ast.Decl, pkg *packages.Package) {
	pkgInfo := &loader.PackageInfo{
		Pkg: pkg.Types,
		Importable: true,
		TransitivelyErrorFree: true,
		Files: pkg.Syntax,
		Info: *pkg.TypesInfo,
	}
	fset := pkg.Fset
	varMap := make(map[*types.Var]int)
	typeMap := make(map[string]int, 0)
	pkgMap := make(map[*types.Package]int, 0)
	funcMap := make(map[*types.Func]int, 0)
	lookup := &CFGLookup{}

	vLookup := func(v *types.Var) int {
		return lookupVar(lookup, varMap, v)
	}
	pLookup := func(pkg *types.Package) int {
		return lookupPkg(lookup, pkgMap, pkg)
	}
	tLookup := func(typ types.Type) int {
		return lookupType(lookup, typeMap, typ)
	}
	fLookup := func(f *types.Func) int {
		return lookupFunc(lookup, funcMap, f)
	}
	lookup.Var = vLookup
	lookup.Pkg = pLookup
	lookup.Type = tLookup
	lookup.Func = fLookup
	lookup.varList = make([]base.CFGVar, 0)
	lookup.pkgList = make([]base.CFGPkg, 0)
	lookup.typeList = make([]base.CFGType, 0)
	lookup.funcList = make([]base.CFGFunc, 0)

	goblin.SetTypesInfo(pkg.TypesInfo)
	goblin.SetVarDumper(func(v *types.Var) interface{} {return vLookup(v)})
	goblin.SetPkgDumper(func(pkg *types.Package) interface{} {return pLookup(pkg)})
	goblin.SetTypeDumper(func(typ types.Type) interface{} {return tLookup(typ)})
	goblin.SetFuncDumper(func(f *types.Func) interface{} {return fLookup(f)})

	var c *cfg.CFG
	var cfgType string
	var cfgBlocks []base.CFGBlock
	recvIds := make([]int, 0)
	paramIds := make([]int, 0)
	resultIds := make([]int, 0)
	cfgNames := make([]string, 0)
	cfgDefs := make([]int, 0)
	cfgPkg := -1

	switch cdecl := decl.(type) {
	case *ast.FuncDecl:
		id := cdecl.Name
		fId := -1
		cfgNames = append(cfgNames, id.String())
		if obj := pkg.TypesInfo.ObjectOf(id); obj != nil {
			cfgPkg = pLookup(obj.Pkg())
			if f, ok := obj.(*types.Func); ok {
				fId = fLookup(f)
			}
		}
		cfgDefs = append(cfgDefs, fId)

		ft := cdecl.Type
		params := ft.Params
		results := ft.Results
		receivers := cdecl.Recv

		if receivers != nil && receivers.List != nil {
			for i, recv := range receivers.List {
				rName := fmt.Sprintf("[rec%d]", i)
				for _, v := range fieldToVars(rName, recv, pkgInfo) {
					recvIds = append(recvIds, vLookup(v))
				}
			}
		}
		if params != nil && params.List != nil {
			for i, param := range params.List {
				pName := fmt.Sprintf("[param%d]", i)
				for _, v := range fieldToVars(pName, param, pkgInfo) {
					paramIds = append(paramIds, vLookup(v))
				}
			}
		}
		if results != nil && results.List != nil {
			for i, res := range results.List {
				rName := fmt.Sprintf("[res%d]", i)
				for _, v := range fieldToVars(rName, res, pkgInfo) {
					resultIds = append(resultIds, vLookup(v))
				}
			}
		}

		if cdecl.Body != nil {
			cfgType = "function"
			c = cfg.FromFunc(cdecl)
		} else {
			cfgType = "external"
			inIds := make([]int, len(paramIds) + len(recvIds))
			inIds = append(inIds, recvIds...)
			inIds = append(inIds, paramIds...)
			cfgBlocks = []base.CFGBlock{{
				Code: "[entry]",
				Entry: true,
				Exit: false,
				LineStart: -1,
				LineEnd: -1,
				InVars: inIds,
				OutVars: inIds,
				UseVars: []int{},
				DeclVars: []int{},
				AssignVars: []int{},
				UpdateVars: []int{},
				Succs: []int{1},
			}, {
				Code: "[exit]",
				Entry: false,
				Exit: true,
				LineStart: -1,
				LineEnd: -1,
				UseVars: []int{},
				DeclVars: []int{},
				AssignVars: []int{},
				UpdateVars: []int{},
				Succs: []int{},
			}}
		}
	case *ast.GenDecl:
		switch cdecl.Tok {
		case token.IMPORT:
			panic("Cannot create CFG for import declaration.")
		case token.TYPE:
			cfgType = "type"
			for _, s := range cdecl.Specs {
				id := s.(*ast.TypeSpec).Name
				tId := -1
				cfgNames = append(cfgNames, id.Name)
				if obj := pkg.TypesInfo.ObjectOf(id); obj != nil {
					if cfgPkg == -1 {
						cfgPkg = pLookup(obj.Pkg())
					}
					if t, ok := obj.(*types.TypeName); ok {
						tId = tLookup(t.Type())
					}
				}
				cfgDefs = append(cfgDefs, tId)
			}
		case token.VAR, token.CONST:
			cfgType = "variable"
			for _, s := range cdecl.Specs {
				for _, id := range s.(*ast.ValueSpec).Names {
					vId := -1
					cfgNames = append(cfgNames, id.Name)
					if obj := pkg.TypesInfo.ObjectOf(id); obj != nil {
						if cfgPkg == -1 {
							cfgPkg = pLookup(obj.Pkg())
						}
						if v, ok := obj.(*types.Var); ok {
							vId = vLookup(v)
						}
					}
					cfgDefs = append(cfgDefs, vId)
				}
			}
		default:
			panic("Unknown GenDecl token.")
		}
		declStmt := ast.DeclStmt{Decl: cdecl}
		c = cfg.FromStmts([]ast.Stmt{&declStmt})
	default:
		panic("Unknown declaration type.")
	}

	if c != nil {
		inVarMap, outVarMap := dataflow.LiveVars(c, pkgInfo)

		blocks := c.Blocks()
		c.Sort(blocks)
		cfgBlocks = make([]base.CFGBlock, 0, len(blocks))
		blockToId := make(map[ast.Stmt]int, len(blocks))
		for i, block := range blocks {
			blockToId[block] = i
			var blockStr string
			var lineStart, lineEnd int
			var blockAst interface{}
			entry := false
			exit := false
			switch block {
			case c.Entry:
				blockStr = "[entry]"
				entry = true
				lineStart, lineEnd = -1, -1
			case c.Exit:
				blockStr = "[exit]"
				exit = true
				lineStart, lineEnd = -1, -1
			default:
				blockStr = nodeToString(block, fset)
				blockPos := fset.File(block.Pos()).Position(block.Pos())
				blockEnd := fset.File(block.End()).Position(block.End())
				lineStart = blockPos.Line
				lineEnd = blockEnd.Line
				blockAst = makeBlockAst(block, fset, c)
			}
			inVars := inVarMap[block]
			outVars := outVarMap[block]
			inIds := make([]int, 0, len(inVars))
			outIds := make([]int, 0, len(outVars))
			for v := range inVars {
				inIds = append(inIds, vLookup(v))
			}
			for v := range outVars {
				outIds = append(outIds, vLookup(v))
			}

			asVars, upVars, decVars, useVars := dataflow.ReferencedVars(
				[]ast.Stmt{block}, pkgInfo)
			asIds := make([]int, 0, len(asVars))
			upIds := make([]int, 0, len(upVars))
			decIds := make([]int, 0, len(decVars))
			useIds := make([]int, 0, len(useVars))
			for v := range asVars {
				asIds = append(asIds, vLookup(v))
			}
			for v := range upVars {
				upIds = append(upIds, vLookup(v))
			}
			for v := range decVars {
				decIds = append(decIds, vLookup(v))
			}
			for v := range useVars {
				useIds = append(useIds, vLookup(v))
			}

			cfgBlocks = append(cfgBlocks, base.CFGBlock{
				Code: blockStr,
				Ast: blockAst,
				LineStart: lineStart,
				LineEnd: lineEnd,
				Entry: entry,
				Exit: exit,
				InVars: inIds,
				OutVars: outIds,
				AssignVars: asIds,
				UpdateVars: upIds,
				DeclVars: decIds,
				UseVars: useIds,
			})
		}
		for i, block := range blocks {
			succs := c.Succs(block)
			cfgSuccs := make([]int, len(succs))
			for j, succ := range succs {
				cfgSuccs[j] = blockToId[succ]
			}
			cfgBlocks[i].Succs = cfgSuccs
		}
	}

	var sb strings.Builder
	format.Node(&sb, fset, decl)
	cfgCode := sb.String()
	declPos := fset.File(decl.Pos()).Position(decl.Pos())
	declEnd := fset.File(decl.End()).Position(decl.End())

	b, err := json.Marshal(base.CFG{
		Names: cfgNames,
		Type: cfgType,
		Pkg: cfgPkg,
		Defines: cfgDefs,
		Code: cfgCode,
		LineStart: declPos.Line,
		LineEnd: declEnd.Line,
		Blocks: cfgBlocks,
		Variables: lookup.varList,
		Types: lookup.typeList,
		Pkgs: lookup.pkgList,
		Funcs: lookup.funcList,
		Receivers: recvIds,
		Params: paramIds,
		Results: resultIds,
	})
	if err != nil {
		panic("Could not encode CFG as JSON.")
	}
	f.Write(b)
}

func selectDecl(selectedPkg *packages.Package, selectedFile *ast.File, lineNumber int, matches [][]int, maxDist int) ast.Decl {
	decls := selectedFile.Decls
	var selectedDecl ast.Decl
	var matchesOffset int = 0
	var selectedLineToDeclDist int = -1

	for _, decl := range decls {
		if _, ok := decl.(*ast.BadDecl); ok {
			continue
		}

		declStart := selectedPkg.Fset.File(decl.Pos()).Position(decl.Pos())
		declEnd := selectedPkg.Fset.File(decl.End()).Position(decl.End())

		// fmt.Println(lineNumber, matches, declStart, declEnd)

		if matches != nil {
			lineToDeclDist := base.IntervalDistance(lineNumber, declStart.Line, declEnd.Line)
			foundMatch := false
			for _, match := range matches[matchesOffset:] {
				if match[0] >= declStart.Offset && match[1] <= declEnd.Offset {
					matchesOffset++
					foundMatch = true
				} else if foundMatch {
					break
				}
			}
			if !foundMatch {
				continue
			} else if selectedLineToDeclDist != -1 && lineToDeclDist > selectedLineToDeclDist {
				break
			}
			selectedLineToDeclDist = lineToDeclDist
			selectedDecl = decl
		} else if lineNumber >= declStart.Line && lineNumber <= declEnd.Line {
			selectedDecl = decl
			break
		} else if lineNumber < declStart.Line {
			break
		}
	}

	if maxDist > -1 && selectedLineToDeclDist > maxDist {
		println("Dist:", selectedLineToDeclDist, maxDist, selectedFile.Name.String())
		panic("Maximum fuzzy declaration distance exceeded.")
	}

	return selectedDecl
}

func declCode(code string, decl ast.Decl, fset *token.FileSet) string {
	dStart := decl.Pos()
	dEnd := decl.End()
	startOffset := fset.File(dStart).Offset(dStart)
	endOffset := fset.File(dEnd).Offset(dEnd)
	return code[startOffset:endOffset]
}

func GetCFG(query *base.CFGQuery, projectsDir string)  {
	// fmt.Fprintf(
	// 	os.Stderr, "Loading CFG for %s in %s at %s:%d (%s)...\n",
	// 	query.PackageName, query.ProjectName, query.FileName, query.LineNumber, query.Snippet)
	checkoutDir := fmt.Sprintf("%s/%s", projectsDir, query.ProjectName)
	cgoFileMap := make(map[string]string)
	lineRegex := regexp.MustCompile("(?m)^// ?line ([^:]+):")
	snippetRegex := regexp.MustCompile(regexp.QuoteMeta(query.Snippet))
	lock := sync.Mutex{}

	query.FileName = strings.TrimPrefix(query.FileName, "/root/")
	isCacheQuery := strings.HasPrefix(query.FileName, ".cache")

	if !isCacheQuery {
		query.FileName = "/" + query.FileName
	}

	noCgoParse := func(fset *token.FileSet, filename string, src []byte) (*ast.File, error) {
		if !isCacheQuery {
			srcStr := string(src)
			if strings.HasPrefix(srcStr, "// Code generated by cmd/cgo") {
				matches := lineRegex.FindStringSubmatch(srcStr)
				if matches != nil {
					if origSrc, err := ioutil.ReadFile(matches[1]); err == nil {
						lock.Lock()
						cgoFileMap[filename] = matches[1]
						lock.Unlock()
						filename = matches[1]
						src = origSrc
					}
				}
			}
		}
		return parser.ParseFile(fset, filename, src, parser.ParseComments)
	}

	parsedPkgs, err := packages.Load(&packages.Config{
		Mode:       packages.NeedImports | packages.NeedDeps | packages.NeedSyntax |
					packages.NeedFiles | packages.NeedName | packages.NeedTypes |
					packages.NeedCompiledGoFiles | packages.NeedTypesInfo,
		ParseFile:  noCgoParse,
		Tests:      false,
		Dir:		checkoutDir,
	}, query.PackageName)
	if err != nil {
		println(err.Error())
		panic("error loading packages")
	}

	var selectedPkg *packages.Package
	var selectedFile *ast.File
	var selectedDecl ast.Decl
	var selectedName string

	for _, parsedPkg := range parsedPkgs {
		for i, file := range parsedPkg.Syntax {
			name := parsedPkg.CompiledGoFiles[i]
			if mName, ok := cgoFileMap[name]; ok {
				name = mName
			}
			if strings.HasSuffix(name, query.FileName) {
				selectedPkg = parsedPkg
				selectedFile = file
				selectedName = name
				if b, err := ioutil.ReadFile(name); err == nil {
					code := string(b)
					decl := selectDecl(selectedPkg, selectedFile, query.LineNumber, nil, -1)

					// Fuzzy matching to handle small line-number discrepancies:
					if decl == nil && query.MaxDist > 0 {
						snippetMatches := snippetRegex.FindAllStringIndex(code, -1)
						if snippetMatches != nil {
							decl = selectDecl(selectedPkg, selectedFile, query.LineNumber, snippetMatches, query.MaxDist)
						}
					}

					if decl != nil {
						codeSlice := declCode(code, decl, parsedPkg.Fset)
						if strings.Contains(codeSlice, query.Snippet) {
							selectedDecl = decl
							break
						}
					}
				}
			}
		}
	}

	if selectedFile == nil {
		if !isCacheQuery {
			panic("Could not find non-cache file.")
		}

		// Try finding cache file based on file contents:
		for _, parsedPkg := range parsedPkgs {
			for i, file := range parsedPkg.Syntax {
				name := parsedPkg.CompiledGoFiles[i]
				if strings.Contains(name, ".cache") {
					b, err := ioutil.ReadFile(name)
					if err == nil {
						code := string(b)
						snippetMatches := snippetRegex.FindAllStringIndex(code, -1)
						if snippetMatches != nil {
							selectedPkg = parsedPkg
							selectedFile = file
							selectedName = name
							decl := selectDecl(selectedPkg, selectedFile, query.LineNumber, snippetMatches, query.MaxCacheDist)
							if decl != nil {
								codeSlice := declCode(code, decl, parsedPkg.Fset)
								if strings.Contains(codeSlice, query.Snippet) {
									selectedDecl = decl
									break
								}
							}
						}
					}
				}
			}
		}
		if selectedFile == nil {
			panic("Could not find cache file.")
		}
	}

	if selectedDecl == nil {
		println("Selected file name:", selectedName)
		panic("Queried function declaration not found.")
	}

	printCFG(os.Stdout, selectedDecl, selectedPkg)

	fmt.Println()
}
