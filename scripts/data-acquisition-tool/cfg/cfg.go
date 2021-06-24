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
}

func lookupPkg(lut *CFGLookup, pkgs []base.CFGPkg, pkgMap map[*types.Package]int, pkg *types.Package) ([]base.CFGPkg, int) {
	var pkgId int = -1
	if pkg != nil {
		var ok bool
		pkgId, ok = pkgMap[pkg]
		if !ok {
			pkgs = append(pkgs, base.CFGPkg{
				Name: pkg.Name(),
				Path: pkg.Path(),
			})
			pkgId = len(pkgs) - 1
			pkgMap[pkg] = pkgId
		}
	}
	return pkgs, pkgId
}

func lookupType(lut *CFGLookup, ctypes []base.CFGType, typeMap map[string]int, typ types.Type) ([]base.CFGType, int) {
	var typeId int = -1
	if typ != nil {
		ts := typ.String()
		var ok bool
		typeId, ok = typeMap[ts]
		if !ok {
			var underId, elemId int
			typeId = len(ctypes)
			typeMap[ts] = typeId
			ctyp := base.CFGType{}
			ctypes = append(ctypes, ctyp)
			switch t := typ.(type) {
			case *types.Array:
				ctyp["type"] = "Array"
				ctypes, elemId = lookupType(lut, ctypes, typeMap, t.Elem())
				ctyp["elem"] = elemId
			case *types.Basic:
				ctyp["type"] = "Basic"
				if t.Kind() == types.UnsafePointer {
					ctyp["local-name"] = "Pointer"
					ctyp["package"] = lut.Pkg(types.Unsafe)
				}
			case *types.Chan:
				ctyp["type"] = "Chan"
				ctypes, elemId = lookupType(lut, ctypes, typeMap, t.Elem())
				ctyp["elem"] = elemId
				ctyp["dir"] = goblin.DumpChanDir(goblin.ConvertChanDir(t.Dir()))
			case *types.Interface:
				ctyp["type"] = "Interface"
				methods := make([]map[string]interface{}, t.NumMethods())
				for i := 0; i < t.NumMethods(); i++ {
					m := t.Method(i)
					var mtyp int
					ctypes, mtyp = lookupType(lut, ctypes, typeMap, m.Type())
					methods[i] = map[string]interface{}{
						"name": m.Name(),
						"type": mtyp,
					}
				}
				ctyp["type"] = methods
			case *types.Map:
				ctyp["type"] = "Map"
				ctypes, elemId = lookupType(lut, ctypes, typeMap, t.Elem())
				ctyp["elem"] = elemId
				ctypes, elemId = lookupType(lut, ctypes, typeMap, t.Key())
				ctyp["key"] = elemId
			case *types.Named:
				ctyp["type"] = "Named"
				if tname := t.Obj(); tname != nil {
					ctyp["package"] = lut.Pkg(tname.Pkg())
					ctyp["local-name"] = tname.Name()
				}
			case *types.Pointer:
				ctyp["type"] = "Pointer"
				ctypes, elemId = lookupType(lut, ctypes, typeMap, t.Elem())
				ctyp["elem"] = elemId
			case *types.Signature:
				ctyp["type"] = "Signature"
				var params, res int
				ctypes, params = lookupType(lut, ctypes, typeMap, t.Params())
				ctypes, res = lookupType(lut, ctypes, typeMap, t.Results())
				ctyp["params"] = params
				ctyp["recv"] = lut.Var(t.Recv())
				ctyp["results"] = res
				ctyp["variadic"] = t.Variadic()
			case *types.Slice:
				ctyp["type"] = "Slice"
				ctypes, elemId = lookupType(lut, ctypes, typeMap, t.Elem())
				ctyp["elem"] = elemId
			case *types.Struct:
				ctyp["type"] = "Struct"
				fields := make([]map[string]interface{}, t.NumFields())
				for i := 0; i < t.NumFields(); i++ {
					f := t.Field(i)
					var ftyp int
					ctypes, ftyp = lookupType(lut, ctypes, typeMap, f.Type())
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
					ctypes, ftyp = lookupType(lut, ctypes, typeMap, f.Type())
					fields[i] = map[string]interface{}{
						"name": f.Name(),
						"type": ftyp,
					}
				}
				ctyp["fields"] = fields
			default:
			}
			ctyp["name"] = typ.String()
			ctypes, underId = lookupType(lut, ctypes, typeMap, typ.Underlying())
			ctyp["underlying"] = underId
		}
	}
	return ctypes, typeId
}

func lookupVar(lut *CFGLookup, vars []base.CFGVar, varMap map[*types.Var]int, v *types.Var) ([]base.CFGVar, int) {
	var varId int = -1
	if v != nil {
		var ok bool
		varId, ok = varMap[v]
		if !ok {
			vars = append(vars, base.CFGVar{
				Name: v.Name(),
				Pkg: lut.Pkg(v.Pkg()),
				Type: lut.Type(v.Type()),
				Exported: v.Exported(),
			})
			varId = len(vars) - 1
			varMap[v] = varId
		}
	}
	return vars, varId
}

func lookupFunc(lut *CFGLookup, funcs []base.CFGFunc, funcMap map[*types.Func]int, f *types.Func) ([]base.CFGFunc, int) {
	var funcId int = -1
	if f != nil {
		var ok bool
		funcId, ok = funcMap[f]
		if !ok {
			funcs = append(funcs, base.CFGFunc{
				Name: f.Name(),
				Pkg: lut.Pkg(f.Pkg()),
				Type: lut.Type(f.Type()),
				Exported: f.Exported(),
			})
			funcId = len(funcs) - 1
			funcMap[f] = funcId
		}
	}
	return funcs, funcId
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
	cvars := make([]base.CFGVar, 0)
	ctypes := make([]base.CFGType, 0)
	cpkgs := make([]base.CFGPkg, 0)
	cfuncs := make([]base.CFGFunc, 0)
	varMap := make(map[*types.Var]int)
	typeMap := make(map[string]int, 0)
	pkgMap := make(map[*types.Package]int, 0)
	funcMap := make(map[*types.Func]int, 0)
	lookup := &CFGLookup{}

	vLookup := func(v *types.Var) int {
		cv, typeId := lookupVar(lookup, cvars, varMap, v)
		cvars = cv
		return typeId
	}
	pLookup := func(pkg *types.Package) int {
		cp, pkgId := lookupPkg(lookup, cpkgs, pkgMap, pkg)
		cpkgs = cp
		return pkgId
	}
	tLookup := func(typ types.Type) int {
		ct, typeId := lookupType(lookup, ctypes, typeMap, typ)
		ctypes = ct
		return typeId
	}
	fLookup := func(f *types.Func) int {
		cf, funcId := lookupFunc(lookup, cfuncs, funcMap, f)
		cfuncs = cf
		return funcId
	}
	lookup.Var = vLookup
	lookup.Pkg = pLookup
	lookup.Type = tLookup
	lookup.Func = fLookup

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

	switch cdecl := decl.(type) {
	case *ast.FuncDecl:
		ft := cdecl.Type
		params := ft.Params.List
		results := ft.Results.List
		receivers := cdecl.Recv

		if receivers != nil {
			for i, recv := range receivers.List {
				rName := fmt.Sprintf("[rec%d]", i)
				for _, v := range fieldToVars(rName, recv, pkgInfo) {
					recvIds = append(recvIds, vLookup(v))
				}
			}
		}

		for i, param := range params {
			pName := fmt.Sprintf("[param%d]", i)
			for _, v := range fieldToVars(pName, param, pkgInfo) {
				paramIds = append(paramIds, vLookup(v))
			}
		}
		for i, res := range results {
			rName := fmt.Sprintf("[res%d]", i)
			for _, v := range fieldToVars(rName, res, pkgInfo) {
				resultIds = append(resultIds, vLookup(v))
			}
		}

		if cdecl.Body != nil {
			cfgType = "function"
			c = cfg.FromFunc(cdecl)
		} else {
			cfgType = "external"
			cfgBlocks = []base.CFGBlock{{
				Code: "entry",
				Entry: true,
				Exit: false,
				LineNumber: -1,
				InVars: paramIds,
				OutVars: paramIds,
				UseVars: []int{},
				DeclVars: []int{},
				AssignVars: []int{},
				UpdateVars: []int{},
				Succs: []int{1},
			}, {
				Code: "exit",
				Entry: false,
				Exit: true,
				LineNumber: -1,
				UseVars: []int{},
				DeclVars: []int{},
				AssignVars: []int{},
				UpdateVars: []int{},
				Succs: []int{},
			}}
		}
	case *ast.GenDecl:
		cfgType = "generic"
		declStmt := ast.DeclStmt{Decl: cdecl}
		c = cfg.FromStmts([]ast.Stmt{&declStmt})
	default:
		cfgType = "unknown"
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
			var line int
			var blockAst interface{}
			entry := false
			exit := false
			switch block {
			case c.Entry:
				blockStr = "entry"
				entry = true
				line = -1
			case c.Exit:
				blockStr = "exit"
				exit = true
				line = -1
			default:
				blockStr = nodeToString(block, fset)
				blockPos := fset.File(block.Pos()).Position(block.Pos())
				line = blockPos.Line
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
				LineNumber: line,
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
			c.Sort(succs)
			for j, succ := range succs {
				cfgSuccs[j] = blockToId[succ]
			}
			cfgBlocks[i].Succs = cfgSuccs
		}
	}

	var sb strings.Builder
	format.Node(&sb, fset, decl)
	cfgCode := sb.String()

	b, err := json.Marshal(base.CFG{
		Type: cfgType,
		Code: cfgCode,
		Blocks: cfgBlocks,
		Variables: cvars,
		Types: ctypes,
		Pkgs: cpkgs,
		Funcs: cfuncs,
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

const MAX_DIST = 3
const CACHE_MAX_DIST = 20

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

	for _, parsedPkg := range parsedPkgs {
		for i, file := range parsedPkg.Syntax {
			name := parsedPkg.CompiledGoFiles[i]
			if mName, ok := cgoFileMap[name]; ok {
				name = mName
			}
			if strings.HasSuffix(name, query.FileName) {
				selectedPkg = parsedPkg
				selectedFile = file
				selectedDecl = selectDecl(selectedPkg, selectedFile, query.LineNumber, nil, -1)

				// Fuzzy matching to handle small line-number discrepancies:
				if selectedDecl == nil {
					b, err := ioutil.ReadFile(name)
					if err == nil {
						code := string(b)
						snippetMatches := snippetRegex.FindAllStringIndex(code, -1)
						if snippetMatches != nil {
							decl := selectDecl(selectedPkg, selectedFile, query.LineNumber, snippetMatches, MAX_DIST)
							if decl != nil {
								codeSlice := declCode(code, decl, parsedPkg.Fset)
								if strings.Contains(codeSlice, query.Snippet) {
									selectedDecl = decl
									break
								}
							}
						}
					}
				} else {
					break
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
							decl := selectDecl(selectedPkg, selectedFile, query.LineNumber, snippetMatches, CACHE_MAX_DIST)
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
		panic("Queried function declaration not found.")
	}

	printCFG(os.Stdout, selectedDecl, selectedPkg)

	fmt.Println()
}
