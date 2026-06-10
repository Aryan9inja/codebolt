package rules

import (
	"reflect"
	"testing"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

func TestErrorIgnored(t *testing.T) {
	// ErrorIgnored is currently a placeholder returning nil.
	// We verify it does not panic and returns nil.
	ctx := &analyzer.FileContext{
		FilePath:     "test.go",
		ChangedLines: map[int]bool{3: true},
	}
	content := `package test
func run() {
	foo()
}`
	got := runRule(t, content, ErrorIgnored, ctx)
	if len(got) != 0 {
		t.Errorf("expected 0 findings from placeholder ErrorIgnored, got %d", len(got))
	}
}

func TestEmptyErrorCatch(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "empty err != nil block",
			content: `package test
func run(err error) {
	if err != nil {
	}
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    []int{3},
		},
		{
			name: "non-empty err != nil block",
			content: `package test
func run(err error) {
	if err != nil {
		panic(err)
	}
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    nil,
		},
		{
			name: "other comparison operator (err == nil) empty block",
			content: `package test
func run(err error) {
	if err == nil {
	}
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    nil,
		},
		{
			name: "other variable name empty block",
			content: `package test
func run(otherErr error) {
	if otherErr != nil {
	}
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, EmptyErrorCatch, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "empty-error-catch" {
					t.Errorf("expected rule empty-error-catch, got %s", f.Rule)
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

func TestNakedReturn(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "naked return in function with named return values",
			content: `package test
func get() (result int) {
	result = 42
	return
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "explicit return in function with named return values",
			content: `package test
func get() (result int) {
	return 42
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    nil,
		},
		{
			name: "naked return in function with unnamed return values",
			content: `package test
func get() int {
	return 42
}`,
			changedLines: map[int]bool{3: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, NakedReturn, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "naked-return" {
					t.Errorf("expected rule naked-return, got %s", f.Rule)
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

func TestMissingCancel(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "cancel function discarded with _ in WithCancel",
			content: `package test
import "context"
func run(ctx context.Context) {
	ctx, _ = context.WithCancel(ctx)
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "cancel function discarded with _ in WithTimeout",
			content: `package test
import (
	"context"
	"time"
)
func run(ctx context.Context) {
	ctx, _ = context.WithTimeout(ctx, time.Second)
}`,
			changedLines: map[int]bool{7: true},
			wantLines:    []int{7},
		},
		{
			name: "cancel function assigned correctly",
			content: `package test
import "context"
func run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
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
			got := runRule(t, tt.content, MissingCancel, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "missing-cancel" {
					t.Errorf("expected rule missing-cancel, got %s", f.Rule)
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
