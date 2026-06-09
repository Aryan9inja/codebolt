package rules

import (
	"go/ast"
	"go/token"

	"github.com/Aryan9inja/codebolt/internal/analyzer"
)

// FmtSprintfInPrintln flags fmt.Println(fmt.Sprintf(...)) — just use fmt.Printf.
func FmtSprintfInPrintln(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	outer, ok := node.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if !isSelectorExpr(outer.Fun, "fmt", "Println") && !isSelectorExpr(outer.Fun, "fmt", "Print") {
		return nil
	}
	if len(outer.Args) != 1 {
		return nil
	}
	if !isCallTo(outer.Args[0], "fmt", "Sprintf") {
		return nil
	}
	if !onChangedLine(outer, fset, ctx) {
		return nil
	}
	line := posLine(outer.Pos(), fset)
	return []analyzer.Finding{finding(
		"fmt-sprintf-in-println",
		analyzer.SeverityInfo,
		"fmt.Println(fmt.Sprintf(...)) can be simplified to fmt.Printf(...)",
		line, ctx,
	)}
}
