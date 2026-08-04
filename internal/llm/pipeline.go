package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	analyzerTypes "github.com/Aryan9inja/codebolt/internal/analyzer/types"
	"github.com/Aryan9inja/codebolt/internal/embeddings"
)

const (
	defaultMaxTokens = 1024
	// reviewerMaxTokens is larger because the Reviewer echoes every input field
	// (message, explanation, suggested_fix) back in its response before adding
	// confidence and decision — free-tier models frequently hit the 1024 limit
	// mid-JSON when those fields contain multi-line code snippets.
	reviewerMaxTokens  = 2048
	defaultTemperature = 0.2

	maxParseRetries = 2
	retryBaseDelay  = 500 * time.Millisecond
)

// Pipeline runs the Detector -> Suggester -> Reviewer agent sequence
// for a single file's content and AST findings.
// embeddingProvider and store are optional - pass nil for both to run
// without cross-PR context (no Postgres required).
type Pipeline struct {
	provider          LLMProvider
	model             string
	embeddingProvider embeddings.EmbeddingProvider
	store             *embeddings.Store
}

func NewPipeline(provider LLMProvider, model string, embProvider embeddings.EmbeddingProvider, store *embeddings.Store) *Pipeline {
	return &Pipeline{
		provider:          provider,
		model:             model,
		embeddingProvider: embProvider,
		store:             store,
	}
}

// CloneWithOverrides creates a shallow copy of the pipeline with a new model and provider.
// If providerName is set, it attempts to load keys from environment to create the new provider.
func (p *Pipeline) CloneWithOverrides(providerName, model string) *Pipeline {
	clone := *p
	if model != "" {
		clone.model = model
	}
	if providerName != "" {
		switch providerName {
		case "gemini":
			key := os.Getenv("GEMINI_API_KEY")
			if key != "" {
				clone.provider = NewGeminiProvider(key)
			}
		case "openrouter":
			key := os.Getenv("OPENROUTER_API_KEY")
			if key != "" {
				clone.provider = NewOpenRouterProvider(key)
			}
		}
	}
	return &clone
}

// RunForFile executes the full agent pipeline for one file and returns
// EnhancedFindings, with DiffPos resolved via lineToDiffPos.
// repo and prNumber are used for embedding storage/query only - no effect
// on the review logic if embeddings are not configured.
func (p *Pipeline) RunForFile(
	ctx context.Context,
	repo string,
	prNumber int,
	filePath, content string,
	astFindings []analyzerTypes.Finding,
	lineToDiffPos map[int]int,
) ([]EnhancedFinding, error) {
	log.Printf("[llm-pipeline] starting RunForFile for path: %s | content length: %d | AST findings count: %d", filePath, len(content), len(astFindings))

	// 0. Query pgvector for similar past findings in this repo to give Detector
	// additional context. Skipped gracefully if embeddings are not configured.
	var similarFindings []embeddings.FindingRecord
	if p.embeddingProvider != nil && p.store != nil {
		queryText := buildEmbeddingQueryText(filePath, astFindings)
		vec, err := p.embeddingProvider.Embed(ctx, queryText)
		if err != nil {
			log.Printf("[llm-pipeline] embedding query failed for %s: %v (continuing without past-PR context)", filePath, err)
		} else {
			similar, err := p.store.SearchSimilar(ctx, repo, vec, 5)
			if err != nil {
				log.Printf("[llm-pipeline] store search failed for %s: %v (continuing without past-PR context)", filePath, err)
			} else {
				similarFindings = similar
				log.Printf("[llm-pipeline] retrieved %d similar past findings for %s", len(similarFindings), filePath)
			}
		}
	}

	detected, err := p.runDetector(ctx, filePath, content, astFindings, similarFindings)
	if err != nil {
		log.Printf("[llm-pipeline] Detector step failed for %s: %v", filePath, err)
		return nil, fmt.Errorf("detector failed for %s: %w", filePath, err)
	}
	if len(detected.Candidates) == 0 {
		log.Printf("[llm-pipeline] Detector returned 0 candidates for %s | early exit", filePath)
		return nil, nil
	}

	suggested, err := p.runSuggester(ctx, filePath, content, detected.Candidates)
	if err != nil {
		log.Printf("[llm-pipeline] Suggester step failed for %s: %v", filePath, err)
		return nil, fmt.Errorf("suggester failed for %s: %w", filePath, err)
	}
	if len(suggested.Items) == 0 {
		log.Printf("[llm-pipeline] Suggester returned 0 items for %s | early exit", filePath)
		return nil, nil
	}

	reviewed, err := p.runReviewer(ctx, suggested.Items)
	if err != nil {
		log.Printf("[llm-pipeline] Reviewer step failed for %s: %v", filePath, err)
		return nil, fmt.Errorf("reviewer failed for %s: %w", filePath, err)
	}

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

	// Embed and store each non-dropped finding for future cross-PR context.
	// Errors here are non-fatal — review posting is never blocked by embedding failures.
	if p.embeddingProvider != nil && p.store != nil {
		for _, finding := range results {
			embText := fmt.Sprintf("[%s] %s. Fix: %s", finding.Rule, finding.Message, finding.SuggestedFix)
			vec, err := p.embeddingProvider.Embed(ctx, embText)
			if err != nil {
				log.Printf("[llm-pipeline] embed failed for finding %s in %s: %v (skipping save)", finding.Rule, filePath, err)
				continue
			}
			rec := embeddings.FindingRecord{
				Repo:      repo,
				FilePath:  filePath,
				Rule:      finding.Rule,
				Message:   finding.Message,
				PRNumber:  prNumber,
				Embedding: vec,
			}
			if err := p.store.SaveFinding(ctx, rec); err != nil {
				log.Printf("[llm-pipeline] store save failed for finding %s in %s: %v (non-fatal)", finding.Rule, filePath, err)
			}
		}
	}

	log.Printf("[llm-pipeline] completed RunForFile for %s | returning %d findings", filePath, len(results))
	return results, nil
}

func (p *Pipeline) runDetector(ctx context.Context, filePath, content string, astFindings []analyzerTypes.Finding, similarFindings []embeddings.FindingRecord) (detectorOutput, error) {
	log.Printf("[llm-pipeline] running Detector for %s", filePath)
	resp, err := p.provider.Complete(ctx, CompletionRequest{
		Model:       p.model,
		System:      detectorSystemPrompt,
		User:        buildDetectorPrompt(filePath, content, astFindings, similarFindings),
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

	prompt := buildReviewerPrompt(items)
	var (
		lastRaw string
		lastErr error
	)

	for attempt := 0; attempt <= maxParseRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay * (1 << (attempt - 1)) // 500ms, 1s, …
			log.Printf("[llm-pipeline] Reviewer retry %d/%d after %v (parse error: %v)", attempt, maxParseRetries, delay, lastErr)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return reviewerOutput{}, ctx.Err()
			}
		}

		resp, err := p.provider.Complete(ctx, CompletionRequest{
			Model:       p.model,
			System:      reviewerSystemPrompt,
			User:        prompt,
			MaxTokens:   reviewerMaxTokens,
			Temperature: defaultTemperature,
			JSONMode:    true,
		})
		if err != nil {
			return reviewerOutput{}, err
		}

		log.Printf("[llm-pipeline] Reviewer raw response (attempt %d):\n%s", attempt+1, resp.Content)
		cleaned := cleanJSON(resp.Content)
		log.Printf("[llm-pipeline] Reviewer cleaned response (attempt %d):\n%s", attempt+1, cleaned)

		var out reviewerOutput
		if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
			log.Printf("[llm-pipeline] Reviewer failed to parse JSON (attempt %d): %v", attempt+1, err)
			lastRaw = resp.Content
			lastErr = err
			continue
		}

		log.Printf("[llm-pipeline] Reviewer succeeded | items returned: %d", len(out.Items))
		return out, nil
	}

	// All retries exhausted — fall back to promoting the Suggester items
	// directly so findings are never silently dropped. Each item gets a
	// conservative confidence of 0.75 (→ SeverityWarning) and decision
	// "inline" so they still surface as PR comments.
	log.Printf("[llm-pipeline] Reviewer failed after %d attempts, falling back to Suggester output (last error: %v, raw: %s)",
		maxParseRetries+1, lastErr, lastRaw)
	fallbackItems := make([]reviewerItem, 0, len(items))
	for _, it := range items {
		fallbackItems = append(fallbackItems, reviewerItem{
			Category:     it.Category,
			Line:         it.Line,
			Message:      it.Message,
			Explanation:  it.Explanation,
			SuggestedFix: it.SuggestedFix,
			Confidence:   0.75,
			Decision:     "inline",
		})
	}
	return reviewerOutput{Items: fallbackItems}, nil
}

// buildEmbeddingQueryText builds the text to embed for the pre-Detector pgvector
// search. Uses AST finding messages as the semantic signal - richer than a bare
// file path, avoids sending the full file to the embedding model.
func buildEmbeddingQueryText(filePath string, astFindings []analyzerTypes.Finding) string {
	if len(astFindings) == 0 {
		return filePath
	}
	msgs := make([]string, 0, len(astFindings))
	for _, f := range astFindings {
		msgs = append(msgs, f.Message)
	}
	return filePath + ": " + strings.Join(msgs, "; ")
}

// cleanJSON extracts valid JSON content from model output, handling markdown
// code fences and arbitrary conversational text surrounding the JSON object.
//
// Important: fence-stripping is only performed when the response itself is
// wrapped in a top-level code fence (i.e. the trimmed string starts with
// "```"). If backtick fences appear only inside JSON string values (e.g.
// inside a "suggested_fix" field) the outer JSON must not be disturbed.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "```") {
		remainder := s[3:]
		remainder = strings.TrimPrefix(remainder, "json")
		endIdx := strings.Index(remainder, "```")
		if endIdx != -1 {
			s = remainder[:endIdx]
		}
	}

	s = strings.TrimSpace(s)

	firstBrace := strings.Index(s, "{")
	lastBrace := strings.LastIndex(s, "}")
	if firstBrace != -1 && lastBrace != -1 && lastBrace > firstBrace {
		s = s[firstBrace : lastBrace+1]
	}

	return s
}

// severityFromConfidence maps a confidence score to a Severity for
// EnhancedFinding, so it flows through the same comment-building logic as AST findings.
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
