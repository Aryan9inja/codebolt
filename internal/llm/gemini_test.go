package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewGeminiProvider(t *testing.T) {
	t.Run("correct initialization", func(t *testing.T) {
		provider := NewGeminiProvider("test-key")
		if provider == nil {
			t.Fatal("expected provider to be non-nil")
		}
		if provider.apiKey != "test-key" {
			t.Errorf("expected apiKey to be 'test-key', got %s", provider.apiKey)
		}
		if provider.httpClient == nil {
			t.Fatal("expected httpClient to be initialized")
		}
		if provider.httpClient.Timeout != 180*time.Second {
			t.Errorf("expected httpClient timeout to be 180s, got %v", provider.httpClient.Timeout)
		}
	})
}

func TestGeminiProvider_Complete(t *testing.T) {
	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	tests := []struct {
		name          string
		apiKey        string
		req           CompletionRequest
		mockStatus    int
		mockBody      string
		mockErr       error
		cancelContext bool
		wantContent   string
		wantErr       string
		verifyRequest func(t *testing.T, req *http.Request)
	}{
		{
			name:   "happy path - default model",
			apiKey: "key-123",
			req: CompletionRequest{
				User: "hello",
			},
			mockStatus:  http.StatusOK,
			mockBody:    `{"candidates": [{"content": {"parts": [{"text": "hello response"}]}}]}`,
			wantContent: "hello response",
			verifyRequest: func(t *testing.T, req *http.Request) {
				t.Helper()
				if req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected Content-Type 'application/json', got %q", req.Header.Get("Content-Type"))
				}
				if !strings.Contains(req.URL.String(), "key=key-123") {
					t.Errorf("expected URL to contain key=key-123, got %s", req.URL.String())
				}
				if !strings.Contains(req.URL.String(), DefaultGeminiModel) {
					t.Errorf("expected URL to contain default model %s, got %s", DefaultGeminiModel, req.URL.String())
				}
				var reqBody geminiRequest
				err := json.NewDecoder(req.Body).Decode(&reqBody)
				if err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				if len(reqBody.Contents) != 1 {
					t.Fatalf("expected 1 content block, got %d", len(reqBody.Contents))
				}
				if reqBody.Contents[0].Role != "user" || len(reqBody.Contents[0].Parts) == 0 || reqBody.Contents[0].Parts[0].Text != "hello" {
					t.Errorf("unexpected user message: %+v", reqBody.Contents[0])
				}
				if reqBody.SystemInstruction != nil {
					t.Errorf("expected SystemInstruction to be nil, got %+v", reqBody.SystemInstruction)
				}
			},
		},
		{
			name:   "happy path - custom model, system prompt, JSON mode",
			apiKey: "key-123",
			req: CompletionRequest{
				Model:       "gemini-custom",
				System:      "system prompt",
				User:        "hello json",
				Temperature: 0.7,
				JSONMode:    true,
			},
			mockStatus:  http.StatusOK,
			mockBody:    `{"candidates": [{"content": {"parts": [{"text": "{\"result\": \"ok\"}"}]}}]}`,
			wantContent: `{"result": "ok"}`,
			verifyRequest: func(t *testing.T, req *http.Request) {
				t.Helper()
				if !strings.Contains(req.URL.String(), "gemini-custom") {
					t.Errorf("expected URL to contain gemini-custom, got %s", req.URL.String())
				}
				var reqBody geminiRequest
				err := json.NewDecoder(req.Body).Decode(&reqBody)
				if err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				if reqBody.GenerationConfig == nil {
					t.Fatal("expected generation config")
				}
				if reqBody.GenerationConfig.Temperature != 0.7 {
					t.Errorf("expected Temperature 0.7, got %f", reqBody.GenerationConfig.Temperature)
				}
				if reqBody.GenerationConfig.ResponseMimeType != "application/json" {
					t.Errorf("expected responseMimeType to be application/json, got %s", reqBody.GenerationConfig.ResponseMimeType)
				}
				if reqBody.SystemInstruction == nil || len(reqBody.SystemInstruction.Parts) == 0 || reqBody.SystemInstruction.Parts[0].Text != "system prompt" {
					t.Errorf("unexpected system instruction: %+v", reqBody.SystemInstruction)
				}
				if len(reqBody.Contents) != 1 || reqBody.Contents[0].Role != "user" || reqBody.Contents[0].Parts[0].Text != "hello json" {
					t.Errorf("unexpected user message: %+v", reqBody.Contents)
				}
			},
		},
		{
			name:       "HTTP status error",
			apiKey:     "key-123",
			req:        CompletionRequest{User: "hello"},
			mockStatus: http.StatusInternalServerError,
			mockBody:   "Internal Server Error",
			wantErr:    "gemini request failed: status 500, body: Internal Server Error",
		},
		{
			name:       "API returned error in JSON payload",
			apiKey:     "key-123",
			req:        CompletionRequest{User: "hello"},
			mockStatus: http.StatusOK,
			mockBody:   `{"error": {"message": "rate limit exceeded", "code": 429}}`,
			wantErr:    "gemini returned error: rate limit exceeded",
		},
		{
			name:       "invalid JSON response",
			apiKey:     "key-123",
			req:        CompletionRequest{User: "hello"},
			mockStatus: http.StatusOK,
			mockBody:   `{bad json}`,
			wantErr:    "failed to decode gemini response",
		},
		{
			name:       "empty choices in response",
			apiKey:     "key-123",
			req:        CompletionRequest{User: "hello"},
			mockStatus: http.StatusOK,
			mockBody:   `{"candidates": []}`,
			wantErr:    "gemini returned no content",
		},
		{
			name:    "http transport error",
			apiKey:  "key-123",
			req:     CompletionRequest{User: "hello"},
			mockErr: errors.New("network unreachable"),
			wantErr: "network unreachable",
		},
		{
			name:          "context cancelled",
			apiKey:        "key-123",
			req:           CompletionRequest{User: "hello"},
			cancelContext: true,
			mockErr:       context.Canceled,
			wantErr:       "context canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			http.DefaultTransport = &mockRoundTripper{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if tt.verifyRequest != nil {
						tt.verifyRequest(t, req)
					}
					if tt.mockErr != nil {
						return nil, tt.mockErr
					}
					return &http.Response{
						StatusCode: tt.mockStatus,
						Body:       io.NopCloser(strings.NewReader(tt.mockBody)),
					}, nil
				},
			}

			provider := NewGeminiProvider(tt.apiKey)
			ctx := context.Background()
			if tt.cancelContext {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel() // cancel immediately
			}

			resp, err := provider.Complete(ctx, tt.req)
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
				if resp.Content != tt.wantContent {
					t.Errorf("expected content %q, got %q", tt.wantContent, resp.Content)
				}
			}
		})
	}
}
