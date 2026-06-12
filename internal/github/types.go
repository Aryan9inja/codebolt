package github

// ReviewComment represents a single inline comment on a pull request review.
// Position must be the DiffLine.DiffPos value (GitHub's diff-relative
// position), not the file's line number.
type ReviewComment struct {
	Path     string `json:"path"`
	Position int    `json:"position"`
	Body     string `json:"body"`
}

// ReviewRequest is the payload for creating a review on a pull request.
type ReviewRequest struct {
	CommitID string          `json:"commit_id"`
	Body     string          `json:"body,omitempty"`
	Event    string          `json:"event"`
	Comments []ReviewComment `json:"comments,omitempty"`
}
