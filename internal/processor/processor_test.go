package processor

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Aryan9inja/codebolt/internal/github"
	"github.com/Aryan9inja/codebolt/internal/llm"
	"github.com/Aryan9inja/gotaskq/taskq"
)

type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func generateTempPrivateKey(t *testing.T) string {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	privDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privBlock := pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	}
	f, err := os.CreateTemp("", "private-key-*.pem")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	if err := pem.Encode(f, &privBlock); err != nil {
		t.Fatalf("failed to encode pem: %v", err)
	}
	f.Close()
	return f.Name()
}

func TestHandlePRReview(t *testing.T) {
	keyPath := generateTempPrivateKey(t)
	defer os.Remove(keyPath)

	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	var postReviewCalled bool

	http.DefaultTransport = &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "access_tokens") {
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"token": "v1.test"}`)),
				}, nil
			}
			if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "pulls") {
				// Provide a go file diff with an error (e.g., fmt.Print instead of log) or something
				// Or we could use the test case payloads to control diff content, but let's just use a fixed one with .go
				diffData := `diff --git a/test.go b/test.go
--- a/test.go
+++ b/test.go
@@ -1 +1 @@
-old
+func main() { panic("test") }`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(diffData)),
				}, nil
			}
			if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "contents") {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`package main; func main() { panic("test") }`)),
				}, nil
			}
			if req.Method == http.MethodPost && strings.Contains(req.URL.Path, "reviews") {
				postReviewCalled = true
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	ghClient := github.NewClient("test-app", keyPath)
	proc := NewProcessor(ghClient, nil)

	tests := []struct {
		name             string
		payload          string
		expectError      bool
		expectPostReview bool
	}{
		{
			name:             "happy path with go file",
			payload:          `{"installation_id": 123, "repo_full_name": "test/repo", "owner": "test", "repo_name": "repo", "pr_number": 1, "head_sha": "abc1234"}`,
			expectError:      false,
			expectPostReview: true, // It triggers the panic rule
		},
		{
			name:             "invalid payload",
			payload:          `{bad json}`,
			expectError:      true,
			expectPostReview: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			postReviewCalled = false
			job := &taskq.Job{
				Payload: []byte(tt.payload),
			}

			err := proc.HandlePRReview(context.Background(), job)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if postReviewCalled != tt.expectPostReview {
					t.Errorf("expected postReviewCalled=%v, got %v", tt.expectPostReview, postReviewCalled)
				}
			}
		})
	}
}

func TestHandlePRReview_GitHubAPIFailure(t *testing.T) {
	keyPath := generateTempPrivateKey(t)
	defer os.Remove(keyPath)

	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	// Mock transport that always fails
	http.DefaultTransport = &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	ghClient := github.NewClient("test-app", keyPath)
	proc := NewProcessor(ghClient, nil)

	job := &taskq.Job{
		Payload: []byte(`{"installation_id": 123, "repo_full_name": "test/repo", "owner": "test", "repo_name": "repo", "pr_number": 1}`),
	}

	err := proc.HandlePRReview(context.Background(), job)
	if err == nil {
		t.Errorf("expected error on GitHub API failure, got none")
	}
}

func TestParseGoVersion(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "standard go version 1.22",
			content:  "module github.com/foo/bar\n\ngo 1.22\n",
			expected: "1.22",
		},
		{
			name:     "standard go version 1.22.0",
			content:  "module github.com/foo/bar\ngo 1.22.0",
			expected: "1.22.0",
		},
		{
			name:     "multiple spaces",
			content:  "go    1.26.4",
			expected: "1.26.4",
		},
		{
			name:     "indented go directive",
			content:  "  go 1.23.0  ",
			expected: "1.23.0",
		},
		{
			name:     "commented go directive",
			content:  "// go 1.22",
			expected: "",
		},
		{
			name:     "no go directive",
			content:  "module github.com/foo/bar\n",
			expected: "",
		},
		{
			name:     "go prefix only",
			content:  "go",
			expected: "",
		},
		{
			name:     "empty content",
			content:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGoVersion(tt.content)
			if got != tt.expected {
				t.Errorf("parseGoVersion() = %q, want %q", got, tt.expected)
			}
		})
	}
}

type mockLLMProvider struct {
	completeFn func(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error)
}

func (m *mockLLMProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	return m.completeFn(ctx, req)
}

func TestHandlePRReview_WithLLM(t *testing.T) {
	keyPath := generateTempPrivateKey(t)
	defer os.Remove(keyPath)

	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	var postReviewCalled bool
	var reviewPayload string
	var getGoModCalled bool

	http.DefaultTransport = &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "access_tokens") {
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"token": "v1.test"}`)),
				}, nil
			}
			if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "pulls") {
				diffData := `diff --git a/test.go b/test.go
--- a/test.go
+++ b/test.go
@@ -1 +1 @@
-old
+func main() { panic("test") }`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(diffData)),
				}, nil
			}
			if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "contents") {
				if strings.Contains(req.URL.Path, "go.mod") {
					getGoModCalled = true
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader("module testmod\ngo 1.25.0")),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`package main; func main() { panic("test") }`)),
				}, nil
			}
			if req.Method == http.MethodPost && strings.Contains(req.URL.Path, "reviews") {
				postReviewCalled = true
				bodyBytes, err := io.ReadAll(req.Body)
				if err == nil {
					reviewPayload = string(bodyBytes)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	detectorResp := `{"candidates": [{"category": "bug", "line": 1, "message": "llm infinite loop"}]}`
	suggesterResp := `{"items": [{"category": "bug", "line": 1, "message": "llm infinite loop", "explanation": "explain loop", "suggested_fix": "for {}"}]}`
	reviewerResp := `{"items": [
		{"category": "bug", "line": 1, "message": "llm infinite loop", "explanation": "explain loop", "suggested_fix": "for {}", "confidence": 0.95, "decision": "inline"},
		{"category": "style", "line": 2, "message": "llm fallback style", "explanation": "explain style", "suggested_fix": "", "confidence": 0.8, "decision": "inline"}
	]}`

	callCount := 0
	mockLLM := &mockLLMProvider{
		completeFn: func(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
			callCount++
			switch callCount {
			case 1:
				return llm.CompletionResponse{Content: detectorResp}, nil
			case 2:
				return llm.CompletionResponse{Content: suggesterResp}, nil
			case 3:
				return llm.CompletionResponse{Content: reviewerResp}, nil
			default:
				return llm.CompletionResponse{}, errors.New("unexpected llm provider call")
			}
		},
	}

	ghClient := github.NewClient("test-app", keyPath)
	pipeline := llm.NewPipeline(mockLLM, "test-model", nil, nil)
	proc := NewProcessor(ghClient, pipeline)

	job := &taskq.Job{
		Payload: []byte(`{"installation_id": 123, "repo_full_name": "test/repo", "owner": "test", "repo_name": "repo", "pr_number": 1, "head_sha": "abc1234"}`),
	}

	err := proc.HandlePRReview(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !getGoModCalled {
		t.Error("expected GetFileContent to be called for go.mod")
	}

	if !postReviewCalled {
		t.Fatal("expected post review to be called")
	}

	// Verify the comments contain both AST and LLM findings
	// AST finding: func main() { panic("test") } triggers the panic rule -> line 1.
	// LLM finding 1: inline on line 1, resolved to diffPos 1.
	// LLM finding 2: inline on line 2, not in diffPos (diffPos 0), fallback to summary.
	if !strings.Contains(reviewPayload, "panic") {
		t.Errorf("expected review payload to contain AST panic comment, got: %s", reviewPayload)
	}
	if !strings.Contains(reviewPayload, "llm-bug") {
		t.Errorf("expected review payload to contain LLM bug comment, got: %s", reviewPayload)
	}
	if !strings.Contains(reviewPayload, "llm infinite loop") {
		t.Errorf("expected review payload to contain LLM bug message, got: %s", reviewPayload)
	}
	if !strings.Contains(reviewPayload, "**Suggested Fix:**\\n```go\\nfor {}") {
		t.Errorf("expected review payload to contain suggested fix code block, got: %s", reviewPayload)
	}

	// LLM fallback summary: Decision inline on line 2 became summary note because line 2 is not in diff
	if !strings.Contains(reviewPayload, "llm-style") || !strings.Contains(reviewPayload, "llm fallback style") {
		t.Errorf("expected review payload summary section to contain LLM style summary note, got: %s", reviewPayload)
	}
}

func TestHandlePRReview_LLMFailureFaultTolerance(t *testing.T) {
	keyPath := generateTempPrivateKey(t)
	defer os.Remove(keyPath)

	oldTransport := http.DefaultTransport
	defer func() { http.DefaultTransport = oldTransport }()

	var postReviewCalled bool
	var reviewPayload string

	http.DefaultTransport = &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "access_tokens") {
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"token": "v1.test"}`)),
				}, nil
			}
			if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "pulls") {
				diffData := `diff --git a/test.go b/test.go
--- a/test.go
+++ b/test.go
@@ -1 +1 @@
-old
+func main() { panic("test") }`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(diffData)),
				}, nil
			}
			if req.Method == http.MethodGet && strings.Contains(req.URL.Path, "contents") {
				if strings.Contains(req.URL.Path, "go.mod") {
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(strings.NewReader("")),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`package main; func main() { panic("test") }`)),
				}, nil
			}
			if req.Method == http.MethodPost && strings.Contains(req.URL.Path, "reviews") {
				postReviewCalled = true
				bodyBytes, err := io.ReadAll(req.Body)
				if err == nil {
					reviewPayload = string(bodyBytes)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{}`)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	mockLLM := &mockLLMProvider{
		completeFn: func(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
			return llm.CompletionResponse{}, errors.New("llm provider completely down")
		},
	}

	ghClient := github.NewClient("test-app", keyPath)
	pipeline := llm.NewPipeline(mockLLM, "test-model", nil, nil)
	proc := NewProcessor(ghClient, pipeline)

	job := &taskq.Job{
		Payload: []byte(`{"installation_id": 123, "repo_full_name": "test/repo", "owner": "test", "repo_name": "repo", "pr_number": 1, "head_sha": "abc1234"}`),
	}

	// Even if LLM fails, we should handle it gracefully and still post AST findings.
	err := proc.HandlePRReview(context.Background(), job)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !postReviewCalled {
		t.Fatal("expected post review to be called for AST findings even though LLM failed")
	}

	if !strings.Contains(reviewPayload, "panic") {
		t.Errorf("expected review payload to contain AST panic comment, got: %s", reviewPayload)
	}
}
