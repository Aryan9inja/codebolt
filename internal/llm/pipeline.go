package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	analyzerTypes "github.com/Aryan9inja/codebolt/internal/analyzer/types"
)

const (
	defaultMaxTokens   = 1024
	defaultTemperature = 0.2
)

// Pipeline runs the Detector -> Suggester -> Reviewer agent sequence
// for a single file's content and AST findings.
type Pipeline struct {
	provider LLMProvider
	model    string
}

func NewPipeline(provider LLMProvider, model string) *Pipeline {
	return &Pipeline{provider: provider, model: model}
}

// RunForFile executes the full agent pipeline for one file and returns
// EnhancedFindings, with DiffPos resolved via lineToDiffPos.
// Findings with decision "drop" are discarded; "summary" findings get
// DiffPos == 0 so they are routed to the review body, same as AST
// file-level findings.
func (p *Pipeline) RunForFile(
	ctx context.Context,
	filePath, content string,
	astFindings []analyzerTypes.Finding,
	lineToDiffPos map[int]int,
) ([]EnhancedFinding, error) {
	log.Printf("[llm-pipeline] starting RunForFile for path: %s | content length: %d | AST findings count: %d", filePath, len(content), len(astFindings))

	// 1. Detector
	detected, err := p.runDetector(ctx, filePath, content, astFindings)
	if err != nil {
		log.Printf("[llm-pipeline] Detector step failed for %s: %v", filePath, err)
		return nil, fmt.Errorf("detector failed for %s: %w", filePath, err)
	}
	if len(detected.Candidates) == 0 {
		log.Printf("[llm-pipeline] Detector returned 0 candidates for %s | early exit", filePath)
		return nil, nil
	}

	// 2. Suggester
	suggested, err := p.runSuggester(ctx, filePath, content, detected.Candidates)
	if err != nil {
		log.Printf("[llm-pipeline] Suggester step failed for %s: %v", filePath, err)
		return nil, fmt.Errorf("suggester failed for %s: %w", filePath, err)
	}
	if len(suggested.Items) == 0 {
		log.Printf("[llm-pipeline] Suggester returned 0 items for %s | early exit", filePath)
		return nil, nil
	}

	// 3. Reviewer
	reviewed, err := p.runReviewer(ctx, suggested.Items)
	if err != nil {
		log.Printf("[llm-pipeline] Reviewer step failed for %s: %v", filePath, err)
		return nil, fmt.Errorf("reviewer failed for %s: %w", filePath, err)
	}

	// 4. Convert to EnhancedFinding, resolving DiffPos and applying decisions.
	log.Printf("[llm-pipeline] processing reviewed items for %s | items received: %d", filePath, len(reviewed.Items))
	results := make([]EnhancedFinding, 0, len(reviewed.Items))
	for idx, item := range reviewed.Items {
		log.Printf("[llm-pipeline] [%s] item %d: line=%d, category=%s, decision=%s, confidence=%.2f, message=%q",
			filePath, idx, item.Line, item.Category, item.Decision, item.Confidence, item.Message)

		if item.Decision == "drop" {
			log.Printf("[llm-pipeline] [%s] item %d was dropped", filePath, idx)
			continue
		}

		diffPos := 0
		if item.Decision == "inline" {
			diffPos = lineToDiffPos[item.Line]
			if diffPos == 0 {
				log.Printf("[llm-pipeline] [%s] item %d on line %d has diffPos = 0, falling back to summary note", filePath, idx, item.Line)
				// No diff position for this line (e.g. unchanged context line) -
				// fall back to a file-level summary note instead of dropping it.
				item.Decision = "summary"
			} else {
				log.Printf("[llm-pipeline] [%s] item %d resolved line %d to diffPos %d", filePath, idx, item.Line, diffPos)
			}
		}

		results = append(results, EnhancedFinding{
			Rule:         "llm-" + item.Category,
			Severity:     severityFromConfidence(item.Confidence),
			Message:      item.Message,
			Line:         item.Line,
			DiffPos:      diffPos,
			FilePath:     filePath,
			Confidence:   item.Confidence,
			SuggestedFix: item.SuggestedFix,
		})
	}

	log.Printf("[llm-pipeline] completed RunForFile for %s | returning %d findings", filePath, len(results))
	return results, nil
}

func (p *Pipeline) runDetector(ctx context.Context, filePath, content string, astFindings []analyzerTypes.Finding) (detectorOutput, error) {
	log.Printf("[llm-pipeline] running Detector for %s", filePath)
	resp, err := p.provider.Complete(ctx, CompletionRequest{
		Model:       p.model,
		System:      detectorSystemPrompt,
		User:        buildDetectorPrompt(filePath, content, astFindings),
		MaxTokens:   defaultMaxTokens,
		Temperature: defaultTemperature,
		JSONMode:    true,
	})
	if err != nil {
		return detectorOutput{}, err
	}

	log.Printf("[llm-pipeline] Detector raw response for %s:\n%s", filePath, resp.Content)
	cleaned := cleanJSON(resp.Content)
	log.Printf("[llm-pipeline] Detector cleaned response for %s:\n%s", filePath, cleaned)

	var out detectorOutput
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		log.Printf("[llm-pipeline] Detector failed to parse JSON: %v", err)
		return detectorOutput{}, fmt.Errorf("failed to parse detector output: %w (raw: %s)", err, resp.Content)
	}
	log.Printf("[llm-pipeline] Detector succeeded for %s | candidates found: %d", filePath, len(out.Candidates))
	return out, nil
}

func (p *Pipeline) runSuggester(ctx context.Context, filePath, content string, candidates []detectorCandidate) (suggesterOutput, error) {
	log.Printf("[llm-pipeline] running Suggester for %s | candidates: %d", filePath, len(candidates))
	resp, err := p.provider.Complete(ctx, CompletionRequest{
		Model:       p.model,
		System:      suggesterSystemPrompt,
		User:        buildSuggesterPrompt(filePath, content, candidates),
		MaxTokens:   defaultMaxTokens,
		Temperature: defaultTemperature,
		JSONMode:    true,
	})
	if err != nil {
		return suggesterOutput{}, err
	}

	log.Printf("[llm-pipeline] Suggester raw response for %s:\n%s", filePath, resp.Content)
	cleaned := cleanJSON(resp.Content)
	log.Printf("[llm-pipeline] Suggester cleaned response for %s:\n%s", filePath, cleaned)

	var out suggesterOutput
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		log.Printf("[llm-pipeline] Suggester failed to parse JSON: %v", err)
		return suggesterOutput{}, fmt.Errorf("failed to parse suggester output: %w (raw: %s)", err, resp.Content)
	}
	log.Printf("[llm-pipeline] Suggester succeeded for %s | items returned: %d", filePath, len(out.Items))
	return out, nil
}

func (p *Pipeline) runReviewer(ctx context.Context, items []suggesterItem) (reviewerOutput, error) {
	log.Printf("[llm-pipeline] running Reviewer with %d items", len(items))
	resp, err := p.provider.Complete(ctx, CompletionRequest{
		Model:       p.model,
		System:      reviewerSystemPrompt,
		User:        buildReviewerPrompt(items),
		MaxTokens:   defaultMaxTokens,
		Temperature: defaultTemperature,
		JSONMode:    true,
	})
	if err != nil {
		return reviewerOutput{}, err
	}

	log.Printf("[llm-pipeline] Reviewer raw response:\n%s", resp.Content)
	cleaned := cleanJSON(resp.Content)
	log.Printf("[llm-pipeline] Reviewer cleaned response:\n%s", cleaned)

	var out reviewerOutput
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		log.Printf("[llm-pipeline] Reviewer failed to parse JSON: %v", err)
		return reviewerOutput{}, fmt.Errorf("failed to parse reviewer output: %w (raw: %s)", err, resp.Content)
	}
	log.Printf("[llm-pipeline] Reviewer succeeded | items returned: %d", len(out.Items))
	return out, nil
}

// cleanJSON extracts valid JSON content from model output, handling markdown code fences
// and arbitrary conversational text surrounding the JSON object.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)

	// If it contains a markdown code block, try to isolate its content
	if strings.Contains(s, "```") {
		startIdx := strings.Index(s, "```")
		if startIdx != -1 {
			remainder := s[startIdx+3:]
			// Drop a leading "json" language hint if present (unconditionally trim prefix).
			remainder = strings.TrimPrefix(remainder, "json")
			endIdx := strings.Index(remainder, "```")
			if endIdx != -1 {
				s = remainder[:endIdx]
			}
		}
	}

	s = strings.TrimSpace(s)

	// Robust fallback: locate the first '{' and the last '}' to extract the core JSON object
	firstBrace := strings.Index(s, "{")
	lastBrace := strings.LastIndex(s, "}")
	if firstBrace != -1 && lastBrace != -1 && lastBrace > firstBrace {
		s = s[firstBrace : lastBrace+1]
	}

	return s
}

// severityFromConfidence maps a confidence score to a Severity for
// EnhancedFinding, so it can flow through the same comment-building
// logic as AST findings.
func severityFromConfidence(confidence float64) analyzerTypes.Severity {
	switch {
	case confidence >= 0.85:
		return analyzerTypes.SeverityError
	case confidence >= 0.7:
		return analyzerTypes.SeverityWarning
	default:
		return analyzerTypes.SeverityInfo
	}
}
