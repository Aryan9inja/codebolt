package rules

import (
	"reflect"
	"testing"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

func TestMutexCopiedByValue(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "direct mutex assignment",
			content: `package test
import "sync"
func main() {
	mu := sync.Mutex{}
	_ = mu
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "direct rwmutex assignment",
			content: `package test
import "sync"
func main() {
	mu := sync.RWMutex{}
	_ = mu
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "mutex passed by value in call expression",
			content: `package test
import "sync"
func foo(mu sync.Mutex) {}
func main() {
	foo(sync.Mutex{})
}`,
			changedLines: map[int]bool{5: true},
			wantLines:    []int{5},
		},
		{
			name: "rwmutex passed by value in call expression",
			content: `package test
import "sync"
func foo(mu sync.RWMutex) {}
func main() {
	foo(sync.RWMutex{})
}`,
			changedLines: map[int]bool{5: true},
			wantLines:    []int{5},
		},
		{
			name: "pointer to mutex assignment (should not trigger)",
			content: `package test
import "sync"
func main() {
	mu := &sync.Mutex{}
	_ = mu
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
		{
			name: "pointer to mutex passed to function (should not trigger)",
			content: `package test
import "sync"
func foo(mu *sync.Mutex) {}
func main() {
	foo(&sync.Mutex{})
}`,
			changedLines: map[int]bool{5: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, MutexCopiedByValue, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "mutex-copied-by-value" {
					t.Errorf("expected rule name mutex-copied-by-value, got %s", f.Rule)
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
