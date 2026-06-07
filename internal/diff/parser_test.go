package diff

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		rawDiff  string
		expected []FileDiff
	}{
		{
			name: "single file modification",
			rawDiff: `diff --git a/main.go b/main.go
--- a/main.go
+++ b/main.go
@@ -10,3 +10,4 @@
 func main() {
-	fmt.Println("old")
+	fmt.Println("new")
+	return
 }`,
			expected: []FileDiff{
				{
					Path:     "main.go",
					OldPath:  "main.go",
					Language: "go",
					Hunks: []Hunk{
						{
							OldStart: 10, OldLines: 3,
							NewStart: 10, NewLines: 4,
							Lines: []DiffLine{
								{Type: LineContext, Content: "func main() {", OldLine: 10, NewLine: 10, DiffPos: 2},
								{Type: LineRemoved, Content: "	fmt.Println(\"old\")", OldLine: 11, NewLine: 0, DiffPos: 3},
								{Type: LineAdded, Content: "	fmt.Println(\"new\")", OldLine: 0, NewLine: 11, DiffPos: 4},
								{Type: LineAdded, Content: "	return", OldLine: 0, NewLine: 12, DiffPos: 5},
								{Type: LineContext, Content: "}", OldLine: 12, NewLine: 13, DiffPos: 6},
							},
						},
					},
				},
			},
		},
		{
			name: "new file added",
			rawDiff: `diff --git a/new.js b/new.js
new file mode 100644
--- /dev/null
+++ b/new.js
@@ -0,0 +1,2 @@
+console.log("hello");
+console.log("world");`,
			expected: []FileDiff{
				{
					Path:     "new.js",
					OldPath:  "",
					Language: "javascript",
					IsNew:    true,
					Hunks: []Hunk{
						{
							OldStart: 0, OldLines: 0,
							NewStart: 1, NewLines: 2,
							Lines: []DiffLine{
								{Type: LineAdded, Content: "console.log(\"hello\");", OldLine: 0, NewLine: 1, DiffPos: 2},
								{Type: LineAdded, Content: "console.log(\"world\");", OldLine: 0, NewLine: 2, DiffPos: 3},
							},
						},
					},
				},
			},
		},
		{
			name: "file deleted",
			rawDiff: `diff --git a/old.py b/old.py
--- a/old.py
+++ /dev/null
@@ -1,1 +0,0 @@
-print("gone")`,
			expected: []FileDiff{
				{
					Path:     "old.py",
					OldPath:  "old.py",
					IsDelete: true,
					Hunks: []Hunk{
						{
							OldStart: 1, OldLines: 1,
							NewStart: 0, NewLines: 0,
							Lines: []DiffLine{
								{Type: LineRemoved, Content: "print(\"gone\")", OldLine: 1, NewLine: 0, DiffPos: 2},
							},
						},
					},
				},
			},
		},
		{
			name: "file renamed",
			rawDiff: `diff --git a/source.txt b/dest.txt
--- a/source.txt
+++ b/dest.txt
@@ -1 +1 @@
-old text
+new text`,
			expected: []FileDiff{
				{
					Path:     "dest.txt",
					OldPath:  "source.txt",
					Language: "", // .txt not in extToLang
					Hunks: []Hunk{
						{
							OldStart: 1, OldLines: 1, // Defaulting to 1 if omitted
							NewStart: 1, NewLines: 1,
							Lines: []DiffLine{
								{Type: LineRemoved, Content: "old text", OldLine: 1, NewLine: 0, DiffPos: 2},
								{Type: LineAdded, Content: "new text", OldLine: 0, NewLine: 1, DiffPos: 3},
							},
						},
					},
				},
			},
		},
		{
			name: "ignores no newline marker",
			rawDiff: `diff --git a/test.txt b/test.txt
--- a/test.txt
+++ b/test.txt
@@ -1 +1 @@
-old
+new
\ No newline at end of file`,
			expected: []FileDiff{
				{
					Path:    "test.txt",
					OldPath: "test.txt",
					Hunks: []Hunk{
						{
							OldStart: 1,
							OldLines: 1,
							NewStart: 1,
							NewLines: 1,
							Lines: []DiffLine{
								{
									Type:    LineRemoved,
									Content: "old",
									OldLine: 1,
									DiffPos: 2,
								},
								{
									Type:    LineAdded,
									Content: "new",
									NewLine: 1,
									DiffPos: 3,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Parse(tt.rawDiff)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Parse() =\n%+v\nWant:\n%+v", got, tt.expected)
			}
		})
	}
}

func TestFileDiff_AddedLines(t *testing.T) {
	tests := []struct {
		name string
		fd   FileDiff
		want []DiffLine
	}{
		{
			name: "extracts only added lines",
			fd: FileDiff{
				Hunks: []Hunk{
					{
						Lines: []DiffLine{
							{Type: LineContext, Content: "context"},
							{Type: LineAdded, Content: "added 1", NewLine: 10},
							{Type: LineRemoved, Content: "removed 1"},
						},
					},
					{
						Lines: []DiffLine{
							{Type: LineAdded, Content: "added 2", NewLine: 20},
						},
					},
				},
			},
			want: []DiffLine{
				{Type: LineAdded, Content: "added 1", NewLine: 10},
				{Type: LineAdded, Content: "added 2", NewLine: 20},
			},
		},
		{
			name: "no added lines",
			fd: FileDiff{
				Hunks: []Hunk{
					{Lines: []DiffLine{{Type: LineContext}, {Type: LineRemoved}}},
				},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fd.AddedLines()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AddedLines() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFileDiff_ChangedLineNumbers(t *testing.T) {
	tests := []struct {
		name string
		fd   FileDiff
		want map[int]bool
	}{
		{
			name: "collects new line numbers",
			fd: FileDiff{
				Hunks: []Hunk{
					{
						Lines: []DiffLine{
							{Type: LineAdded, NewLine: 15},
							{Type: LineAdded, NewLine: 16},
							{Type: LineRemoved, OldLine: 14}, // Should be ignored
							{Type: LineContext, NewLine: 17}, // Should be ignored
						},
					},
				},
			},
			want: map[int]bool{15: true, 16: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fd.ChangedLineNumbers()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ChangedLineNumbers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLangFromExt(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".go", "go"},
		{".JS", "javascript"}, // tests case insensitivity
		{".unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			if got := langFromExt(tt.ext); got != tt.want {
				t.Errorf("langFromExt(%q) = %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}
