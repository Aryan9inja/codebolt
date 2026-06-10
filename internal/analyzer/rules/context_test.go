package rules

import (
	"reflect"
	"testing"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

func TestContextNotPropagated(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "http.Get called without context parameter on changed line",
			content: `package test
import (
	"context"
	"net/http"
)
func Fetch(ctx context.Context) {
	_, _ = http.Get("https://example.com")
}`,
			changedLines: map[int]bool{7: true},
			wantLines:    []int{7},
		},
		{
			name: "http.Get called with custom context parameter name (propagated)",
			content: `package test
import (
	"context"
	"net/http"
)
func Fetch(myCtx context.Context) {
	// Simple heuristic: if first arg is 'myCtx', it is assumed correct.
	// (Note: in actual Go, http.Get doesn't take a context, but we check if it is passed)
	_, _ = http.Get(myCtx, "https://example.com")
}`,
			changedLines: map[int]bool{8: true},
			wantLines:    nil,
		},
		{
			name: "http.Get called with custom context parameter name (not propagated)",
			content: `package test
import (
	"context"
	"net/http"
)
func Fetch(myCtx context.Context) {
	_, _ = http.Get("https://example.com")
}`,
			changedLines: map[int]bool{7: true},
			wantLines:    []int{7},
		},
		{
			name: "sql.Query called without context parameter",
			content: `package test
import (
	"context"
	"database/sql"
)
func QueryData(ctx context.Context) {
	_, _ = sql.Query("SELECT * FROM users")
}`,
			changedLines: map[int]bool{7: true},
			wantLines:    []int{7},
		},
		{
			name: "function has no context parameter",
			content: `package test
import "net/http"
func Fetch() {
	_, _ = http.Get("https://example.com")
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
		{
			name: "anonymous context parameter (does not panic)",
			content: `package test
import (
	"context"
	"net/http"
)
func Fetch(context.Context) {
	_, _ = http.Get("https://example.com")
}`,
			changedLines: map[int]bool{7: true},
			wantLines:    []int{7},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, ContextNotPropagated, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "context-not-propagated" {
					t.Errorf("expected rule name context-not-propagated, got %s", f.Rule)
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
