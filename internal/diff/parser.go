package diff

import (
	"path/filepath"
	"strings"
)

type LineType int

const (
	LineAdded LineType = iota
	LineRemoved
	LineContext
)

type DiffLine struct {
	Type    LineType
	Content string // line content without the leading +, -, or space
	OldLine int    // 0 if added line, otherwise the line number in the old file
	NewLine int    // 0 if removed line, otherwise the line number in the new file

	// DiffPosition is the 1-based sequential position of this line within
	// the file's diff output (counting hunk headers too). This is what
	// GitHub's Review Comments API wants in the `position` field
	// Not the file line number.
	DiffPos int
}

type Hunk struct {
	OldStart int
	OldLines int
	NewStart int
	NewLines int
	Lines    []DiffLine
}

type FileDiff struct {
	Path     string // path in new file (b/ side); old path if deleted
	OldPath  string // path in old file (a/ side); empty if new file
	Language string // derived from extension, empty if unknown
	IsNew    bool
	IsDelete bool
	Hunks    []Hunk
}

// Parse parses a unified diff string and returns a slice of FileDiffs.
// It handles standard git diffs including renames and binary file notices.
func Parse(raw string) []FileDiff {
	var files []FileDiff
	var currFile *FileDiff
	var currHunk *Hunk
	diffPos := 0 // resets for each file

	oldLine := 0
	newLine := 0

	lines := strings.SplitSeq(raw, "\n")
	for line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			// Flush previous hunk and file
			if currHunk != nil && currFile != nil {
				currFile.Hunks = append(currFile.Hunks, *currHunk)
				currHunk = nil
			}
			if currFile != nil {
				files = append(files, *currFile)
			}
			currFile = &FileDiff{}
			diffPos = 0

		case currFile == nil:
			// Haven't seen a diff header yet, skip
			continue

		case strings.HasPrefix(line, "---"):
			oldFilePath := strings.TrimPrefix(line, "--- ")
			if oldFilePath != "/dev/null" {
				currFile.OldPath = strings.TrimPrefix(oldFilePath, "a/")
			}

		case strings.HasPrefix(line, "+++"):
			newFilePath := strings.TrimPrefix(line, "+++ ")
			if newFilePath == "/dev/null" {
				currFile.IsDelete = true

				// Keep the old path as the main path for deleted files
				if currFile.Path == "" {
					currFile.Path = currFile.OldPath
				}
			} else {
				currFile.Path = strings.TrimPrefix(newFilePath, "b/")
				currFile.Language = langFromExt(filepath.Ext(currFile.Path))
			}
			// Rename: oldFilePath != newFilePath and both are not /dev/null
			if currFile.OldPath != "" && currFile.OldPath != currFile.Path {
				// Old path is already set; Path is set to new path.
				// No special handling needed here since we already have both paths.
			}

		case strings.HasPrefix(line, "new file mode"):
			currFile.IsNew = true

		case strings.HasPrefix(line, "@@ "):
			// Flush previous hunk
			if currHunk != nil {
				currFile.Hunks = append(currFile.Hunks, *currHunk)
			}
			currHunk = &Hunk{}
			diffPos++ // hunk header counts as a line in the diff output
			parseHunkHeader(line, currHunk)
			oldLine = currHunk.OldStart
			newLine = currHunk.NewStart

		case line == `\ No newline at end of file`:
			continue

		case currHunk != nil:
			if len(line) == 0 {
				continue
			}

			diffPos++
			dl := DiffLine{
				Content: line[1:], // strip the leading +, -, or space
				DiffPos: diffPos,
			}

			switch {
			case strings.HasPrefix(line, "+"):
				dl.Type = LineAdded
				dl.NewLine = newLine
				newLine++

			case strings.HasPrefix(line, "-"):
				dl.Type = LineRemoved
				dl.OldLine = oldLine
				oldLine++

			default: // context line (space prefix) or empty line (no prefix)
				dl.Type = LineContext
				dl.OldLine = oldLine
				dl.NewLine = newLine
				oldLine++
				newLine++
			}

			currHunk.Lines = append(currHunk.Lines, dl)
		}
	}

	// Flush last file and hunk
	if currHunk != nil && currFile != nil {
		currFile.Hunks = append(currFile.Hunks, *currHunk)
		currHunk = nil
	}
	if currFile != nil {
		files = append(files, *currFile)
		currFile = nil
	}

	return files
}

// AddedLines returns only the added lines across all hunks.
// Useful for feeding just the new code into the AST analyzer.
func (f *FileDiff) AddedLines() []DiffLine {
	var out []DiffLine
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.Type == LineAdded {
				out = append(out, l)
			}
		}
	}
	return out
}

// ChangedLineNumbers returns the set of new-file line numbers that were
// added or modified. Used to filter AST findings to only changed code.
func (f *FileDiff) ChangedLineNumbers() map[int]bool {
	out := make(map[int]bool)
	for _, h := range f.Hunks {
		for _, l := range h.Lines {
			if l.Type == LineAdded && l.NewLine > 0 {
				out[l.NewLine] = true
			}
		}
	}
	return out
}

func parseHunkHeader(line string, h *Hunk) {
	// line looks like: "@@ -10,6 +10,12 @@ func Something() {"
	inner := strings.TrimSuffix(line, "@@ ")
	inner, _, _ = strings.Cut(line[3:], " @@")

	parts := strings.Fields(inner) // ["-10,6", "+10,12"]
	if len(parts) != 2 {
		return
	}

	h.OldStart, h.OldLines = parseRange(parts[0][1:]) // skip the leading '-'
	h.NewStart, h.NewLines = parseRange(parts[1][1:]) // skip the leading '+'
}

func parseRange(s string) (start, count int) {
	before, after, found := strings.Cut(s, ",")
	start = atoi(before)
	if !found {
		count = 1
	} else {
		count = atoi(after)
	}

	return
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

var extToLang = map[string]string{
	".go":   "go",
	".js":   "javascript",
	".ts":   "typescript",
	".jsx":  "javascript",
	".tsx":  "typescript",
	".py":   "python",
	".rb":   "ruby",
	".java": "java",
	".rs":   "rust",
	".c":    "c",
	".cpp":  "cpp",
	".h":    "c",
	".cs":   "csharp",
	".php":  "php",
	".sh":   "bash",
	".yaml": "yaml",
	".yml":  "yaml",
	".json": "json",
	".md":   "markdown",
	".sql":  "sql",
}

func langFromExt(ext string) string {
	return extToLang[strings.ToLower(ext)]
}
