package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
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
		h.handlePullRequest(w, r)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (h *Handler) validateSignature(signature string, payload []byte) bool {
	if len(signature) < 7 { // "sha256=" prefix is 7 characters
		return false
	}

	mac := hmac.New(sha256.New, h.secret)
	mac.Write(payload)
	expectedSignature := "sha256=" + string(mac.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

func (h *Handler) handlePullRequest(w http.ResponseWriter, r *http.Request) {
	log.Println("Handling pull request event")
	w.WriteHeader(http.StatusAccepted)
}
