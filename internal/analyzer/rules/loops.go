package rules

import (
	"fmt"
	"go/ast"
	"go/token"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

// DeferredCloseInLoop flags defer inside for/range bodies.
// defer in a loop doesn't run until the function returns, not each iteration.
func DeferredCloseInLoop(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	var body *ast.BlockStmt
	switch n := node.(type) {
	case *ast.ForStmt:
		body = n.Body
	case *ast.RangeStmt:
		body = n.Body
	default:
		return nil
	}

	var findings []analyzer.Finding
	for _, stmt := range body.List {
		deferStmt, ok := stmt.(*ast.DeferStmt)
		if !ok {
			continue
		}
		if !onChangedLine(deferStmt, fset, ctx) {
			continue
		}
		line := posLine(deferStmt.Pos(), fset)
		findings = append(findings, finding(
			"deferred-close-in-a-loop",
			analyzer.SeverityWarning,
			"defer inside loop: deferred call won't execute until function returns, not each iteration - consider a helper function or explicit close",
			line, ctx,
		))
	}
	return findings
}

// TimeAfterInLoop flags time.After calls inside loops.
// Each call allocates a new timer that is never garbage collected until it fires.
func TimeAfterInLoop(node any, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	var body *ast.BlockStmt
	switch n := node.(type) {
	case *ast.RangeStmt:
		body = n.Body
	case *ast.ForStmt:
		body = n.Body
	default:
		return nil
	}

	var findings []analyzer.Finding
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if !isSelectorExpr(call.Fun, "time", "After") {
			return true
		}
		if !onChangedLine(call, fset, ctx) {
			return true
		}
		line := posLine(call.Pos(), fset)
		findings = append(findings, finding(
			"time-after-in-loop",
			analyzer.SeverityWarning,
			"time.After inside loop allocates a new timer each iteration that leaks until it fires — use time.NewTimer and reset it instead",
			line, ctx,
		))
		return true
	})
	return findings
}

// GoroutineLoopCapture flags goroutines launched inside loops that reference
// the loop variable by closure. Fixed in Go 1.22+ loop semantics but still
// relevant for modules with lower minimums.
func GoroutineLoopCapture(node interface{}, fset *token.FileSet, ctx *analyzer.FileContext) []analyzer.Finding {
	// Skip if module is Go 1.22+
	if goVersionAtLeast(ctx.GoVersion, 1, 22) {
		return nil
	}

	var (
		body     *ast.BlockStmt
		loopVars []string
	)

	switch n := node.(type) {
	case *ast.RangeStmt:
		body = n.Body
		if id, ok := n.Key.(*ast.Ident); ok {
			loopVars = append(loopVars, id.Name)
		}
		if id, ok := n.Value.(*ast.Ident); ok {
			loopVars = append(loopVars, id.Name)
		}
	case *ast.ForStmt:
		body = n.Body
		// collect vars assigned in init statement
		if assign, ok := n.Init.(*ast.AssignStmt); ok {
			for _, lhs := range assign.Lhs {
				if id, ok := lhs.(*ast.Ident); ok {
					loopVars = append(loopVars, id.Name)
				}
			}
		}
	}

	if body == nil || len(loopVars) == 0 {
		return nil
	}

	loopVarSet := make(map[string]bool, len(loopVars))
	for _, v := range loopVars {
		loopVarSet[v] = true
	}

	var findings []analyzer.Finding
	ast.Inspect(body, func(n ast.Node) bool {
		goStmt, ok := n.(*ast.GoStmt)
		if !ok {
			return true
		}
		// look for loop vars inside the goroutine function literal
		ast.Inspect(goStmt.Call.Fun, func(inner ast.Node) bool {
			id, ok := inner.(*ast.Ident)
			if !ok {
				return true
			}
			if loopVarSet[id.Name] && onChangedLine(goStmt, fset, ctx) {
				line := posLine(goStmt.Pos(), fset)
				findings = append(findings, finding(
					"goroutine-loop-capture",
					analyzer.SeverityWarning,
					"goroutine closure captures loop variable '"+id.Name+"' by reference - shadow it before the goroutine or upgrade go.mod minimum to 1.22",
					line, ctx,
				))
				return false // one finding per goroutine is enough
			}
			return true
		})
		return true
	})
	return findings
}

func goVersionAtLeast(version string, major, minor int) bool {
	var vMaj, vMin int
	// version is something like "1.22" or "1.22.3"
	_, err := parseVersion(version, &vMaj, &vMin)
	if err != nil {
		return false
	}

	if vMaj != major {
		return vMaj > major
	}
	return vMin >= minor
}

var errBadVersion = fmt.Errorf("bad version")

func parseVersion(v string, major, minor *int) (int, error) {
	// simple parser: "1.22" or "1.22.3"
	parts := splitVersion(v)
	if len(parts) < 2 {
		return 0, errBadVersion
	}
	*major = atoi(parts[0])
	*minor = atoi(parts[1])
	return 0, nil
}

func splitVersion(version string) []string {
	var parts []string
	current := ""
	for _, c := range version {
		if c == '.' {
			parts = append(parts, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
