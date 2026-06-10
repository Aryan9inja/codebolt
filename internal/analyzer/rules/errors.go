package rules

import (
	"go/ast"
	"go/token"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

// ErrorIgnored flags function calls whose error return value is discarded.
// Matches: foo() as a standalone ExprStmt where foo's last return is named "error".
// Note: without type info we approximate — we flag any ExprStmt call where the
// callee name suggests error returns aren't being caught. Full accuracy needs
// go/types; this catches the obvious cases.
func ErrorIgnored(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	expr, ok := node.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if !onChangedLine(expr, fset, ctx) {
		return nil
	}
	// Without type info we can't know the return signature.
	// Flag calls that are assigned to blank identifier or not assigned at all
	// - this rule is best-effort at AST level. The LLM pass will refine.
	_ = call
	return nil // placeholder - full impl needs go/types, deferred till next phase
}

// EmptyErrorCatch flags `if err != nil {}` with an empty body.
func EmptyErrorCatch(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	ifStmt, ok := node.(*ast.IfStmt)
	if !ok {
		return nil
	}
	if !onChangedLine(ifStmt, fset, ctx) {
		return nil
	}

	// Must be `err != nil` binary expression
	bin, ok := ifStmt.Cond.(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	if bin.Op.String() != "!=" {
		return nil
	}
	if !isIdent(bin.X, "err") || !isIdent(bin.Y, "nil") {
		return nil
	}

	// Body must be empty
	if len(ifStmt.Body.List) != 0 {
		return nil
	}

	line := posLine(ifStmt.Pos(), fset)
	return []analyzer.Finding{finding(
		"empty-error-catch",
		analyzer.SeverityError,
		"error is checked but the if-body is empty - either handle the error, return it, or remove the check",
		line, ctx,
	)}
}

// NakedReturn flags bare `return` statements in functions with named return values
func NakedReturn(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	fn, ok := node.(*ast.FuncDecl)
	if !ok || fn.Type.Results == nil || fn.Body == nil {
		return nil
	}

	// Check if any return values are named
	hasNamedReturns := false
	for _, field := range fn.Type.Results.List {
		if len(field.Names) > 0 {
			hasNamedReturns = true
			break
		}
	}
	if !hasNamedReturns {
		return nil
	}

	var findings []analyzer.Finding
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok {
			return true
		}
		if len(ret.Results) != 0 {
			return true // not naked
		}
		if !onChangedLine(ret, fset, ctx) {
			return true
		}
		line := posLine(ret.Pos(), fset)
		findings = append(findings, finding(
			"naked-return",
			analyzer.SeverityWarning,
			"naked return in function with named results — explicitly return values for clarity",
			line, ctx,
		))
		return true
	})
	return findings
}

// MissingCancel flags context.WithCancel / WithTimeout / WithDeadline calls
// where the cancel function is assigned to `_`.
func MissingCancel(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	assign, ok := node.(*ast.AssignStmt)
	if !ok {
		return nil
	}
	if !onChangedLine(assign, fset, ctx) {
		return nil
	}

	contextFuncs := map[string]bool{
		"WithCancel":   true,
		"WithTimeout":  true,
		"WithDeadline": true,
	}

	for _, rhs := range assign.Rhs {
		call, ok := rhs.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		if !isIdent(sel.X, "context") || !contextFuncs[sel.Sel.Name] {
			continue
		}
		// LHS should have 2 values: ctx, cancel
		// Flag if cancel (second) is blank identifier
		if len(assign.Lhs) < 2 {
			continue
		}
		if isIdent(assign.Lhs[1], "_") {
			line := posLine(assign.Pos(), fset)
			return []analyzer.Finding{finding(
				"missing-cancel",
				analyzer.SeverityError,
				"cancel function from context."+sel.Sel.Name+" is discarded - this leaks the context; assign and defer cancel()",
				line, ctx,
			)}
		}
	}
	return nil
}
