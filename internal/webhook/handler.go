package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/Aryan9inja/gotaskq/taskq"
)

type taskQueue interface {
	Enqueue(ctx context.Context, opts taskq.JobOptions) (*taskq.Job, error)
}

type Handler struct {
	secret []byte
	queue  taskQueue
}

func NewHandler(secret string, queue taskQueue) *Handler {
	return &Handler{secret: []byte(secret), queue: queue}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 25*1024*1024) // Limit request body to 25MB
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request too large or failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	if !h.validateSignature(r.Header.Get("X-Hub-Signature-256"), payload) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	log.Printf("Received event: %s", eventType)

	switch eventType {
	case "ping":
		w.WriteHeader(http.StatusOK)
	case "pull_request":
		h.handlePullRequest(w, r, payload)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) validateSignature(signature string, payload []byte) bool {
	if len(signature) < 7 {
		return false
	}
	mac := hmac.New(sha256.New, h.secret)
	mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}

func (h *Handler) handlePullRequest(w http.ResponseWriter, r *http.Request, payload []byte) {
	var event PullRequestEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		http.Error(w, "failed to parse payload", http.StatusBadRequest)
		return
	}

	if event.Action != "opened" && event.Action != "synchronize" {
		w.WriteHeader(http.StatusOK)
		return
	}

	jobPayload, err := json.Marshal(PRReviewJobPayload{
		InstallationID: event.Installation.ID,
		RepoFullName:   event.Repository.FullName,
		Owner:          event.Repository.Owner.Login,
		RepoName:       event.Repository.Name,
		PRNumber:       event.Number,
		HeadSHA:        event.PullRequest.Head.SHA,
		BaseSHA:        event.PullRequest.Base.SHA,
	})
	if err != nil {
		http.Error(w, "failed to marshal job payload", http.StatusInternalServerError)
		return
	}

	_, err = h.queue.Enqueue(r.Context(), taskq.JobOptions{
		Type:       "pr-review",
		Payload:    jobPayload,
		MaxRetries: 3,
	})
	if err != nil {
		log.Printf("Failed to enqueue job: %v", err)
		http.Error(w, "failed to enqueue job", http.StatusInternalServerError)
		return
	}

	log.Printf("enqueued PR #%d review job for %s", event.Number, event.Repository.FullName)
	w.WriteHeader(http.StatusAccepted)
}
