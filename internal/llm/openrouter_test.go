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

type mockRoundTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestNewOpenRouterProvider(t *testing.T) {
	t.Run("correct initialization", func(t *testing.T) {
		provider := NewOpenRouterProvider("test-key")
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

func TestOpenRouterProvider_Complete(t *testing.T) {
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
			mockBody:    `{"choices": [{"message": {"role": "assistant", "content": "hello response"}}]}`,
			wantContent: "hello response",
			verifyRequest: func(t *testing.T, req *http.Request) {
				t.Helper()
				if req.Header.Get("Authorization") != "Bearer key-123" {
					t.Errorf("expected Auth header 'Bearer key-123', got %q", req.Header.Get("Authorization"))
				}
				if req.Header.Get("Content-Type") != "application/json" {
					t.Errorf("expected Content-Type 'application/json', got %q", req.Header.Get("Content-Type"))
				}
				var reqBody openRouterRequest
				err := json.NewDecoder(req.Body).Decode(&reqBody)
				if err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				if reqBody.Model != DefaultModel {
					t.Errorf("expected default model %q, got %q", DefaultModel, reqBody.Model)
				}
				if len(reqBody.Messages) != 1 {
					t.Fatalf("expected 1 message, got %d", len(reqBody.Messages))
				}
				if reqBody.Messages[0].Role != "user" || reqBody.Messages[0].Content != "hello" {
					t.Errorf("unexpected user message: %+v", reqBody.Messages[0])
				}
				if reqBody.ResponseFmt != nil {
					t.Errorf("expected response_format to be nil, got %+v", reqBody.ResponseFmt)
				}
			},
		},
		{
			name:   "happy path - custom model, system prompt, JSON mode",
			apiKey: "key-123",
			req: CompletionRequest{
				Model:       "custom-model",
				System:      "system prompt",
				User:        "hello json",
				MaxTokens:   500,
				Temperature: 0.7,
				JSONMode:    true,
			},
			mockStatus:  http.StatusOK,
			mockBody:    `{"choices": [{"message": {"role": "assistant", "content": "{\"result\": \"ok\"}"}}]}`,
			wantContent: `{"result": "ok"}`,
			verifyRequest: func(t *testing.T, req *http.Request) {
				t.Helper()
				var reqBody openRouterRequest
				err := json.NewDecoder(req.Body).Decode(&reqBody)
				if err != nil {
					t.Fatalf("failed to decode request body: %v", err)
				}
				if reqBody.Model != "custom-model" {
					t.Errorf("expected model 'custom-model', got %q", reqBody.Model)
				}
				if reqBody.MaxTokens != 500 {
					t.Errorf("expected MaxTokens 500, got %d", reqBody.MaxTokens)
				}
				if reqBody.Temperature != 0.7 {
					t.Errorf("expected Temperature 0.7, got %f", reqBody.Temperature)
				}
				if reqBody.ResponseFmt == nil || reqBody.ResponseFmt.Type != "json_object" {
					t.Errorf("expected JSON response_format, got %+v", reqBody.ResponseFmt)
				}
				if len(reqBody.Messages) != 2 {
					t.Fatalf("expected 2 messages, got %d", len(reqBody.Messages))
				}
				if reqBody.Messages[0].Role != "system" || reqBody.Messages[0].Content != "system prompt" {
					t.Errorf("unexpected system message: %+v", reqBody.Messages[0])
				}
				if reqBody.Messages[1].Role != "user" || reqBody.Messages[1].Content != "hello json" {
					t.Errorf("unexpected user message: %+v", reqBody.Messages[1])
				}
			},
		},
		{
			name:       "HTTP status error",
			apiKey:     "key-123",
			req:        CompletionRequest{User: "hello"},
			mockStatus: http.StatusInternalServerError,
			mockBody:   "Internal Server Error",
			wantErr:    "openrouter request failed: status 500, body: Internal Server Error",
		},
		{
			name:       "API returned error in JSON payload",
			apiKey:     "key-123",
			req:        CompletionRequest{User: "hello"},
			mockStatus: http.StatusOK,
			mockBody:   `{"error": {"message": "rate limit exceeded"}}`,
			wantErr:    "openrouter returned error: rate limit exceeded",
		},
		{
			name:       "invalid JSON response",
			apiKey:     "key-123",
			req:        CompletionRequest{User: "hello"},
			mockStatus: http.StatusOK,
			mockBody:   `{bad json}`,
			wantErr:    "failed to decode openrouter response",
		},
		{
			name:       "empty choices in response",
			apiKey:     "key-123",
			req:        CompletionRequest{User: "hello"},
			mockStatus: http.StatusOK,
			mockBody:   `{"choices": []}`,
			wantErr:    "openrouter returned no choices",
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

			provider := NewOpenRouterProvider(tt.apiKey)
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
