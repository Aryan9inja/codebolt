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
	"github.com/Aryan9inja/codebolt/internal/config"
	"github.com/Aryan9inja/codebolt/internal/diff"
	"github.com/Aryan9inja/codebolt/internal/github"
	"github.com/Aryan9inja/codebolt/internal/llm"
	"github.com/Aryan9inja/codebolt/internal/webhook"
	"github.com/Aryan9inja/gotaskq/taskq"
	"github.com/bmatcuk/doublestar/v4"
)

const (
	defaultConfigName  = "codebolt.yaml"
	fallbackConfigName = "codebolt.yml"
	goModFileName      = "go.mod"
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

	goVersion := p.fetchGoVersion(ctx, token, &payload)
	repoConfig := p.fetchConfig(ctx, token, &payload)

	allFindings, llmFiles := p.runAnalyzer(ctx, token, &payload, files, goVersion, repoConfig)

	// Run the LLM pipeline (Detector -> Suggester -> Reviewer) for each
	// changed Go file independently. AST findings are passed as context only;
	// LLM findings flow into a separate stream merged in step 6.
	llmFindings := p.runLLMPipeline(ctx, &payload, llmFiles, repoConfig)

	if len(allFindings) == 0 && len(llmFindings) == 0 {
		log.Printf("[processor] no findings for PR #%d | repo: %s", payload.PRNumber, payload.RepoFullName)
		return nil
	}

	// Split findings into inline comments vs file-level (DiffPos == 0).
	// AST findings (allFindings) and LLM findings (llmFindings) are
	// independent streams, merged here only at the comment-building step.
	return p.postReview(ctx, token, &payload, allFindings, llmFindings)
}

func (p *Processor) fetchGoVersion(ctx context.Context, token string, payload *webhook.PRReviewJobPayload) string {
	goModContent, err := p.github.GetFileContent(ctx, token, payload.Owner, payload.RepoName, goModFileName, payload.HeadSHA)
	if err != nil {
		log.Printf("[processor] could not fetch go.mod, proceeding without go version: %v", err)
		return ""
	}
	return parseGoVersion(goModContent)
}

func (p *Processor) fetchConfig(ctx context.Context, token string, payload *webhook.PRReviewJobPayload) *config.CodeBoltConfig {
	configSHA := payload.BaseSHA
	if configSHA == "" {
		configSHA = payload.HeadSHA
	}
	configContent, err := p.github.GetFileContent(ctx, token, payload.Owner, payload.RepoName, defaultConfigName, configSHA)
	if err != nil {
		// Fallback to .yml extension
		configContent, err = p.github.GetFileContent(ctx, token, payload.Owner, payload.RepoName, fallbackConfigName, configSHA)
		if err != nil {
			log.Printf("[processor] no codebolt.yaml or codebolt.yml found for repo %s (or error: %v)", payload.RepoFullName, err)
			return nil
		}
	}

	parsedConfig, err := config.Parse([]byte(configContent))
	if err != nil {
		log.Printf("[processor] failed to parse codebolt.yaml: %v", err)
		return nil
	}

	log.Printf("[processor] loaded codebolt.yaml for repo %s", payload.RepoFullName)
	return parsedConfig
}

type fileForLLM struct {
	path          string
	content       string
	astFindings   []analyzerTypes.Finding
	lineToDiffPos map[int]int
}

func (p *Processor) runAnalyzer(
	ctx context.Context,
	token string,
	payload *webhook.PRReviewJobPayload,
	files []diff.FileDiff,
	goVersion string,
	repoConfig *config.CodeBoltConfig,
) ([]analyzerTypes.Finding, []fileForLLM) {
	az := analyzer.NewAnalyzer()
	var allFindings []analyzerTypes.Finding
	var llmFiles []fileForLLM

	for _, f := range files {
		// Currently only go lang is supported
		if f.Language != "go" {
			continue
		}

		skip := false
		if repoConfig != nil {
			for _, pattern := range repoConfig.Analyzer.ExcludePaths {
				matched, err := doublestar.Match(pattern, f.Path)
				if err == nil && matched {
					skip = true
					break
				}
			}
		}
		if skip {
			log.Printf("[processor] skipping %s due to exclude_paths", f.Path)
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

		var filteredFindings []analyzerTypes.Finding
		for _, finding := range findings {
			if isRuleEnabled(finding.Rule, repoConfig) {
				filteredFindings = append(filteredFindings, finding)
			}
		}

		allFindings = append(allFindings, filteredFindings...)
		llmFiles = append(llmFiles, fileForLLM{
			path:          f.Path,
			content:       content,
			astFindings:   filteredFindings,
			lineToDiffPos: fileLineToDiffPos,
		})
	}

	return allFindings, llmFiles
}

func (p *Processor) runLLMPipeline(
	ctx context.Context,
	payload *webhook.PRReviewJobPayload,
	llmFiles []fileForLLM,
	repoConfig *config.CodeBoltConfig,
) []llm.EnhancedFinding {
	var llmFindings []llm.EnhancedFinding
	activePipeline := p.llm

	if activePipeline != nil && repoConfig != nil && (repoConfig.LLM.Provider != "" || repoConfig.LLM.Model != "") {
		activePipeline = activePipeline.CloneWithOverrides(repoConfig.LLM.Provider, repoConfig.LLM.Model)
	}

	if activePipeline != nil {
		for _, lf := range llmFiles {
			enhanced, err := activePipeline.RunForFile(ctx, payload.RepoName, payload.PRNumber, lf.path, lf.content, lf.astFindings, lf.lineToDiffPos)
			if err != nil {
				log.Printf("[processor] llm pipeline failed for %s: %v", lf.path, err)
				continue
			}
			llmFindings = append(llmFindings, enhanced...)
		}
	}

	return llmFindings
}

func (p *Processor) postReview(
	ctx context.Context,
	token string,
	payload *webhook.PRReviewJobPayload,
	allFindings []analyzerTypes.Finding,
	llmFindings []llm.EnhancedFinding,
) error {
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

	reviewBody := "## CodeBolt Review\n\n"
	if len(fileLevelNotes) > 0 {
		reviewBody += "### Additional findings\n" + strings.Join(fileLevelNotes, "\n") + "\n"
	}

	err := p.github.PostReview(
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

// parseGoVersion extracts the version string from a go.mod file's `go` directive.
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

func isRuleEnabled(ruleName string, cfg *config.CodeBoltConfig) bool {
	if cfg == nil {
		return true
	}
	for _, r := range cfg.Analyzer.Rules {
		if r.Name == ruleName {
			return r.Enabled
		}
	}
	return true
}
