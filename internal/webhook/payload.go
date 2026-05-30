package webhook

type PullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest PullRequest `json:"pull_request"`
	Repository Repository `json:"repository"`
	Installation Installation `json:"installation"`
}

type PullRequest struct {
	Number int `json:"number"`
	Title string `json:"title"`
	State string `json:"state"`
	Head Ref `json:"head"`
	Base Ref `json:"base"`
	DiffURL string `json:"diff_url"`
}

type Ref struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type Repository struct {
	FullName string `json:"full_name"`
	Owner Owner `json:"owner"`
	Name string `json:"name"`
}

type Owner struct {
	Login string `json:"login"`
}

type Installation struct {
	ID int64 `json:"id"`
}