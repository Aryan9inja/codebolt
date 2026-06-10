package rules

import (
	"go/ast"
	"go/token"
	"strings"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

type assignInfo struct {
	line int
	pos  token.Pos
}

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

// ReflectDeepEqualPrimitives flags reflect.DeepEqual calls where both
// arguments appear to be primitive-like expressions.
//
// For primitive comparable values, == is usually:
//   - faster
//   - clearer
//   - more idiomatic
func ReflectDeepEqualPrimitives(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if !isSelectorExpr(call.Fun, "reflect", "DeepEqual") {
		return nil
	}
	if len(call.Args) != 2 {
		return nil
	}
	if !isPrimitiveLike(call.Args[0]) || !isPrimitiveLike(call.Args[1]) {
		return nil
	}
	if !onChangedLine(call, fset, ctx) {
		return nil
	}
	line := posLine(call.Pos(), fset)
	return []analyzer.Finding{finding(
		"reflect-deepequal-primitives",
		analyzer.SeverityInfo,
		"reflect.DeepEqual used on primitive-like values — use == instead for correctness and performance",
		line, ctx,
	)}
}

func isPrimitiveLike(expr ast.Expr) bool {
	switch expr.(type) {
	case *ast.BasicLit: // string, int, float literals
		return true
	case *ast.Ident: // true, false, nil, or simple var
		return true
	}
	return false
}

// UnreachableCodeAfterReturn flags statements that follow a return in the same block.
func UnreachableCodeAfterReturn(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	block, ok := node.(*ast.BlockStmt)
	if !ok {
		return nil
	}
	var findings []analyzer.Finding
	for i, stmt := range block.List {
		if _, isReturn := stmt.(*ast.ReturnStmt); isReturn {
			for _, unreachable := range block.List[i+1:] {
				if !onChangedLine(unreachable, fset, ctx) {
					continue
				}
				line := posLine(unreachable.Pos(), fset)
				findings = append(findings, finding(
					"unreachable-code-after-return",
					analyzer.SeverityWarning,
					"unreachable code: this statement follows a return and will never execute",
					line, ctx,
				))
			}
			break // only need the first return
		}
	}
	return findings
}

// InefficientAssignment flags consecutive assignments to the same variable
// within the same block where the first value is never used.
func InefficientAssignment(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	block, ok := node.(*ast.BlockStmt)
	if !ok {
		return nil
	}

	// Track last assignment to each variable.
	lastAssign := make(map[string]assignInfo)
	var findings []analyzer.Finding

	for _, stmt := range block.List {
		assign, ok := stmt.(*ast.AssignStmt)
		if !ok {
			// Any non-assignment statement — clear vars that appear as operands
			// (simplified: clear all to avoid false positives across complex stmts)
			clearUsedVars(stmt, lastAssign)
			continue
		}

		for _, lhs := range assign.Lhs {
			id, ok := lhs.(*ast.Ident)
			if !ok || id.Name == "_" {
				continue
			}
			if prev, seen := lastAssign[id.Name]; seen {
				// Variable was assigned before and not used in between
				if onChangedLine(assign, fset, ctx) || ctx.ChangedLines[prev.line] {
					findings = append(findings, finding(
						"ineffective-assignment",
						analyzer.SeverityWarning,
						"value assigned to '"+id.Name+"' is never used before being overwritten",
						prev.line, ctx,
					))
				}
			}
			lastAssign[id.Name] = assignInfo{
				line: posLine(assign.Pos(), fset),
				pos:  assign.Pos(),
			}
		}
		// Clear RHS idents from lastAssign (they were read)
		for _, rhs := range assign.Rhs {
			clearIdentRefs(rhs, lastAssign)
		}
	}
	return findings
}

func clearUsedVars(stmt ast.Stmt, lastAssign map[string]assignInfo) {
	ast.Inspect(stmt, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok {
			delete(lastAssign, id.Name)
		}
		return true
	})
}

func clearIdentRefs(expr ast.Expr, lastAssign map[string]assignInfo) {
	ast.Inspect(expr, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if ok {
			delete(lastAssign, id.Name)
		}
		return true
	})
}

// StructTagIssues flags malformed or duplicate struct field tags.
//
// This is a lightweight syntax-level validator for struct tags.
// It detects:
//   - duplicate keys
//   - missing colon separators
//   - missing opening quotes
//   - unterminated quoted values
//
// NOTE:
// This is intentionally heuristic-based and does not fully implement
// the reflect.StructTag parser semantics.
func StructTagIssues(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	structType, ok := node.(*ast.StructType)
	if !ok || structType.Fields == nil {
		return nil
	}

	var findings []analyzer.Finding

	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			continue
		}

		if !onChangedLine(field, fset, ctx) {
			continue
		}

		raw := field.Tag.Value

		// Tags should be quoted with backticks or double quotes.
		if len(raw) < 2 {
			continue
		}

		tagStr := raw[1 : len(raw)-1]
		line := posLine(field.Pos(), fset)

		seenKeys := make(map[string]bool)

		for len(tagStr) > 0 {
			tagStr = strings.TrimSpace(tagStr)
			if tagStr == "" {
				break
			}

			// Find key separator.
			colon := strings.IndexByte(tagStr, ':')
			if colon <= 0 {
				findings = append(findings, finding(
					"struct-tag-malformed",
					analyzer.SeverityError,
					"struct tag appears malformed - missing ':' separator",
					line,
					ctx,
				))
				break
			}

			key := strings.TrimSpace(tagStr[:colon])

			if key == "" {
				findings = append(findings, finding(
					"struct-tag-malformed",
					analyzer.SeverityError,
					"struct tag contains empty key",
					line,
					ctx,
				))
				break
			}

			if seenKeys[key] {
				findings = append(findings, finding(
					"struct-tag-duplicate",
					analyzer.SeverityWarning,
					"struct tag key '"+key+"' appears more than once",
					line,
					ctx,
				))
			}

			seenKeys[key] = true

			tagStr = tagStr[colon+1:]

			// Expect opening quote.
			if len(tagStr) == 0 || tagStr[0] != '"' {
				findings = append(findings, finding(
					"struct-tag-malformed",
					analyzer.SeverityError,
					"struct tag for key '"+key+"' is missing opening quote",
					line,
					ctx,
				))
				break
			}

			// Scan quoted string while respecting escapes.
			i := 1
			escaped := false

			for i < len(tagStr) {
				ch := tagStr[i]

				if escaped {
					escaped = false
					i++
					continue
				}

				if ch == '\\' {
					escaped = true
					i++
					continue
				}

				if ch == '"' {
					break
				}

				i++
			}

			// Unterminated quote.
			if i >= len(tagStr) {
				findings = append(findings, finding(
					"struct-tag-malformed",
					analyzer.SeverityError,
					"struct tag for key '"+key+"' is missing closing quote",
					line,
					ctx,
				))
				break
			}

			// Move past closing quote.
			tagStr = tagStr[i+1:]
		}
	}

	return findings
}
