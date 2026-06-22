package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	geminiEmbedModel = "gemini-embedding-001"
	geminiEmbedURL   = "https://generativelanguage.googleapis.com/v1beta/models/" + geminiEmbedModel + ":embedContent"
	outputDimensions = 768 // MRL-truncated; stays within pgvector's practical index dimension ceiling
)

type GeminiEmbeddingProvider struct {
	apiKey string
	client *http.Client
}

func NewGeminiEmbeddingProvider(apiKey string) *GeminiEmbeddingProvider {
	return &GeminiEmbeddingProvider{
		apiKey: apiKey,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type geminiEmbedRequest struct {
	Model                string        `json:"model"`
	Content              geminiContent `json:"content"`
	OutputDimensionality int           `json:"outputDimensionality"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

func (p *GeminiEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := geminiEmbedRequest{
		Model:                "models/" + geminiEmbedModel,
		Content:              geminiContent{Parts: []geminiPart{{Text: text}}},
		OutputDimensionality: outputDimensions,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embeddings: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiEmbedURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("embeddings: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embeddings: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings: gemini returned %d: %s", resp.StatusCode, string(body))
	}

	var parsed geminiEmbedResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("embeddings: unmarshal response: %w", err)
	}
	if len(parsed.Embedding.Values) == 0 {
		return nil, fmt.Errorf("embeddings: empty embedding returned")
	}

	return parsed.Embedding.Values, nil
}
