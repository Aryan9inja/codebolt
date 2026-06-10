package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Aryan9inja/codebolt/internal/analyzer"
	"github.com/Aryan9inja/codebolt/internal/diff"
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

	prDiff, err := p.github.GetPullRequestDiff(ctx, token, payload.Owner, payload.RepoName, payload.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to get pull request diff: %w", err)
	}

	log.Printf("[processor] PR #%d | repo: %s | diff: %d bytes", payload.PRNumber, payload.RepoFullName, len(prDiff))

	files := diff.Parse(prDiff)
	for _, file := range files {
		log.Printf("[diff] %s (%s) | hunks: %d | added: %d lines",
			file.Path, file.Language, len(file.Hunks), len(file.AddedLines()))
	}

	// Build a map of new line numbers to their corresponding
	// diff positions for quick lookup during analysis.
	lineToDiffPos := make(map[int]int)
	for _, fileDiff := range files {
		for _, hunk := range fileDiff.Hunks {
			for _, dl := range hunk.Lines {
				if dl.NewLine > 0 {
					lineToDiffPos[dl.NewLine] = dl.DiffPos
				}
			}
		}
	}

	az := analyzer.NewAnalyzer()
	for _, f := range files {
		// Currently only go lang is supported
		// Later other languages support will be added
		if f.Language != "go" {
			continue
		}
		content, err := p.github.GetFileContent(ctx, token, payload.Owner, payload.RepoName, f.Path, payload.HeadSHA)
		if err != nil {
			log.Printf("[processor] skipping %s: %v", f.Path, err)
			continue
		}
		findings := az.Analyze(f.Path, content, f.ChangedLineNumbers(), lineToDiffPos, "") // TODO : Go version from go.mod

		for _, finding := range findings {
			log.Printf("[%s] %s L%d: %s", finding.Severity, finding.Rule, finding.Line, finding.Message)
		}
	}

	return nil
}
