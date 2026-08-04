package llm

import "context"

type CompletionRequest struct {
	Model       string
	System      string
	User        string
	MaxTokens   int
	Temperature float64
	JSONMode    bool // hint to the provider to constrain output to JSON
}

type CompletionResponse struct {
	Content string
}

// LLMProvider is implemented by any LLM backend (OpenRouter, Gemini, etc.).
// The agent pipeline only depends on this interface, never on a concrete
// provider, so a provider selector can be introduced later without
// touching pipeline.go or prompts.go.
type LLMProvider interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}
