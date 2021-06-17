package cfg

import (
	"os"
	"io"
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
	"github.com/ReconfigureIO/goblin"
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

func printCFG(f io.Writer, decl *ast.FuncDecl, pkg *packages.Package) {
	c := cfg.FromFunc(decl)
	pkgInfo := &loader.PackageInfo{
		Pkg: pkg.Types,
		Importable: true,
		TransitivelyErrorFree: true,
		Files: pkg.Syntax,
		Info: *pkg.TypesInfo,
	}
	varMap := dataflow.Vars(decl, pkgInfo)
	inVarMap, outVarMap := dataflow.LiveVars(c, pkgInfo)
	fset := pkg.Fset
	vars := make([]*types.Var, 0, len(varMap))
	varIds := make(map[*types.Var]int, len(varMap))
	for v := range varMap {
		varIds[v] = len(vars)
		vars = append(vars, v)
	}
	v, t, p := varsToJSON(vars)

	blocks := c.Blocks()
	c.Sort(blocks)
	blockToId := make(map[ast.Stmt]int, len(blocks))
	cfgBlocks := make([]base.CFGBlock, 0, len(blocks))
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

	b, err := json.Marshal(base.CFG{
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
	// fmt.Fprintf(os.Stderr, "Loading CFG for %s in %s at %s:%d...\n", query.PackageName, query.ProjectName, query.FileName, query.LineNumber)
	checkoutDir := fmt.Sprintf("%s/%s", projectsDir, query.ProjectName)

	parsedPkgs, err := packages.Load(&packages.Config{
		Mode:       packages.NeedImports | packages.NeedDeps | packages.NeedSyntax |
					packages.NeedFiles | packages.NeedName | packages.NeedTypes |
					packages.NeedCompiledGoFiles | packages.NeedTypesInfo,
		Tests:      false,
		Dir:		checkoutDir,
	}, query.PackageName)
	if err != nil {
		println(err)
		panic("error loading packages")
	}

	for _, parsedPkg := range parsedPkgs {
		for i, file := range parsedPkg.Syntax {
			name := parsedPkg.CompiledGoFiles[i]
			if strings.HasSuffix(name, query.FileName) {
				var selectedDecl *ast.FuncDecl

				declLoop: for _, decl := range file.Decls {
					declPos := parsedPkg.Fset.File(decl.Pos()).Position(decl.Pos())

					if declPos.Line > query.LineNumber {
						break declLoop
					}

					switch funcDecl := decl.(type) {
					case *ast.FuncDecl:
						selectedDecl = funcDecl
					default:
					}
				}
				if selectedDecl == nil {
					panic("Queried function declaration not found.")
				}

				printCFG(os.Stdout, selectedDecl, parsedPkg)
				break
			}
		}
	}
	fmt.Println()
}
