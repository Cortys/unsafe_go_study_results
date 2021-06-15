package cfg

import (
	"fmt"
	"github.com/stg-tud/unsafe_go_study_results/scripts/data-acquisition-tool/base"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/cfg"
	"go/ast"
	"strings"
)

func mayReturn(block *ast.CallExpr) bool {
	return true
}

func GetCFG(query *base.CFGQuery, projectsDir string)  {
	fmt.Printf("Loading CFG for %s in %s at %s:%d...\n", query.PackageName, query.ProjectName, query.FileName, query.LineNumber)
	checkoutDir := fmt.Sprintf("%s/%s", projectsDir, query.ProjectName)

	parsedPkgs, err := packages.Load(&packages.Config{
		Mode:       packages.NeedImports | packages.NeedDeps | packages.NeedSyntax |
					packages.NeedFiles | packages.NeedName | packages.NeedTypes | packages.NeedCompiledGoFiles,
		Tests:      false,
		Dir:		checkoutDir,
	}, query.PackageName)
	if err != nil {
		fmt.Println(err)
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
				fmt.Println(selectedDecl.Name)
				graph := cfg.New(selectedDecl.Body, mayReturn)
				// for i, block := range graph.Blocks {
				// 	for j, node := range block.Nodes {
				// 		fmt.Println(i, j, node)
				// 	}
				// }
				fmt.Println(graph.Format(parsedPkg.Fset))
			}
		}
	}

	fmt.Println("Done.")
}
