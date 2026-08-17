package llm

import (
	"context"
	"testing"
)

func TestCloneWithOverrides(t *testing.T) {
	origProvider := &mockLLMProvider{
		completeFn: func(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
			return CompletionResponse{Content: req.Model}, nil
		},
	}
	p := NewPipeline(origProvider, "original-model", nil, nil)

	// Clone with a new model
	clone := p.CloneWithOverrides("", "new-model")
	
	if clone.model != "new-model" {
		t.Errorf("expected clone.model to be 'new-model', got %q", clone.model)
	}
	if clone.provider != origProvider {
		t.Errorf("expected clone.provider to be original provider")
	}

	// Verify the original is untouched
	if p.model != "original-model" {
		t.Errorf("expected original p.model to remain 'original-model'")
	}
}
