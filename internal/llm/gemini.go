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

// DefaultGeminiModel is the default model to use for Gemini requests.
const DefaultGeminiModel = "gemini-3.5-flash"

type GeminiProvider struct {
	apiKey     string
	httpClient *http.Client
}

func NewGeminiProvider(apiKey string) *GeminiProvider {
	return &GeminiProvider{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens  int     `json:"maxOutputTokens,omitempty"`
	Temperature      float64 `json:"temperature,omitempty"`
	ResponseMimeType string  `json:"responseMimeType,omitempty"`
}

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

func (p *GeminiProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	model := req.Model
	if model == "" {
		model = DefaultGeminiModel
	}

	body := geminiRequest{
		Contents: []geminiContent{
			{
				Role: "user",
				Parts: []geminiPart{
					{Text: req.User},
				},
			},
		},
	}

	if req.System != "" {
		body.SystemInstruction = &geminiContent{
			Parts: []geminiPart{
				{Text: req.System},
			},
		}
	}

	genConfig := &geminiGenerationConfig{
		MaxOutputTokens: req.MaxTokens,
		Temperature:     req.Temperature,
	}
	if req.JSONMode {
		genConfig.ResponseMimeType = "application/json"
	}
	body.GenerationConfig = genConfig

	payload, err := json.Marshal(body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to marshal gemini request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", model, p.apiKey)

	log.Printf("[gemini] sending request | model: %s | max_tokens: %d | temp: %.2f | json_mode: %t",
		model, req.MaxTokens, req.Temperature, req.JSONMode)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	startTime := time.Now()
	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[gemini] request failed for model %s: %v", model, err)
		return CompletionResponse{}, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)
	log.Printf("[gemini] response received | status: %d | duration: %v", resp.StatusCode, duration)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[gemini] HTTP error: status %d, body: %s", resp.StatusCode, string(respBody))
		return CompletionResponse{}, fmt.Errorf("gemini request failed: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	var result geminiResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		log.Printf("[gemini] failed to decode response JSON: %v | raw response: %s", err, string(respBody))
		return CompletionResponse{}, fmt.Errorf("failed to decode gemini response: %w", err)
	}

	if result.Error != nil {
		log.Printf("[gemini] API returned error: %s", result.Error.Message)
		return CompletionResponse{}, fmt.Errorf("gemini returned error: %s", result.Error.Message)
	}

	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		log.Printf("[gemini] API returned empty choices/parts")
		return CompletionResponse{}, fmt.Errorf("gemini returned no content")
	}

	content := result.Candidates[0].Content.Parts[0].Text

	log.Printf("[gemini] request succeeded | candidates: %d | content length: %d", len(result.Candidates), len(content))
	return CompletionResponse{Content: content}, nil
}
