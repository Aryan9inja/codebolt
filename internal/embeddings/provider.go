package embeddings

import "context"

// EmbeddingProvider mirrors llm.LLMProvider's seam - pipeline/store code
// depends only on this interface, never on Gemini specifics.
type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}