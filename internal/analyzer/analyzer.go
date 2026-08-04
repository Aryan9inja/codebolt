package analyzer

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/Aryan9inja/codebolt/internal/analyzer/rules"
	"github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

type Analyzer struct {
	dispatch map[reflect.Type][]types.RuleFunc
	fset     *token.FileSet
}

func NewAnalyzer() *Analyzer {
	a := &Analyzer{
		dispatch: make(map[reflect.Type][]types.RuleFunc),
		fset:     token.NewFileSet(),
	}
	a.registerAll()
	return a
}

// register wires a rule function to be called for specific AST node types.
func (a *Analyzer) register(rule types.RuleFunc, nodeTypes ...any) {
	for _, t := range nodeTypes {
		typ := reflect.TypeOf(t)
		a.dispatch[typ] = append(a.dispatch[typ], rule)
	}
}

func (a *Analyzer) registerAll() {
	// concurrency
	a.register(rules.MutexCopiedByValue, (*ast.AssignStmt)(nil), (*ast.CallExpr)(nil))

	// context
	a.register(rules.ContextNotPropagated, (*ast.FuncDecl)(nil))

	// error handling
	a.register(rules.ErrorIgnored, (*ast.ExprStmt)(nil))
	a.register(rules.EmptyErrorCatch, (*ast.IfStmt)(nil))
	a.register(rules.NakedReturn, (*ast.FuncDecl)(nil))
	a.register(rules.MissingCancel, (*ast.AssignStmt)(nil))

	// library hygiene
	a.register(rules.OsExitInLibrary, (*ast.CallExpr)(nil))
	a.register(rules.PanicOutsideMain, (*ast.CallExpr)(nil))

	// loops
	a.register(rules.DeferredCloseInLoop, (*ast.RangeStmt)(nil), (*ast.ForStmt)(nil))
	a.register(rules.TimeAfterInLoop, (*ast.RangeStmt)(nil), (*ast.ForStmt)(nil))
	a.register(rules.GoroutineLoopCapture, (*ast.RangeStmt)(nil), (*ast.ForStmt)(nil))

	// style or correctness
	a.register(rules.FmtSprintfInPrintln, (*ast.CallExpr)(nil))
	a.register(rules.ReflectDeepEqualPrimitives, (*ast.CallExpr)(nil))
	a.register(rules.UnreachableCodeAfterReturn, (*ast.BlockStmt)(nil))
	a.register(rules.InefficientAssignment, (*ast.BlockStmt)(nil))
	a.register(rules.StructTagIssues, (*ast.StructType)(nil))
}

func (a *Analyzer) Analyze(filePath, content string, changedLines map[int]bool, lineToDiffPos map[int]int, goVersion string) []types.Finding {
	ctx := &types.FileContext{
		FilePath:      filePath,
		IsMain:        filepath.Base(filePath) == "main.go",
		IsTest:        strings.HasSuffix(filePath, "_test.go"),
		ChangedLines:  changedLines,
		LineToDiffPos: lineToDiffPos,
		GoVersion:     goVersion,
	}

	fset := a.fset
	f, err := parser.ParseFile(fset, filePath, content, parser.ParseComments)
	if err != nil {
		// Invalid syntax is handled upstream; skip analysis.
		return nil
	}

	var findings []types.Finding

	ast.Inspect(f, func(node ast.Node) bool {
		if node == nil {
			return true
		}
		t := reflect.TypeOf(node)
		if fns, ok := a.dispatch[t]; ok {
			for _, fn := range fns {
				findings = append(findings, fn(node, fset, ctx)...)
			}
		}
		return true
	})

	// Scan for unresolved TODO/FIXME/HACK markers in comments.
	findings = append(findings, rules.TodoScanner(f.Comments, fset, ctx)...)

	return findings
}
