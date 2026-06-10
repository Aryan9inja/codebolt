package rules

import (
	"go/ast"
	"go/token"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

// ContextNotPropagated flags functions that accept a context.Context parameter
// but make outgoing calls (http.Get, sql.Query, etc.) without passing it.
// Pure AST heuristic — checks for known stdlib calls that have ctx variants.
func ContextNotPropagated(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	fn, ok := node.(*ast.FuncDecl)
	if !ok || fn.Body == nil || fn.Type.Params == nil {
		return nil
	}

	// Does the function take a context.Context param?
	hasContext := false
	var ctxParamName string
	for _, field := range fn.Type.Params.List {
		if isSelectorExpr(field.Type, "context", "Context") {
			hasContext = true
			if len(field.Names) > 0 {
				ctxParamName = field.Names[0].Name
			}
			break
		}
	}
	if !hasContext {
		return nil
	}

	// Known calls that should receive ctx but have context-unaware variants
	// flagged if called without the ctx param name as first argument
	suspectCalls := map[string]map[string]bool{
		"http": {"Get": true, "Post": true, "Do": true},
		"sql":  {"Query": true, "Exec": true, "QueryRow": true},
	}

	var findings []analyzer.Finding
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkgIdent, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		methods, pkgKnown := suspectCalls[pkgIdent.Name]
		if !pkgKnown || !methods[sel.Sel.Name] {
			return true
		}
		// Check if the first argument is the context param
		if len(call.Args) > 0 {
			if isIdent(call.Args[0], ctxParamName) {
				return true // correctly propagated
			}
		}
		if !onChangedLine(call, fset, ctx) {
			return true
		}
		line := posLine(call.Pos(), fset)
		findings = append(findings, finding(
			"context-not-propagated",
			analyzer.SeverityWarning,
			pkgIdent.Name+"."+sel.Sel.Name+" called without propagating context - use the WithContext variant",
			line, ctx,
		))
		return true
	})
	return findings
}
