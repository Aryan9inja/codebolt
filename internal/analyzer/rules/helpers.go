package rules

import (
	"go/ast"
	"go/token"

	"github.com/Aryan9inja/codebolt/internal/analyzer"
)

// onChangedLine returns true if the node's start position is on a line
// touched by this PR. Rules must gate every Finding through this.
func onChangedLine(node ast.Node, fset *token.FileSet, ctx *analyzer.FileContext) bool {
	if len(ctx.ChangedLines) == 0 {
		return false
	}
	line := fset.Position(node.Pos()).Line
	return ctx.ChangedLines[line]
}

func posLine(pos token.Pos, fset *token.FileSet) int {
	return fset.Position(pos).Line
}

// isIdent returns true if the expression is an identifier with the given name.
func isIdent(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}

// isSelectorExpr returns true if expr is pkg.Name.
func isSelectorExpr(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return isIdent(sel.X, pkg) && sel.Sel.Name == name
}

// isCallTo returns true if expr is a call to pkg.Func.
func isCallTo(expr ast.Expr, pkg, fn string) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return false
	}
	return isSelectorExpr(call.Fun, pkg, fn)
}

func finding(rule string, sev analyzer.Severity, msg string, line int, ctx *analyzer.FileContext) analyzer.Finding {
	return analyzer.Finding{
		Rule:     rule,
		Severity: sev,
		Message:  msg,
		Line:     line,
		DiffPos:  ctx.LineToDiffPos[line],
		FilePath: ctx.FilePath,
	}
}
