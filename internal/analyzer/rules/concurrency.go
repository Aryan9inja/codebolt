package rules

import (
	"go/ast"
	"go/token"

	"github.com/Aryan9inja/codebolt/internal/analyzer"
)

// MutexCopiedByValue detects suspicious value copies that may involve
// sync.Mutex or sync.RWMutex.
//
// NOTE:
// Pure AST analysis cannot determine the actual type of an identifier or
// expression. Accurate mutex-copy detection requires semantic type
// information from go/types.
//
// This rule currently only detects obvious direct composite literal usage
// and serves as a placeholder for the future typed-analysis phase.
func MutexCopiedByValue(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	switch n := node.(type) {
	case *ast.AssignStmt:
		return checkMutexAssign(n, fset, ctx)
	case *ast.CallExpr:
		return checkMutexCallArgs(n, fset, ctx)
	}
	return nil
}

func checkMutexAssign(assign *ast.AssignStmt, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	if !onChangedLine(assign, fset, ctx) {
		return nil
	}
	for _, rhs := range assign.Rhs {
		// Detect direct composite literals like:
		//
		//	mu := sync.Mutex{}
		//
		// This is only a heuristic and does NOT detect general mutex copies.
		if isSelectorExpr(rhs, "sync", "Mutex") || isSelectorExpr(rhs, "sync", "RWMutex") {
			line := posLine(assign.Pos(), fset)
			return []analyzer.Finding{finding(
				"mutex-copied-by-value",
				analyzer.SeverityError,
				"sync.Mutex or sync.RWMutex copied by value — use a pointer receiver or pointer field to avoid subtle data races",
				line, ctx,
			)}
		}
	}
	return nil
}

func checkMutexCallArgs(call *ast.CallExpr, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	if !onChangedLine(call, fset, ctx) {
		return nil
	}
	for _, arg := range call.Args {
		// Detect direct inline literals:
		//
		//	foo(sync.Mutex{})
		//
		// General mutex-copy detection requires go/types.
		if isSelectorExpr(arg, "sync", "Mutex") || isSelectorExpr(arg, "sync", "RWMutex") {
			line := posLine(call.Pos(), fset)
			return []analyzer.Finding{finding(
				"mutex-copied-by-value",
				analyzer.SeverityError,
				"sync.Mutex or sync.RWMutex passed by value to function — pass a pointer instead",
				line, ctx,
			)}
		}
	}
	return nil
}
