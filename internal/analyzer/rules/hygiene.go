package rules

import (
	"go/ast"
	"go/token"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

// OsExitInLibrary flags os.Exit calls in non-main, non-test files.
func OsExitInLibrary(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	if ctx.IsMain || ctx.IsTest {
		return nil
	}
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if !isSelectorExpr(call.Fun, "os", "Exit") {
		return nil
	}
	if !onChangedLine(call, fset, ctx) {
		return nil
	}
	line := posLine(call.Pos(), fset)
	return []analyzer.Finding{finding(
		"os-exit-in-library",
		analyzer.SeverityError,
		"os.Exit in library code prevents deferred functions from running and makes the package untestable - return an error instead",
		line, ctx,
	)}
}

// PanicOutsideMain flags panic() calls outside main and test files.
func PanicOutsideMain(node interface{}, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	if ctx.IsMain || ctx.IsTest {
		return nil
	}
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if !isIdent(call.Fun, "panic") {
		return nil
	}
	if !onChangedLine(call, fset, ctx) {
		return nil
	}
	line := posLine(call.Pos(), fset)
	return []analyzer.Finding{finding(
		"panic-outside-main",
		analyzer.SeverityWarning,
		"panic in library code crashes the entire program - return an error instead",
		line, ctx,
	)}
}