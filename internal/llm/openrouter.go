package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const openRouterBaseURL = "https://openrouter.ai/api/v1/chat/completions"

// DefaultModel is the default model to use for OpenRouter requests.
const DefaultModel = "poolside/laguna-m.1:free"

type OpenRouterProvider struct {
	apiKey     string
	httpClient *http.Client
}

func NewOpenRouterProvider(apiKey string) *OpenRouterProvider {
	return &OpenRouterProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterRequest struct {
	Model       string               `json:"model"`
	Messages    []openRouterMessage  `json:"messages"`
	MaxTokens   int                  `json:"max_tokens,omitempty"`
	Temperature float64              `json:"temperature,omitempty"`
	ResponseFmt *openRouterRespFmt   `json:"response_format,omitempty"`
	Reasoning   *openRouterReasoning `json:"reasoning,omitempty"`
}

type openRouterReasoning struct {
	Exclude bool `json:"exclude"`
}

type openRouterRespFmt struct {
	Type string `json:"type"`
}

type openRouterResponse struct {
	Choices []struct {
		Message openRouterMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (p *OpenRouterProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = DefaultModel
	}

	messages := []openRouterMessage{}
	if req.System != "" {
		messages = append(messages, openRouterMessage{Role: "system", Content: req.System})
	}
	messages = append(messages, openRouterMessage{Role: "user", Content: req.User})

	body := openRouterRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	if req.JSONMode {
		body.ResponseFmt = &openRouterRespFmt{Type: "json_object"}
	}
	body.Reasoning = &openRouterReasoning{Exclude: true}

	payload, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to marshal openrouter request: %w", err)
	}

	log.Printf("[openrouter] sending request to %s | model: %s | max_tokens: %d | temp: %.2f | json_mode: %t",
		openRouterBaseURL, model, req.MaxTokens, req.Temperature, req.JSONMode)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openRouterBaseURL, bytes.NewReader(payload))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[openrouter] request failed for model %s: %v", model, err)
		return CompletionResponse{}, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)
	log.Printf("[openrouter] response received | status: %d | duration: %v", resp.StatusCode, duration)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[openrouter] HTTP error: status %d, body: %s", resp.StatusCode, string(respBody))
		return CompletionResponse{}, fmt.Errorf("openrouter request failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result openRouterResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		log.Printf("[openrouter] failed to decode response JSON: %v | raw response: %s", err, string(respBody))
		return CompletionResponse{}, fmt.Errorf("failed to decode openrouter response: %w", err)
	}

	if result.Error != nil {
		log.Printf("[openrouter] API returned error: %s", result.Error.Message)
		return CompletionResponse{}, fmt.Errorf("openrouter returned error: %s", result.Error.Message)
	}

	if len(result.Choices) == 0 {
		log.Printf("[openrouter] API returned empty choices")
		return CompletionResponse{}, fmt.Errorf("openrouter returned no choices")
	}

	log.Printf("[openrouter] request succeeded | choices: %d | content length: %d", len(result.Choices), len(result.Choices[0].Message.Content))
	return CompletionResponse{Content: result.Choices[0].Message.Content}, nil
}
