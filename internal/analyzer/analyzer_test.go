package analyzer

import (
	"reflect"
	"testing"

	"github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

func TestNewAnalyzer(t *testing.T) {
	a := NewAnalyzer()
	if a == nil {
		t.Fatal("NewAnalyzer returned nil")
	}
	if a.dispatch == nil {
		t.Error("Analyzer dispatch map not initialized")
	}
	if a.fset == nil {
		t.Error("Analyzer FileSet not initialized")
	}
}

func TestAnalyzer_Analyze(t *testing.T) {
	tests := []struct {
		name          string
		filePath      string
		content       string
		changedLines  map[int]bool
		lineToDiffPos map[int]int
		goVersion     string
		wantFindings  []types.Finding
	}{
		{
			name:     "invalid syntax returns nil findings",
			filePath: "lib.go",
			content:  `package lib; func broken( {`, // missing parameters and body braces
			changedLines: map[int]bool{
				1: true,
			},
			wantFindings: nil,
		},
		{
			name:     "empty content returns nil findings",
			filePath: "lib.go",
			content:  "",
			changedLines: map[int]bool{
				1: true,
			},
			wantFindings: nil,
		},
		{
			name:     "rule triggered on changed line",
			filePath: "lib.go",
			content: `package lib
func test() {
	panic("crash")
}`,
			changedLines: map[int]bool{
				3: true,
			},
			lineToDiffPos: map[int]int{
				3: 10,
			},
			wantFindings: []types.Finding{
				{
					Rule:     "panic-outside-main",
					Severity: types.SeverityWarning,
					Message:  "panic in library code crashes the entire program - return an error instead",
					Line:     3,
					DiffPos:  10,
					FilePath: "lib.go",
				},
			},
		},
		{
			name:     "rule NOT triggered on unchanged line",
			filePath: "lib.go",
			content: `package lib
func test() {
	panic("crash")
}`,
			changedLines: map[int]bool{
				1: true,
			},
			wantFindings: nil,
		},
		{
			name:     "rule NOT triggered in main.go (panic allowed in main)",
			filePath: "main.go",
			content: `package main
func main() {
	panic("crash")
}`,
			changedLines: map[int]bool{
				3: true,
			},
			wantFindings: nil,
		},
		{
			name:     "rule NOT triggered in test file (panic allowed in tests)",
			filePath: "lib_test.go",
			content: `package lib
import "testing"
func TestSomething(t *testing.T) {
	panic("crash")
}`,
			changedLines: map[int]bool{
				4: true,
			},
			wantFindings: nil,
		},
		{
			name:     "TODO comment scanner on changed line",
			filePath: "lib.go",
			content: `package lib
// TODO: implement this function
func todo() {}`,
			changedLines: map[int]bool{
				2: true,
			},
			lineToDiffPos: map[int]int{
				2: 5,
			},
			wantFindings: []types.Finding{
				{
					Rule:     "todo-comment",
					Severity: types.SeverityInfo,
					Message:  "unresolved comment marker in changed code: // TODO: implement this function",
					Line:     2,
					DiffPos:  5,
					FilePath: "lib.go",
				},
			},
		},
		{
			name:     "TODO comment scanner on unchanged line is ignored",
			filePath: "lib.go",
			content: `package lib
// TODO: implement this function
func todo() {}`,
			changedLines: map[int]bool{
				3: true,
			},
			wantFindings: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAnalyzer()
			got := a.Analyze(tt.filePath, tt.content, tt.changedLines, tt.lineToDiffPos, tt.goVersion)

			if len(got) == 0 && len(tt.wantFindings) == 0 {
				return
			}

			if !reflect.DeepEqual(got, tt.wantFindings) {
				t.Errorf("Analyze() =\n%+v\nwant:\n%+v", got, tt.wantFindings)
			}
		})
	}
}
