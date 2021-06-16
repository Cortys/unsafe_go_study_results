package cfg

import (
	"os"
	"io"
	"fmt"
	"go/ast"
	"go/token"
	"strings"
	"go/format"
	"encoding/json"
	"golang.org/x/tools/go/packages"
	"github.com/godoctor/godoctor/analysis/cfg"
	// "github.com/godoctor/godoctor/analysis/dataflow"
	"github.com/stg-tud/unsafe_go_study_results/scripts/data-acquisition-tool/base"
)

func printCFG(f io.Writer, c *cfg.CFG, fset *token.FileSet) {
	blocks := c.Blocks()
	c.Sort(blocks)
	blockToId := make(map[ast.Stmt]int, len(blocks))
	cfgBlocks := make([]base.CFGBlock, 0, len(blocks))
	edges := make([][2]int, 0, len(blocks))
	for i, block := range blocks {
		blockToId[block] = i
		var blockStr string
		var line int
		switch block {
		case c.Entry:
			blockStr = "<entry>"
			line = -1
		case c.Exit:
			blockStr = "<exit>"
			line = -1
		default:
			var sb strings.Builder
			format.Node(&sb, fset, block)
			blockStr = sb.String()
			blockPos := fset.File(block.Pos()).Position(block.Pos())
			line = blockPos.Line
		}

		cfgBlocks = append(cfgBlocks, base.CFGBlock{
			Code: blockStr,
			LineNumber: line,
		})
	}
	for i, block := range blocks {
		succs := c.Succs(block)
		c.Sort(succs)
		for _, succ := range succs {
			j := blockToId[succ]
			edges = append(edges, [2]int{i, j})
		}
	}

	b, err := json.Marshal(base.CFG{
		Blocks: cfgBlocks,
		Edges: edges,
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
					packages.NeedFiles | packages.NeedName | packages.NeedTypes | packages.NeedCompiledGoFiles,
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
				graph := cfg.FromFunc(selectedDecl)
				printCFG(os.Stdout, graph, parsedPkg.Fset)
				break
			}
		}
	}
	fmt.Println()
}
