package rules

import (
	"go/parser"
	"go/token"
	"reflect"
	"testing"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

func TestOsExitInLibrary(t *testing.T) {
	tests := []struct {
		name         string
		filePath     string
		isMain       bool
		isTest       bool
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name:     "os.Exit in library file on changed line",
			filePath: "lib.go",
			isMain:   false,
			isTest:   false,
			content: `package lib
import "os"
func run() {
	os.Exit(1)
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name:     "os.Exit in main file is ignored",
			filePath: "main.go",
			isMain:   true,
			isTest:   false,
			content: `package main
import "os"
func main() {
	os.Exit(1)
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
		{
			name:     "os.Exit in test file is ignored",
			filePath: "lib_test.go",
			isMain:   false,
			isTest:   true,
			content: `package lib
import "os"
import "testing"
func TestLib(t *testing.T) {
	os.Exit(1)
}`,
			changedLines: map[int]bool{5: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     tt.filePath,
				IsMain:       tt.isMain,
				IsTest:       tt.isTest,
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, OsExitInLibrary, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "os-exit-in-library" {
					t.Errorf("expected rule name os-exit-in-library, got %s", f.Rule)
				}
				if f.Severity != analyzer.SeverityError {
					t.Errorf("expected SeverityError, got %v", f.Severity)
				}
			}
			if !reflect.DeepEqual(gotLines, tt.wantLines) {
				t.Errorf("got findings on lines %v, want %v", gotLines, tt.wantLines)
			}
		})
	}
}

func TestPanicOutsideMain(t *testing.T) {
	tests := []struct {
		name         string
		filePath     string
		isMain       bool
		isTest       bool
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name:     "panic in library file on changed line",
			filePath: "lib.go",
			isMain:   false,
			isTest:   false,
			content: `package lib
func run() {
	panic("fatal error")
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    []int{3},
		},
		{
			name:     "panic in main file is ignored",
			filePath: "main.go",
			isMain:   true,
			isTest:   false,
			content: `package main
func main() {
	panic("fatal error")
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    nil,
		},
		{
			name:     "panic in test file is ignored",
			filePath: "lib_test.go",
			isMain:   false,
			isTest:   true,
			content: `package lib
import "testing"
func TestLib(t *testing.T) {
	panic("fatal error")
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     tt.filePath,
				IsMain:       tt.isMain,
				IsTest:       tt.isTest,
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, PanicOutsideMain, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "panic-outside-main" {
					t.Errorf("expected rule name panic-outside-main, got %s", f.Rule)
				}
				if f.Severity != analyzer.SeverityWarning {
					t.Errorf("expected SeverityWarning, got %v", f.Severity)
				}
			}
			if !reflect.DeepEqual(gotLines, tt.wantLines) {
				t.Errorf("got findings on lines %v, want %v", gotLines, tt.wantLines)
			}
		})
	}
}

func TestTodoScanner(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "TODO and FIXME comments on changed lines",
			content: `package lib
// TODO: clean up code
func run() {
	// FIXME: fix race condition
	// HACK: bypass validation
	// regular comment
}`,
			changedLines: map[int]bool{2: true, 4: true, 5: true, 6: true},
			wantLines:    []int{2, 4, 5},
		},
		{
			name: "TODO and FIXME comments on unchanged lines",
			content: `package lib
// TODO: clean up code
func run() {
	// FIXME: fix race condition
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "lib.go", tt.content, parser.ParseComments)
			if err != nil {
				t.Fatalf("failed to parse: %v", err)
			}
			ctx := &analyzer.FileContext{
				FilePath:     "lib.go",
				ChangedLines: tt.changedLines,
			}
			got := TodoScanner(f.Comments, fset, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "todo-comment" {
					t.Errorf("expected rule todo-comment, got %s", f.Rule)
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
