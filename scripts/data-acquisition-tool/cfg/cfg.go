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

func lookupPkg(pkgs []base.CFGPkg, pkgMap map[*types.Package]int, pkg *types.Package) ([]base.CFGPkg, int) {
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

func lookupType(types []base.CFGType, typeMap map[string]int, typ types.Type) ([]base.CFGType, int) {
	var typeId int = -1
	if typ != nil {
		ts := typ.String()
		var ok bool
		typeId, ok = typeMap[ts]
		if !ok {
			var underId int
			typeId = len(types)
			typeMap[ts] = typeId
			types = append(types, base.CFGType{
				Name: ts,
			})
			types, underId = lookupType(types, typeMap, typ.Underlying())
			types[typeId].Underlying = underId
		}
	}
	return types, typeId
}

func varsToJSON(vars []*types.Var) ([]base.CFGVar, []base.CFGType, []base.CFGPkg) {
	cvars := make([]base.CFGVar, len(vars))
	ctypes := make([]base.CFGType, 0)
	cpkgs := make([]base.CFGPkg, 0)
	typeMap := make(map[string]int, 0)
	pkgMap := make(map[*types.Package]int, 0)
	for i, v := range vars {
		var pkgId, typeId int
		cpkgs, pkgId = lookupPkg(cpkgs, pkgMap, v.Pkg())
		ctypes, typeId = lookupType(ctypes, typeMap, v.Type())

		cvars[i] = base.CFGVar{
			Name: v.Name(),
			Pkg: pkgId,
			Type: typeId,
		}
	}

	return cvars, ctypes, cpkgs
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

func printCFG(f io.Writer, decl ast.Decl, pkg *packages.Package) {
	pkgInfo := &loader.PackageInfo{
		Pkg: pkg.Types,
		Importable: true,
		TransitivelyErrorFree: true,
		Files: pkg.Syntax,
		Info: *pkg.TypesInfo,
	}
	varMap := dataflow.Vars(decl, pkgInfo)
	fset := pkg.Fset
	vars := make([]*types.Var, 0, len(varMap))
	varIds := make(map[*types.Var]int, len(varMap))
	for v := range varMap {
		varIds[v] = len(vars)
		vars = append(vars, v)
	}
	v, t, p := varsToJSON(vars)

	var c *cfg.CFG
	var cfgBlocks []base.CFGBlock

	switch cdecl := decl.(type) {
	case *ast.FuncDecl:
		if cdecl.Body != nil {
			c = cfg.FromFunc(cdecl)
		}
	case *ast.GenDecl:
		declStmt := ast.DeclStmt{Decl: cdecl}
		c = cfg.FromStmts([]ast.Stmt{&declStmt})
	default:
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
				inIds = append(inIds, varIds[v])
			}
			for v := range outVars {
				outIds = append(outIds, varIds[v])
			}

			asVars, upVars, decVars, useVars := dataflow.ReferencedVars(
				[]ast.Stmt{block}, pkgInfo)
			asIds := make([]int, 0, len(asVars))
			upIds := make([]int, 0, len(upVars))
			decIds := make([]int, 0, len(decVars))
			useIds := make([]int, 0, len(useVars))
			for v := range asVars {
				asIds = append(asIds, varIds[v])
			}
			for v := range upVars {
				upIds = append(upIds, varIds[v])
			}
			for v := range decVars {
				decIds = append(decIds, varIds[v])
			}
			for v := range useVars {
				useIds = append(useIds, varIds[v])
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
		Code: cfgCode,
		Blocks: cfgBlocks,
		Variables: v,
		Types: t,
		Pkgs: p,
	})
	if err != nil {
		panic("Could not encode CFG as JSON.")
	}
	f.Write(b)
}

func selectDecl(selectedPkg *packages.Package, selectedFile *ast.File, lineNumber int, matches [][]int) ast.Decl {
	var selectedDecl ast.Decl
	var matchesOffset int = 0
	var selectedLineToDeclDist int = -1

	for _, decl := range selectedFile.Decls {
		if _, ok := decl.(*ast.BadDecl); ok {
			continue
		}

		declStart := selectedPkg.Fset.File(decl.Pos()).Position(decl.Pos())
		declEnd := selectedPkg.Fset.File(decl.End()).Position(decl.End())

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
	return selectedDecl
}

func GetCFG(query *base.CFGQuery, projectsDir string)  {
	// fmt.Fprintf(
	// 	os.Stderr, "Loading CFG for %s in %s at %s:%d (%s)...\n",
	// 	query.PackageName, query.ProjectName, query.FileName, query.LineNumber, query.Snippet)
	checkoutDir := fmt.Sprintf("%s/%s", projectsDir, query.ProjectName)
	cgoFileMap := make(map[string]string)
	lineRegex := regexp.MustCompile("(?m)^// ?line ([^:]+):")
	lock := sync.Mutex{}

	query.FileName = strings.TrimPrefix(query.FileName, "/root/")
	isCacheQuery := strings.HasPrefix(query.FileName, ".cache")

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
				selectedDecl = selectDecl(selectedPkg, selectedFile, query.LineNumber, nil)
				break
			}
		}
	}

	if selectedFile == nil {
		if !isCacheQuery {
			panic("Could not find non-cache file.")
		}
		snippetRegex := regexp.MustCompile(regexp.QuoteMeta(query.Snippet))
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
							decl := selectDecl(selectedPkg, selectedFile, query.LineNumber, snippetMatches)
							if decl != nil {
								dStart := decl.Pos()
								dEnd := decl.End()
								startOffset := parsedPkg.Fset.File(dStart).Offset(dStart)
								endOffset := parsedPkg.Fset.File(dEnd).Offset(dEnd)
								codeSlice := code[startOffset:endOffset]
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
