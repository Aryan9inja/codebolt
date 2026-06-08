package github

import (
	"bytes"
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

func TestGetInstallationToken(t *testing.T) {
	keyPath := generateTempPrivateKey(t)
	defer os.Remove(keyPath)

	client := NewClient("test-app", keyPath)

	tests := []struct {
		name          string
		mockStatus    int
		mockBody      string
		expectError   bool
		expectedToken string
	}{
		{
			name:          "happy path",
			mockStatus:    http.StatusCreated,
			mockBody:      `{"token": "v1.abc"}`,
			expectError:   false,
			expectedToken: "v1.abc",
		},
		{
			name:        "unauthorized",
			mockStatus:  http.StatusUnauthorized,
			mockBody:    `{}`,
			expectError: true,
		},
		{
			name:        "bad json",
			mockStatus:  http.StatusCreated,
			mockBody:    `{bad json}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.httpClient.Transport = &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if !strings.Contains(req.URL.Path, "access_tokens") {
						t.Errorf("unexpected url: %s", req.URL.String())
					}
					return &http.Response{
						StatusCode: tt.mockStatus,
						Body:       io.NopCloser(bytes.NewBufferString(tt.mockBody)),
					}, nil
				},
			}

			token, err := client.GetInstallationToken(context.Background(), 12345)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if token != tt.expectedToken {
					t.Errorf("expected token %q, got %q", tt.expectedToken, token)
				}
			}
		})
	}
}

func TestGetInstallationToken_InvalidKeyPath(t *testing.T) {
	client := NewClient("test-app", "/path/does/not/exist.pem")
	_, err := client.GetInstallationToken(context.Background(), 12345)
	if err == nil {
		t.Errorf("expected error when private key file does not exist")
	}
}

func TestGetPullRequestDiff(t *testing.T) {
	client := NewClient("test-app", "dummy")

	tests := []struct {
		name        string
		mockStatus  int
		mockBody    string
		expectError bool
		expected    string
	}{
		{
			name:        "happy path",
			mockStatus:  http.StatusOK,
			mockBody:    "diff --git a/a b/b",
			expectError: false,
			expected:    "diff --git a/a b/b",
		},
		{
			name:        "server error",
			mockStatus:  http.StatusInternalServerError,
			mockBody:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.httpClient.Transport = &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if req.Header.Get("Accept") != "application/vnd.github.diff" {
						t.Errorf("missing diff accept header")
					}
					return &http.Response{
						StatusCode: tt.mockStatus,
						Body:       io.NopCloser(strings.NewReader(tt.mockBody)),
					}, nil
				},
			}

			diff, err := client.GetPullRequestDiff(context.Background(), "token", "owner", "repo", 1)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if diff != tt.expected {
					t.Errorf("got %q, want %q", diff, tt.expected)
				}
			}
		})
	}
}

func TestGetFileContent(t *testing.T) {
	client := NewClient("test-app", "dummy")

	tests := []struct {
		name        string
		mockStatus  int
		mockBody    string
		expectError bool
		expected    string
	}{
		{
			name:        "happy path",
			mockStatus:  http.StatusOK,
			mockBody:    "file contents",
			expectError: false,
			expected:    "file contents",
		},
		{
			name:        "not found",
			mockStatus:  http.StatusNotFound,
			mockBody:    "",
			expectError: true,
		},
		{
			name:        "server error",
			mockStatus:  http.StatusInternalServerError,
			mockBody:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.httpClient.Transport = &mockTransport{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if req.Header.Get("Accept") != "application/vnd.github.raw+json" {
						t.Errorf("missing raw+json accept header")
					}
					return &http.Response{
						StatusCode: tt.mockStatus,
						Body:       io.NopCloser(strings.NewReader(tt.mockBody)),
					}, nil
				},
			}

			content, err := client.GetFileContent(context.Background(), "token", "owner", "repo", "path", "ref")
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if content != tt.expected {
					t.Errorf("got %q, want %q", content, tt.expected)
				}
			}
		})
	}
}
