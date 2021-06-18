package cfg

import (
	"os"
	"io"
	"io/ioutil"
	"fmt"
	"go/ast"
	"go/types"
	"strings"
	"go/format"
	"encoding/json"
	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/packages"
	"github.com/godoctor/godoctor/analysis/cfg"
	"github.com/godoctor/godoctor/analysis/dataflow"
	"github.com/GaloisInc/goblin"
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
				var sb strings.Builder
				format.Node(&sb, fset, block)
				blockStr = sb.String()
				blockPos := fset.File(block.Pos()).Position(block.Pos())
				line = blockPos.Line
				blockAst = goblin.DumpStmt(block, fset)
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

func GetCFG(query *base.CFGQuery, projectsDir string)  {
	// fmt.Fprintf(
	// 	os.Stderr, "Loading CFG for %s in %s at %s:%d (%s)...\n",
	// 	query.PackageName, query.ProjectName, query.FileName, query.LineNumber, query.Snippet)
	checkoutDir := fmt.Sprintf("%s/%s", projectsDir, query.ProjectName)

	parsedPkgs, err := packages.Load(&packages.Config{
		Mode:       packages.NeedImports | packages.NeedDeps | packages.NeedSyntax |
					packages.NeedFiles | packages.NeedName | packages.NeedTypes |
					packages.NeedCompiledGoFiles | packages.NeedTypesInfo,
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

	query.FileName = strings.TrimPrefix(query.FileName, "/root/")
	for _, parsedPkg := range parsedPkgs {
		for i, file := range parsedPkg.Syntax {
			name := parsedPkg.CompiledGoFiles[i]
			if strings.HasSuffix(name, query.FileName) {
				selectedPkg = parsedPkg
				selectedFile = file
				break
			}
		}
	}

	if selectedFile == nil {
		if !strings.HasPrefix(query.FileName, ".cache") {
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
						if strings.Contains(code, query.Snippet) {
							selectedPkg = parsedPkg
							selectedFile = file
							break
						}
					}
				}
			}
		}
		if selectedFile == nil {
			panic("Could not find cache file.")
		}
	}

	declLoop: for _, decl := range selectedFile.Decls {
		declPos := selectedPkg.Fset.File(decl.Pos()).Position(decl.Pos())

		if declPos.Line > query.LineNumber {
			break declLoop
		}

		switch sdecl := decl.(type) {
		case *ast.BadDecl:
		default:
			selectedDecl = sdecl
		}
	}
	if selectedDecl == nil {
		panic("Queried function declaration not found.")
	}

	printCFG(os.Stdout, selectedDecl, selectedPkg)

	fmt.Println()
}
