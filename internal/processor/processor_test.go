package processor

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Aryan9inja/codebolt/internal/github"
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

	http.DefaultTransport = &mockTransport{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Path, "access_tokens") {
				return &http.Response{
					StatusCode: http.StatusCreated,
					Body:       io.NopCloser(strings.NewReader(`{"token": "v1.test"}`)),
				}, nil
			}
			if strings.Contains(req.URL.Path, "pulls") {
				diffData := `diff --git a/test.txt b/test.txt
--- a/test.txt
+++ b/test.txt
@@ -1 +1 @@
-old
+new`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(diffData)),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	ghClient := github.NewClient("test-app", keyPath)
	proc := NewProcessor(ghClient)

	tests := []struct {
		name        string
		payload     string
		expectError bool
	}{
		{
			name:        "happy path",
			payload:     `{"installation_id": 123, "repo_full_name": "test/repo", "owner": "test", "repo_name": "repo", "pr_number": 1}`,
			expectError: false,
		},
		{
			name:        "invalid payload",
			payload:     `{bad json}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
	proc := NewProcessor(ghClient)

	job := &taskq.Job{
		Payload: []byte(`{"installation_id": 123, "repo_full_name": "test/repo", "owner": "test", "repo_name": "repo", "pr_number": 1}`),
	}

	err := proc.HandlePRReview(context.Background(), job)
	if err == nil {
		t.Errorf("expected error on GitHub API failure, got none")
	}
}
