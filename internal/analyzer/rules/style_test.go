package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

// helper to run a rule on all nodes in a Go source code string
func runRule(t *testing.T, content string, rule analyzer.RuleFunc, ctx *analyzer.FileContext) []analyzer.Finding {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, ctx.FilePath, content, parser.ParseComments)
	if err != nil {
		t.Fatalf("failed to parse Go code in test: %v", err)
	}

	var findings []analyzer.Finding
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		findings = append(findings, rule(n, fset, ctx)...)
		return true
	})
	return findings
}

func TestFmtSprintfInPrintln(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "Println with Sprintf on changed line",
			content: `package test
import "fmt"
func main() {
	fmt.Println(fmt.Sprintf("hello %s", "world"))
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "Println with Sprintf on unchanged line",
			content: `package test
import "fmt"
func main() {
	fmt.Println(fmt.Sprintf("hello %s", "world"))
}`,
			changedLines: map[int]bool{1: true},
			wantLines:    nil,
		},
		{
			name: "Print with Sprintf on changed line",
			content: `package test
import "fmt"
func main() {
	fmt.Print(fmt.Sprintf("hello %s", "world"))
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "Println with multiple args including Sprintf",
			content: `package test
import "fmt"
func main() {
	fmt.Println("prefix", fmt.Sprintf("hello %s", "world"))
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
		{
			name: "Println with simple string literal",
			content: `package test
import "fmt"
func main() {
	fmt.Println("hello world")
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, FmtSprintfInPrintln, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "fmt-sprintf-in-println" {
					t.Errorf("expected rule name fmt-sprintf-in-println, got %s", f.Rule)
				}
				if f.Severity != analyzer.SeverityInfo {
					t.Errorf("expected SeverityInfo, got %v", f.Severity)
				}
			}
			if !reflect.DeepEqual(gotLines, tt.wantLines) {
				t.Errorf("got findings on lines %v, want %v", gotLines, tt.wantLines)
			}
		})
	}
}

func TestReflectDeepEqualPrimitives(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "DeepEqual on primitive literals",
			content: `package test
import "reflect"
func main() {
	_ = reflect.DeepEqual(1, 2)
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "DeepEqual on primitive idents",
			content: `package test
import "reflect"
func main() {
	a, b := 1, 2
	_ = reflect.DeepEqual(a, b)
}`,
			changedLines: map[int]bool{5: true},
			wantLines:    []int{5},
		},
		{
			name: "DeepEqual on composite literals",
			content: `package test
import "reflect"
func main() {
	_ = reflect.DeepEqual(struct{}{}, struct{}{})
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, ReflectDeepEqualPrimitives, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "reflect-deepequal-primitives" {
					t.Errorf("expected rule reflect-deepequal-primitives, got %s", f.Rule)
				}
			}
			if !reflect.DeepEqual(gotLines, tt.wantLines) {
				t.Errorf("got findings on lines %v, want %v", gotLines, tt.wantLines)
			}
		})
	}
}

func TestUnreachableCodeAfterReturn(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "unreachable statement directly after return",
			content: `package test
func run() int {
	return 1
	println("unreachable")
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "unreachable statement not on changed line",
			content: `package test
func run() int {
	return 1
	println("unreachable")
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    nil,
		},
		{
			name: "return at the end of block",
			content: `package test
func run() int {
	println("reachable")
	return 1
}`,
			changedLines: map[int]bool{3: true, 4: true},
			wantLines:    nil,
		},
		{
			name: "return inside if-block does not affect parent block siblings",
			content: `package test
func run(b bool) int {
	if b {
		return 1
	}
	println("reachable")
	return 2
}`,
			changedLines: map[int]bool{6: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, UnreachableCodeAfterReturn, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "unreachable-code-after-return" {
					t.Errorf("expected rule unreachable-code-after-return, got %s", f.Rule)
				}
			}
			if !reflect.DeepEqual(gotLines, tt.wantLines) {
				t.Errorf("got findings on lines %v, want %v", gotLines, tt.wantLines)
			}
		})
	}
}

func TestInefficientAssignment(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "consecutive assignments to same variable without use",
			content: `package test
func main() {
	x := 1
	x = 2
	_ = x
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{3},
		},
		{
			name: "consecutive assignments, but first is read in between",
			content: `package test
func main() {
	x := 1
	y := x
	x = 2
	_, _ = x, y
}`,
			changedLines: map[int]bool{3: true, 5: true},
			wantLines:    nil,
		},
		{
			name: "consecutive assignments, read on RHS of second assignment",
			content: `package test
func main() {
	x := 1
	x = x + 1
	_ = x
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
		{
			name: "assignment to blank identifier is not flagged",
			content: `package test
func main() {
	_ = 1
	_ = 2
}`,
			changedLines: map[int]bool{3: true, 4: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, InefficientAssignment, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
			}
			if !reflect.DeepEqual(gotLines, tt.wantLines) {
				t.Errorf("got findings on lines %v, want %v", gotLines, tt.wantLines)
			}
		})
	}
}

func TestStructTagIssues(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
		wantRules    []string
	}{
		{
			name: "duplicate keys in struct tag",
			content: `package test
type User struct {
	Name string ` + "`" + `json:"name" json:"username"` + "`" + `
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    []int{3},
			wantRules:    []string{"struct-tag-duplicate"},
		},
		{
			name: "missing colon in struct tag",
			content: `package test
type User struct {
	Name string ` + "`" + `json"name"` + "`" + `
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    []int{3},
			wantRules:    []string{"struct-tag-malformed"},
		},
		{
			name: "missing opening quote in struct tag",
			content: `package test
type User struct {
	Name string ` + "`" + `json:name"` + "`" + `
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    []int{3},
			wantRules:    []string{"struct-tag-malformed"},
		},
		{
			name: "missing closing quote in struct tag",
			content: `package test
type User struct {
	Name string ` + "`" + `json:"name` + "`" + `
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    []int{3},
			wantRules:    []string{"struct-tag-malformed"},
		},
		{
			name: "valid struct tag",
			content: `package test
type User struct {
	Name string ` + "`" + `json:"name" xml:"Name"` + "`" + `
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    nil,
			wantRules:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, StructTagIssues, ctx)
			var gotLines []int
			var gotRules []string
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				gotRules = append(gotRules, f.Rule)
			}
			if !reflect.DeepEqual(gotLines, tt.wantLines) {
				t.Errorf("got findings on lines %v, want %v", gotLines, tt.wantLines)
			}
			if !reflect.DeepEqual(gotRules, tt.wantRules) {
				t.Errorf("got rules %v, want %v", gotRules, tt.wantRules)
			}
		})
	}
}
