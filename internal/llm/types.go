package llm

import analyzerTypes "github.com/Aryan9inja/codebolt/internal/analyzer/types"

// EnhancedFinding represents a finding originated by the LLM pipeline.
// It is intentionally distinct from analyzerTypes.Finding: AST findings
// are deterministic and auditable on their own terms (rule -> line),
// while EnhancedFinding carries LLM-specific provenance (confidence,
// suggested fix) and is never produced by the analyzer.
type EnhancedFinding struct {
	Rule         string
	Severity     analyzerTypes.Severity
	Message      string
	Line         int
	DiffPos      int
	FilePath     string
	Confidence   float64
	SuggestedFix string
}

// detectorCandidate is Detector's raw output shape: a candidate logic/behavioral
// issue, with no fix or confidence yet.
type detectorCandidate struct {
	Category string `json:"category"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

type detectorOutput struct {
	Candidates []detectorCandidate `json:"candidates"`
}

// suggesterItem extends a detectorCandidate with a suggested fix and explanation.
type suggesterItem struct {
	Category     string `json:"category"`
	Line         int    `json:"line"`
	Message      string `json:"message"`
	Explanation  string `json:"explanation"`
	SuggestedFix string `json:"suggested_fix"`
}

type suggesterOutput struct {
	Items []suggesterItem `json:"items"`
}

// reviewerItem is the final, confidence-scored item with a surfacing decision.
type reviewerItem struct {
	Category     string  `json:"category"`
	Line         int     `json:"line"`
	Message      string  `json:"message"`
	Explanation  string  `json:"explanation"`
	SuggestedFix string  `json:"suggested_fix"`
	Confidence   float64 `json:"confidence"`
	Decision     string  `json:"decision"` // "inline", "summary", or "drop"
}

type reviewerOutput struct {
	Items []reviewerItem `json:"items"`
}
