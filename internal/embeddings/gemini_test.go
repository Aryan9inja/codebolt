package embeddings

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// roundTripFunc is a function that implements http.RoundTripper, allowing tests
// to inject arbitrary HTTP responses without a real network.
type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// newProviderWithTransport creates a GeminiEmbeddingProvider with a custom
// HTTP transport so tests never hit the real Gemini API.
func newProviderWithTransport(t *testing.T, rt http.RoundTripper) *GeminiEmbeddingProvider {
	t.Helper()
	p := NewGeminiEmbeddingProvider("test-api-key")
	p.client = &http.Client{Transport: rt}
	return p
}

// makeEmbedResponse marshals a geminiEmbedResponse with the given values into
// a JSON string, for use as a mock HTTP response body.
func makeEmbedResponse(values []float32) string {
	resp := geminiEmbedResponse{}
	resp.Embedding.Values = values
	b, _ := json.Marshal(resp)
	return string(b)
}

func TestGeminiEmbeddingProvider_Embed(t *testing.T) {
	validVec := make([]float32, outputDimensions)
	for i := range validVec {
		validVec[i] = float32(i) * 0.001
	}

	tests := []struct {
		name      string
		text      string
		transport roundTripFunc
		wantLen   int
		wantErr   string
		wantFirst float32 // spot-check first element
	}{
		{
			name: "happy path - returns embedding vector",
			text: "some diagnostic message",
			transport: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(makeEmbedResponse(validVec))),
				}, nil
			},
			wantLen:   outputDimensions,
			wantFirst: validVec[0],
		},
		{
			name: "transport error",
			text: "any text",
			transport: func(req *http.Request) (*http.Response, error) {
				return nil, errors.New("dial tcp: connection refused")
			},
			wantErr: "embeddings: request failed",
		},
		{
			name: "non-200 status returns error with body",
			text: "any text",
			transport: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(strings.NewReader(`{"error": "invalid api key"}`)),
				}, nil
			},
			wantErr: "embeddings: gemini returned 401",
		},
		{
			name: "500 internal server error",
			text: "any text",
			transport: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(strings.NewReader("internal error")),
				}, nil
			},
			wantErr: "embeddings: gemini returned 500",
		},
		{
			name: "invalid json in response body",
			text: "any text",
			transport: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{not valid json}`)),
				}, nil
			},
			wantErr: "embeddings: unmarshal response",
		},
		{
			name: "empty embedding values in response",
			text: "any text",
			transport: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(makeEmbedResponse(nil))),
				}, nil
			},
			wantErr: "embeddings: empty embedding returned",
		},
		{
			name: "empty text input - still makes request",
			text: "",
			transport: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(makeEmbedResponse([]float32{0.1, 0.2}))),
				}, nil
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newProviderWithTransport(t, tt.transport)
			got, err := p.Embed(context.Background(), tt.text)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				if got != nil {
					t.Errorf("expected nil vector on error, got len=%d", len(got))
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len(embedding) = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantFirst != 0 && got[0] != tt.wantFirst {
				t.Errorf("embedding[0] = %v, want %v", got[0], tt.wantFirst)
			}
		})
	}
}

func TestGeminiEmbeddingProvider_Embed_RequestShape(t *testing.T) {
	// Verify that the outgoing HTTP request is correctly shaped:
	// correct URL, headers, and JSON body fields.
	var capturedReq *http.Request
	var capturedBody []byte

	p := newProviderWithTransport(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedReq = req
		body, _ := io.ReadAll(req.Body)
		capturedBody = body
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(makeEmbedResponse([]float32{0.5}))),
		}, nil
	}))

	_, err := p.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// URL
	if !strings.Contains(capturedReq.URL.String(), geminiEmbedModel) {
		t.Errorf("URL %q does not contain model name %q", capturedReq.URL.String(), geminiEmbedModel)
	}

	// Method
	if capturedReq.Method != http.MethodPost {
		t.Errorf("Method = %q, want POST", capturedReq.Method)
	}

	// Headers
	if ct := capturedReq.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if key := capturedReq.Header.Get("x-goog-api-key"); key != "test-api-key" {
		t.Errorf("x-goog-api-key = %q, want test-api-key", key)
	}

	// Body
	var parsed geminiEmbedRequest
	if err := json.Unmarshal(capturedBody, &parsed); err != nil {
		t.Fatalf("could not parse request body: %v", err)
	}
	if parsed.OutputDimensionality != outputDimensions {
		t.Errorf("OutputDimensionality = %d, want %d", parsed.OutputDimensionality, outputDimensions)
	}
	if len(parsed.Content.Parts) != 1 || parsed.Content.Parts[0].Text != "hello world" {
		t.Errorf("request body parts = %+v, want single part with text %q", parsed.Content.Parts, "hello world")
	}
	if !strings.Contains(parsed.Model, geminiEmbedModel) {
		t.Errorf("model field = %q, does not contain %q", parsed.Model, geminiEmbedModel)
	}
}

func TestGeminiEmbeddingProvider_Embed_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	p := newProviderWithTransport(t, roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	}))

	_, err := p.Embed(ctx, "some text")
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "embeddings: request failed") {
		t.Errorf("error = %q, want it to contain 'embeddings: request failed'", err.Error())
	}
}

func BenchmarkGeminiEmbeddingProvider_Embed(b *testing.B) {
	vec := make([]float32, outputDimensions)
	for i := range vec {
		vec[i] = float32(i) * 0.001
	}
	body := makeEmbedResponse(vec)

	p := NewGeminiEmbeddingProvider("bench-key")
	p.client = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		}),
	}

	b.ResetTimer()
	for range b.N {
		_, _ = p.Embed(context.Background(), "benchmark text for embedding")
	}
}
