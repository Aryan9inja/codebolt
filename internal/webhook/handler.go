package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
)

type Handler struct {
	secret []byte
}

func NewHandler(secret string) *Handler {
	return &Handler{secret: []byte(secret)}
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
		h.handlePullRequest(w, payload)
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

func (h *Handler) handlePullRequest(w http.ResponseWriter, payload []byte) {
	var event PullRequestEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		http.Error(w, "failed to parse payload", http.StatusBadRequest)
		return
	}

	if event.Action != "opened" && event.Action != "synchronize" {
		w.WriteHeader(http.StatusOK)
		return
	}

	log.Printf("Pull request #%d in %s/%s: %s", event.PullRequest.Number, event.Repository.Owner.Login, event.Repository.Name, event.PullRequest.Title)
	w.WriteHeader(http.StatusOK)
}
