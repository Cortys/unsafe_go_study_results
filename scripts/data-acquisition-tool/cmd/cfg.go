package cmd

import (
	"github.com/spf13/cobra"
	"github.com/stg-tud/unsafe_go_study_results/scripts/data-acquisition-tool/base"
	"github.com/stg-tud/unsafe_go_study_results/scripts/data-acquisition-tool/cfg"
)

var projectsDir string
var projectName string
var packageName string
var fileName string
var snippet string
var lineNumber int
var maxDist int
var maxCacheDist int

var GetCFGCmd = &cobra.Command{
	Use:   "cfg",
	Short: "Gets relevant local CFG for given project, package, line number combination.",
	Long:  `Resulting CFG is written to stdout as JSON for further processing.`,
	Run: func(cmd *cobra.Command, args []string) {
		cfg.GetCFG(&base.CFGQuery{
			ProjectName: projectName,
			PackageName: packageName,
			FileName: fileName,
			Snippet: snippet,
			LineNumber: lineNumber,
			MaxDist: maxDist,
			MaxCacheDist: maxCacheDist,
		}, projectsDir)
	},
}

func init() {
	RootCmd.AddCommand(GetCFGCmd)

	GetCFGCmd.Flags().StringVar(&projectsDir, "base", "", "Base directory path")
	GetCFGCmd.Flags().StringVar(&projectName, "project", "", "Project name")
	GetCFGCmd.Flags().StringVar(&packageName, "package", "", "Package name")
	GetCFGCmd.Flags().StringVar(&fileName, "file", "", "File name")
	GetCFGCmd.Flags().StringVar(&snippet, "snippet", "", "Snippet line")
	GetCFGCmd.Flags().IntVar(&lineNumber, "line", -1, "Line number")
	GetCFGCmd.Flags().IntVar(&maxDist, "dist", 0, "Max match distance")
	GetCFGCmd.Flags().IntVar(&maxCacheDist, "cacheDist", 0, "Max cache match distance")
}
