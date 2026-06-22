package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	analyzerTypes "github.com/Aryan9inja/codebolt/internal/analyzer/types"
	"github.com/Aryan9inja/codebolt/internal/embeddings"
)

type mockLLMProvider struct {
	completeFn func(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

func (m *mockLLMProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if m.completeFn != nil {
		return m.completeFn(ctx, req)
	}
	return CompletionResponse{}, errors.New("not implemented")
}

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain json object",
			input:    `{"candidates": []}`,
			expected: `{"candidates": []}`,
		},
		{
			name:     "json block with surrounding spaces",
			input:    `   {"candidates": []}   `,
			expected: `{"candidates": []}`,
		},
		{
			name:     "markdown json block",
			input:    "```json\n{\"candidates\": []}\n```",
			expected: `{"candidates": []}`,
		},
		{
			name:     "markdown code block without json tag",
			input:    "```\n{\"candidates\": []}\n```",
			expected: `{"candidates": []}`,
		},
		{
			name:     "surrounding conversational text",
			input:    "Here is the result:\n```json\n{\"candidates\": []}\n```\nHope it helps!",
			expected: `{"candidates": []}`,
		},
		{
			name:     "conversational wrapper without markdown blocks",
			input:    "Sure, I can help. {\"candidates\": []} is the final result.",
			expected: `{"candidates": []}`,
		},
		{
			name:     "no braces in input",
			input:    "no braces here",
			expected: "no braces here",
		},
		{
			name:     "only left brace",
			input:    "some { text",
			expected: "some { text",
		},
		{
			name:     "only right brace",
			input:    "some } text",
			expected: "some } text",
		},
		{
			// Regression: the Suggester LLM returns valid JSON whose
			// "suggested_fix" field contains a ```go ... ``` code fence.
			// cleanJSON must NOT strip the outer JSON - it must only strip
			// fences when the entire response is itself wrapped in a fence.
			name: "json with embedded code fence in string value",
			input: `{
  "items": [
    {
      "category": "out-of-bounds",
      "line": 15,
      "message": "Loop iterates beyond slice length",
      "suggested_fix": "Change to:\n` + "```go\nfor i := 0; i < len(values); i++ {\n    fmt.Println(values[i])\n}\n```" + `"
    }
  ]
}`,
			expected: `{
  "items": [
    {
      "category": "out-of-bounds",
      "line": 15,
      "message": "Loop iterates beyond slice length",
      "suggested_fix": "Change to:\n` + "```go\nfor i := 0; i < len(values); i++ {\n    fmt.Println(values[i])\n}\n```" + `"
    }
  ]
}`,
		},
		{
			// Outer markdown fence wrapping plain JSON (no fences inside values)
			// — the outer fence should be stripped.
			name:     "outer fence wrapping plain json",
			input:    "```json\n{\"category\": \"bug\", \"line\": 10}\n```",
			expected: `{"category": "bug", "line": 10}`,
		},
		{
			name:     "reversed braces",
			input:    "some } left { text",
			expected: "some } left { text",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanJSON(tt.input)
			if got != tt.expected {
				t.Errorf("cleanJSON() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func FuzzCleanJSON(f *testing.F) {
	f.Add(`{"candidates": []}`)
	f.Add("```json\n{\"candidates\": []}\n```")
	f.Add("plain text without braces")
	f.Add("{ unbalanced left")
	f.Add("unbalanced right }")
	f.Add("nested { braces { inside } more } strings")

	f.Fuzz(func(t *testing.T, s string) {
		got := cleanJSON(s)
		// Ensure cleanJSON never panics and returns a trimmed string
		if strings.TrimSpace(got) != got {
			t.Errorf("cleanJSON returned untrimmed string: %q", got)
		}
	})
}

func TestSeverityFromConfidence(t *testing.T) {
	tests := []struct {
		confidence float64
		expected   analyzerTypes.Severity
	}{
		{0.9, analyzerTypes.SeverityError},
		{0.85, analyzerTypes.SeverityError},
		{0.84, analyzerTypes.SeverityWarning},
		{0.7, analyzerTypes.SeverityWarning},
		{0.69, analyzerTypes.SeverityInfo},
		{0.0, analyzerTypes.SeverityInfo},
		{-1.0, analyzerTypes.SeverityInfo},
	}

	for _, tt := range tests {
		t.Run(testing.TB(t).Name(), func(t *testing.T) {
			got := severityFromConfidence(tt.confidence)
			if got != tt.expected {
				t.Errorf("severityFromConfidence(%f) = %v, want %v", tt.confidence, got, tt.expected)
			}
		})
	}
}

func TestBuildPrompts(t *testing.T) {
	t.Run("buildDetectorPrompt - no findings no similar", func(t *testing.T) {
		prompt := buildDetectorPrompt("main.go", "package main", nil, nil)
		if !strings.Contains(prompt, "main.go") {
			t.Errorf("expected prompt to contain file path")
		}
		if !strings.Contains(prompt, "Static analysis findings: none.") {
			t.Errorf("expected empty findings placeholder in detector prompt")
		}
		// No similar findings section should appear when similarFindings is nil.
		if strings.Contains(prompt, "Similar issues found") {
			t.Errorf("expected no similar-findings section when similarFindings is nil")
		}
	})

	t.Run("buildDetectorPrompt - with AST findings", func(t *testing.T) {
		astFindings := []analyzerTypes.Finding{
			{Line: 12, Rule: "panic-rule", Message: "found panic"},
		}
		prompt := buildDetectorPrompt("main.go", "package main", astFindings, nil)
		if !strings.Contains(prompt, "panic-rule") {
			t.Errorf("expected prompt to contain ast findings rule")
		}
		if !strings.Contains(prompt, "package main") {
			t.Errorf("expected prompt to contain content")
		}
		if strings.Contains(prompt, "Similar issues found") {
			t.Errorf("expected no similar-findings section when similarFindings is nil")
		}
	})

	t.Run("buildDetectorPrompt - with similar findings", func(t *testing.T) {
		similar := []embeddings.FindingRecord{
			{Rule: "llm-nil-deref", Message: "possible nil dereference", PRNumber: 42, FilePath: "pkg/foo.go"},
			{Rule: "llm-race", Message: "data race on map", PRNumber: 7, FilePath: "pkg/bar.go"},
		}
		prompt := buildDetectorPrompt("main.go", "package main", nil, similar)
		if !strings.Contains(prompt, "Similar issues found in past PRs") {
			t.Errorf("expected similar-findings header")
		}
		if !strings.Contains(prompt, "llm-nil-deref") || !strings.Contains(prompt, "possible nil dereference") {
			t.Errorf("expected first similar finding to appear in prompt")
		}
		if !strings.Contains(prompt, "PR #42") {
			t.Errorf("expected PR number in similar finding")
		}
		if !strings.Contains(prompt, "pkg/foo.go") {
			t.Errorf("expected file path in similar finding")
		}
	})

	t.Run("buildSuggesterPrompt", func(t *testing.T) {
		candidates := []detectorCandidate{
			{Category: "bug", Line: 5, Message: "suspicious loop"},
		}
		prompt := buildSuggesterPrompt("main.go", "package main", candidates)
		if !strings.Contains(prompt, "main.go") {
			t.Errorf("expected prompt to contain file path")
		}
		if !strings.Contains(prompt, "suspicious loop") {
			t.Errorf("expected prompt to contain candidate message")
		}
		if !strings.Contains(prompt, "package main") {
			t.Errorf("expected prompt to contain content")
		}
	})

	t.Run("buildReviewerPrompt", func(t *testing.T) {
		items := []suggesterItem{
			{Category: "bug", Line: 10, Message: "err check", Explanation: "explain", SuggestedFix: "fix"},
		}
		prompt := buildReviewerPrompt(items)
		if !strings.Contains(prompt, "err check") {
			t.Errorf("expected prompt to contain item message")
		}
		if !strings.Contains(prompt, "explain") {
			t.Errorf("expected prompt to contain explanation")
		}
		if !strings.Contains(prompt, "fix") {
			t.Errorf("expected prompt to contain suggested fix")
		}
	})
}

func TestPipeline_RunForFile(t *testing.T) {
	detectorHappy := `{"candidates": [{"category": "bug", "line": 10, "message": "dangerous condition"}]}`
	suggesterHappy := `{"items": [{"category": "bug", "line": 10, "message": "dangerous condition", "explanation": "explain", "suggested_fix": "fix"}]}`
	reviewerHappy := `{"items": [
		{"category": "bug", "line": 10, "message": "inline finding", "explanation": "explain", "suggested_fix": "fix", "confidence": 0.9, "decision": "inline"},
		{"category": "style", "line": 15, "message": "fallback summary", "explanation": "explain", "suggested_fix": "", "confidence": 0.8, "decision": "inline"},
		{"category": "opt", "line": 20, "message": "pure summary", "explanation": "explain", "suggested_fix": "", "confidence": 0.75, "decision": "summary"},
		{"category": "minor", "line": 30, "message": "dropped finding", "explanation": "explain", "suggested_fix": "", "confidence": 0.5, "decision": "drop"}
	]}`

	tests := []struct {
		name          string
		detectorResp  string
		detectorErr   error
		suggesterResp string
		suggesterErr  error
		reviewerResp  string
		reviewerErr   error
		wantCount     int
		wantErr       string
		verifyResults func(t *testing.T, findings []EnhancedFinding)
	}{
		{
			name:          "happy path - all decision options and fallbacks",
			detectorResp:  detectorHappy,
			suggesterResp: suggesterHappy,
			reviewerResp:  reviewerHappy,
			wantCount:     3, // inline, fallback summary (line 15 not in lineToDiffPos), pure summary. dropped is skipped.
			verifyResults: func(t *testing.T, findings []EnhancedFinding) {
				t.Helper()
				// Expect inline finding on line 10 to resolve to diffPos 5
				f1 := findings[0]
				if f1.Rule != "llm-bug" || f1.Line != 10 || f1.DiffPos != 5 || f1.Severity != analyzerTypes.SeverityError {
					t.Errorf("unexpected inline finding: %+v", f1)
				}

				// Expect fallback summary on line 15 (diffPos 0 because it's not in map)
				f2 := findings[1]
				if f2.Rule != "llm-style" || f2.Line != 15 || f2.DiffPos != 0 || f2.Severity != analyzerTypes.SeverityWarning {
					t.Errorf("unexpected fallback summary finding: %+v", f2)
				}

				// Expect pure summary on line 20
				f3 := findings[2]
				if f3.Rule != "llm-opt" || f3.Line != 20 || f3.DiffPos != 0 || f3.Severity != analyzerTypes.SeverityWarning {
					t.Errorf("unexpected summary finding: %+v", f3)
				}
			},
		},
		{
			name:        "detector error propagation",
			detectorErr: errors.New("detector api timeout"),
			wantErr:     "detector failed for main.go: detector api timeout",
		},
		{
			name:         "detector empty candidates early exit",
			detectorResp: `{"candidates": []}`,
			wantCount:    0,
		},
		{
			name:         "detector invalid json syntax error",
			detectorResp: `{invalid json}`,
			wantErr:      "failed to parse detector output",
		},
		{
			name:         "suggester error propagation",
			detectorResp: detectorHappy,
			suggesterErr: errors.New("suggester failed to complete"),
			wantErr:      "suggester failed for main.go: suggester failed to complete",
		},
		{
			name:          "suggester empty items early exit",
			detectorResp:  detectorHappy,
			suggesterResp: `{"items": []}`,
			wantCount:     0,
		},
		{
			name:          "suggester invalid json syntax error",
			detectorResp:  detectorHappy,
			suggesterResp: `{invalid json}`,
			wantErr:       "failed to parse suggester output",
		},
		{
			name:          "reviewer error propagation",
			detectorResp:  detectorHappy,
			suggesterResp: suggesterHappy,
			reviewerErr:   errors.New("reviewer validation error"),
			wantErr:       "reviewer failed for main.go: reviewer validation error",
		},
		{
			name:          "reviewer invalid json syntax error",
			detectorResp:  detectorHappy,
			suggesterResp: suggesterHappy,
			reviewerResp:  `{invalid json}`,
			wantErr:       "failed to parse reviewer output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			mockProvider := &mockLLMProvider{
				completeFn: func(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
					callCount++
					switch callCount {
					case 1:
						if tt.detectorErr != nil {
							return CompletionResponse{}, tt.detectorErr
						}
						return CompletionResponse{Content: tt.detectorResp}, nil
					case 2:
						if tt.suggesterErr != nil {
							return CompletionResponse{}, tt.suggesterErr
						}
						return CompletionResponse{Content: tt.suggesterResp}, nil
					case 3:
						if tt.reviewerErr != nil {
							return CompletionResponse{}, tt.reviewerErr
						}
						return CompletionResponse{Content: tt.reviewerResp}, nil
					default:
						return CompletionResponse{}, errors.New("too many calls to provider")
					}
				},
			}

			pipeline := NewPipeline(mockProvider, "test-model", nil, nil)
			lineToDiffPos := map[int]int{10: 5}

			findings, err := pipeline.RunForFile(context.Background(), "test/repo", 1, "main.go", "package main", nil, lineToDiffPos)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error %q to contain %q", err.Error(), tt.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if len(findings) != tt.wantCount {
					t.Fatalf("expected %d findings, got %d", tt.wantCount, len(findings))
				}
				if tt.verifyResults != nil {
					tt.verifyResults(t, findings)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestBuildEmbeddingQueryText
// ---------------------------------------------------------------------------

func TestBuildEmbeddingQueryText(t *testing.T) {
	tests := []struct {
		name        string
		filePath    string
		astFindings []analyzerTypes.Finding
		wantContain []string
		wantExact   string // non-empty means exact match
	}{
		{
			name:        "no findings - returns file path only",
			filePath:    "internal/foo/bar.go",
			astFindings: nil,
			wantExact:   "internal/foo/bar.go",
		},
		{
			name:        "empty findings slice - returns file path only",
			filePath:    "cmd/main.go",
			astFindings: []analyzerTypes.Finding{},
			wantExact:   "cmd/main.go",
		},
		{
			name:     "single finding - path: message",
			filePath: "pkg/server.go",
			astFindings: []analyzerTypes.Finding{
				{Message: "potential nil dereference"},
			},
			wantExact: "pkg/server.go: potential nil dereference",
		},
		{
			name:     "multiple findings - joined with semicolon",
			filePath: "pkg/handler.go",
			astFindings: []analyzerTypes.Finding{
				{Message: "context not propagated"},
				{Message: "error ignored"},
				{Message: "panic in library"},
			},
			wantContain: []string{
				"pkg/handler.go: ",
				"context not propagated",
				"error ignored",
				"panic in library",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildEmbeddingQueryText(tt.filePath, tt.astFindings)
			if tt.wantExact != "" && got != tt.wantExact {
				t.Errorf("buildEmbeddingQueryText() = %q, want %q", got, tt.wantExact)
			}
			for _, want := range tt.wantContain {
				if !strings.Contains(got, want) {
					t.Errorf("buildEmbeddingQueryText() = %q, does not contain %q", got, want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// mock embedding provider & store for RunForFile embedding path tests
// ---------------------------------------------------------------------------

type mockEmbeddingProvider struct {
	embedFn func(ctx context.Context, text string) ([]float32, error)
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFn != nil {
		return m.embedFn(ctx, text)
	}
	return nil, errors.New("not implemented")
}

// mockStore wraps the embeddings.Store interface at the function level so we
// don't need a real Postgres connection.  Because embeddings.Store is a
// concrete struct (not an interface) we instead accept a *embeddings.Store
// in the pipeline — so we test the nil-store code path (embeddings skipped)
// in TestPipeline_RunForFile and add a separate test that exercises the
// embedding provider error branch.

func TestPipeline_RunForFile_EmbeddingProviderNilStore_Skipped(t *testing.T) {
	// The pipeline guard: embeddings are only attempted when BOTH embeddingProvider
	// AND store are non-nil. When store is nil the Embed call must be skipped;
	// the pipeline should still complete successfully using just the LLM chain.
	detectorResp := `{"candidates": [{"category": "bug", "line": 5, "message": "off-by-one"}]}`
	suggesterResp := `{"items": [{"category": "bug", "line": 5, "message": "off-by-one", "explanation": "explain", "suggested_fix": "i <= n"}]}`
	reviewerResp := `{"items": [{"category": "bug", "line": 5, "message": "off-by-one", "explanation": "explain", "suggested_fix": "i <= n", "confidence": 0.9, "decision": "inline"}]}`

	callCount := 0
	mockProvider := &mockLLMProvider{
		completeFn: func(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
			callCount++
			switch callCount {
			case 1:
				return CompletionResponse{Content: detectorResp}, nil
			case 2:
				return CompletionResponse{Content: suggesterResp}, nil
			case 3:
				return CompletionResponse{Content: reviewerResp}, nil
			default:
				return CompletionResponse{}, errors.New("too many calls")
			}
		},
	}

	embedCallCount := 0
	mockEmb := &mockEmbeddingProvider{
		embedFn: func(ctx context.Context, text string) ([]float32, error) {
			embedCallCount++
			return nil, errors.New("should never be called")
		},
	}

	// store is nil → the pipeline guard (embeddingProvider != nil && store != nil)
	// evaluates to false, so Embed must NOT be called.
	pipeline := NewPipeline(mockProvider, "test-model", mockEmb, nil)
	lineToDiffPos := map[int]int{5: 2}

	findings, err := pipeline.RunForFile(context.Background(), "test/repo", 99, "main.go", "package main", nil, lineToDiffPos)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].Rule != "llm-bug" {
		t.Errorf("expected Rule=llm-bug, got %q", findings[0].Rule)
	}
	if findings[0].DiffPos != 2 {
		t.Errorf("expected DiffPos=2, got %d", findings[0].DiffPos)
	}
	// Because store == nil the guard short-circuits and Embed is never invoked.
	if embedCallCount != 0 {
		t.Errorf("expected embedding provider NOT to be called when store is nil, got %d calls", embedCallCount)
	}
}

func TestPipeline_RunForFile_NilEmbeddingProvider_Skipped(t *testing.T) {
	// When embeddingProvider itself is nil, the guard also short-circuits.
	// This is the most common production path (embeddings disabled).
	detectorResp := `{"candidates": [{"category": "style", "line": 2, "message": "unused var"}]}`
	suggesterResp := `{"items": [{"category": "style", "line": 2, "message": "unused var", "explanation": "e", "suggested_fix": "s"}]}`
	reviewerResp := `{"items": [{"category": "style", "line": 2, "message": "unused var", "explanation": "e", "suggested_fix": "s", "confidence": 0.72, "decision": "summary"}]}`

	callCount := 0
	mockProvider := &mockLLMProvider{
		completeFn: func(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
			callCount++
			switch callCount {
			case 1:
				return CompletionResponse{Content: detectorResp}, nil
			case 2:
				return CompletionResponse{Content: suggesterResp}, nil
			case 3:
				return CompletionResponse{Content: reviewerResp}, nil
			default:
				return CompletionResponse{}, errors.New("too many calls")
			}
		},
	}

	pipeline := NewPipeline(mockProvider, "test-model", nil, nil)

	findings, err := pipeline.RunForFile(context.Background(), "org/repo", 1, "util.go", "package util", nil, map[int]int{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	// summary decision → DiffPos = 0
	if findings[0].DiffPos != 0 {
		t.Errorf("expected DiffPos=0 for summary, got %d", findings[0].DiffPos)
	}
}

func TestPipeline_RunForFile_RepoAndPRNumberPropagated(t *testing.T) {
	// Verify that repo and prNumber are threaded through to RunForFile without
	// altering review output — observable via the Rule prefix in the returned findings.
	detectorResp := `{"candidates": [{"category": "logic", "line": 3, "message": "wrong condition"}]}`
	suggesterResp := `{"items": [{"category": "logic", "line": 3, "message": "wrong condition", "explanation": "e", "suggested_fix": "s"}]}`
	reviewerResp := `{"items": [{"category": "logic", "line": 3, "message": "wrong condition", "explanation": "e", "suggested_fix": "s", "confidence": 0.75, "decision": "summary"}]}`

	callCount := 0
	mockProvider := &mockLLMProvider{
		completeFn: func(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
			callCount++
			switch callCount {
			case 1:
				return CompletionResponse{Content: detectorResp}, nil
			case 2:
				return CompletionResponse{Content: suggesterResp}, nil
			case 3:
				return CompletionResponse{Content: reviewerResp}, nil
			default:
				return CompletionResponse{}, errors.New("too many calls")
			}
		},
	}

	pipeline := NewPipeline(mockProvider, "test-model", nil, nil)

	findings, err := pipeline.RunForFile(context.Background(), "owner/my-repo", 777, "svc/api.go", "package api", nil, map[int]int{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	f := findings[0]
	if f.Rule != "llm-logic" {
		t.Errorf("Rule = %q, want %q", f.Rule, "llm-logic")
	}
	if f.FilePath != "svc/api.go" {
		t.Errorf("FilePath = %q, want %q", f.FilePath, "svc/api.go")
	}
	// summary decision → DiffPos should be 0.
	if f.DiffPos != 0 {
		t.Errorf("DiffPos = %d, want 0 for summary decision", f.DiffPos)
	}
}

// ---------------------------------------------------------------------------
// BenchmarkBuildEmbeddingQueryText
// ---------------------------------------------------------------------------

func BenchmarkBuildEmbeddingQueryText(b *testing.B) {
	findings := make([]analyzerTypes.Finding, 20)
	for i := range findings {
		findings[i] = analyzerTypes.Finding{Message: "some diagnostic message about the code"}
	}
	b.ResetTimer()
	for range b.N {
		buildEmbeddingQueryText("internal/very/long/package/path/file.go", findings)
	}
}
