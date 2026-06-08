package webhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Aryan9inja/gotaskq/taskq"
)

func signPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookHandler(t *testing.T) {
	secret := "test-secret"
	queue, err := taskq.New(taskq.Options{DefaultQueueName: "test"})
	if err != nil {
		t.Fatalf("failed to create taskq: %v", err)
	}

	handler := NewHandler(secret, queue)

	tests := []struct {
		name           string
		event          string
		payload        string
		signatureFunc  func(payload []byte) string
		expectedStatus int
	}{
		{
			name:           "ping event",
			event:          "ping",
			payload:        `{"zen": "non-blocking"}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid signature",
			event:          "ping",
			payload:        `{"zen": "non-blocking"}`,
			signatureFunc:  func(p []byte) string { return "sha256=invalid" },
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "missing signature",
			event:          "ping",
			payload:        `{"zen": "non-blocking"}`,
			signatureFunc:  func(p []byte) string { return "" },
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "pull request opened",
			event:          "pull_request",
			payload:        `{"action": "opened", "number": 1, "repository": {"full_name": "test/repo"}, "installation": {"id": 123}}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusAccepted,
		},
		{
			name:           "pull request synchronize",
			event:          "pull_request",
			payload:        `{"action": "synchronize", "number": 1, "repository": {"full_name": "test/repo"}, "installation": {"id": 123}}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusAccepted,
		},
		{
			name:           "pull request closed (ignored)",
			event:          "pull_request",
			payload:        `{"action": "closed", "number": 1}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid JSON payload",
			event:          "pull_request",
			payload:        `{invalid}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody := []byte(tt.payload)
			req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(reqBody))
			req.Header.Set("X-GitHub-Event", tt.event)
			req.Header.Set("X-Hub-Signature-256", tt.signatureFunc(reqBody))

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tt.expectedStatus)
			}
		})
	}
}
