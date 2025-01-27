package geiger

import (
	"fmt"
	"github.com/stg-tud/unsafe_go_study_results/scripts/data-acquisition-tool/base"
	"go/ast"
	"golang.org/x/tools/go/ast/inspector"
	"golang.org/x/tools/go/packages"
	"strings"
)

func geigerPackages(project *base.ProjectData, pkgs []*base.PackageData, fileToLineCountMap, fileToByteCountMap map[string]int) {
	fmt.Println("  parsing packages and counting unsafe using geiger...")

	pkgsMap := make(map[string]*base.PackageData)
	for _, pkg := range pkgs {
		pkgsMap[pkg.ImportPath] = pkg
	}

	paths := make([]string, 0)
	for _, pkg := range pkgs {
		paths = append(paths, pkg.ImportPath)
	}

	fmt.Println(paths)
	fmt.Println(project)

	parsedPkgs, err := packages.Load(&packages.Config{
		Mode:       packages.NeedImports | packages.NeedDeps | packages.NeedSyntax |
					packages.NeedFiles | packages.NeedName | packages.NeedTypes,
		Tests:      false,
		Dir:		project.CheckoutPath,
	}, paths...)
	if err != nil {
		fmt.Println(err)
		panic("error loading packages")
	}

	for _, parsedPkg := range parsedPkgs {
		pkg := pkgsMap[parsedPkg.PkgPath]
		geigerSinglePackage(parsedPkg, pkg, fileToLineCountMap, fileToByteCountMap)
	}
}

func geigerSinglePackage(parsedPkg *packages.Package, pkg *base.PackageData, fileToLineCountMap, fileToByteCountMap map[string]int) {
	inspectResult := inspector.New(parsedPkg.Syntax)

	seenSelectorExprs := map[*ast.SelectorExpr]bool{}
	inspectResult.WithStack([]ast.Node{(*ast.SelectorExpr)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
		node := n.(*ast.SelectorExpr)
		_, ok := seenSelectorExprs[node]
		if ok {
			return true
		}
		seenSelectorExprs[node] = true

		matchType := "unknown"
		contextType := "unknown"

		if isUnsafePointer(node) {
			pkg.UnsafePointerSum++
			matchType = "unsafe.Pointer"
			if isInAssignment(stack) {
				contextType = "assignment"
				pkg.UnsafePointerAssignment++
			} else if isArgument(stack) {
				contextType = "call"
				pkg.UnsafePointerCall++
			} else if isParameter(stack) {
				contextType = "parameter"
				pkg.UnsafePointerParameter++
			} else if isInVariableDefinition(stack) {
				contextType = "variable"
				pkg.UnsafePointerVariable++
			} else {
				contextType = "other"
				pkg.UnsafePointerOther++
			}
		}
		if isUnsafeSizeof(node) {
			pkg.UnsafeSizeofSum++
			matchType = "unsafe.Sizeof"
			if isInAssignment(stack) {
				contextType = "assignment"
				pkg.UnsafeSizeofAssignment++
			} else if isArgument(stack) {
				contextType = "call"
				pkg.UnsafeSizeofCall++
			} else if isParameter(stack) {
				contextType = "parameter"
				pkg.UnsafeSizeofParameter++
			} else if isInVariableDefinition(stack) {
				contextType = "variable"
				pkg.UnsafeSizeofVariable++
			} else {
				contextType = "other"
				pkg.UnsafeSizeofOther++
			}
		}
		if isUnsafeOffsetof(node) {
			pkg.UnsafeOffsetofSum++
			matchType = "unsafe.Offsetof"
			if isInAssignment(stack) {
				contextType = "assignment"
				pkg.UnsafeOffsetofAssignment++
			} else if isArgument(stack) {
				contextType = "call"
				pkg.UnsafeOffsetofCall++
			} else if isParameter(stack) {
				contextType = "parameter"
				pkg.UnsafeOffsetofParameter++
			} else if isInVariableDefinition(stack) {
				contextType = "variable"
				pkg.UnsafeOffsetofVariable++
			} else {
				contextType = "other"
				pkg.UnsafeOffsetofOther++
			}
		}
		if isUnsafeAlignof(node) {
			pkg.UnsafeAlignofSum++
			matchType = "unsafe.Alignof"
			if isInAssignment(stack) {
				contextType = "assignment"
				pkg.UnsafeAlignofAssignment++
			} else if isArgument(stack) {
				contextType = "call"
				pkg.UnsafeAlignofCall++
			} else if isParameter(stack) {
				contextType = "parameter"
				pkg.UnsafeAlignofParameter++
			} else if isInVariableDefinition(stack) {
				contextType = "variable"
				pkg.UnsafeAlignofVariable++
			} else {
				contextType = "other"
				pkg.UnsafeAlignofOther++
			}
		}
		if isReflectSliceHeader(node) {
			pkg.SliceHeaderSum++
			matchType = "reflect.SliceHeader"
			if isInAssignment(stack) {
				contextType = "assignment"
				pkg.SliceHeaderAssignment++
			} else if isArgument(stack) {
				contextType = "call"
				pkg.SliceHeaderCall++
			} else if isParameter(stack) {
				contextType = "parameter"
				pkg.SliceHeaderParameter++
			} else if isInVariableDefinition(stack) {
				contextType = "variable"
				pkg.SliceHeaderVariable++
			} else {
				contextType = "other"
				pkg.SliceHeaderOther++
			}
		}
		if isReflectStringHeader(node) {
			pkg.StringHeaderSum++
			matchType = "reflect.StringHeader"
			if isInAssignment(stack) {
				contextType = "assignment"
				pkg.StringHeaderAssignment++
			} else if isArgument(stack) {
				contextType = "call"
				pkg.StringHeaderCall++
			} else if isParameter(stack) {
				contextType = "parameter"
				pkg.StringHeaderParameter++
			} else if isInVariableDefinition(stack) {
				contextType = "variable"
				pkg.StringHeaderVariable++
			} else {
				contextType = "other"
				pkg.StringHeaderOther++
			}
		}

		if matchType != "unknown" {
			writeData(n, parsedPkg, pkg, matchType, contextType, fileToLineCountMap, fileToByteCountMap)
		}

		return true
	})

	seenIdents := map[*ast.Ident]bool{}
	inspectResult.WithStack([]ast.Node{(*ast.Ident)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
		node := n.(*ast.Ident)
		_, ok := seenIdents[node]
		if ok {
			return true
		}
		seenIdents[node] = true

		matchType := "unknown"
		contextType := "unknown"

		if isUintptr(node) {
			pkg.UintptrSum++
			matchType = "uintptr"
			if isInAssignment(stack) {
				contextType = "assignment"
				pkg.UintptrAssignment++
			} else if isArgument(stack) {
				contextType = "call"
				pkg.UintptrCall++
			} else if isParameter(stack) {
				contextType = "parameter"
				pkg.UintptrParameter++
			} else if isInVariableDefinition(stack) {
				contextType = "variable"
				pkg.UintptrVariable++
			} else {
				contextType = "other"
				pkg.UintptrOther++
			}
		}

		if matchType != "unknown" {
			writeData(n, parsedPkg, pkg, matchType, contextType, fileToLineCountMap, fileToByteCountMap)
		}

		return true
	})

	pkg.UnsafeSum = pkg.UnsafePointerSum + pkg.UnsafeSizeofSum + pkg.UnsafeOffsetofSum +
		pkg.UnsafeAlignofSum + pkg.StringHeaderSum + pkg.SliceHeaderSum + pkg.UintptrSum
}

func writeData(n ast.Node, parsedPkg *packages.Package, pkg *base.PackageData, matchType, contextType string,
	fileToLineCountMap, fileToByteCountMap map[string]int) {

	nodePosition := parsedPkg.Fset.File(n.Pos()).Position(n.Pos())
	text, context := getCodeContext(parsedPkg, n)

	var filename string
	if strings.Contains(nodePosition.Filename, ".cache/go-build") {
		filename = nodePosition.Filename
	} else {
		filename = nodePosition.Filename[len(pkg.Dir)+1:]
	}

	err := base.WriteGeigerFinding(base.GeigerFindingData{
		Text:              text,
		Context:           context,
		LineNumber:        nodePosition.Line,
		Column:            nodePosition.Column,
		AbsoluteOffset:    nodePosition.Offset,
		MatchType:         matchType,
		ContextType:       contextType,
		FileName:          filename,
		FileLoc:           fileToLineCountMap[nodePosition.Filename],
		FileByteSize:      fileToByteCountMap[nodePosition.Filename],
		PackageImportPath: pkg.ImportPath,
		PackageDir:        pkg.Dir,
		ModulePath:        pkg.ModulePath,
		ModuleVersion:     pkg.ModuleVersion,
		ProjectName:       pkg.ProjectName,
	})

	if err != nil {
		_ = base.WriteErrorCondition(base.ErrorConditionData{
			Stage:             "geiger-save",
			ProjectName:       pkg.ProjectName,
			PackageImportPath: pkg.ImportPath,
			FileName:          parsedPkg.Fset.File(n.Pos()).Name(),
			Message:           err.Error(),
		})
		fmt.Println("SAVING ERROR!")
	}
}
