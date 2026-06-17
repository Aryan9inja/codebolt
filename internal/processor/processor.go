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
	"github.com/Aryan9inja/codebolt/internal/llm"
	"github.com/Aryan9inja/codebolt/internal/webhook"
	"github.com/Aryan9inja/gotaskq/taskq"
)

type Processor struct {
	github *github.Client
	llm    *llm.Pipeline
}

func NewProcessor(githubClient *github.Client, llmPipeline *llm.Pipeline) *Processor {
	return &Processor{
		github: githubClient,
		llm:    llmPipeline,
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

	az := analyzer.NewAnalyzer()

	// Fetch go.mod once for this PR's head commit to determine the module's
	// Go version, used by rules like goroutine-loop-capture that need to know
	// whether the loop variable capture semantics changed (Go 1.22+).
	goVersion := ""
	goModContent, err := p.github.GetFileContent(ctx, token, payload.Owner, payload.RepoName, "go.mod", payload.HeadSHA)
	if err != nil {
		log.Printf("[processor] could not fetch go.mod, proceeding without go version: %v", err)
	} else {
		goVersion = parseGoVersion(goModContent)
	}

	// 4. Run analyzer on each Go file, collect findings.
	allFindings := []analyzerTypes.Finding{}
	type fileForLLM struct {
		path          string
		content       string
		astFindings   []analyzerTypes.Finding
		lineToDiffPos map[int]int
	}
	var llmFiles []fileForLLM

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

		// Build a map of new line numbers to their corresponding
		// diff positions for quick lookup during analysis for this file.
		fileLineToDiffPos := make(map[int]int)
		for _, hunk := range f.Hunks {
			for _, dl := range hunk.Lines {
				if dl.NewLine > 0 {
					fileLineToDiffPos[dl.NewLine] = dl.DiffPos
				}
			}
		}
		findings := az.Analyze(f.Path, content, f.ChangedLineNumbers(), fileLineToDiffPos, goVersion)

		allFindings = append(allFindings, findings...)

		llmFiles = append(llmFiles, fileForLLM{
			path:          f.Path,
			content:       content,
			astFindings:   findings,
			lineToDiffPos: fileLineToDiffPos,
		})
	}

	// 5. Run the LLM pipeline (Detector -> Suggester -> Reviewer) for each
	// changed Go file independently. AST findings are passed as context only;
	// LLM findings flow into a separate stream merged in step 6.
	var llmFindings []llm.EnhancedFinding
	if p.llm != nil {
		for _, lf := range llmFiles {
			enhanced, err := p.llm.RunForFile(ctx, lf.path, lf.content, lf.astFindings, lf.lineToDiffPos)
			if err != nil {
				log.Printf("[processor] llm pipeline failed for %s: %v", lf.path, err)
				continue
			}
			llmFindings = append(llmFindings, enhanced...)
		}
	}

	if len(allFindings) == 0 && len(llmFindings) == 0 {
		log.Printf("[processor] no findings for PR #%d | repo: %s", payload.PRNumber, payload.RepoFullName)
		return nil
	}

	// 6. Split findings into inline comments vs file-level (DiffPos == 0).
	// AST findings (allFindings) and LLM findings (llmFindings) are
	// independent streams, merged here only at the comment-building step.
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

	for _, f := range llmFindings {
		body := fmt.Sprintf("**[%s]** %s (confidence: %.2f)", f.Rule, f.Message, f.Confidence)
		if f.SuggestedFix != "" {
			body += fmt.Sprintf("\n\nSuggested fix:\n```go\n%s\n```", f.SuggestedFix)
		}

		if f.DiffPos > 0 {
			comments = append(comments, github.ReviewComment{
				Path:     f.FilePath,
				Position: f.DiffPos,
				Body:     body,
			})
			log.Printf("[llm-finding] %s:%d | DiffPos: %d | confidence: %.2f | %s", f.FilePath, f.Line, f.DiffPos, f.Confidence, f.Message)
		} else {
			fileLevelNotes = append(fileLevelNotes, fmt.Sprintf("`%s` line %d: %s", f.FilePath, f.Line, body))
		}
	}

	// 7. Build review body
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
		"[processor] posted review for PR #%d | repo: %s | comments: %d (ast: %d, llm: %d)",
		payload.PRNumber,
		payload.RepoFullName,
		len(comments),
		len(allFindings),
		len(llmFindings),
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
