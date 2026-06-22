package llm

import (
	"fmt"
	"strings"

	analyzerTypes "github.com/Aryan9inja/codebolt/internal/analyzer/types"
	"github.com/Aryan9inja/codebolt/internal/embeddings"
)

const detectorSystemPrompt = `You are the Detector agent in a Go code review pipeline.
Static analysis (AST-based) has already run on this file and found the issues listed below.
Do NOT re-report any of those - they are already covered.

Your job is ONLY to find NEW issues that static analysis cannot catch:
logic errors, incorrect conditions, wrong variable usage, behavioral bugs,
edge cases, or mismatches between code intent and implementation.

Do NOT report style, formatting, or structural issues - that is static analysis's job.

Respond ONLY with a JSON object of this exact shape, with no extra text:
{"candidates": [{"category": "short-category-slug", "line": <int>, "message": "<short description, max 200 chars>"}]}

If you find nothing, respond with {"candidates": []}. Keep the list short - only flag issues you are reasonably confident about.`

const suggesterSystemPrompt = `You are the Suggester agent in a Go code review pipeline.
You will be given a list of candidate issues found by the Detector agent, along with the source file.

For each candidate, provide a brief explanation (max 300 chars) of why it matters and a concise suggested fix
(a short code snippet or diff-like change, max 400 chars - not a full file rewrite).

Respond ONLY with a JSON object of this exact shape, with no extra text:
{"items": [{"category": "...", "line": <int>, "message": "...", "explanation": "...", "suggested_fix": "..."}]}

Preserve the category, line, and message fields from the input exactly.`

const reviewerSystemPrompt = `You are the Reviewer agent in a Go code review pipeline - the final quality gate.
You will be given a list of candidate issues with explanations and suggested fixes from the Suggester agent.

For each item, assign:
- "confidence": a float 0.0-1.0 reflecting how likely this is a genuine, actionable issue.
- "decision": one of "inline" (high-confidence, comment on the specific line), "summary" (worth mentioning but
  not confident enough for an inline comment), or "drop" (not worth surfacing - false positive or trivial).

Be conservative: only mark "inline" for issues you are quite confident about (typically confidence >= 0.7).

Respond ONLY with a JSON object of this exact shape, with no extra text:
{"items": [{"category": "...", "line": <int>, "message": "...", "explanation": "...", "suggested_fix": "...", "confidence": <float>, "decision": "inline|summary|drop"}]}

Preserve all input fields exactly; only add "confidence" and "decision".`

// buildDetectorPrompt formats the user prompt for the Detector agent.
// similarFindings from pgvector are injected as additional context when available.
func buildDetectorPrompt(filePath, content string, astFindings []analyzerTypes.Finding, similarFindings []embeddings.FindingRecord) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "File: %s\n\n", filePath)

	if len(astFindings) == 0 {
		sb.WriteString("Static analysis findings: none.\n\n")
	} else {
		sb.WriteString("Static analysis findings (already covered, do not re-report):\n")
		for _, f := range astFindings {
			fmt.Fprintf(&sb, "- line %d: [%s] %s\n", f.Line, f.Rule, f.Message)
		}
		sb.WriteString("\n")
	}

	if len(similarFindings) > 0 {
		sb.WriteString("Similar issues found in past PRs in this repo (treat as context, not ground truth — they may or may not apply here):\n")
		for _, f := range similarFindings {
			fmt.Fprintf(&sb, "- [%s] %s (PR #%d, file: %s)\n", f.Rule, f.Message, f.PRNumber, f.FilePath)
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Source file:\n```go\n")
	sb.WriteString(content)
	sb.WriteString("\n```\n")

	return sb.String()
}

// buildSuggesterPrompt formats the user prompt for the Suggester agent.
func buildSuggesterPrompt(filePath, content string, candidates []detectorCandidate) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "File: %s\n\n", filePath)

	sb.WriteString("Candidate issues:\n")
	for _, c := range candidates {
		fmt.Fprintf(&sb, "- line %d: [%s] %s\n", c.Line, c.Category, c.Message)
	}
	sb.WriteString("\n")

	sb.WriteString("Source file:\n```go\n")
	sb.WriteString(content)
	sb.WriteString("\n```\n")

	return sb.String()
}

// buildReviewerPrompt formats the user prompt for the Reviewer agent.
func buildReviewerPrompt(items []suggesterItem) string {
	var sb strings.Builder

	sb.WriteString("Candidate issues with suggested fixes:\n")
	for _, it := range items {
		fmt.Fprintf(&sb, "- line %d: [%s] %s\n  explanation: %s\n  suggested_fix: %s\n",
			it.Line, it.Category, it.Message, it.Explanation, it.SuggestedFix)
	}

	return sb.String()
}
