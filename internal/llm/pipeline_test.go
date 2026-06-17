package llm

import (
	"context"
	"errors"
	"strings"
	"testing"

	analyzerTypes "github.com/Aryan9inja/codebolt/internal/analyzer/types"
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
	t.Run("buildDetectorPrompt", func(t *testing.T) {
		astFindings := []analyzerTypes.Finding{
			{Line: 12, Rule: "panic-rule", Message: "found panic"},
		}
		prompt := buildDetectorPrompt("main.go", "package main", astFindings)
		if !strings.Contains(prompt, "main.go") {
			t.Errorf("expected prompt to contain file path")
		}
		if !strings.Contains(prompt, "panic-rule") {
			t.Errorf("expected prompt to contain ast findings rule")
		}
		if !strings.Contains(prompt, "package main") {
			t.Errorf("expected prompt to contain content")
		}

		emptyPrompt := buildDetectorPrompt("main.go", "package main", nil)
		if !strings.Contains(emptyPrompt, "Static analysis findings: none.") {
			t.Errorf("expected empty findings placeholder in detector prompt")
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

			pipeline := NewPipeline(mockProvider, "test-model")
			lineToDiffPos := map[int]int{10: 5}

			findings, err := pipeline.RunForFile(context.Background(), "main.go", "package main", nil, lineToDiffPos)
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
