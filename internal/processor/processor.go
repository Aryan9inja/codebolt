package processor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/Aryan9inja/codebolt/internal/analyzer"
	analyzerTypes "github.com/Aryan9inja/codebolt/internal/analyzer/types"
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

	// 1. Authenticate with GitHub using the app's credentials to get an installation token.
	token, err := p.github.GetInstallationToken(ctx, payload.InstallationID)
	if err != nil {
		return fmt.Errorf("failed to get installation token: %w", err)
	}

	// 2. Fetch the pull request diff using the GitHub API.
	prDiff, err := p.github.GetPullRequestDiff(ctx, token, payload.Owner, payload.RepoName, payload.PRNumber)
	if err != nil {
		return fmt.Errorf("failed to get pull request diff: %w", err)
	}

	log.Printf("[processor] PR #%d | repo: %s | diff: %d bytes", payload.PRNumber, payload.RepoFullName, len(prDiff))

	// 3. Parse the diff to identify changed files and their line numbers.
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

	// 4. Run analyzer on each Go file, collect findings.
	allFindings := []analyzerTypes.Finding{}
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

		allFindings = append(allFindings, findings...)
	}

	if len(allFindings) == 0 {
		log.Printf("[processor] no findings for PR #%d | repo: %s", payload.PRNumber, payload.RepoFullName)
		return nil
	}

	// 5. Split findings into inline comments vs file-level (DiffPos == 0)
	var comments []github.ReviewComment
	var fileLevelNotes []string

	for _, f := range allFindings {
		if f.DiffPos > 0 {
			comments = append(comments, github.ReviewComment{
				Path:     f.FilePath,
				Position: f.DiffPos,
				Body:     fmt.Sprintf("**[%s]** %s", f.Rule, f.Message),
			})
			log.Printf("[finding] %s:%d | DiffPos: %d | %s", f.FilePath, f.Line, f.DiffPos, f.Message)
		} else {
			fileLevelNotes = append(fileLevelNotes, f.Message)
		}
	}

	// 6. Build review body
	reviewBody := "## CodeBolt Review\n\n"
	if len(fileLevelNotes) > 0 {
		reviewBody += "### Additional findings\n" + strings.Join(fileLevelNotes, "\n") + "\n"
	}

	// 7. Post review to GitHub
	err = p.github.PostReview(
		ctx,
		token,
		payload.Owner,
		payload.RepoName,
		payload.PRNumber,
		payload.HeadSHA,
		reviewBody,
		comments,
	)
	if err != nil {
		return fmt.Errorf("failed to post review: %w", err)
	}

	log.Printf(
		"[processor] posted review for PR #%d | repo: %s | comments: %d",
		payload.PRNumber,
		payload.RepoFullName,
		len(comments),
	)

	return nil
}

// parseGoVersion extracts the version string from a go.mod file's `go` directive,
// e.g. "go 1.22" or "go 1.22.0" -> "1.22" / "1.22.0". Returns "" if not found.
func parseGoVersion(goModContent string) string {
	scanner := bufio.NewScanner(strings.NewReader(goModContent))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "go ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			return fields[1]
		}
	}
	return ""
}
