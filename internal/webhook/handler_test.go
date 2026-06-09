package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Aryan9inja/gotaskq/taskq"
)

type mockQueue struct {
	enqueuedJobs []taskq.JobOptions
}

func (m *mockQueue) Enqueue(ctx context.Context, opts taskq.JobOptions) (*taskq.Job, error) {
	m.enqueuedJobs = append(m.enqueuedJobs, opts)
	return &taskq.Job{}, nil
}

func signPayload(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookHandler(t *testing.T) {
	secret := "test-secret"
	mockQ := &mockQueue{}

	handler := NewHandler(secret, mockQ)

	tests := []struct {
		name           string
		event          string
		payload        string
		signatureFunc  func(payload []byte) string
		expectedStatus int
		expectEnqueue  bool
		verifyJob      func(t *testing.T, job taskq.JobOptions)
	}{
		{
			name:           "ping event",
			event:          "ping",
			payload:        `{"zen": "non-blocking"}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusOK,
			expectEnqueue:  false,
		},
		{
			name:           "invalid signature",
			event:          "ping",
			payload:        `{"zen": "non-blocking"}`,
			signatureFunc:  func(p []byte) string { return "sha256=invalid" },
			expectedStatus: http.StatusUnauthorized,
			expectEnqueue:  false,
		},
		{
			name:           "missing signature",
			event:          "ping",
			payload:        `{"zen": "non-blocking"}`,
			signatureFunc:  func(p []byte) string { return "" },
			expectedStatus: http.StatusUnauthorized,
			expectEnqueue:  false,
		},
		{
			name:           "pull request opened",
			event:          "pull_request",
			payload:        `{"action": "opened", "number": 1, "repository": {"full_name": "test/repo", "name": "repo", "owner": {"login": "test"}}, "installation": {"id": 123}, "pull_request": {"head": {"sha": "headsha"}, "base": {"sha": "basesha"}}}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusAccepted,
			expectEnqueue:  true,
			verifyJob: func(t *testing.T, job taskq.JobOptions) {
				if job.Type != "pr-review" {
					t.Errorf("expected job type pr-review, got %s", job.Type)
				}
				var payload PRReviewJobPayload
				if err := json.Unmarshal(job.Payload, &payload); err != nil {
					t.Fatalf("failed to unmarshal job payload: %v", err)
				}
				if payload.PRNumber != 1 {
					t.Errorf("expected PR number 1, got %d", payload.PRNumber)
				}
				if payload.RepoFullName != "test/repo" {
					t.Errorf("expected repo full name test/repo, got %s", payload.RepoFullName)
				}
				if payload.InstallationID != 123 {
					t.Errorf("expected installation ID 123, got %d", payload.InstallationID)
				}
				if payload.HeadSHA != "headsha" {
					t.Errorf("expected head SHA headsha, got %s", payload.HeadSHA)
				}
				if payload.BaseSHA != "basesha" {
					t.Errorf("expected base SHA basesha, got %s", payload.BaseSHA)
				}
			},
		},
		{
			name:           "pull request synchronize",
			event:          "pull_request",
			payload:        `{"action": "synchronize", "number": 2, "repository": {"full_name": "test/repo", "name": "repo", "owner": {"login": "test"}}, "installation": {"id": 123}, "pull_request": {"head": {"sha": "newheadsha"}, "base": {"sha": "basesha"}}}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusAccepted,
			expectEnqueue:  true,
			verifyJob: func(t *testing.T, job taskq.JobOptions) {
				var payload PRReviewJobPayload
				json.Unmarshal(job.Payload, &payload)
				if payload.PRNumber != 2 {
					t.Errorf("expected PR number 2, got %d", payload.PRNumber)
				}
				if payload.HeadSHA != "newheadsha" {
					t.Errorf("expected head SHA newheadsha, got %s", payload.HeadSHA)
				}
			},
		},
		{
			name:           "pull request closed (ignored)",
			event:          "pull_request",
			payload:        `{"action": "closed", "number": 1}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusOK,
			expectEnqueue:  false,
		},
		{
			name:           "invalid JSON payload",
			event:          "pull_request",
			payload:        `{invalid}`,
			signatureFunc:  func(p []byte) string { return signPayload(secret, p) },
			expectedStatus: http.StatusBadRequest,
			expectEnqueue:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockQ.enqueuedJobs = nil // Reset enqueued jobs for each test case
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

			if tt.expectEnqueue {
				if len(mockQ.enqueuedJobs) != 1 {
					t.Errorf("expected 1 job to be enqueued, got %d", len(mockQ.enqueuedJobs))
				} else if tt.verifyJob != nil {
					tt.verifyJob(t, mockQ.enqueuedJobs[0])
				}
			} else {
				if len(mockQ.enqueuedJobs) != 0 {
					t.Errorf("expected 0 jobs to be enqueued, got %d", len(mockQ.enqueuedJobs))
				}
			}
		})
	}
}
