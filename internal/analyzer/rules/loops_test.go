package rules

import (
	"reflect"
	"testing"

	analyzer "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

func TestDeferredCloseInLoop(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "defer directly inside for loop body on changed line",
			content: `package test
func run() {
	for i := 0; i < 5; i++ {
		defer cleanup()
	}
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "defer directly inside range loop body on changed line",
			content: `package test
func run(items []int) {
	for range items {
		defer cleanup()
	}
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name: "defer inside nested block (like if statement) in loop body is NOT flagged, will be checked down the pipeline",
			content: `package test
func run() {
	for i := 0; i < 5; i++ {
		if true {
			defer cleanup()
		}
	}
}`,
			changedLines: map[int]bool{5: true},
			wantLines:    nil, // Loop body list contains *ast.IfStmt, not *ast.DeferStmt directly
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, DeferredCloseInLoop, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "deferred-close-in-a-loop" {
					t.Errorf("expected rule deferred-close-in-a-loop, got %s", f.Rule)
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

func TestTimeAfterInLoop(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name: "time.After directly in for loop",
			content: `package test
import "time"
func run() {
	for {
		_ = time.After(time.Second)
	}
}`,
			changedLines: map[int]bool{5: true},
			wantLines:    []int{5},
		},
		{
			name: "time.After nested in select block inside for loop",
			content: `package test
import "time"
func run(ch chan int) {
	for {
		select {
		case <-ch:
		case <-time.After(time.Second):
		}
	}
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
			got := runRule(t, tt.content, TimeAfterInLoop, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "time-after-in-loop" {
					t.Errorf("expected rule time-after-in-loop, got %s", f.Rule)
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

func TestGoroutineLoopCapture(t *testing.T) {
	tests := []struct {
		name         string
		goVersion    string
		content      string
		changedLines map[int]bool
		wantLines    []int
	}{
		{
			name:      "loop variable captured in closure on Go < 1.22",
			goVersion: "1.21",
			content: `package test
func run() {
	for i := 0; i < 5; i++ {
		go func() {
			println(i)
		}()
	}
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4}, // the go stmt is line 4
		},
		{
			name:      "loop variable captured in range loop closure on Go < 1.22",
			goVersion: "1.21",
			content: `package test
func run(items []int) {
	for _, x := range items {
		go func() {
			println(x)
		}()
	}
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    []int{4},
		},
		{
			name:      "loop variable captured in closure but Go version is >= 1.22",
			goVersion: "1.22",
			content: `package test
func run() {
	for i := 0; i < 5; i++ {
		go func() {
			println(i)
		}()
	}
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
		{
			name:      "loop variable passed as argument (not captured by closure)",
			goVersion: "1.21",
			content: `package test
func run() {
	for i := 0; i < 5; i++ {
		go func(val int) {
			println(val)
		}(i)
	}
}`,
			changedLines: map[int]bool{4: true},
			wantLines:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &analyzer.FileContext{
				FilePath:     "test.go",
				GoVersion:    tt.goVersion,
				ChangedLines: tt.changedLines,
			}
			got := runRule(t, tt.content, GoroutineLoopCapture, ctx)
			var gotLines []int
			for _, f := range got {
				gotLines = append(gotLines, f.Line)
				if f.Rule != "goroutine-loop-capture" {
					t.Errorf("expected rule goroutine-loop-capture, got %s", f.Rule)
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

func TestVersionComparison(t *testing.T) {
	tests := []struct {
		ver       string
		maj, min  int
		wantAtLst bool
	}{
		{"1.22", 1, 22, true},
		{"1.22.3", 1, 22, true},
		{"1.23", 1, 22, true},
		{"1.21", 1, 22, false},
		{"2.0", 1, 22, true},
		{"bad", 1, 22, false},
		{"", 1, 22, false},
	}

	for _, tt := range tests {
		got := goVersionAtLeast(tt.ver, tt.maj, tt.min)
		if got != tt.wantAtLst {
			t.Errorf("goVersionAtLeast(%q, %d, %d) = %t, want %t", tt.ver, tt.maj, tt.min, got, tt.wantAtLst)
		}
	}
}
