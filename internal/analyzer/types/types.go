package types

import "go/token"

type Severity int

const (
	SeverityInfo Severity = iota
	SeverityWarning
	SeverityError
)

func (s Severity) String() string {
	switch s {
	case SeverityInfo:
		return "info"
	case SeverityWarning:
		return "warning"
	case SeverityError:
		return "error"
	default:
		return "unknown"
	}
}

type Finding struct {
	Rule     string // a string identifier for the rule that generated this finding, e.g. "no-unused-vars"
	Severity Severity
	Message  string
	Line     int // line number in the source file (for logging / LLM context)
	DiffPos  int // GitHub Reviews API `position` field - 0 if not on a changed line
	FilePath string
}

// FileContext is read-only context passed to every rule function.
type FileContext struct {
	FilePath      string
	IsMain        bool         // basename == "main.go"
	IsTest        bool         // has _test.go suffix
	ChangedLines  map[int]bool // line numbers touched by this PR (from diff parser)
	LineToDiffPos map[int]int  // mapping from line numbers to diff positions
	GoVersion     string       // module go directive e.g. "1.22" — empty if unknown
}

// RuleFunc is the signature every rule must implement.
// node is guaranteed to be the type the rule registered for.
type RuleFunc func(node any, fset *token.FileSet, ctx *FileContext) []Finding
