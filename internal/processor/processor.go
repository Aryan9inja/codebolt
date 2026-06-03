package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Aryan9inja/codebolt/internal/github"
	"github.com/Aryan9inja/codebolt/internal/webhook"
	"github.com/Aryan9inja/gotaskq/taskq"
)

type Processor struct {
	github *github.Client
}

func NewProcessor(githubClient *github.Client) *Processor {
	return &Processor{
		github: githubClient,
	}
}

func (p *Processor) HandlePRReview(ctx context.Context, job *taskq.Job) error {
	var payload webhook.PRReviewJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal PR review job payload: %w", err)
	}

	log.Printf("[processor] PR #%d | repo: %s | head: %.8s", payload.PRNumber, payload.RepoFullName, payload.HeadSHA)
	
	token, err := p.github.GetInstallationToken(ctx, payload.InstallationID)
	if err != nil {
		return fmt.Errorf("failed to get installation token: %w", err)
	}

	diff, err := p.github.GetPullRequestDiff(ctx, token, payload.Owner, payload.RepoName, payload.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to get pull request diff: %w", err)
	}

	log.Printf("[processor] PR #%d | repo: %s | diff: %d bytes", payload.PRNumber, payload.RepoFullName, len(diff))

	// TODO: Pass diff to AST analysis + LLM pipeline

	return nil
}
